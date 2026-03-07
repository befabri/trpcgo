package trpcgo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
)

type httpHandler struct {
	router     *Router          // kept for ExecuteEntry (shared with RawCall)
	procedures *ProcedureMap    // frozen snapshot — no lock on the hot path
	opts       *routerOptions   // pointer to router's opts (immutable after construction)
	basePath   string
}

func (h *httpHandler) lookup(path string) (*ProcedureEntry, bool) {
	return h.procedures.Lookup(path)
}

type callResult struct {
	response any
	status   int
}

func (h *httpHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Only allow GET and POST
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		h.writeErrorResponse(w, NewError(CodeMethodNotSupported, "only GET and POST are supported"), "", nil, "")
		return
	}

	isBatch := isBatchRequest(r)

	// Check if batching is allowed
	if isBatch && !h.opts.allowBatching {
		h.writeErrorResponse(w, NewError(CodeBadRequest, "batching is not enabled"), "", nil, "")
		return
	}

	// For batch requests, enforce the batch size limit early — before parsing
	// inputs or iterating paths — to prevent amplification from oversized URL paths.
	if isBatch {
		paths := parsePaths(r, h.basePath)
		if h.opts.maxBatchSize > 0 && len(paths) > h.opts.maxBatchSize {
			h.writeErrorResponse(w, NewError(CodeBadRequest, fmt.Sprintf("batch size %d exceeds limit of %d", len(paths), h.opts.maxBatchSize)), "", nil, "")
			return
		}
		// Subscriptions cannot be batched (tRPC spec).
		for _, path := range paths {
			if proc, ok := h.lookup(path); ok && proc.typ == ProcedureSubscription {
				h.writeErrorResponse(w, NewError(CodeBadRequest, "subscriptions cannot be batched"), "", nil, "")
				return
			}
		}
	}

	// Parse all procedure calls from the request.
	calls, err := parseRequest(r, h.basePath, isBatch, h.opts.maxBodySize)
	if err != nil {
		if trpcErr, ok := errors.AsType[*Error](err); ok {
			h.writeErrorResponse(w, trpcErr, "", nil, "")
		} else {
			h.writeErrorResponse(w, NewError(CodeInternalServerError, "internal server error"), "", nil, "")
		}
		return
	}

	// Create context.
	// If createContext returns a context not derived from r.Context(),
	// we must still propagate the request's cancellation so that SSE
	// subscriptions and long-running handlers stop when the client
	// disconnects.
	ctx := r.Context()
	if h.opts.createContext != nil {
		userCtx := h.opts.createContext(r.Context(), r)
		if userCtx != ctx {
			var cancel context.CancelFunc
			ctx, cancel = mergeContexts(ctx, userCtx)
			defer cancel()
		}
	}
	ctx = WithResponseMetadata(ctx)

	// JSONL streaming: concurrent execution with progressive chunk delivery.
	if isBatch && r.Header.Get("trpc-accept") == "application/jsonl" {
		h.writeJSONLStream(ctx, w, r, calls)
		return
	}

	// Execute all procedure calls sequentially.
	results := make([]callResult, len(calls))
	for i, call := range calls {
		resp, status := h.executeCall(ctx, r, call)
		results[i] = callResult{response: resp, status: status}
	}

	// Check if single result is a streamer (SSE subscription).
	if !isBatch {
		if s, ok := results[0].response.(streamer); ok {
			path := calls[0].path
			sseInput := calls[0].input

			// Enforce concurrent SSE connection limit.
			if max := h.opts.sseMaxConnections; max > 0 {
				n := h.router.sseConnections.Add(1)
				if n > int64(max) {
					h.router.sseConnections.Add(-1)
					h.writeErrorResponse(w, NewError(CodeTooManyRequests, "too many concurrent subscriptions"), path, ctx, ProcedureSubscription)
					return
				}
				defer h.router.sseConnections.Add(-1)
			}

			if err := s.writeSSE(ctx, w, sseOptions{
				pingInterval:               h.opts.ssePingInterval,
				maxDuration:                h.opts.sseMaxDuration,
				reconnectAfterInactivityMs: h.opts.sseReconnectAfterInactivityMs,
				isDev:                      h.opts.isDev,
				formatError: func(sseErr *Error) any {
					return formatError(h.opts, sseErr, path, sseInput, ctx, ProcedureSubscription)
				},
				onError: func(sseErr *Error) {
					if h.opts.onError != nil {
						h.opts.onError(ctx, sseErr, path)
					}
				},
			}); err != nil {
				if h.opts.onError != nil {
					if trpcErr, ok := errors.AsType[*Error](err); ok {
						h.opts.onError(ctx, trpcErr, path)
					} else {
						h.opts.onError(ctx, WrapError(CodeInternalServerError, "internal server error", err), path)
					}
				}
				h.writeErrorResponse(w, SanitizeError(err), "", ctx, "")
			}
			return
		}
	}

	// Write response
	if !isBatch {
		w.Header().Set("Content-Type", "application/json")
		data, err := json.Marshal(results[0].response)
		if err != nil {
			h.writeErrorResponse(w, NewError(CodeInternalServerError, "failed to serialize response"), "", ctx, "")
			return
		}
		ApplyResponseMetadata(ctx, w)
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
		h.writeErrorResponse(w, NewError(CodeInternalServerError, "failed to serialize response"), "", ctx, "")
		return
	}
	statusCode := determineBatchStatus(results)
	ApplyResponseMetadata(ctx, w)
	w.WriteHeader(statusCode)
	_, _ = w.Write(data)
}

func (h *httpHandler) executeCall(ctx context.Context, r *http.Request, call parsedRequest) (any, int) {
	proc, ok := h.lookup(call.path)
	if !ok {
		trpcErr := NewError(CodeNotFound, "procedure not found")
		if h.opts.onError != nil {
			h.opts.onError(ctx, trpcErr, call.path)
		}
		return formatError(h.opts, trpcErr, call.path, call.input, ctx, ""), HTTPStatusFromCode(CodeNotFound)
	}

	// Inject procedure metadata into context for middleware access.
	ctx = WithProcedureMeta(ctx, ProcedureMeta{
		Path: call.path,
		Type: proc.typ,
		Meta: proc.meta,
	})

	// For subscriptions, merge lastEventId into input (tRPC reconnection protocol).
	if proc.typ == ProcedureSubscription {
		call.input = mergeLastEventId(r, call.input)
	}

	// Validate HTTP method matches procedure type
	if err := validateMethod(r.Method, proc.typ, h.opts.allowMethodOverride); err != nil {
		if h.opts.onError != nil {
			h.opts.onError(ctx, err, call.path)
		}
		return formatError(h.opts, err, call.path, call.input, ctx, proc.typ), HTTPStatusFromCode(err.Code)
	}

	// Decode, validate, and execute through middleware chain.
	result, err := h.router.ExecuteEntry(ctx, proc, call.input)
	if err != nil {
		trpcErr, ok := errors.AsType[*Error](err)
		if h.opts.onError != nil {
			// Pass the original error so the server can log it.
			callbackErr := trpcErr
			if !ok {
				callbackErr = WrapError(CodeInternalServerError, "internal server error", err)
			}
			h.opts.onError(ctx, callbackErr, call.path)
		}
		if !ok {
			// Never leak internal error details to clients.
			trpcErr = NewError(CodeInternalServerError, "internal server error")
		}
		return formatError(h.opts, trpcErr, call.path, call.input, ctx, proc.typ), HTTPStatusFromCode(trpcErr.Code)
	}

	// Return streamers unwrapped so the handler can detect and dispatch SSE.
	if _, ok := result.(streamer); ok {
		return result, http.StatusOK
	}

	return newResultEnvelope(result), http.StatusOK
}

func validateMethod(method string, typ ProcedureType, allowOverride bool) *Error {
	switch typ {
	case ProcedureQuery:
		if method == http.MethodGet || allowOverride {
			return nil
		}
		if method == http.MethodPost {
			return NewError(CodeMethodNotSupported, "queries must use GET (or enable methodOverride)")
		}
	case ProcedureMutation:
		if method == http.MethodPost {
			return nil
		}
		return NewError(CodeMethodNotSupported, "mutations must use POST")
	case ProcedureSubscription:
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
	return http.StatusMultiStatus // 207
}

// mergeLastEventId reads the lastEventId from the request (header or query params)
// and merges it into the input JSON for subscription reconnection support.
// Precedence: Last-Event-Id header > lastEventId query > Last-Event-Id query.
func mergeLastEventId(r *http.Request, input json.RawMessage) json.RawMessage {
	lastEventId := r.Header.Get("Last-Event-Id")
	if lastEventId == "" {
		lastEventId = r.URL.Query().Get("lastEventId")
	}
	if lastEventId == "" {
		lastEventId = r.URL.Query().Get("Last-Event-Id")
	}
	if lastEventId == "" {
		return input
	}

	// Merge into input object.
	if len(input) == 0 || string(input) == "null" {
		// No input — create object with just lastEventId.
		merged, _ := json.Marshal(map[string]string{"lastEventId": lastEventId})
		return merged
	}

	// Try to merge into existing object.
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(input, &obj); err != nil {
		// Input is not an object (e.g., a string or number) — return as-is.
		return input
	}
	idVal, _ := json.Marshal(lastEventId)
	obj["lastEventId"] = idVal
	merged, _ := json.Marshal(obj)
	return merged
}

// writeJSONLStream handles JSONL batch responses with concurrent execution
// and progressive chunk delivery, matching tRPC's httpBatchStreamLink protocol.
//
// Wire format:
//
//	Head:  {"0":[[0],[null,0,0]],"1":[[0],[null,0,1]]}\n
//	Chunk: [chunkId,0,[[envelope]]]\n  (per result, in completion order)
func (h *httpHandler) writeJSONLStream(ctx context.Context, w http.ResponseWriter, r *http.Request, calls []parsedRequest) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		h.writeErrorResponse(w, NewError(CodeInternalServerError, "streaming not supported"), "", ctx, "")
		return
	}

	n := len(calls)

	// Build head line with placeholders for all results.
	// Each entry: [[placeholder], [null, PROMISE_TYPE(0), chunkId]]
	head := make(map[string]any, n)
	for i := range n {
		head[fmt.Sprintf("%d", i)] = []any{[]any{0}, []any{nil, 0, i}}
	}
	headData, err := json.Marshal(head)
	if err != nil {
		h.writeErrorResponse(w, NewError(CodeInternalServerError, "failed to serialize response"), "", ctx, "")
		return
	}

	// Write headers and head line before executing any handlers.
	// Note: response metadata from handlers is not applied for JSONL streaming
	// because headers must be sent before concurrent execution begins. Metadata
	// set before this point (e.g. from createContext) is still applied.
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Transfer-Encoding", "chunked")
	w.Header().Set("Vary", "trpc-accept")
	ApplyResponseMetadata(ctx, w)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(headData)
	_, _ = w.Write([]byte("\n"))
	flusher.Flush()

	// Execute all calls concurrently.
	// Each goroutine must recover panics — an unrecovered panic in a spawned
	// goroutine kills the entire process (net/http only recovers in the handler
	// goroutine). On panic, we send an INTERNAL_SERVER_ERROR result for that call.
	type indexedResult struct {
		index    int
		response any
	}
	ch := make(chan indexedResult, n)
	for i, call := range calls {
		go func(idx int, c parsedRequest) {
			defer func() {
				if rv := recover(); rv != nil {
					trpcErr := NewError(CodeInternalServerError, "internal server error")
					ch <- indexedResult{
						index:    idx,
						response: formatError(h.opts, trpcErr, c.path, c.input, ctx, ""),
					}
				}
			}()
			resp, _ := h.executeCall(ctx, r, c)
			ch <- indexedResult{index: idx, response: resp}
		}(i, call)
	}

	// Stream chunk lines as results arrive (in completion order).
	for range n {
		res := <-ch
		// [chunkId, FULFILLED(0), [[envelope]]]
		chunk := []any{res.index, 0, []any{[]any{res.response}}}
		chunkData, _ := json.Marshal(chunk)
		_, _ = w.Write(chunkData)
		_, _ = w.Write([]byte("\n"))
		flusher.Flush()
	}
}

func (h *httpHandler) writeErrorResponse(w http.ResponseWriter, err *Error, path string, ctx context.Context, typ ProcedureType) {
	w.Header().Set("Content-Type", "application/json")
	var formatted any
	if ctx != nil {
		formatted = formatError(h.opts, err, path, nil, ctx, typ)
	} else {
		// Pre-context errors (e.g., method not allowed) use default formatting.
		formatted = defaultErrorEnvelope(err, path, h.opts.isDev)
	}
	data, marshalErr := json.Marshal(formatted)
	if marshalErr != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	if ctx != nil {
		ApplyResponseMetadata(ctx, w)
	}
	w.WriteHeader(HTTPStatusFromCode(err.Code))
	_, _ = w.Write(data)
}
