// Package trpc provides an HTTP handler that serves trpcgo procedures using
// the tRPC wire format. Use [NewHandler] to create an http.Handler from a
// [trpcgo.Router].
//
// This is the sub-package equivalent of [trpcgo.Router.Handler]. It uses the
// exported dispatch API so protocol handling is fully decoupled from the core.
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
}

// NewHandler creates a tRPC HTTP handler from a trpcgo Router.
// basePath is the URL prefix stripped before procedure lookup
// (e.g., "/trpc" means /trpc/user.getById → procedure "user.getById").
func NewHandler(r *trpcgo.Router, basePath string) *Handler {
	return &Handler{
		router:     r,
		procedures: r.BuildProcedureMap(),
		basePath:   basePath,
	}
}

type callResult struct {
	response any
	status   int
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		h.writeErrorResponse(w, trpcgo.NewError(trpcgo.CodeMethodNotSupported, "only GET and POST are supported"), "", nil, "")
		return
	}

	isBatch := isBatchRequest(r)

	if isBatch && !h.router.AllowBatching() {
		h.writeErrorResponse(w, trpcgo.NewError(trpcgo.CodeBadRequest, "batching is not enabled"), "", nil, "")
		return
	}

	if isBatch {
		paths := parsePaths(r, h.basePath)
		if max := h.router.MaxBatchSize(); max > 0 && len(paths) > max {
			h.writeErrorResponse(w, trpcgo.NewError(trpcgo.CodeBadRequest, fmt.Sprintf("batch size %d exceeds limit of %d", len(paths), max)), "", nil, "")
			return
		}
		for _, path := range paths {
			if proc, ok := h.procedures.Lookup(path); ok && proc.Type() == trpcgo.ProcedureSubscription {
				h.writeErrorResponse(w, trpcgo.NewError(trpcgo.CodeBadRequest, "subscriptions cannot be batched"), "", nil, "")
				return
			}
		}
	}

	calls, err := parseRequest(r, h.basePath, isBatch, h.router.MaxBodySize())
	if err != nil {
		if trpcErr, ok := errors.AsType[*trpcgo.Error](err); ok {
			h.writeErrorResponse(w, trpcErr, "", nil, "")
		} else {
			h.writeErrorResponse(w, trpcgo.NewError(trpcgo.CodeInternalServerError, "internal server error"), "", nil, "")
		}
		return
	}

	// Create context.
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

	// JSONL streaming batch.
	if isBatch && r.Header.Get("trpc-accept") == "application/jsonl" {
		h.writeJSONLStream(ctx, w, r, calls)
		return
	}

	// Execute all calls sequentially.
	results := make([]callResult, len(calls))
	for i, call := range calls {
		resp, status := h.executeCall(ctx, r, call)
		results[i] = callResult{response: resp, status: status}
	}

	// Check for SSE subscription.
	if !isBatch {
		if trpcgo.IsStreamResult(results[0].response) {
			h.handleStream(ctx, w, results[0].response, calls[0])
			return
		}
	}

	// Write response.
	if !isBatch {
		w.Header().Set("Content-Type", "application/json")
		data, err := json.Marshal(results[0].response)
		if err != nil {
			h.writeErrorResponse(w, trpcgo.NewError(trpcgo.CodeInternalServerError, "failed to serialize response"), "", ctx, "")
			return
		}
		trpcgo.ApplyResponseMetadata(ctx, w)
		w.WriteHeader(results[0].status)
		_, _ = w.Write(data)
		return
	}

	// Standard JSON array batch response.
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Vary", "trpc-accept")
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

	if max := h.router.MaxSSEConnections(); max > 0 {
		n := h.router.TrackSSEConnection(1)
		if n > int64(max) {
			h.router.TrackSSEConnection(-1)
			h.writeErrorResponse(w, trpcgo.NewError(trpcgo.CodeTooManyRequests, "too many concurrent subscriptions"), call.path, ctx, trpcgo.ProcedureSubscription)
			return
		}
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
			writeSSENamedEvent(w, "return", nil)
			flusher.Flush()
			return
		case <-pingTicker.C:
			writeSSENamedEvent(w, "ping", nil)
			flusher.Flush()
		case item := <-recvCh:
			if item.err == io.EOF {
				writeSSENamedEvent(w, "return", nil)
				flusher.Flush()
				return
			}
			if item.err != nil {
				sseErr := trpcgo.WrapError(trpcgo.CodeInternalServerError, "internal server error", item.err)
				if cb := h.router.ErrorCallback(); cb != nil {
					cb(ctx, sseErr, call.path)
				}
				formatted := h.fmtError(trpcgo.SanitizeError(item.err), call.path, call.input, ctx, trpcgo.ProcedureSubscription)
				errData, _ := json.Marshal(formatted)
				writeSSENamedEvent(w, "serialized-error", errData)
				flusher.Flush()
				return
			}

			var data []byte
			var marshalErr error
			data, marshalErr = json.Marshal(item.data)
			if marshalErr != nil {
				sseErr := trpcgo.NewError(trpcgo.CodeInternalServerError, "failed to serialize subscription data")
				if cb := h.router.ErrorCallback(); cb != nil {
					cb(ctx, sseErr, call.path)
				}
				formatted := h.fmtError(sseErr, call.path, call.input, ctx, trpcgo.ProcedureSubscription)
				errData, _ := json.Marshal(formatted)
				writeSSENamedEvent(w, "serialized-error", errData)
				flusher.Flush()
				return
			}
			writeSSEData(w, data, item.id)
			flusher.Flush()
			pingTicker.Reset(pingInterval)
		}
	}
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
	w.Header().Set("Vary", "trpc-accept")
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
func writeSSEData(w http.ResponseWriter, data []byte, id string) {
	_, _ = fmt.Fprintf(w, "data: %s\n", data)
	if id != "" {
		id = strings.NewReplacer("\n", "", "\r", "").Replace(id)
		_, _ = fmt.Fprintf(w, "id: %s\n", id)
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
