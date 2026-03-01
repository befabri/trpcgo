package trpcgo

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
)

type httpHandler struct {
	router   *Router
	basePath string
}

type callResult struct {
	response any
	status   int
}

func (h *httpHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Only allow GET and POST
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		writeErrorResponse(w, NewError(CodeMethodNotSupported, "only GET and POST are supported"), "", http.StatusMethodNotAllowed)
		return
	}

	isBatch := isBatchRequest(r)

	// Check if batching is allowed
	if isBatch && !h.router.opts.allowBatching {
		writeErrorResponse(w, NewError(CodeBadRequest, "batching is not enabled"), "", http.StatusBadRequest)
		return
	}

	// Subscriptions cannot be batched (tRPC spec).
	if isBatch {
		paths := parsePaths(r, h.basePath)
		for _, path := range paths {
			if proc, ok := h.router.lookup(path); ok && proc.typ == ProcedureSubscription {
				writeErrorResponse(w, NewError(CodeBadRequest, "subscriptions cannot be batched"), "", http.StatusBadRequest)
				return
			}
		}
	}

	// Parse all procedure calls from the request
	calls, err := parseRequest(r, h.basePath, isBatch, h.router.opts.maxBodySize)
	if err != nil {
		if trpcErr, ok := errors.AsType[*Error](err); ok {
			writeErrorResponse(w, trpcErr, "", HTTPStatusFromCode(trpcErr.Code))
		} else {
			writeErrorResponse(w, NewError(CodeInternalServerError, "internal server error"), "", http.StatusInternalServerError)
		}
		return
	}

	// Create context
	ctx := r.Context()
	if h.router.opts.createContext != nil {
		ctx = h.router.opts.createContext(r)
	}

	// Execute all procedure calls
	results := make([]callResult, len(calls))

	for i, call := range calls {
		resp, status := h.executeCall(ctx, r, call)
		results[i] = callResult{response: resp, status: status}
	}

	// Check if single result is a streamer (SSE subscription).
	if !isBatch {
		if s, ok := results[0].response.(streamer); ok {
			if err := s.writeSSE(ctx, w, sseOptions{
				pingInterval:              h.router.opts.ssePingInterval,
				maxDuration:               h.router.opts.sseMaxDuration,
				reconnectAfterInactivityMs: h.router.opts.sseReconnectAfterInactivityMs,
			}); err != nil {
				writeErrorResponse(w, NewError(CodeInternalServerError, err.Error()), "", http.StatusInternalServerError)
			}
			return
		}
	}

	// Write response
	w.Header().Set("Content-Type", "application/json")

	if !isBatch {
		data, err := json.Marshal(results[0].response)
		if err != nil {
			writeErrorResponse(w, NewError(CodeInternalServerError, "failed to serialize response"), "", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(results[0].status)
		w.Write(data)
		return
	}

	// Batch response: determine status code
	responses := make([]any, len(results))
	for i, res := range results {
		responses[i] = res.response
	}
	data, err := json.Marshal(responses)
	if err != nil {
		writeErrorResponse(w, NewError(CodeInternalServerError, "failed to serialize response"), "", http.StatusInternalServerError)
		return
	}
	statusCode := determineBatchStatus(results)
	w.WriteHeader(statusCode)
	w.Write(data)
}

func (h *httpHandler) executeCall(ctx context.Context, r *http.Request, call parsedRequest) (any, int) {
	proc, ok := h.router.lookup(call.path)
	if !ok {
		trpcErr := NewError(CodeNotFound, "procedure not found")
		if h.router.opts.onError != nil {
			h.router.opts.onError(ctx, trpcErr, call.path)
		}
		return newErrorEnvelope(trpcErr, call.path), HTTPStatusFromCode(CodeNotFound)
	}

	// Validate HTTP method matches procedure type
	if err := validateMethod(r.Method, proc.typ, h.router.opts.allowMethodOverride); err != nil {
		if h.router.opts.onError != nil {
			h.router.opts.onError(ctx, err, call.path)
		}
		return newErrorEnvelope(err, call.path), HTTPStatusFromCode(err.Code)
	}

	// Execute with pre-computed middleware chain.
	result, err := proc.wrappedHandler(ctx, call.input)
	if err != nil {
		trpcErr, ok := errors.AsType[*Error](err)
		if h.router.opts.onError != nil {
			// Pass the original error so the server can log it.
			callbackErr := trpcErr
			if !ok {
				callbackErr = WrapError(CodeInternalServerError, "internal server error", err)
			}
			h.router.opts.onError(ctx, callbackErr, call.path)
		}
		if !ok {
			// Never leak internal error details to clients.
			trpcErr = NewError(CodeInternalServerError, "internal server error")
		}
		return newErrorEnvelope(trpcErr, call.path), HTTPStatusFromCode(trpcErr.Code)
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

func writeErrorResponse(w http.ResponseWriter, err *Error, path string, status int) {
	w.Header().Set("Content-Type", "application/json")
	data, marshalErr := json.Marshal(newErrorEnvelope(err, path))
	if marshalErr != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.WriteHeader(status)
	w.Write(data)
}
