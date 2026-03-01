package trpcgo

import (
	"context"
	"reflect"
)

// ProcedureType distinguishes queries, mutations, and subscriptions.
type ProcedureType string

const (
	ProcedureQuery        ProcedureType = "query"
	ProcedureMutation     ProcedureType = "mutation"
	ProcedureSubscription ProcedureType = "subscription"
)

// HandlerFunc is the procedure handler signature. The input parameter is
// the already-decoded struct (or nil for void procedures). Middleware receives
// the same decoded input — no json.RawMessage at any layer.
type HandlerFunc func(ctx context.Context, input any) (any, error)

// procedure is an internal registration entry.
type procedure struct {
	typ            ProcedureType
	handler        HandlerFunc
	wrappedHandler HandlerFunc // pre-computed: middleware chain around handler
	middleware     []Middleware
	meta           any
	inputType      reflect.Type
	outputType     reflect.Type
}

// ProcedureOption configures a single procedure registration.
type ProcedureOption func(*procedureConfig)

type procedureConfig struct {
	middleware []Middleware
	meta       any
}

func collectProcedureConfig(opts []ProcedureOption) procedureConfig {
	var cfg procedureConfig
	for _, opt := range opts {
		opt(&cfg)
	}
	return cfg
}

// Use adds per-procedure middleware.
func Use(mw ...Middleware) ProcedureOption {
	return func(c *procedureConfig) {
		c.middleware = append(c.middleware, mw...)
	}
}

// WithMeta attaches metadata to a procedure, accessible in middleware
// via GetProcedureMeta(ctx).
func WithMeta(meta any) ProcedureOption {
	return func(c *procedureConfig) {
		c.meta = meta
	}
}

func makeHandler[I any, O any](fn func(ctx context.Context, input I) (O, error)) HandlerFunc {
	return func(ctx context.Context, input any) (any, error) {
		return fn(ctx, input.(I))
	}
}

func makeVoidHandler[O any](fn func(ctx context.Context) (O, error)) HandlerFunc {
	return func(ctx context.Context, _ any) (any, error) {
		return fn(ctx)
	}
}
