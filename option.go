package trpcgo

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/befabri/trpcgo/zodconfig"
)

const defaultMaxBatchSize int = 10 //

func defaultMaxBodySize() int64 { return 1 << 20 } // 1 MB

func defaultSSEMaxDuration() time.Duration { return 30 * time.Minute } // 30 minutes

type routerOptions struct {
	allowBatching                 bool
	allowMethodOverride           bool
	isDev                         bool
	strictInput                   bool
	maxBodySize                   int64
	maxBatchSize                  int
	onError                       func(ctx context.Context, err *Error, path string)
	createContext                 func(ctx context.Context, r *http.Request) context.Context
	errorFormatter                func(ErrorFormatterInput) any
	validator                     func(any) error
	ssePingInterval               time.Duration
	sseMaxDuration                time.Duration
	sseMaxConnections             int
	sseReconnectAfterInactivityMs int
	typeOutput                    string
	zodOutput                     string
	enumsOutput                   string
	zodMini                       bool
	zodValidation                 zodconfig.Config
	watchPackages                 []string
}

// ErrorFormatterInput is passed to a custom error formatter.
// It includes the default error shape so the formatter can extend or replace it.
//
// Security: Ctx carries the full request context, which may contain auth tokens
// or other sensitive values. Avoid including context values in formatted error responses.
type ErrorFormatterInput struct {
	Error *Error
	Type  ProcedureType
	Path  string
	Input json.RawMessage // raw JSON input; nil for pre-execution errors
	Ctx   context.Context
	Shape ErrorEnvelope // the default tRPC error shape
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
// The ctx argument is the request's existing context (r.Context()), so values and
// cancellation propagate automatically when the returned context is derived from it.
func WithContextCreator(fn func(ctx context.Context, r *http.Request) context.Context) Option {
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
// connection. httpSubscriptionLink treats this as completion; start a new
// subscription to continue listening.
// Default is 30 minutes. Set to -1 for unlimited. Passing 0 keeps the default.
func WithSSEMaxDuration(d time.Duration) Option {
	return func(o *routerOptions) {
		switch {
		case d > 0:
			o.sseMaxDuration = d
		case d < 0:
			o.sseMaxDuration = 0 // internal 0 = unlimited (no timer created)
		}
		// d == 0: no-op, keep default
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

// WithSSEMaxConnections sets the maximum number of concurrent SSE subscriptions.
// When the limit is reached, new subscription requests are rejected with
// a TOO_MANY_REQUESTS (429) error. Default is 0 (unlimited).
// Set to -1 to explicitly disable the limit. Passing 0 keeps the default.
func WithSSEMaxConnections(n int) Option {
	return func(o *routerOptions) {
		switch {
		case n > 0:
			o.sseMaxConnections = n
		case n < 0:
			o.sseMaxConnections = 0 // unlimited
		}
		// n == 0: no-op, keep default
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

// WithStrictInput configures strict JSON input parsing. When true, typed
// procedure inputs that contain unknown fields or trailing JSON tokens are
// rejected. Unknown fields return BAD_REQUEST; malformed JSON and trailing
// tokens return PARSE_ERROR. This uses json.Decoder's DisallowUnknownFields
// under the hood and is enabled by default.
//
// Keys that tRPC clients add on their own are never rejected: "direction",
// which infinite queries send to a query whose input declares "cursor", and
// "lastEventId", which a reconnecting subscription sends. When the input
// struct does not declare them, they are dropped before decoding.
func WithStrictInput(enabled bool) Option {
	return func(o *routerOptions) {
		o.strictInput = enabled
	}
}

// WithErrorFormatter sets a custom error formatter that transforms error
// responses. The function receives the default error shape and can return
// a modified or entirely different shape. This matches tRPC's errorFormatter.
//
// For SSE, an ErrorEnvelope or *ErrorEnvelope result is unwrapped to its Error
// field; any other value is sent as-is and needs numeric code, message, and
// data fields for tRPC clients.
func WithErrorFormatter(fn func(ErrorFormatterInput) any) Option {
	return func(o *routerOptions) {
		o.errorFormatter = fn
	}
}

// WithValidator sets a function that validates procedure inputs.
// The function is called once with every typed input after JSON decoding,
// including scalar, map, slice and typed nil inputs. Void procedures skip it.
//
// A validator that accepts only structs, such as validate.Struct, rejects a
// slice or scalar root. Wrap it with [StructValidator] so every struct in the
// input is validated and other roots are left alone:
//
//	router := trpcgo.NewRouter(trpcgo.WithValidator(trpcgo.StructValidator(validate.Struct)))
//
// Supply a plain callback when root scalars or collections need rules of
// their own, for example through validate.Var.
func WithValidator(fn func(any) error) Option {
	return func(o *routerOptions) {
		o.validator = fn
	}
}

// WithTypeOutput enables automatic TypeScript type generation.
// The path specifies where the TypeScript AppRouter type file is written.
// Use with the top-level registration functions (Query, Mutation,
// Subscribe, etc.) to capture type info.
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

// WithZodValidation declares client counterparts for configured Go validation.
// It does not register or execute server validators. The same configuration can
// be supplied to source generation using the CLI's --zod-config JSON file.
// Configuration is copied so later caller mutations cannot affect generation.
func WithZodValidation(config zodconfig.Config) Option {
	config = config.Clone()
	return func(o *routerOptions) {
		o.zodValidation = config.Clone()
	}
}

// WithEnumsOutput writes runtime `as const` objects for named string enums in
// dev mode. Requires WithTypeOutput and static analysis.
func WithEnumsOutput(path string) Option {
	return func(o *routerOptions) {
		o.enumsOutput = path
	}
}

// WithWatchPackages restricts dev watcher + static regeneration to the given
// Go package patterns (go/packages syntax, e.g. "./cmd/api", "./internal/...").
//
// When unset, the watcher auto-detects Go directories under the working
// directory. This option is only used by the dev watcher path
// (WithDev + WithTypeOutput).
func WithWatchPackages(patterns ...string) Option {
	return func(o *routerOptions) {
		out := make([]string, 0, len(patterns))
		for _, p := range patterns {
			if p == "" {
				continue
			}
			out = append(out, p)
		}
		if len(out) > 0 {
			o.watchPackages = out
		}
	}
}
