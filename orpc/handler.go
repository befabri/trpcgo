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
	"strings"
	"time"

	"github.com/befabri/trpcgo"
)

// Handler is an http.Handler that serves trpcgo procedures over the oRPC
// wire protocol. Create one via [NewHandler].
type Handler struct {
	router     *trpcgo.Router
	procedures *trpcgo.ProcedureMap
	basePath   string
}

// NewHandler creates an oRPC HTTP handler from a trpcgo Router.
// basePath is the URL prefix to strip before procedure lookup
// (e.g., "/rpc" means /rpc/planet/list → procedure "planet.list").
func NewHandler(r *trpcgo.Router, basePath string) *Handler {
	return &Handler{
		router:     r,
		procedures: r.BuildProcedureMap(),
		basePath:   strings.TrimRight(basePath, "/"),
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
	// Only GET and POST.
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		body, status := encodeError(CodeMethodNotSupported, StatusFromCode(CodeMethodNotSupported), "only GET and POST are supported", nil, false)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write(body)
		return
	}

	// Check for batch request (x-orpc-batch header).
	if batchMode := r.Header.Get("x-orpc-batch"); batchMode != "" {
		h.handleBatch(w, r, batchMode)
		return
	}

	// Map URL path to procedure dot-path.
	path := h.resolvePath(r.URL.Path)
	entry, ok := h.procedures.Lookup(path)
	if !ok {
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

// resolvePath converts a URL path to a procedure dot-path.
// /basePath/planet/list → planet.list
func (h *Handler) resolvePath(urlPath string) string {
	p := strings.TrimPrefix(urlPath, h.basePath)
	p = strings.TrimPrefix(p, "/")
	p = strings.TrimSuffix(p, "/")
	return strings.ReplaceAll(p, "/", ".")
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
		data any
		id   string
		err  error
	}
	recvCh := make(chan recvResult, 1)
	go func() {
		for {
			data, id, err := consumer.Recv(ctx)
			recvCh <- recvResult{data, id, err}
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
				_, _ = fmt.Fprint(w, "event: done\ndata: \n\n")
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
func (h *Handler) executeBatchItem(ctx context.Context, _ *http.Request, item batchRequestItem) (int, any) {
	// Extract procedure path from the item URL.
	path := h.resolveItemPath(item.URL)
	entry, ok := h.procedures.Lookup(path)
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
func (h *Handler) resolveItemPath(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return h.resolvePath(rawURL)
	}
	return h.resolvePath(u.Path)
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

// writeBatchStreaming writes results as NDJSON as they complete.
func (h *Handler) writeBatchStreaming(ctx context.Context, w http.ResponseWriter, ch <-chan batchResult, n int, batchStatus int) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		// Fall back to buffered if flushing not supported.
		h.writeBatchBuffered(ctx, w, ch, n, batchStatus)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Transfer-Encoding", "chunked")
	trpcgo.ApplyResponseMetadata(ctx, w)
	w.WriteHeader(batchStatus)

	_, _ = w.Write([]byte("["))
	flusher.Flush()

	first := true
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
		if !first {
			_, _ = w.Write([]byte(","))
		}
		first = false
		_, _ = w.Write(itemData)
		flusher.Flush()
	}

	_, _ = w.Write([]byte("]"))
	flusher.Flush()
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
