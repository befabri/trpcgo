package trpcgo

import (
	"context"
	"net/http"
	"time"
)

const defaultMaxBodySize int64 = 1 << 20 // 1 MB

type routerOptions struct {
	allowBatching                  bool
	allowMethodOverride            bool
	maxBodySize                    int64
	onError                        func(ctx context.Context, err *Error, path string)
	createContext                  func(r *http.Request) context.Context
	ssePingInterval                time.Duration
	sseMaxDuration                 time.Duration
	sseReconnectAfterInactivityMs  int
	typeOutput                     string
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
// Default is 0 (unlimited).
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

// WithMaxBodySize sets the maximum allowed request body size in bytes.
// Default is 1 MB. Set to 0 for no limit.
func WithMaxBodySize(n int64) Option {
	return func(o *routerOptions) {
		o.maxBodySize = n
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
