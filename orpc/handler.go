// Package orpc provides an HTTP handler that serves trpcgo procedures using
// the oRPC wire format. Use [NewHandler] to create an http.Handler from a
// [trpcgo.Router].
//
// Wire format:
//   - Request:  POST body or GET ?data= is { "json": <input>, "meta": [...] }
//   - Response: { "json": <output>, "meta": [...] }
//   - Error:    { "json": { "defined": bool, "code": string, "status": int, "message": string, "data": any }, "meta": [] }
//   - SSE:      event: message / done / error with oRPC-wrapped data; comments for keepalive
package orpc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/befabri/trpcgo"
)

// Handler is an http.Handler that serves trpcgo procedures over the oRPC
// wire protocol. Create one via [NewHandler].
type Handler struct {
	router   *trpcgo.Router
	basePath string
	routes   routeTable
}

type routeTable struct {
	exact    map[string][]*routeMatch
	template []*routeMatch
}

type routeMatch struct {
	path     string
	method   string
	entry    *trpcgo.ProcedureEntry
	segments []routeSegment
}

type routeSegment struct {
	value     string
	isParam   bool
	paramName string
}

// NewHandler creates an oRPC HTTP handler from a trpcgo Router.
// basePath is the URL prefix to strip before procedure lookup
// (e.g., "/rpc" means /rpc/planet/list → procedure "planet.list").
func NewHandler(r *trpcgo.Router, basePath string) *Handler {
	pm := r.BuildProcedureMap()
	bp := strings.TrimRight(basePath, "/")
	if bp != "" && !strings.HasPrefix(bp, "/") {
		bp = "/" + bp
	}
	return &Handler{
		router:   r,
		basePath: bp,
		routes:   buildRouteTable(pm),
	}
}

// batchRequestItem is a single request within an oRPC batch.
type batchRequestItem struct {
	URL     string            `json:"url"`
	Method  string            `json:"method,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
	Body    json.RawMessage   `json:"body,omitempty"`
}

// batchResponseItem is a single response within an oRPC batch.
type batchResponseItem struct {
	Index  int `json:"index"`
	Status int `json:"status,omitempty"`
	Body   any `json:"body"`
}

// batchResult is an internal type for passing results between goroutines.
type batchResult struct {
	index  int
	status int
	body   any
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch:
		// Supported methods.
	default:
		body, status := encodeError(CodeMethodNotSupported, StatusFromCode(CodeMethodNotSupported), "unsupported HTTP method", nil, false)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write(body)
		return
	}

	// Check for batch request (x-orpc-batch header).
	// Batch only supports GET and POST.
	if batchMode := r.Header.Get("x-orpc-batch"); batchMode != "" {
		if r.Method != http.MethodGet && r.Method != http.MethodPost {
			body, status := encodeError(CodeMethodNotSupported, StatusFromCode(CodeMethodNotSupported), "batch requests require GET or POST", nil, false)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			_, _ = w.Write(body)
			return
		}
		h.handleBatch(w, r, batchMode)
		return
	}

	// Map URL path + method to a procedure.
	path, entry, pathParams, ok := h.resolvePath(r.URL.Path, r.Method)
	if !ok {
		// Distinguish 404 (path not found) from 405 (path exists, wrong method).
		if h.hasRoute(r.URL.Path) {
			body, status := encodeError(CodeMethodNotSupported, StatusFromCode(CodeMethodNotSupported), "method not allowed", nil, false)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			_, _ = w.Write(body)
			return
		}
		body, status := encodeError(CodeNotFound, StatusFromCode(CodeNotFound), "procedure not found", nil, false)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write(body)
		return
	}

	// Build context.
	ctx := r.Context()
	if cc := h.router.ContextCreator(); cc != nil {
		userCtx := cc(r.Context(), r)
		if userCtx != ctx {
			var cancel context.CancelFunc
			ctx, cancel = mergeContexts(ctx, userCtx)
			defer cancel()
		}
	}
	ctx = trpcgo.WithResponseMetadata(ctx)
	ctx = trpcgo.WithProcedureMeta(ctx, trpcgo.ProcedureMeta{
		Path: path,
		Type: entry.Type(),
		Meta: entry.Meta(),
	})

	// Decode input from oRPC wire format.
	raw, err := h.decodeRequest(r)
	if err != nil {
		h.writeError(w, ctx, CodeBadRequest, "malformed request body", nil, false)
		return
	}
	raw = mergePathParams(raw, pathParams, entry.InputType())

	// For subscriptions, merge Last-Event-Id into input for SSE reconnection.
	if entry.Type() == trpcgo.ProcedureSubscription {
		raw = mergeLastEventId(r, raw)
	}

	// Execute the procedure.
	result, err := h.router.ExecuteEntry(ctx, entry, raw)
	if err != nil {
		h.handleError(w, ctx, err, path)
		return
	}

	// Check for streaming (subscription) result.
	if trpcgo.IsStreamResult(result) {
		h.handleStream(ctx, w, result, path)
		return
	}

	// Non-streaming success response.
	successStatus := http.StatusOK
	if route := entry.Route(); route.SuccessStatus > 0 {
		successStatus = route.SuccessStatus
	}

	body, err := encodeSuccess(result)
	if err != nil {
		h.writeError(w, ctx, CodeInternalServerError, "failed to serialize response", nil, false)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	trpcgo.ApplyResponseMetadata(ctx, w)
	w.WriteHeader(successStatus)
	_, _ = w.Write(body)
}

// resolvePath resolves the URL path + method to a registered procedure.
func (h *Handler) resolvePath(urlPath, method string) (string, *trpcgo.ProcedureEntry, map[string]string, bool) {
	rawPath, ok := stripBasePath(urlPath, h.basePath)
	if !ok || rawPath == "" {
		return "", nil, nil, false
	}
	normalized := "/" + rawPath
	method = strings.ToUpper(method)

	if matches, ok := h.routes.exact[normalized]; ok {
		for _, m := range matches {
			if !methodAllowed(m.method, method) {
				continue
			}
			return m.path, m.entry, nil, true
		}
	}

	for _, m := range h.routes.template {
		if !methodAllowed(m.method, method) {
			continue
		}
		params, matched := matchTemplate(normalized, m.segments)
		if matched {
			return m.path, m.entry, params, true
		}
	}

	return "", nil, nil, false
}

// decodeRequest extracts the raw JSON input from the request.
// GET: input from ?data= query param (JSON or oRPC-wrapped).
// POST: input from request body (oRPC-wrapped { json, meta }).
func (h *Handler) decodeRequest(r *http.Request) (json.RawMessage, error) {
	if r.Method == http.MethodGet {
		data := r.URL.Query().Get("data")
		if data == "" {
			return nil, nil
		}
		return decodeInput([]byte(data))
	}

	// POST: read body with size limit.
	var reader io.Reader = r.Body
	if max := h.router.MaxBodySize(); max > 0 {
		reader = io.LimitReader(r.Body, max+1)
	}
	body, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}
	if max := h.router.MaxBodySize(); max > 0 && int64(len(body)) > max {
		return nil, fmt.Errorf("request body too large")
	}
	if len(body) == 0 {
		return nil, nil
	}
	return decodeInput(body)
}

// handleError converts an execution error to an oRPC error response.
func (h *Handler) handleError(w http.ResponseWriter, ctx context.Context, err error, path string) {
	// Report to error callback.
	if cb := h.router.ErrorCallback(); cb != nil {
		if trpcErr, ok := errors.AsType[*trpcgo.Error](err); ok {
			cb(ctx, trpcErr, path)
		} else {
			cb(ctx, trpcgo.WrapError(trpcgo.CodeInternalServerError, "internal server error", err), path)
		}
	}

	// Sanitize for the client.
	safe := trpcgo.SanitizeError(err)
	code := CodeFromTRPC(safe.Code)
	h.writeError(w, ctx, code, safe.Message, nil, safe.Code != trpcgo.CodeInternalServerError)
}

// writeError writes an oRPC error response.
func (h *Handler) writeError(w http.ResponseWriter, ctx context.Context, code string, message string, data any, defined bool) {
	status := StatusFromCode(code)
	body, _ := encodeError(code, status, message, data, defined)
	w.Header().Set("Content-Type", "application/json")
	if ctx != nil {
		trpcgo.ApplyResponseMetadata(ctx, w)
	}
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

// handleStream writes an SSE stream for subscription results.
func (h *Handler) handleStream(ctx context.Context, w http.ResponseWriter, result any, path string) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		h.writeError(w, ctx, CodeInternalServerError, "streaming not supported", nil, false)
		return
	}

	// Enforce SSE connection limit.
	if max := h.router.MaxSSEConnections(); max > 0 {
		n := h.router.TrackSSEConnection(1)
		if n > int64(max) {
			h.router.TrackSSEConnection(-1)
			h.writeError(w, ctx, CodeTooManyRequests, "too many concurrent subscriptions", nil, false)
			return
		}
		defer h.router.TrackSSEConnection(-1)
	}

	consumer := trpcgo.ConsumeStream(result)
	if consumer == nil {
		h.writeError(w, ctx, CodeInternalServerError, "invalid stream result", nil, false)
		return
	}

	// Set SSE headers.
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("X-Accel-Buffering", "no")
	w.Header().Set("Connection", "keep-alive")
	trpcgo.ApplyResponseMetadata(ctx, w)
	w.WriteHeader(http.StatusOK)

	// Initial comment to flush headers (oRPC convention).
	_, _ = fmt.Fprint(w, ": \n\n")
	flusher.Flush()

	pingInterval := h.router.SSEPingInterval()
	if pingInterval == 0 {
		pingInterval = 5 * time.Second
	}
	pingTicker := time.NewTicker(pingInterval)
	defer pingTicker.Stop()

	var maxTimer <-chan time.Time
	if d := h.router.SSEMaxDuration(); d > 0 {
		t := time.NewTimer(d)
		defer t.Stop()
		maxTimer = t.C
	}

	// Recv blocks, so run it in a goroutine to multiplex with timers.
	type recvResult struct {
		data  any
		id    string
		retry int
		err   error
	}
	recvCh := make(chan recvResult, 1)
	go func() {
		for {
			data, id, retry, err := consumer.Recv(ctx)
			recvCh <- recvResult{data, id, retry, err}
			if err != nil {
				return
			}
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case <-maxTimer:
			_, _ = fmt.Fprint(w, "event: done\ndata: \n\n")
			flusher.Flush()
			return
		case <-pingTicker.C:
			_, _ = fmt.Fprint(w, ": \n\n")
			flusher.Flush()
		case item := <-recvCh:
			if item.err == io.EOF {
				if item.data != nil {
					doneBytes, _ := encodeSuccess(item.data)
					_, _ = fmt.Fprintf(w, "event: done\ndata: %s\n\n", doneBytes)
				} else {
					_, _ = fmt.Fprint(w, "event: done\ndata: \n\n")
				}
				flusher.Flush()
				return
			}
			if item.err != nil {
				if cb := h.router.ErrorCallback(); cb != nil {
					cb(ctx, trpcgo.WrapError(trpcgo.CodeInternalServerError, "internal server error", item.err), path)
				}
				safe := trpcgo.SanitizeError(item.err)
				code := CodeFromTRPC(safe.Code)
				errBody, _ := encodeError(code, StatusFromCode(code), safe.Message, nil, false)
				_, _ = fmt.Fprintf(w, "event: error\ndata: %s\n\n", errBody)
				flusher.Flush()
				return
			}

			itemBytes, marshalErr := encodeSuccess(item.data)
			if marshalErr != nil {
				if cb := h.router.ErrorCallback(); cb != nil {
					cb(ctx, trpcgo.NewError(trpcgo.CodeInternalServerError, "failed to serialize subscription data"), path)
				}
				errBody, _ := encodeError(CodeInternalServerError, StatusFromCode(CodeInternalServerError), "failed to serialize subscription data", nil, false)
				_, _ = fmt.Fprintf(w, "event: error\ndata: %s\n\n", errBody)
				flusher.Flush()
				return
			}

			_, _ = fmt.Fprintf(w, "event: message\ndata: %s\n", itemBytes)
			if item.id != "" {
				id := strings.NewReplacer("\n", "", "\r", "").Replace(item.id)
				_, _ = fmt.Fprintf(w, "id: %s\n", id)
			}
			if item.retry > 0 {
				_, _ = fmt.Fprintf(w, "retry: %d\n", item.retry)
			}
			_, _ = fmt.Fprint(w, "\n")
			flusher.Flush()
			pingTicker.Reset(pingInterval)
		}
	}
}

// handleBatch processes an oRPC batch request.
// Batch wire format:
//   - GET:  /__batch__?batch=[{url, body, method, headers}, ...]
//   - POST: /__batch__ with body [{url, body, method, headers}, ...]
//   - Response: 207 with [{index, status, body}, ...] (buffered or streaming)
func (h *Handler) handleBatch(w http.ResponseWriter, r *http.Request, mode string) {
	if !h.router.AllowBatching() {
		body, status := encodeError(CodeBadRequest, StatusFromCode(CodeBadRequest), "batching is not enabled", nil, false)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write(body)
		return
	}

	items, err := parseBatchItems(r, h.router.MaxBodySize())
	if err != nil {
		body, status := encodeError(CodeBadRequest, StatusFromCode(CodeBadRequest), "invalid batch request", nil, false)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write(body)
		return
	}

	if max := h.router.MaxBatchSize(); max > 0 && len(items) > max {
		body, status := encodeError(CodePayloadTooLarge, StatusFromCode(CodePayloadTooLarge),
			fmt.Sprintf("batch size %d exceeds limit of %d", len(items), max), nil, false)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write(body)
		return
	}

	// Context creation is deferred to per-item so that batch item headers
	// are visible to the context creator (e.g. per-item auth tokens).
	ctx := r.Context()
	ctx = trpcgo.WithResponseMetadata(ctx)

	// Execute all items concurrently.
	ch := make(chan batchResult, len(items))

	for i, item := range items {
		go func(idx int, it batchRequestItem) {
			defer func() {
				if rv := recover(); rv != nil {
					errBody, _ := encodeError(CodeInternalServerError, StatusFromCode(CodeInternalServerError), "internal server error", nil, false)
					ch <- batchResult{index: idx, status: http.StatusInternalServerError, body: json.RawMessage(errBody)}
				}
			}()

			status, body := h.executeBatchItem(ctx, r, it)
			ch <- batchResult{index: idx, status: status, body: body}
		}(i, item)
	}

	batchStatus := http.StatusMultiStatus // 207

	if mode == "streaming" {
		h.writeBatchStreaming(ctx, w, ch, len(items), batchStatus)
	} else {
		h.writeBatchBuffered(ctx, w, ch, len(items), batchStatus)
	}
}

// parseBatchItems extracts individual request items from an oRPC batch request.
func parseBatchItems(r *http.Request, maxBodySize int64) ([]batchRequestItem, error) {
	var raw []byte

	if r.Method == http.MethodGet {
		raw = []byte(r.URL.Query().Get("batch"))
	} else {
		var reader io.Reader = r.Body
		if maxBodySize > 0 {
			reader = io.LimitReader(r.Body, maxBodySize+1)
		}
		var err error
		raw, err = io.ReadAll(reader)
		if err != nil {
			return nil, err
		}
		if maxBodySize > 0 && int64(len(raw)) > maxBodySize {
			return nil, fmt.Errorf("request body too large")
		}
	}

	if len(raw) == 0 {
		return nil, fmt.Errorf("empty batch request")
	}

	var items []batchRequestItem
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, err
	}
	return items, nil
}

// executeBatchItem executes a single procedure from a batch request.
func (h *Handler) executeBatchItem(ctx context.Context, baseReq *http.Request, item batchRequestItem) (int, any) {
	// Extract procedure path from the item URL.
	method := strings.ToUpper(item.Method)
	if method == "" {
		method = strings.ToUpper(baseReq.Method)
	}
	path, entry, pathParams, ok := h.resolveItemPath(item.URL, method)
	if !ok {
		errBody, _ := encodeError(CodeNotFound, StatusFromCode(CodeNotFound), "procedure not found", nil, false)
		return http.StatusNotFound, json.RawMessage(errBody)
	}

	// Subscriptions cannot be batched.
	if entry.Type() == trpcgo.ProcedureSubscription {
		errBody, _ := encodeError(CodeBadRequest, StatusFromCode(CodeBadRequest),
			"subscriptions cannot be batched", nil, false)
		return http.StatusBadRequest, json.RawMessage(errBody)
	}

	// Per-item context: merge item headers with the base request so
	// the context creator sees per-item auth tokens, etc.
	if cc := h.router.ContextCreator(); cc != nil {
		req := baseReq
		if len(item.Headers) > 0 {
			req = baseReq.Clone(ctx)
			for k, v := range item.Headers {
				req.Header.Set(k, v)
			}
		}
		userCtx := cc(ctx, req)
		if userCtx != ctx {
			var cancel context.CancelFunc
			ctx, cancel = mergeContexts(ctx, userCtx)
			defer cancel()
		}
	}

	ctx = trpcgo.WithProcedureMeta(ctx, trpcgo.ProcedureMeta{
		Path: path,
		Type: entry.Type(),
		Meta: entry.Meta(),
	})

	// Decode input from the item body.
	var raw json.RawMessage
	if len(item.Body) > 0 {
		decoded, err := decodeInput(item.Body)
		if err != nil {
			errBody, _ := encodeError(CodeBadRequest, StatusFromCode(CodeBadRequest), "malformed request body", nil, false)
			return http.StatusBadRequest, json.RawMessage(errBody)
		}
		raw = decoded
	}
	raw = mergePathParams(raw, pathParams, entry.InputType())

	result, err := h.router.ExecuteEntry(ctx, entry, raw)
	if err != nil {
		safe := trpcgo.SanitizeError(err)
		if cb := h.router.ErrorCallback(); cb != nil {
			if trpcErr, ok := errors.AsType[*trpcgo.Error](err); ok {
				cb(ctx, trpcErr, path)
			} else {
				cb(ctx, trpcgo.WrapError(trpcgo.CodeInternalServerError, "internal server error", err), path)
			}
		}
		code := CodeFromTRPC(safe.Code)
		errBody, _ := encodeError(code, StatusFromCode(code), safe.Message, nil, safe.Code != trpcgo.CodeInternalServerError)
		return StatusFromCode(code), json.RawMessage(errBody)
	}

	// Streams should not reach here (filtered above), but guard anyway.
	if trpcgo.IsStreamResult(result) {
		errBody, _ := encodeError(CodeBadRequest, StatusFromCode(CodeBadRequest),
			"subscriptions cannot be batched", nil, false)
		return http.StatusBadRequest, json.RawMessage(errBody)
	}

	successStatus := http.StatusOK
	if route := entry.Route(); route.SuccessStatus > 0 {
		successStatus = route.SuccessStatus
	}

	body, encErr := encodeSuccess(result)
	if encErr != nil {
		errBody, _ := encodeError(CodeInternalServerError, StatusFromCode(CodeInternalServerError), "failed to serialize response", nil, false)
		return http.StatusInternalServerError, json.RawMessage(errBody)
	}
	return successStatus, json.RawMessage(body)
}

// resolveItemPath extracts the procedure dot-path from a batch item URL.
// The item URL is a full URL like "http://host/rpc/planet/list".
func (h *Handler) resolveItemPath(rawURL, method string) (string, *trpcgo.ProcedureEntry, map[string]string, bool) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return h.resolvePath(rawURL, method)
	}
	return h.resolvePath(u.Path, method)
}

// writeBatchBuffered collects all results and writes a single JSON array.
func (h *Handler) writeBatchBuffered(ctx context.Context, w http.ResponseWriter, ch <-chan batchResult, n int, batchStatus int) {
	results := make([]batchResponseItem, 0, n)
	for range n {
		res := <-ch
		item := batchResponseItem{Index: res.index, Body: res.body}
		if res.status != batchStatus {
			item.Status = res.status
		}
		results = append(results, item)
	}

	data, err := json.Marshal(results)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	trpcgo.ApplyResponseMetadata(ctx, w)
	w.WriteHeader(batchStatus)
	_, _ = w.Write(data)
}

// writeBatchStreaming writes results as SSE events as they complete.
// Each batch item is sent as an "event: message" with JSON data,
// followed by an "event: done" when all items have been sent.
func (h *Handler) writeBatchStreaming(ctx context.Context, w http.ResponseWriter, ch <-chan batchResult, n int, batchStatus int) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		// Fall back to buffered if flushing not supported.
		h.writeBatchBuffered(ctx, w, ch, n, batchStatus)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("X-Accel-Buffering", "no")
	w.Header().Set("Connection", "keep-alive")
	trpcgo.ApplyResponseMetadata(ctx, w)
	w.WriteHeader(batchStatus)

	for range n {
		res := <-ch
		item := batchResponseItem{Index: res.index, Body: res.body}
		if res.status != batchStatus {
			item.Status = res.status
		}
		itemData, err := json.Marshal(item)
		if err != nil {
			continue
		}
		_, _ = fmt.Fprintf(w, "event: message\ndata: %s\n\n", itemData)
		flusher.Flush()
	}

	_, _ = fmt.Fprint(w, "event: done\ndata: \n\n")
	flusher.Flush()
}

// methodAllowed checks whether the request method is accepted by a route.
// Routes with an explicit method (from WithRoute) require an exact match.
// Default routes (empty method) accept only GET and POST.
func methodAllowed(routeMethod, requestMethod string) bool {
	if routeMethod != "" {
		return requestMethod == routeMethod
	}
	return requestMethod == "GET" || requestMethod == "POST"
}

func stripBasePath(path, basePath string) (string, bool) {
	if basePath == "" {
		return strings.TrimPrefix(path, "/"), true
	}
	// basePath is pre-normalized by NewHandler: leading "/" and no trailing "/".
	if path == basePath {
		return "", true
	}
	if after, ok := strings.CutPrefix(path, basePath+"/"); ok {
		return after, true
	}
	return "", false
}

func buildRouteTable(pm *trpcgo.ProcedureMap) routeTable {
	table := routeTable{exact: map[string][]*routeMatch{}}
	matches := make([]*routeMatch, 0, pm.Len())

	for path, entry := range pm.All() {
		r := entry.Route()
		routePath := r.Path
		if routePath == "" {
			routePath = "/" + strings.ReplaceAll(path, ".", "/")
		}
		routePath = normalizeRoutePath(routePath)
		matches = append(matches, &routeMatch{
			path:     path,
			method:   strings.ToUpper(strings.TrimSpace(r.Method)),
			entry:    entry,
			segments: parseRouteSegments(routePath),
		})
	}

	// Detect route conflicts: same path pattern + same method.
	type routeKey struct {
		pattern string
		method  string
	}
	seen := make(map[routeKey]string, len(matches))
	for _, m := range matches {
		key := routeKey{pattern: m.pathFromSegments(), method: m.method}
		if existing, ok := seen[key]; ok {
			method := m.method
			if method == "" {
				method = "(any)"
			}
			panic(fmt.Sprintf("orpc: route conflict: procedures %q and %q both register %s %s",
				existing, m.path, method, key.pattern))
		}
		seen[key] = m.path
	}

	sort.Slice(matches, func(i, j int) bool {
		a := matches[i]
		b := matches[j]
		aStatic, aParams := routeSpecificity(a.segments)
		bStatic, bParams := routeSpecificity(b.segments)
		if aStatic != bStatic {
			return aStatic > bStatic
		}
		if aParams != bParams {
			return aParams < bParams
		}
		if a.path != b.path {
			return a.path < b.path
		}
		return a.method < b.method
	})

	for _, m := range matches {
		if hasRouteParams(m.segments) {
			table.template = append(table.template, m)
			continue
		}
		exactPath := m.pathFromSegments()
		table.exact[exactPath] = append(table.exact[exactPath], m)
	}

	return table
}

func normalizeRoutePath(routePath string) string {
	if routePath == "" {
		return "/"
	}
	if !strings.HasPrefix(routePath, "/") {
		routePath = "/" + routePath
	}
	if len(routePath) > 1 {
		routePath = strings.TrimSuffix(routePath, "/")
	}
	return routePath
}

func parseRouteSegments(routePath string) []routeSegment {
	routePath = strings.TrimPrefix(routePath, "/")
	if routePath == "" {
		return nil
	}
	parts := strings.Split(routePath, "/")
	segments := make([]routeSegment, 0, len(parts))
	for _, p := range parts {
		if len(p) >= 3 && strings.HasPrefix(p, "{") && strings.HasSuffix(p, "}") {
			name := strings.TrimSpace(p[1 : len(p)-1])
			if name != "" {
				segments = append(segments, routeSegment{isParam: true, paramName: name})
				continue
			}
		}
		segments = append(segments, routeSegment{value: p})
	}
	return segments
}

func (m *routeMatch) pathFromSegments() string {
	if len(m.segments) == 0 {
		return "/"
	}
	parts := make([]string, 0, len(m.segments))
	for _, s := range m.segments {
		if s.isParam {
			parts = append(parts, "{"+s.paramName+"}")
		} else {
			parts = append(parts, s.value)
		}
	}
	return "/" + strings.Join(parts, "/")
}

func routeSpecificity(segments []routeSegment) (staticCount, paramCount int) {
	for _, s := range segments {
		if s.isParam {
			paramCount++
		} else {
			staticCount++
		}
	}
	return staticCount, paramCount
}

func hasRouteParams(segments []routeSegment) bool {
	for _, s := range segments {
		if s.isParam {
			return true
		}
	}
	return false
}

func matchTemplate(path string, segments []routeSegment) (map[string]string, bool) {
	path = strings.TrimPrefix(path, "/")
	if path == "" {
		if len(segments) == 0 {
			return nil, true
		}
		return nil, false
	}
	parts := strings.Split(path, "/")
	if len(parts) != len(segments) {
		return nil, false
	}
	params := map[string]string{}
	for i, s := range segments {
		part := parts[i]
		if s.isParam {
			params[s.paramName] = part
			continue
		}
		if s.value != part {
			return nil, false
		}
	}
	return params, true
}

func mergePathParams(raw json.RawMessage, params map[string]string, inputType reflect.Type) json.RawMessage {
	if len(params) == 0 {
		return raw
	}
	var obj map[string]any
	if len(raw) == 0 || string(raw) == "null" {
		obj = map[string]any{}
	} else if err := json.Unmarshal(raw, &obj); err != nil {
		return raw
	}
	for k, v := range params {
		obj[k] = coerceForField(v, k, inputType)
	}
	merged, err := json.Marshal(obj)
	if err != nil {
		return raw
	}
	return merged
}

// coerceForField converts a path parameter string to the JSON type matching
// the corresponding struct field. For int fields, "42" becomes a JSON number;
// for string fields it stays a JSON string. Falls back to string if the field
// is not found or the input type is not a struct.
func coerceForField(v, jsonKey string, inputType reflect.Type) any {
	if inputType == nil {
		return v
	}
	t := inputType
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return v
	}
	ft, ok := fieldTypeByJSON(t, jsonKey)
	if !ok {
		return v
	}
	return coerceToType(v, ft)
}

// fieldTypeByJSON finds the reflect.Type of the struct field whose JSON name
// matches jsonKey, walking into anonymous (embedded) structs.
func fieldTypeByJSON(t reflect.Type, jsonKey string) (reflect.Type, bool) {
	for i := 0; i < t.NumField(); i++ {
		sf := t.Field(i)
		if sf.PkgPath != "" && !sf.Anonymous {
			continue
		}
		tag := sf.Tag.Get("json")
		if tag == "-" {
			continue
		}
		name := sf.Name
		if tag != "" {
			if n, _, _ := strings.Cut(tag, ","); n != "" {
				name = n
			}
		}
		if sf.Anonymous && name == sf.Name {
			ft := sf.Type
			for ft.Kind() == reflect.Pointer {
				ft = ft.Elem()
			}
			if ft.Kind() == reflect.Struct {
				if found, ok := fieldTypeByJSON(ft, jsonKey); ok {
					return found, true
				}
			}
			continue
		}
		if name == jsonKey {
			return sf.Type, true
		}
	}
	return nil, false
}

// coerceToType converts a string value to a JSON-compatible representation
// matching the target Go type.
func coerceToType(v string, t reflect.Type) any {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	switch t.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
		reflect.Float32, reflect.Float64:
		if json.Valid([]byte(v)) {
			return json.RawMessage(v)
		}
	case reflect.Bool:
		if v == "true" || v == "false" {
			return json.RawMessage(v)
		}
	}
	return v
}

// hasRoute reports whether any route matches the URL path, regardless of method.
func (h *Handler) hasRoute(urlPath string) bool {
	rawPath, ok := stripBasePath(urlPath, h.basePath)
	if !ok || rawPath == "" {
		return false
	}
	normalized := "/" + rawPath

	if _, ok := h.routes.exact[normalized]; ok {
		return true
	}
	for _, m := range h.routes.template {
		if _, matched := matchTemplate(normalized, m.segments); matched {
			return true
		}
	}
	return false
}

// mergeLastEventId reads the Last-Event-Id header (or lastEventId query param)
// and merges it into the input JSON for SSE reconnection support.
func mergeLastEventId(r *http.Request, input json.RawMessage) json.RawMessage {
	id := r.Header.Get("Last-Event-Id")
	if id == "" {
		id = r.URL.Query().Get("lastEventId")
	}
	if id == "" {
		id = r.URL.Query().Get("Last-Event-Id")
	}
	if id == "" {
		return input
	}
	if len(input) == 0 || string(input) == "null" {
		merged, _ := json.Marshal(map[string]string{"lastEventId": id})
		return merged
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(input, &obj); err != nil {
		return input
	}
	idVal, _ := json.Marshal(id)
	obj["lastEventId"] = idVal
	merged, _ := json.Marshal(obj)
	return merged
}

// mergeContexts returns a context that carries values from valuesCtx but
// cancels when either cancelCtx or valuesCtx is done.
func mergeContexts(cancelCtx, valuesCtx context.Context) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancelCause(valuesCtx)
	stop := context.AfterFunc(cancelCtx, func() {
		cancel(cancelCtx.Err())
	})
	return ctx, func() {
		stop()
		cancel(nil)
	}
}
