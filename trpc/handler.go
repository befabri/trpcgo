// Package trpc provides an HTTP handler that serves trpcgo procedures using
// the tRPC wire format. Use [NewHandler] to create an http.Handler from a
// [trpcgo.Router].
package trpc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/befabri/trpcgo"
)

// Handler is an http.Handler that serves trpcgo procedures over the tRPC
// wire protocol. Create one via [NewHandler].
type Handler struct {
	router     *trpcgo.Router
	procedures *trpcgo.ProcedureMap
	basePath   string
	opts       handlerOptions
}

// NewHandler creates a tRPC HTTP handler from a trpcgo Router.
// basePath is the URL prefix stripped before procedure lookup
// (e.g., "/trpc" means /trpc/user.getById → procedure "user.getById").
func NewHandler(r *trpcgo.Router, basePath string, opts ...HandlerOption) *Handler {
	r.StartDevWatcher()
	handlerOpts := defaultHandlerOptions()
	for _, opt := range opts {
		opt(&handlerOpts)
	}
	return &Handler{
		router:     r,
		procedures: r.BuildProcedureMap(),
		basePath:   basePath,
		opts:       handlerOpts,
	}
}

type callResult struct {
	response any
	status   int
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if h.handleCORS(w, r) {
		return
	}
	if h.rejectUnsupportedMethod(w, r) {
		return
	}
	if h.rejectInvalidContentType(w, r) {
		return
	}
	if h.rejectCSRF(w, r) {
		return
	}

	isBatch := isBatchRequest(r)
	if h.rejectInvalidBatch(w, r, isBatch) {
		return
	}

	calls, err := h.parseCalls(r, isBatch)
	if err != nil {
		h.writeParseError(w, err)
		return
	}

	ctx, cancel := h.requestContext(r)
	defer cancel()
	ctx = trpcgo.WithResponseMetadata(ctx)

	if isBatch && r.Header.Get("trpc-accept") == "application/jsonl" {
		h.writeJSONLStream(ctx, w, r, calls)
		return
	}

	results := h.executeCalls(ctx, r, calls)

	if !isBatch && trpcgo.IsStreamResult(results[0].response) {
		h.handleStream(ctx, w, results[0].response, calls[0])
		return
	}
	h.writeCallResults(ctx, w, isBatch, results)
}

func (h *Handler) rejectUnsupportedMethod(w http.ResponseWriter, r *http.Request) bool {
	if r.Method == http.MethodGet || r.Method == http.MethodPost {
		return false
	}
	h.writeErrorResponse(w, trpcgo.NewError(trpcgo.CodeMethodNotSupported, "only GET and POST are supported"), "", nil, "")
	return true
}

func (h *Handler) rejectInvalidBatch(w http.ResponseWriter, r *http.Request, isBatch bool) bool {
	if !isBatch {
		return false
	}
	if !h.router.AllowBatching() {
		h.writeErrorResponse(w, trpcgo.NewError(trpcgo.CodeBadRequest, "batching is not enabled"), "", nil, "")
		return true
	}
	paths := parsePaths(r, h.basePath)
	if max := h.router.MaxBatchSize(); max > 0 && len(paths) > max {
		h.writeErrorResponse(w, trpcgo.NewError(trpcgo.CodeBadRequest, fmt.Sprintf("batch size %d exceeds limit of %d", len(paths), max)), "", nil, "")
		return true
	}
	if h.hasBatchSubscription(paths) {
		h.writeErrorResponse(w, trpcgo.NewError(trpcgo.CodeBadRequest, "subscriptions cannot be batched"), "", nil, "")
		return true
	}
	return false
}

func (h *Handler) hasBatchSubscription(paths []string) bool {
	for _, path := range paths {
		if proc, ok := h.procedures.Lookup(path); ok && proc.Type() == trpcgo.ProcedureSubscription {
			return true
		}
	}
	return false
}

func (h *Handler) parseCalls(r *http.Request, isBatch bool) ([]parsedRequest, error) {
	return parseRequest(r, h.basePath, isBatch, h.router.MaxBodySize())
}

func (h *Handler) writeParseError(w http.ResponseWriter, err error) {
	if trpcErr, ok := errors.AsType[*trpcgo.Error](err); ok {
		h.writeErrorResponse(w, trpcErr, "", nil, "")
		return
	}
	h.writeErrorResponse(w, trpcgo.NewError(trpcgo.CodeInternalServerError, "internal server error"), "", nil, "")
}

func (h *Handler) requestContext(r *http.Request) (context.Context, context.CancelFunc) {
	ctx := r.Context()
	cc := h.router.ContextCreator()
	if cc == nil {
		return ctx, func() {}
	}
	userCtx := cc(ctx, r)
	if userCtx == ctx {
		return ctx, func() {}
	}
	return mergeContexts(ctx, userCtx)
}

func (h *Handler) executeCalls(ctx context.Context, r *http.Request, calls []parsedRequest) []callResult {
	results := make([]callResult, len(calls))
	for i, call := range calls {
		resp, status := h.executeCall(ctx, r, call)
		results[i] = callResult{response: resp, status: status}
	}
	return results
}

func (h *Handler) writeCallResults(ctx context.Context, w http.ResponseWriter, isBatch bool, results []callResult) {
	if !isBatch {
		h.writeSingleResult(ctx, w, results[0])
		return
	}
	h.writeBatchResults(ctx, w, results)
}

func (h *Handler) writeSingleResult(ctx context.Context, w http.ResponseWriter, result callResult) {
	w.Header().Set("Content-Type", "application/json")
	data, err := json.Marshal(result.response)
	if err != nil {
		h.writeErrorResponse(w, trpcgo.NewError(trpcgo.CodeInternalServerError, "failed to serialize response"), "", ctx, "")
		return
	}
	trpcgo.ApplyResponseMetadata(ctx, w)
	w.WriteHeader(result.status)
	_, _ = w.Write(data)
}

func (h *Handler) writeBatchResults(ctx context.Context, w http.ResponseWriter, results []callResult) {
	w.Header().Set("Content-Type", "application/json")
	addVary(w.Header(), "trpc-accept")
	responses := make([]any, len(results))
	for i, res := range results {
		responses[i] = res.response
	}
	data, err := json.Marshal(responses)
	if err != nil {
		h.writeErrorResponse(w, trpcgo.NewError(trpcgo.CodeInternalServerError, "failed to serialize response"), "", ctx, "")
		return
	}
	statusCode := determineBatchStatus(results)
	trpcgo.ApplyResponseMetadata(ctx, w)
	w.WriteHeader(statusCode)
	_, _ = w.Write(data)
}

func (h *Handler) executeCall(ctx context.Context, r *http.Request, call parsedRequest) (any, int) {
	proc, ok := h.procedures.Lookup(call.path)
	if !ok {
		trpcErr := trpcgo.NewError(trpcgo.CodeNotFound, "procedure not found")
		if cb := h.router.ErrorCallback(); cb != nil {
			cb(ctx, trpcErr, call.path)
		}
		return h.fmtError(trpcErr, call.path, call.input, ctx, ""), trpcgo.HTTPStatusFromCode(trpcgo.CodeNotFound)
	}

	ctx = trpcgo.WithProcedureMeta(ctx, trpcgo.ProcedureMeta{
		Path: call.path,
		Type: proc.Type(),
		Meta: proc.Meta(),
	})

	if proc.Type() == trpcgo.ProcedureSubscription {
		call.input = mergeLastEventId(r, call.input)
	}

	if err := validateMethod(r.Method, proc.Type(), h.router.AllowMethodOverride()); err != nil {
		if cb := h.router.ErrorCallback(); cb != nil {
			cb(ctx, err, call.path)
		}
		return h.fmtError(err, call.path, call.input, ctx, proc.Type()), trpcgo.HTTPStatusFromCode(err.Code)
	}

	result, err := h.router.ExecuteEntry(ctx, proc, call.input)
	if err != nil {
		trpcErr, ok := errors.AsType[*trpcgo.Error](err)
		if cb := h.router.ErrorCallback(); cb != nil {
			callbackErr := trpcErr
			if !ok {
				callbackErr = trpcgo.WrapError(trpcgo.CodeInternalServerError, "internal server error", err)
			}
			cb(ctx, callbackErr, call.path)
		}
		if !ok {
			trpcErr = trpcgo.NewError(trpcgo.CodeInternalServerError, "internal server error")
		}
		return h.fmtError(trpcErr, call.path, call.input, ctx, proc.Type()), trpcgo.HTTPStatusFromCode(trpcErr.Code)
	}

	// Return stream results unwrapped for SSE dispatch.
	if trpcgo.IsStreamResult(result) {
		return result, http.StatusOK
	}

	return trpcgo.NewResultEnvelope(result), http.StatusOK
}

// fmtError formats an error using the router's error formatter.
func (h *Handler) fmtError(err *trpcgo.Error, path string, input json.RawMessage, ctx context.Context, typ trpcgo.ProcedureType) any {
	return h.router.FormatError(err, path, input, ctx, typ)
}

func validateMethod(method string, typ trpcgo.ProcedureType, allowOverride bool) *trpcgo.Error {
	switch typ {
	case trpcgo.ProcedureQuery:
		if method == http.MethodGet || allowOverride {
			return nil
		}
		if method == http.MethodPost {
			return trpcgo.NewError(trpcgo.CodeMethodNotSupported, "queries must use GET (or enable methodOverride)")
		}
	case trpcgo.ProcedureMutation:
		if method == http.MethodPost {
			return nil
		}
		return trpcgo.NewError(trpcgo.CodeMethodNotSupported, "mutations must use POST")
	case trpcgo.ProcedureSubscription:
		return nil
	}
	return nil
}

func determineBatchStatus(results []callResult) int {
	if len(results) == 0 {
		return http.StatusOK
	}
	first := results[0].status
	allSame := true
	for _, r := range results[1:] {
		if r.status != first {
			allSame = false
			break
		}
	}
	if allSame {
		return first
	}
	return http.StatusMultiStatus
}

// handleStream writes SSE for a subscription result using the tRPC SSE format.
func (h *Handler) handleStream(ctx context.Context, w http.ResponseWriter, result any, call parsedRequest) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		h.writeErrorResponse(w, trpcgo.NewError(trpcgo.CodeInternalServerError, "streaming not supported"), "", ctx, "")
		return
	}

	tracked, ok := h.trackSSEConnection(w, ctx, call.path)
	if !ok {
		return
	}
	if tracked {
		defer h.router.TrackSSEConnection(-1)
	}

	consumer := trpcgo.ConsumeStream(result)
	if consumer == nil {
		h.writeErrorResponse(w, trpcgo.NewError(trpcgo.CodeInternalServerError, "invalid stream result"), "", ctx, "")
		return
	}

	// SSE headers.
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("X-Accel-Buffering", "no")
	w.Header().Set("Connection", "keep-alive")
	trpcgo.ApplyResponseMetadata(ctx, w)
	w.WriteHeader(http.StatusOK)

	// Connected event with client options (tRPC protocol).
	connData, _ := json.Marshal(struct {
		ReconnectAfterInactivityMs int `json:"reconnectAfterInactivityMs,omitempty"`
	}{
		ReconnectAfterInactivityMs: h.router.SSEReconnectAfterInactivityMs(),
	})
	writeSSENamedEvent(w, "connected", connData)
	flusher.Flush()

	pingInterval := h.router.SSEPingInterval()
	if pingInterval == 0 {
		pingInterval = 10 * time.Second
	}
	pingTicker := time.NewTicker(pingInterval)
	defer pingTicker.Stop()

	var maxTimer <-chan time.Time
	if d := h.router.SSEMaxDuration(); d > 0 {
		t := time.NewTimer(d)
		defer t.Stop()
		maxTimer = t.C
	}

	// Recv goroutine.
	type recvResult struct {
		data  any
		id    string
		retry int
		err   error
	}
	recvCh := make(chan recvResult, 1)
	sendRecv := func(item recvResult) bool {
		select {
		case recvCh <- item:
			return true
		case <-ctx.Done():
			return false
		}
	}
	go func() {
		defer func() {
			if rv := recover(); rv != nil {
				_ = sendRecv(recvResult{err: fmt.Errorf("subscription stream panic: %v", rv)})
			}
		}()
		for {
			data, id, retry, err := consumer.Recv(ctx)
			if !sendRecv(recvResult{data, id, retry, err}) {
				return
			}
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
			writeSSENamedEvent(w, "return", nil)
			flusher.Flush()
			return
		case <-pingTicker.C:
			writeSSENamedEvent(w, "ping", nil)
			flusher.Flush()
		case item := <-recvCh:
			if h.writeStreamItem(ctx, w, item, call) {
				flusher.Flush()
				return
			}
			flusher.Flush()
			pingTicker.Reset(pingInterval)
		}
	}
}

func (h *Handler) trackSSEConnection(w http.ResponseWriter, ctx context.Context, path string) (tracked bool, ok bool) {
	max := h.router.MaxSSEConnections()
	if max <= 0 {
		return false, true
	}
	n := h.router.TrackSSEConnection(1)
	if n <= int64(max) {
		return true, true
	}
	h.router.TrackSSEConnection(-1)
	h.writeErrorResponse(w, trpcgo.NewError(trpcgo.CodeTooManyRequests, "too many concurrent subscriptions"), path, ctx, trpcgo.ProcedureSubscription)
	return false, false
}

func (h *Handler) writeStreamItem(ctx context.Context, w http.ResponseWriter, item struct {
	data  any
	id    string
	retry int
	err   error
}, call parsedRequest) bool {
	if item.err == io.EOF {
		writeStreamReturn(w, item.data)
		return true
	}
	if item.err != nil {
		h.writeStreamReceiveError(ctx, w, item.err, call)
		return true
	}
	data, err := json.Marshal(item.data)
	if err != nil {
		h.writeStreamError(ctx, w, trpcgo.NewError(trpcgo.CodeInternalServerError, "failed to serialize subscription data"), call)
		return true
	}
	writeSSEData(w, data, item.id, item.retry)
	return false
}

func (h *Handler) writeStreamReceiveError(ctx context.Context, w http.ResponseWriter, err error, call parsedRequest) {
	if cb := h.router.ErrorCallback(); cb != nil {
		cb(ctx, trpcgo.WrapError(trpcgo.CodeInternalServerError, "internal server error", err), call.path)
	}
	formatted := h.fmtError(trpcgo.SanitizeError(err), call.path, call.input, ctx, trpcgo.ProcedureSubscription)
	errData, _ := json.Marshal(formatted)
	writeSSENamedEvent(w, "serialized-error", errData)
}

func writeStreamReturn(w http.ResponseWriter, data any) {
	if data == nil {
		writeSSENamedEvent(w, "return", nil)
		return
	}
	returnData, _ := json.Marshal(data)
	writeSSENamedEvent(w, "return", returnData)
}

func (h *Handler) writeStreamError(ctx context.Context, w http.ResponseWriter, err *trpcgo.Error, call parsedRequest) {
	if cb := h.router.ErrorCallback(); cb != nil {
		cb(ctx, err, call.path)
	}
	formatted := h.fmtError(err, call.path, call.input, ctx, trpcgo.ProcedureSubscription)
	errData, _ := json.Marshal(formatted)
	writeSSENamedEvent(w, "serialized-error", errData)
}

// writeJSONLStream handles JSONL batch responses with concurrent execution.
func (h *Handler) writeJSONLStream(ctx context.Context, w http.ResponseWriter, r *http.Request, calls []parsedRequest) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		h.writeErrorResponse(w, trpcgo.NewError(trpcgo.CodeInternalServerError, "streaming not supported"), "", ctx, "")
		return
	}

	n := len(calls)

	head := make(map[string]any, n)
	for i := range n {
		head[fmt.Sprintf("%d", i)] = []any{[]any{0}, []any{nil, 0, i}}
	}
	headData, err := json.Marshal(head)
	if err != nil {
		h.writeErrorResponse(w, trpcgo.NewError(trpcgo.CodeInternalServerError, "failed to serialize response"), "", ctx, "")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Transfer-Encoding", "chunked")
	addVary(w.Header(), "trpc-accept")
	trpcgo.ApplyResponseMetadata(ctx, w)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(headData)
	_, _ = w.Write([]byte("\n"))
	flusher.Flush()

	type indexedResult struct {
		index    int
		response any
	}
	ch := make(chan indexedResult, n)
	for i, call := range calls {
		go func(idx int, c parsedRequest) {
			defer func() {
				if rv := recover(); rv != nil {
					trpcErr := trpcgo.NewError(trpcgo.CodeInternalServerError, "internal server error")
					ch <- indexedResult{
						index:    idx,
						response: h.fmtError(trpcErr, c.path, c.input, ctx, ""),
					}
				}
			}()
			resp, _ := h.executeCall(ctx, r, c)
			ch <- indexedResult{index: idx, response: resp}
		}(i, call)
	}

	for range n {
		res := <-ch
		chunk := []any{res.index, 0, []any{[]any{res.response}}}
		chunkData, _ := json.Marshal(chunk)
		_, _ = w.Write(chunkData)
		_, _ = w.Write([]byte("\n"))
		flusher.Flush()
	}
}

func (h *Handler) writeErrorResponse(w http.ResponseWriter, err *trpcgo.Error, path string, ctx context.Context, typ trpcgo.ProcedureType) {
	w.Header().Set("Content-Type", "application/json")
	var formatted any
	if ctx != nil {
		formatted = h.fmtError(err, path, nil, ctx, typ)
	} else {
		formatted = trpcgo.DefaultErrorEnvelope(err, path, h.router.IsDev())
	}
	data, marshalErr := json.Marshal(formatted)
	if marshalErr != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	if ctx != nil {
		trpcgo.ApplyResponseMetadata(ctx, w)
	}
	w.WriteHeader(trpcgo.HTTPStatusFromCode(err.Code))
	_, _ = w.Write(data)
}

// writeSSENamedEvent writes an SSE event with an explicit event type.
func writeSSENamedEvent(w http.ResponseWriter, event string, data []byte) {
	_, _ = fmt.Fprintf(w, "event: %s\n", event)
	if len(data) > 0 {
		_, _ = fmt.Fprintf(w, "data: %s\n", data)
	} else {
		_, _ = fmt.Fprint(w, "data: \n")
	}
	_, _ = fmt.Fprint(w, "\n")
}

// writeSSEData writes a data-only SSE message (default "message" event type).
func writeSSEData(w http.ResponseWriter, data []byte, id string, retry int) {
	_, _ = fmt.Fprintf(w, "data: %s\n", data)
	if id != "" {
		id = strings.NewReplacer("\n", "", "\r", "").Replace(id)
		_, _ = fmt.Fprintf(w, "id: %s\n", id)
	}
	if retry > 0 {
		_, _ = fmt.Fprintf(w, "retry: %d\n", retry)
	}
	_, _ = fmt.Fprint(w, "\n")
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
