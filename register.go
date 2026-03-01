package trpcgo

import (
	"context"
	"reflect"
)

// Query registers a query procedure.
func Query[I any, O any](r *Router, path string, fn func(ctx context.Context, input I) (O, error), opts ...ProcedureOption) {
	cfg := collectProcedureConfig(opts)
	r.register(path, ProcedureQuery, wrapHandler(fn), cfg.middleware, cfg.meta, reflect.TypeFor[I](), reflect.TypeFor[O]())
}

// VoidQuery registers a query procedure with no input.
func VoidQuery[O any](r *Router, path string, fn func(ctx context.Context) (O, error), opts ...ProcedureOption) {
	cfg := collectProcedureConfig(opts)
	r.register(path, ProcedureQuery, wrapVoidHandler(fn), cfg.middleware, cfg.meta, nil, reflect.TypeFor[O]())
}

// Mutation registers a mutation procedure.
func Mutation[I any, O any](r *Router, path string, fn func(ctx context.Context, input I) (O, error), opts ...ProcedureOption) {
	cfg := collectProcedureConfig(opts)
	r.register(path, ProcedureMutation, wrapHandler(fn), cfg.middleware, cfg.meta, reflect.TypeFor[I](), reflect.TypeFor[O]())
}

// VoidMutation registers a mutation procedure with no input.
func VoidMutation[O any](r *Router, path string, fn func(ctx context.Context) (O, error), opts ...ProcedureOption) {
	cfg := collectProcedureConfig(opts)
	r.register(path, ProcedureMutation, wrapVoidHandler(fn), cfg.middleware, cfg.meta, nil, reflect.TypeFor[O]())
}

// Subscribe registers a subscription procedure.
func Subscribe[I any, O any](r *Router, path string, fn func(ctx context.Context, input I) (<-chan O, error), opts ...ProcedureOption) {
	cfg := collectProcedureConfig(opts)
	r.register(path, ProcedureSubscription, wrapStreamHandler(fn), cfg.middleware, cfg.meta, reflect.TypeFor[I](), reflect.TypeFor[O]())
}

// VoidSubscribe registers a subscription procedure with no input.
func VoidSubscribe[O any](r *Router, path string, fn func(ctx context.Context) (<-chan O, error), opts ...ProcedureOption) {
	cfg := collectProcedureConfig(opts)
	r.register(path, ProcedureSubscription, wrapVoidStreamHandler(fn), cfg.middleware, cfg.meta, nil, reflect.TypeFor[O]())
}
