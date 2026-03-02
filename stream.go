package trpcgo

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
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

// sseStream wraps a typed channel for the handler to detect and stream.
type sseStream[O any] struct {
	ch <-chan O
}

// streamer is the interface the handler checks to detect subscription results.
type streamer interface {
	writeSSE(ctx context.Context, w http.ResponseWriter, opts sseOptions) error
}

type sseOptions struct {
	pingInterval              time.Duration
	maxDuration               time.Duration
	reconnectAfterInactivityMs int
	isDev                      bool
	formatError                func(*Error) any
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

			// Check if the value is a TrackedEvent.
			var data []byte
			var id string
			var err error
			if te, ok := any(val).(tracked); ok {
				data, err = json.Marshal(te.trackData())
				id = te.trackID()
			} else {
				data, err = json.Marshal(val)
			}

			if err != nil {
				sseErr := NewError(CodeInternalServerError, "failed to serialize subscription data")
				var formatted any
				if opts.formatError != nil {
					formatted = opts.formatError(sseErr)
				} else {
					formatted = defaultErrorEnvelope(sseErr, "", opts.isDev)
				}
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
	fmt.Fprintf(w, "event: %s\n", event)
	if len(data) > 0 {
		fmt.Fprintf(w, "data: %s\n", data)
	} else {
		fmt.Fprint(w, "data: \n")
	}
	fmt.Fprint(w, "\n")
}

// writeSSEData writes a data-only SSE message (no event type field).
// This matches tRPC's wire format where data messages use the default
// "message" event type. If id is non-empty, an id field is included
// for tracked event reconnection support.
func writeSSEData(w http.ResponseWriter, data []byte, id string) {
	fmt.Fprintf(w, "data: %s\n", data)
	if id != "" {
		fmt.Fprintf(w, "id: %s\n", id)
	}
	fmt.Fprint(w, "\n")
}
