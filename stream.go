package trpcgo

import "context"

// TrackedEvent wraps a value with an ID and optional retry interval for SSE.
// When the client disconnects and reconnects, it sends the last received ID
// back in the input (as lastEventId), allowing the handler to resume from
// where it left off. Retry tells the client how many milliseconds to wait
// before reconnecting (0 means not set).
type TrackedEvent[T any] struct {
	ID    string
	Retry int // milliseconds; 0 means not set
	Data  T
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
	trackRetry() int
	trackData() any
}

func (e TrackedEvent[T]) trackID() string { return e.ID }
func (e TrackedEvent[T]) trackRetry() int { return e.Retry }
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

func makeStreamHandlerWithFinal[I any, O any](fn func(ctx context.Context, input I) (<-chan O, func() any, error)) HandlerFunc {
	return func(ctx context.Context, input any) (any, error) {
		ch, final, err := fn(ctx, input.(I))
		if err != nil {
			return nil, err
		}
		return &sseStream[O]{ch: ch, final: final}, nil
	}
}

func makeVoidStreamHandlerWithFinal[O any](fn func(ctx context.Context) (<-chan O, func() any, error)) HandlerFunc {
	return func(ctx context.Context, _ any) (any, error) {
		ch, final, err := fn(ctx)
		if err != nil {
			return nil, err
		}
		return &sseStream[O]{ch: ch, final: final}, nil
	}
}

// parsable is an internal interface that allows executeCommon to inject output
// validators and parsers into a stream before it is consumed by protocol handlers.
type parsable interface {
	setOutputValidator(func(any) error)
	setOutputParser(func(any) (any, error))
}

// sseStream wraps a typed channel for subscription results.
// Protocol handler packages detect it via [IsStreamResult] and consume it
// via [ConsumeStream].
type sseStream[O any] struct {
	outputValidator func(any) error
	outputParser    func(any) (any, error)
	ch              <-chan O
	final           func() any // optional; called when ch closes to get the done event value
}

func (s *sseStream[O]) setOutputValidator(fn func(any) error)     { s.outputValidator = fn }
func (s *sseStream[O]) setOutputParser(fn func(any) (any, error)) { s.outputParser = fn }

func (s *sseStream[O]) streamConsumer() *StreamConsumer {
	return &StreamConsumer{
		recv: func(ctx context.Context) (any, bool) {
			select {
			case <-ctx.Done():
				return nil, false
			case val, ok := <-s.ch:
				if !ok {
					if s.final != nil {
						return s.final(), false
					}
					return nil, false
				}
				return any(val), true
			}
		},
		outputValidator: s.outputValidator,
		outputParser:    s.outputParser,
	}
}
