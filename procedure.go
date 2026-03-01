package trpcgo

import (
	"context"
	"encoding/json"
	"reflect"
)

// ProcedureType distinguishes queries, mutations, and subscriptions.
type ProcedureType string

const (
	ProcedureQuery        ProcedureType = "query"
	ProcedureMutation     ProcedureType = "mutation"
	ProcedureSubscription ProcedureType = "subscription"
)

// HandlerFunc is the raw procedure handler signature, used in Middleware.
type HandlerFunc func(ctx context.Context, rawInput json.RawMessage) (any, error)

// procedure is an internal registration entry.
type procedure struct {
	typ            ProcedureType
	handler        HandlerFunc
	wrappedHandler HandlerFunc
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

func wrapHandler[I any, O any](fn func(ctx context.Context, input I) (O, error)) HandlerFunc {
	return func(ctx context.Context, rawInput json.RawMessage) (any, error) {
		var input I
		if len(rawInput) > 0 {
			if err := json.Unmarshal(rawInput, &input); err != nil {
				return nil, NewError(CodeParseError, "failed to parse input")
			}
		}
		return fn(ctx, input)
	}
}

func wrapVoidHandler[O any](fn func(ctx context.Context) (O, error)) HandlerFunc {
	return func(ctx context.Context, _ json.RawMessage) (any, error) {
		return fn(ctx)
	}
}
