package trpcgo

import (
	"context"
	"reflect"
)

// Query registers a query procedure.
func Query[I any, O any](r *Router, path string, fn func(ctx context.Context, input I) (O, error), opts ...ProcedureOption) {
	cfg := collectProcedureConfig(opts)
	r.register(path, ProcedureQuery, makeHandler(fn), cfg.middleware, cfg.meta, reflect.TypeFor[I](), reflect.TypeFor[O]())
}

// VoidQuery registers a query procedure with no input.
func VoidQuery[O any](r *Router, path string, fn func(ctx context.Context) (O, error), opts ...ProcedureOption) {
	cfg := collectProcedureConfig(opts)
	r.register(path, ProcedureQuery, makeVoidHandler(fn), cfg.middleware, cfg.meta, nil, reflect.TypeFor[O]())
}

// Mutation registers a mutation procedure.
func Mutation[I any, O any](r *Router, path string, fn func(ctx context.Context, input I) (O, error), opts ...ProcedureOption) {
	cfg := collectProcedureConfig(opts)
	r.register(path, ProcedureMutation, makeHandler(fn), cfg.middleware, cfg.meta, reflect.TypeFor[I](), reflect.TypeFor[O]())
}

// VoidMutation registers a mutation procedure with no input.
func VoidMutation[O any](r *Router, path string, fn func(ctx context.Context) (O, error), opts ...ProcedureOption) {
	cfg := collectProcedureConfig(opts)
	r.register(path, ProcedureMutation, makeVoidHandler(fn), cfg.middleware, cfg.meta, nil, reflect.TypeFor[O]())
}

// Subscribe registers a subscription procedure.
func Subscribe[I any, O any](r *Router, path string, fn func(ctx context.Context, input I) (<-chan O, error), opts ...ProcedureOption) {
	cfg := collectProcedureConfig(opts)
	r.register(path, ProcedureSubscription, makeStreamHandler(fn), cfg.middleware, cfg.meta, reflect.TypeFor[I](), reflect.TypeFor[O]())
}

// VoidSubscribe registers a subscription procedure with no input.
func VoidSubscribe[O any](r *Router, path string, fn func(ctx context.Context) (<-chan O, error), opts ...ProcedureOption) {
	cfg := collectProcedureConfig(opts)
	r.register(path, ProcedureSubscription, makeVoidStreamHandler(fn), cfg.middleware, cfg.meta, nil, reflect.TypeFor[O]())
}
