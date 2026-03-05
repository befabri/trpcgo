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
// Implement this interface via [Use], [WithMeta], or [Procedure].
type ProcedureOption interface {
	applyProcedureOption(*procedureConfig)
}

// procedureOptionFunc is the internal adapter that lets a plain function
// satisfy ProcedureOption without exposing procedureConfig publicly.
type procedureOptionFunc func(*procedureConfig)

func (f procedureOptionFunc) applyProcedureOption(c *procedureConfig) { f(c) }

type procedureConfig struct {
	middleware []Middleware
	meta       any
}

func collectProcedureConfig(opts []ProcedureOption) procedureConfig {
	var cfg procedureConfig
	for _, opt := range opts {
		if opt == nil {
			continue
		}
		opt.applyProcedureOption(&cfg)
	}
	return cfg
}

// Use adds per-procedure middleware. It can be passed directly to any
// registration function or to [Procedure] when building a base procedure.
func Use(mw ...Middleware) ProcedureOption {
	return procedureOptionFunc(func(c *procedureConfig) {
		c.middleware = append(c.middleware, mw...)
	})
}

// WithMeta attaches metadata to a procedure, accessible in middleware
// via [GetProcedureMeta].
func WithMeta(meta any) ProcedureOption {
	return procedureOptionFunc(func(c *procedureConfig) {
		c.meta = meta
	})
}

// ProcedureBuilder is a reusable base procedure that accumulates middleware
// and metadata. It is immutable: every chain method returns a new instance.
// A *ProcedureBuilder satisfies [ProcedureOption] and can be passed directly
// to any registration function.
//
// Usage:
//
//	authedProcedure := trpcgo.Procedure().Use(authMiddleware)
//	adminProcedure  := authedProcedure.Use(adminCheck).WithMeta(roleMeta{})
//
//	trpcgo.MustQuery(router, "user.list", listUsers, authedProcedure)
//	trpcgo.MustMutation(router, "admin.ban", banUser, adminProcedure)
type ProcedureBuilder struct {
	opts []ProcedureOption
}

// Procedure creates a new [ProcedureBuilder], optionally pre-seeded with
// existing options or other builders.
func Procedure(base ...ProcedureOption) *ProcedureBuilder {
	opts := make([]ProcedureOption, len(base))
	copy(opts, base)
	return &ProcedureBuilder{opts: opts}
}

// Use returns a new [ProcedureBuilder] with the given middleware appended.
// The receiver is not modified.
func (b *ProcedureBuilder) Use(mw ...Middleware) *ProcedureBuilder {
	next := make([]ProcedureOption, len(b.opts)+1)
	copy(next, b.opts)
	next[len(b.opts)] = Use(mw...)
	return &ProcedureBuilder{opts: next}
}

// WithMeta returns a new [ProcedureBuilder] with the metadata set.
// The receiver is not modified.
func (b *ProcedureBuilder) WithMeta(meta any) *ProcedureBuilder {
	next := make([]ProcedureOption, len(b.opts)+1)
	copy(next, b.opts)
	next[len(b.opts)] = WithMeta(meta)
	return &ProcedureBuilder{opts: next}
}

// applyProcedureOption applies the builder's accumulated options so that
// *ProcedureBuilder satisfies [ProcedureOption].
func (b *ProcedureBuilder) applyProcedureOption(c *procedureConfig) {
	if b == nil {
		return
	}
	for _, opt := range b.opts {
		if opt == nil {
			continue
		}
		opt.applyProcedureOption(c)
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
