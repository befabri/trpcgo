package trpcgo

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"iter"
	"net/http"
	"reflect"
	"time"
)

// ProcedureEntry is a read-only handle to a registered procedure with
// a pre-computed middleware chain. Obtained from [ProcedureMap].
// Safe for concurrent use.
type ProcedureEntry struct {
	typ             ProcedureType
	meta            any
	inputType       reflect.Type
	outputType      reflect.Type
	handler         HandlerFunc
	outputValidator func(any) error
	outputParser    func(any) (any, error)
}

// Type returns the procedure type (query, mutation, subscription).
func (e *ProcedureEntry) Type() ProcedureType { return e.typ }

// Meta returns the user-defined metadata attached via [WithMeta].
func (e *ProcedureEntry) Meta() any { return e.meta }

// InputType returns the Go input type, or nil for void procedures.
func (e *ProcedureEntry) InputType() reflect.Type { return e.inputType }

// OutputType returns the Go output type.
func (e *ProcedureEntry) OutputType() reflect.Type { return e.outputType }

// ProcedureMap is a frozen snapshot of registered procedures with
// pre-computed middleware chains. Safe for concurrent use without locking.
//
// Protocol handler packages use this to build HTTP handlers that serve
// procedures over different wire formats (tRPC, oRPC, etc.).
type ProcedureMap struct {
	entries map[string]*ProcedureEntry
}

// Lookup returns the procedure entry for the given path.
func (pm *ProcedureMap) Lookup(path string) (*ProcedureEntry, bool) {
	e, ok := pm.entries[path]
	return e, ok
}

// All returns an iterator over all registered procedures.
// Iteration order is not guaranteed.
func (pm *ProcedureMap) All() iter.Seq2[string, *ProcedureEntry] {
	return func(yield func(string, *ProcedureEntry) bool) {
		for path, entry := range pm.entries {
			if !yield(path, entry) {
				return
			}
		}
	}
}

// Len returns the number of registered procedures.
func (pm *ProcedureMap) Len() int {
	return len(pm.entries)
}

// BuildProcedureMap creates a frozen snapshot of all registered procedures
// with pre-computed middleware chains. Protocol handler packages call this
// once during handler construction.
//
// The snapshot is independent of the Router — subsequent registrations or
// middleware additions do not affect it.
func (r *Router) BuildProcedureMap() *ProcedureMap {
	r.mu.RLock()
	defer r.mu.RUnlock()

	entries := make(map[string]*ProcedureEntry, len(r.procedures))
	for path, proc := range r.procedures {
		entries[path] = &ProcedureEntry{
			typ:             proc.typ,
			meta:            proc.meta,
			inputType:       proc.inputType,
			outputType:      proc.outputType,
			handler:         applyMiddleware(proc.handler, r.middleware, proc.middleware),
			outputValidator: proc.outputValidator,
			outputParser:    proc.outputParser,
		}
	}
	return &ProcedureMap{entries: entries}
}

// ExecuteEntry decodes raw JSON input, validates it, and runs the procedure's
// handler chain. This is the protocol-agnostic execution path for use by
// handler packages that implement their own HTTP wire format.
//
// The returned result may be a stream (for subscription procedures).
// Use [IsStreamResult] to check and [ConsumeStream] to read items.
func (r *Router) ExecuteEntry(ctx context.Context, entry *ProcedureEntry, raw json.RawMessage) (any, error) {
	return r.executeCommon(ctx, entry.handler, entry.inputType, raw, entry.outputValidator, entry.outputParser)
}

// executeCommon is the shared execution path for both the internal
// executeProcedure (used by RawCall) and the exported ExecuteEntry
// (used by protocol handler packages).
func (r *Router) executeCommon(ctx context.Context, handler HandlerFunc, inputType reflect.Type, raw json.RawMessage, outputValidator func(any) error, outputParser func(any) (any, error)) (any, error) {
	// Decode the raw input into the registered type.
	var input any
	if inputType != nil {
		ptr := reflect.New(inputType)
		if len(raw) > 0 {
			if r.opts.strictInput {
				dec := json.NewDecoder(bytes.NewReader(raw))
				dec.DisallowUnknownFields()
				if err := dec.Decode(ptr.Interface()); err != nil {
					var syntaxErr *json.SyntaxError
					var typeErr *json.UnmarshalTypeError
					switch {
					case errors.As(err, &syntaxErr):
						return nil, NewError(CodeParseError, "failed to parse input")
					case errors.As(err, &typeErr):
						return nil, NewError(CodeBadRequest, "invalid input type")
					default:
						return nil, NewError(CodeBadRequest, "unknown field in input")
					}
				}
			} else if err := json.Unmarshal(raw, ptr.Interface()); err != nil {
				return nil, NewError(CodeParseError, "failed to parse input")
			}
		}
		input = ptr.Elem().Interface()
	}

	// Validate the decoded struct.
	if r.opts.validator != nil && inputType != nil && input != nil {
		t := inputType
		for t.Kind() == reflect.Pointer {
			t = t.Elem()
		}
		if t.Kind() == reflect.Struct {
			if err := r.opts.validator(input); err != nil {
				return nil, WrapError(CodeBadRequest, "input validation failed", err)
			}
		}
	}

	output, err := handler(ctx, input)
	if err != nil {
		return nil, err
	}

	// Run output hooks. For subscriptions the validator/parser are
	// injected into the sseStream and run per-item inside writeSSE or
	// StreamConsumer.Recv. For queries and mutations they run here.
	if outputValidator != nil || outputParser != nil {
		if p, ok := output.(parsable); ok {
			p.setOutputValidator(outputValidator)
			p.setOutputParser(outputParser)
		} else {
			output, err = applyOutputHooks(output, outputValidator, outputParser)
			if err != nil {
				return nil, err
			}
		}
	}

	return output, nil
}

// IsStreamResult reports whether a procedure result is a subscription stream.
// Protocol handler packages use this to detect streams and switch to SSE handling.
func IsStreamResult(result any) bool {
	_, ok := result.(streamer)
	return ok
}

// StreamConsumer provides protocol-agnostic access to a subscription stream.
// Protocol handlers use it to read items and write SSE events in their own
// wire format. Create one via [ConsumeStream].
type StreamConsumer struct {
	recv            func(ctx context.Context) (any, bool)
	outputValidator func(any) error
	outputParser    func(any) (any, error)
}

// ConsumeStream extracts a [StreamConsumer] from a streaming procedure result.
// Returns nil if the result is not a stream. The consumer reads items from the
// underlying channel with output validation/parsing applied.
//
// After calling ConsumeStream, do not also call the tRPC-specific writeSSE
// method on the same result — the stream should be consumed by exactly one reader.
func ConsumeStream(result any) *StreamConsumer {
	if sc, ok := result.(streamConsumable); ok {
		return sc.streamConsumer()
	}
	return nil
}

// Recv returns the next item from the stream. It blocks until an item is
// available, the stream is closed, or the context is cancelled.
//
// On success, data is the payload (with TrackedEvent unwrapped) and id is
// the event ID (empty if not tracked). Returns io.EOF when the stream ends.
// Returns a non-nil, non-EOF error if output validation/parsing fails.
func (sc *StreamConsumer) Recv(ctx context.Context) (data any, id string, err error) {
	item, ok := sc.recv(ctx)
	if !ok {
		return nil, "", io.EOF
	}

	// Apply output hooks.
	if sc.outputValidator != nil || sc.outputParser != nil {
		processed, hookErr := applyOutputHooks(item, sc.outputValidator, sc.outputParser)
		if hookErr != nil {
			return nil, "", hookErr
		}
		item = processed
	}

	// Extract TrackedEvent ID if present.
	if te, isTracked := item.(tracked); isTracked {
		return te.trackData(), te.trackID(), nil
	}

	return item, "", nil
}

// streamConsumable is implemented by streaming results to provide a
// StreamConsumer for protocol handler packages.
type streamConsumable interface {
	streamConsumer() *StreamConsumer
}

// --- Router config accessors for protocol handler packages ---

// IsDev reports whether the router is in development mode.
func (r *Router) IsDev() bool { return r.opts.isDev }

// MaxBodySize returns the configured maximum request body size in bytes.
// Returns 0 for unlimited.
func (r *Router) MaxBodySize() int64 { return r.opts.maxBodySize }

// SSEPingInterval returns the configured SSE keep-alive ping interval.
func (r *Router) SSEPingInterval() time.Duration { return r.opts.ssePingInterval }

// SSEMaxDuration returns the configured SSE maximum duration.
// Returns 0 for unlimited.
func (r *Router) SSEMaxDuration() time.Duration { return r.opts.sseMaxDuration }

// MaxSSEConnections returns the configured SSE connection limit.
// Returns 0 for unlimited.
func (r *Router) MaxSSEConnections() int { return r.opts.sseMaxConnections }

// SSEReconnectAfterInactivityMs returns the configured reconnect-after-inactivity
// value in milliseconds sent to SSE clients.
func (r *Router) SSEReconnectAfterInactivityMs() int { return r.opts.sseReconnectAfterInactivityMs }

// TrackSSEConnection atomically adjusts the SSE connection count by delta
// and returns the new count. Protocol handlers use this to enforce connection limits.
func (r *Router) TrackSSEConnection(delta int64) int64 {
	return r.sseConnections.Add(delta)
}

// ContextCreator returns the user-supplied context creator function, or nil.
func (r *Router) ContextCreator() func(context.Context, *http.Request) context.Context {
	return r.opts.createContext
}

// ErrorCallback returns the user-supplied error callback, or nil.
func (r *Router) ErrorCallback() func(context.Context, *Error, string) {
	return r.opts.onError
}

// --- Exported helpers for protocol handler packages ---

// WithProcedureMeta injects procedure metadata into the context.
// Protocol handler packages call this before executing a procedure
// so middleware can access metadata via [GetProcedureMeta].
func WithProcedureMeta(ctx context.Context, pm ProcedureMeta) context.Context {
	return context.WithValue(ctx, ctxKeyProcedureMeta, pm)
}

// ApplyResponseMetadata writes accumulated cookies and headers from
// [SetCookie] and [SetResponseHeader] calls to the ResponseWriter.
// Must be called before WriteHeader.
func ApplyResponseMetadata(ctx context.Context, w http.ResponseWriter) {
	rm := getResponseMetadata(ctx)
	if rm == nil {
		return
	}
	rm.mu.Lock()
	defer rm.mu.Unlock()
	for key, values := range rm.headers {
		for _, v := range values {
			w.Header().Add(key, v)
		}
	}
	for _, c := range rm.cookies {
		http.SetCookie(w, c)
	}
}

// SanitizeError converts an arbitrary error to a client-safe [*Error].
// If the error is already a [*Error], internal details are stripped.
// Non-tRPC errors are replaced with a generic INTERNAL_SERVER_ERROR.
func SanitizeError(err error) *Error {
	if err == nil {
		return nil
	}
	if trpcErr, ok := errors.AsType[*Error](err); ok {
		return sanitizeErrorForClient(trpcErr)
	}
	return NewError(CodeInternalServerError, "internal server error")
}
