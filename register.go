package trpcgo

import (
	"context"
	"reflect"
)

// Query registers a query procedure.
func Query[I any, O any](r *Router, path string, fn func(ctx context.Context, input I) (O, error), mw ...Middleware) {
	r.register(path, ProcedureQuery, wrapHandler(fn), mw, reflect.TypeFor[I](), reflect.TypeFor[O]())
}

// VoidQuery registers a query procedure with no input.
func VoidQuery[O any](r *Router, path string, fn func(ctx context.Context) (O, error), mw ...Middleware) {
	r.register(path, ProcedureQuery, wrapVoidHandler(fn), mw, nil, reflect.TypeFor[O]())
}

// Mutation registers a mutation procedure.
func Mutation[I any, O any](r *Router, path string, fn func(ctx context.Context, input I) (O, error), mw ...Middleware) {
	r.register(path, ProcedureMutation, wrapHandler(fn), mw, reflect.TypeFor[I](), reflect.TypeFor[O]())
}

// VoidMutation registers a mutation procedure with no input.
func VoidMutation[O any](r *Router, path string, fn func(ctx context.Context) (O, error), mw ...Middleware) {
	r.register(path, ProcedureMutation, wrapVoidHandler(fn), mw, nil, reflect.TypeFor[O]())
}

// Subscribe registers a subscription procedure.
func Subscribe[I any, O any](r *Router, path string, fn func(ctx context.Context, input I) (<-chan O, error), mw ...Middleware) {
	r.register(path, ProcedureSubscription, wrapStreamHandler(fn), mw, reflect.TypeFor[I](), reflect.TypeFor[O]())
}

// VoidSubscribe registers a subscription procedure with no input.
func VoidSubscribe[O any](r *Router, path string, fn func(ctx context.Context) (<-chan O, error), mw ...Middleware) {
	r.register(path, ProcedureSubscription, wrapVoidStreamHandler(fn), mw, nil, reflect.TypeFor[O]())
}
