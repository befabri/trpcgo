package trpcgo

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// TrackedEvent wraps a value with an ID for SSE reconnection support.
// When the client disconnects and reconnects, it sends the last received ID
// back in the input (as lastEventId), allowing the handler to resume from
// where it left off.
type TrackedEvent[T any] struct {
	ID   string
	Data T
}

// Tracked creates a TrackedEvent that associates an ID with data.
// The ID is sent as the SSE id field, enabling client reconnection.
func Tracked[T any](id string, data T) TrackedEvent[T] {
	return TrackedEvent[T]{ID: id, Data: data}
}

// tracked is the interface used at runtime to detect TrackedEvent values
// regardless of their type parameter.
type tracked interface {
	trackID() string
	trackData() any
}

func (e TrackedEvent[T]) trackID() string { return e.ID }
func (e TrackedEvent[T]) trackData() any  { return e.Data }

func makeStreamHandler[I any, O any](fn func(ctx context.Context, input I) (<-chan O, error)) HandlerFunc {
	return func(ctx context.Context, input any) (any, error) {
		ch, err := fn(ctx, input.(I))
		if err != nil {
			return nil, err
		}
		return &sseStream[O]{ch: ch}, nil
	}
}

func makeVoidStreamHandler[O any](fn func(ctx context.Context) (<-chan O, error)) HandlerFunc {
	return func(ctx context.Context, _ any) (any, error) {
		ch, err := fn(ctx)
		if err != nil {
			return nil, err
		}
		return &sseStream[O]{ch: ch}, nil
	}
}

// parsable is an internal interface that allows executeProcedure to inject output
// validators and parsers into a stream without changing the streamer interface or
// sseOptions.
type parsable interface {
	setOutputValidator(func(any) error)
	setOutputParser(func(any) (any, error))
}

// sseStream wraps a typed channel for the handler to detect and stream.
type sseStream[O any] struct {
	outputValidator func(any) error
	outputParser    func(any) (any, error)
	ch              <-chan O
}

func (s *sseStream[O]) setOutputValidator(fn func(any) error)     { s.outputValidator = fn }
func (s *sseStream[O]) setOutputParser(fn func(any) (any, error)) { s.outputParser = fn }

// streamer is the interface the handler checks to detect subscription results.
type streamer interface {
	writeSSE(ctx context.Context, w http.ResponseWriter, opts sseOptions) error
}

type sseOptions struct {
	pingInterval               time.Duration
	maxDuration                time.Duration
	reconnectAfterInactivityMs int
	isDev                      bool
	formatError                func(*Error) any
	onError                    func(*Error)
}

func formatSSEError(opts sseOptions, err *Error) any {
	publicErr := sanitizeErrorForClient(err)
	if opts.formatError != nil {
		return opts.formatError(publicErr)
	}
	return defaultErrorEnvelope(publicErr, "", opts.isDev)
}

func (s *sseStream[O]) writeSSE(ctx context.Context, w http.ResponseWriter, opts sseOptions) error {
	flusher, ok := w.(http.Flusher)
	if !ok {
		return NewError(CodeInternalServerError, "streaming not supported")
	}

	// Set SSE headers.
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("X-Accel-Buffering", "no")
	w.Header().Set("Connection", "keep-alive")
	applyResponseMetadata(ctx, w)
	w.WriteHeader(http.StatusOK)

	// Send connected event with client options.
	connData, _ := json.Marshal(sseClientOptions{
		ReconnectAfterInactivityMs: opts.reconnectAfterInactivityMs,
	})
	writeSSENamedEvent(w, "connected", connData)
	flusher.Flush()

	pingInterval := opts.pingInterval
	if pingInterval == 0 {
		pingInterval = 10 * time.Second
	}
	pingTicker := time.NewTicker(pingInterval)
	defer pingTicker.Stop()

	// Max duration timer.
	var maxTimer <-chan time.Time
	if opts.maxDuration > 0 {
		t := time.NewTimer(opts.maxDuration)
		defer t.Stop()
		maxTimer = t.C
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-maxTimer:
			writeSSENamedEvent(w, "return", nil)
			flusher.Flush()
			return nil
		case <-pingTicker.C:
			writeSSENamedEvent(w, "ping", nil)
			flusher.Flush()
		case val, ok := <-s.ch:
			if !ok {
				// Channel closed — stream complete.
				writeSSENamedEvent(w, "return", nil)
				flusher.Flush()
				return nil
			}

			// Run output hooks on the raw item (type O) before TrackedEvent
			// unwrapping. Validators run before parsers; parsers may transform the
			// value and the returned item is used downstream.
			item := any(val)
			if s.outputValidator != nil || s.outputParser != nil {
				var perr error
				item, perr = applyOutputHooks(item, s.outputValidator, s.outputParser)
				if perr != nil {
					sseErr := WrapError(CodeInternalServerError, "internal server error", perr)
					if opts.onError != nil {
						opts.onError(sseErr)
					}
					formatted := formatSSEError(opts, sseErr)
					errData, _ := json.Marshal(formatted)
					writeSSENamedEvent(w, "serialized-error", errData)
					flusher.Flush()
					return nil
				}
			}

			// Unwrap TrackedEvent from the (possibly transformed) item for serialization.
			var data []byte
			var id string
			var err error
			if te, ok := item.(tracked); ok {
				data, err = json.Marshal(te.trackData())
				id = te.trackID()
			} else {
				data, err = json.Marshal(item)
			}

			if err != nil {
				sseErr := NewError(CodeInternalServerError, "failed to serialize subscription data")
				if opts.onError != nil {
					opts.onError(sseErr)
				}
				formatted := formatSSEError(opts, sseErr)
				errData, _ := json.Marshal(formatted)
				writeSSENamedEvent(w, "serialized-error", errData)
				flusher.Flush()
				return nil
			}
			writeSSEData(w, data, id)
			flusher.Flush()
		}
	}
}

// sseClientOptions is sent in the connected event data, matching tRPC's SSEClientOptions.
type sseClientOptions struct {
	ReconnectAfterInactivityMs int `json:"reconnectAfterInactivityMs,omitempty"`
}

// writeSSENamedEvent writes an SSE event with an explicit event type
// (connected, ping, return, serialized-error).
func writeSSENamedEvent(w http.ResponseWriter, event string, data []byte) {
	_, _ = fmt.Fprintf(w, "event: %s\n", event)
	if len(data) > 0 {
		_, _ = fmt.Fprintf(w, "data: %s\n", data)
	} else {
		_, _ = fmt.Fprint(w, "data: \n")
	}
	_, _ = fmt.Fprint(w, "\n")
}

// writeSSEData writes a data-only SSE message (no event type field).
// This matches tRPC's wire format where data messages use the default
// "message" event type. If id is non-empty, an id field is included
// for tracked event reconnection support.
func writeSSEData(w http.ResponseWriter, data []byte, id string) {
	_, _ = fmt.Fprintf(w, "data: %s\n", data)
	if id != "" {
		// Sanitize newlines to prevent SSE field injection. An id containing
		// \n or \r could inject arbitrary SSE fields (data:, event:, etc.).
		id = strings.NewReplacer("\n", "", "\r", "").Replace(id)
		_, _ = fmt.Fprintf(w, "id: %s\n", id)
	}
	_, _ = fmt.Fprint(w, "\n")
}
