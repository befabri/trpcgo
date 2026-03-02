package trpcgo

import (
	"context"
	"net/http"
	"time"
)

const defaultMaxBodySize int64 = 1 << 20 // 1 MB
const defaultMaxBatchSize int = 10

type routerOptions struct {
	allowBatching                 bool
	allowMethodOverride           bool
	isDev                         bool
	strictInput                   bool
	maxBodySize                   int64
	maxBatchSize                  int
	onError                       func(ctx context.Context, err *Error, path string)
	createContext                 func(r *http.Request) context.Context
	errorFormatter                func(ErrorFormatterInput) any
	validator                     func(any) error
	ssePingInterval               time.Duration
	sseMaxDuration                time.Duration
	sseReconnectAfterInactivityMs int
	typeOutput                    string
	zodOutput                     string
	zodMini                       bool
}

// ErrorFormatterInput is passed to a custom error formatter.
// It includes the default error shape so the formatter can extend or replace it.
type ErrorFormatterInput struct {
	Error *Error
	Type  ProcedureType
	Path  string
	Ctx   context.Context
	Shape errorEnvelope // the default tRPC error shape
}

// Option configures a Router.
type Option func(*routerOptions)

// WithBatching enables or disables batch request support.
func WithBatching(enabled bool) Option {
	return func(o *routerOptions) {
		o.allowBatching = enabled
	}
}

// WithMethodOverride allows clients to override HTTP method (send queries as POST).
func WithMethodOverride(enabled bool) Option {
	return func(o *routerOptions) {
		o.allowMethodOverride = enabled
	}
}

// WithOnError sets a callback invoked when a procedure returns an error.
func WithOnError(fn func(ctx context.Context, err *Error, path string)) Option {
	return func(o *routerOptions) {
		o.onError = fn
	}
}

// WithContextCreator sets a function that creates the base context for each request.
func WithContextCreator(fn func(r *http.Request) context.Context) Option {
	return func(o *routerOptions) {
		o.createContext = fn
	}
}

// WithSSEPingInterval sets the keep-alive ping interval for SSE subscriptions.
// Default is 10 seconds.
func WithSSEPingInterval(d time.Duration) Option {
	return func(o *routerOptions) {
		o.ssePingInterval = d
	}
}

// WithSSEMaxDuration sets the maximum duration for SSE subscriptions.
// After this duration the server sends a "return" event and closes the
// connection; the tRPC client will automatically reconnect.
// Default is 0 (unlimited). In production, set a finite duration or
// use external connection limits to prevent resource exhaustion from
// clients holding connections open indefinitely.
func WithSSEMaxDuration(d time.Duration) Option {
	return func(o *routerOptions) {
		o.sseMaxDuration = d
	}
}

// WithSSEReconnectAfterInactivity tells the client to reconnect after
// the given duration of inactivity. This is sent in the SSE connected
// event as reconnectAfterInactivityMs, matching tRPC's protocol.
// Default is 0 (disabled).
func WithSSEReconnectAfterInactivity(d time.Duration) Option {
	return func(o *routerOptions) {
		o.sseReconnectAfterInactivityMs = int(d.Milliseconds())
	}
}

// WithDev enables development mode. When true, error responses include
// Go stack traces in the data.stack field, matching tRPC's isDev behavior.
func WithDev(enabled bool) Option {
	return func(o *routerOptions) {
		o.isDev = enabled
	}
}

// WithMaxBodySize sets the maximum allowed request body size in bytes.
// Default is 1 MB. Set to -1 for no limit. Passing 0 keeps the default.
func WithMaxBodySize(n int64) Option {
	return func(o *routerOptions) {
		switch {
		case n > 0:
			o.maxBodySize = n
		case n < 0:
			o.maxBodySize = 0 // internal 0 = unlimited (readBody skips MaxBytesReader)
		}
		// n == 0: no-op, keep default
	}
}

// WithMaxBatchSize sets the maximum number of procedures allowed in a single
// batch request. Default is 10. Set to -1 for no limit. Passing 0 keeps the default.
func WithMaxBatchSize(n int) Option {
	return func(o *routerOptions) {
		switch {
		case n > 0:
			o.maxBatchSize = n
		case n < 0:
			o.maxBatchSize = 0 // internal 0 = unlimited (batch check skipped)
		}
		// n == 0: no-op, keep default
	}
}

// WithStrictInput enables strict JSON input parsing. When true, procedure
// inputs that contain unknown fields (fields not present in the input struct)
// are rejected with a BAD_REQUEST error. This uses json.Decoder's
// DisallowUnknownFields under the hood.
//
// By default, Go's json.Unmarshal silently ignores unknown fields.
func WithStrictInput(enabled bool) Option {
	return func(o *routerOptions) {
		o.strictInput = enabled
	}
}

// WithErrorFormatter sets a custom error formatter that transforms error
// responses. The function receives the default error shape and can return
// a modified or entirely different shape. This matches tRPC's errorFormatter.
func WithErrorFormatter(fn func(ErrorFormatterInput) any) Option {
	return func(o *routerOptions) {
		o.errorFormatter = fn
	}
}

// WithValidator sets a function that validates procedure inputs.
// The function is called with the deserialized input struct after JSON
// unmarshaling. Only struct-typed inputs are validated; primitives are skipped.
//
// This matches go-playground/validator directly — pass validate.V.Struct:
//
//	router := trpcgo.NewRouter(trpcgo.WithValidator(validate.V.Struct))
func WithValidator(fn func(any) error) Option {
	return func(o *routerOptions) {
		o.validator = fn
	}
}

// WithTypeOutput enables automatic TypeScript type generation.
// When set, calling Router.Handler() writes the TypeScript AppRouter
// type file to the given path. Use with the top-level registration
// functions (Query, Mutation, Subscribe, etc.) to capture type info.
func WithTypeOutput(path string) Option {
	return func(o *routerOptions) {
		o.typeOutput = path
	}
}

// WithZodOutput enables automatic Zod schema generation alongside
// TypeScript types. Requires WithTypeOutput to be set. The file watcher
// regenerates both files when Go source changes are detected.
func WithZodOutput(path string) Option {
	return func(o *routerOptions) {
		o.zodOutput = path
	}
}

// WithZodMini switches Zod schema output to zod/mini functional syntax.
// Only has effect when WithZodOutput is also set.
func WithZodMini(enabled bool) Option {
	return func(o *routerOptions) {
		o.zodMini = enabled
	}
}
