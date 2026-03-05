package trpcgo

import (
	"context"
	"fmt"
	"reflect"
)

// Query registers a query procedure.
// Returns an error if path is already registered.
func Query[I any, O any](r *Router, path string, fn func(ctx context.Context, input I) (O, error), opts ...ProcedureOption) error {
	cfg := collectProcedureConfig(opts)
	return r.register(path, ProcedureQuery, makeHandler(fn), cfg.middleware, cfg.meta, reflect.TypeFor[I](), reflect.TypeFor[O]())
}

// VoidQuery registers a query procedure with no input.
// Returns an error if path is already registered.
func VoidQuery[O any](r *Router, path string, fn func(ctx context.Context) (O, error), opts ...ProcedureOption) error {
	cfg := collectProcedureConfig(opts)
	return r.register(path, ProcedureQuery, makeVoidHandler(fn), cfg.middleware, cfg.meta, nil, reflect.TypeFor[O]())
}

// Mutation registers a mutation procedure.
// Returns an error if path is already registered.
func Mutation[I any, O any](r *Router, path string, fn func(ctx context.Context, input I) (O, error), opts ...ProcedureOption) error {
	cfg := collectProcedureConfig(opts)
	return r.register(path, ProcedureMutation, makeHandler(fn), cfg.middleware, cfg.meta, reflect.TypeFor[I](), reflect.TypeFor[O]())
}

// VoidMutation registers a mutation procedure with no input.
// Returns an error if path is already registered.
func VoidMutation[O any](r *Router, path string, fn func(ctx context.Context) (O, error), opts ...ProcedureOption) error {
	cfg := collectProcedureConfig(opts)
	return r.register(path, ProcedureMutation, makeVoidHandler(fn), cfg.middleware, cfg.meta, nil, reflect.TypeFor[O]())
}

// Subscribe registers a subscription procedure.
// Returns an error if path is already registered.
func Subscribe[I any, O any](r *Router, path string, fn func(ctx context.Context, input I) (<-chan O, error), opts ...ProcedureOption) error {
	cfg := collectProcedureConfig(opts)
	return r.register(path, ProcedureSubscription, makeStreamHandler(fn), cfg.middleware, cfg.meta, reflect.TypeFor[I](), reflect.TypeFor[O]())
}

// VoidSubscribe registers a subscription procedure with no input.
// Returns an error if path is already registered.
func VoidSubscribe[O any](r *Router, path string, fn func(ctx context.Context) (<-chan O, error), opts ...ProcedureOption) error {
	cfg := collectProcedureConfig(opts)
	return r.register(path, ProcedureSubscription, makeVoidStreamHandler(fn), cfg.middleware, cfg.meta, nil, reflect.TypeFor[O]())
}

// MustQuery is like Query but panics if registration fails.
// Use in application bootstrap code when a registration error is a programmer mistake.
func MustQuery[I any, O any](r *Router, path string, fn func(ctx context.Context, input I) (O, error), opts ...ProcedureOption) {
	if err := Query(r, path, fn, opts...); err != nil {
		panic(fmt.Sprintf("trpcgo: MustQuery %q: %v", path, err))
	}
}

// MustVoidQuery is like VoidQuery but panics if registration fails.
func MustVoidQuery[O any](r *Router, path string, fn func(ctx context.Context) (O, error), opts ...ProcedureOption) {
	if err := VoidQuery(r, path, fn, opts...); err != nil {
		panic(fmt.Sprintf("trpcgo: MustVoidQuery %q: %v", path, err))
	}
}

// MustMutation is like Mutation but panics if registration fails.
func MustMutation[I any, O any](r *Router, path string, fn func(ctx context.Context, input I) (O, error), opts ...ProcedureOption) {
	if err := Mutation(r, path, fn, opts...); err != nil {
		panic(fmt.Sprintf("trpcgo: MustMutation %q: %v", path, err))
	}
}

// MustVoidMutation is like VoidMutation but panics if registration fails.
func MustVoidMutation[O any](r *Router, path string, fn func(ctx context.Context) (O, error), opts ...ProcedureOption) {
	if err := VoidMutation(r, path, fn, opts...); err != nil {
		panic(fmt.Sprintf("trpcgo: MustVoidMutation %q: %v", path, err))
	}
}

// MustSubscribe is like Subscribe but panics if registration fails.
func MustSubscribe[I any, O any](r *Router, path string, fn func(ctx context.Context, input I) (<-chan O, error), opts ...ProcedureOption) {
	if err := Subscribe(r, path, fn, opts...); err != nil {
		panic(fmt.Sprintf("trpcgo: MustSubscribe %q: %v", path, err))
	}
}

// MustVoidSubscribe is like VoidSubscribe but panics if registration fails.
func MustVoidSubscribe[O any](r *Router, path string, fn func(ctx context.Context) (<-chan O, error), opts ...ProcedureOption) {
	if err := VoidSubscribe(r, path, fn, opts...); err != nil {
		panic(fmt.Sprintf("trpcgo: MustVoidSubscribe %q: %v", path, err))
	}
}
