package trpcgo

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// wrapStreamHandler adapts a function that returns a receive channel into an HandlerFunc.
// Used for subscription procedures that stream data via SSE.
func wrapStreamHandler[I any, O any](fn func(ctx context.Context, input I) (<-chan O, error)) HandlerFunc {
	return func(ctx context.Context, rawInput json.RawMessage) (any, error) {
		var input I
		if len(rawInput) > 0 {
			if err := json.Unmarshal(rawInput, &input); err != nil {
				return nil, NewError(CodeParseError, "failed to parse input")
			}
		}
		ch, err := fn(ctx, input)
		if err != nil {
			return nil, err
		}
		return &sseStream[O]{ch: ch}, nil
	}
}

// wrapVoidStreamHandler adapts a function with no input that returns a receive channel.
func wrapVoidStreamHandler[O any](fn func(ctx context.Context) (<-chan O, error)) HandlerFunc {
	return func(ctx context.Context, _ json.RawMessage) (any, error) {
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
	pingInterval time.Duration
	maxDuration  time.Duration
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
	w.WriteHeader(http.StatusOK)

	// Send connected event.
	writeSSEEvent(w, "connected", []byte("{}"))
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
			writeSSEEvent(w, "return", nil)
			flusher.Flush()
			return nil
		case <-pingTicker.C:
			writeSSEEvent(w, "ping", nil)
			flusher.Flush()
		case val, ok := <-s.ch:
			if !ok {
				// Channel closed — stream complete.
				writeSSEEvent(w, "return", nil)
				flusher.Flush()
				return nil
			}
			data, err := json.Marshal(val)
			if err != nil {
				errData, _ := json.Marshal(map[string]any{
					"code":    NameFromCode(CodeInternalServerError),
					"message": "failed to serialize subscription data",
				})
				writeSSEEvent(w, "serialized-error", errData)
				flusher.Flush()
				return nil
			}
			writeSSEEvent(w, "message", data)
			flusher.Flush()
		}
	}
}

func writeSSEEvent(w http.ResponseWriter, event string, data []byte) {
	fmt.Fprintf(w, "event: %s\n", event)
	if len(data) > 0 {
		fmt.Fprintf(w, "data: %s\n", data)
	} else {
		fmt.Fprint(w, "data: \n")
	}
	fmt.Fprint(w, "\n")
}
