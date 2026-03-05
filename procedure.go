package trpcgo

import (
	"context"
	"fmt"
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
	outputParser   func(any) (any, error)
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
	middleware       []Middleware
	meta             any
	outputParser     func(any) (any, error)
	parsedOutputType reflect.Type // non-nil when OutputParser[O,P] provides a concrete P
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

// WithOutputParser sets a per-procedure output parser. The parser is called with
// the handler's return value after a successful handler call. It can validate,
// transform, or sanitize the output — the value it returns is what gets sent to
// the client. If the parser returns an error, the client receives an
// INTERNAL_SERVER_ERROR. For subscriptions, the parser runs on each emitted item.
//
// Use [OutputParser] for a typed alternative. Because the
// parser takes and returns any, generated types fall back to unknown unless a
// typed [OutputParser] override is also present.
func WithOutputParser(fn func(any) (any, error)) ProcedureOption {
	return procedureOptionFunc(func(c *procedureConfig) {
		c.parsedOutputType = nil
		c.outputParser = fn
	})
}

// OutputParser creates a typed per-procedure output parser. The function receives
// the exact output type O and returns a value of type P to send to the client.
// It can validate (O == P, return unchanged), transform (return a reshaped value),
// or sanitize (strip fields). If the parser returns an error the client receives an
// INTERNAL_SERVER_ERROR.
//
// Codegen is aware of the [O, P] type pair: both [Router.GenerateTS] (reflection)
// and [trpcgo generate] (static analysis) emit P as the TypeScript output type,
// keeping the generated contract in sync with what the client actually receives.
// [WithOutputParser] (untyped) degrades codegen to unknown because the exact
// post-parse shape is not statically knowable — use this typed form when the
// output type changes.
//
// For subscriptions where O = [TrackedEvent][T], the parser receives the full
// TrackedEvent so it can inspect the ID and payload together. If the returned
// value also implements [TrackedEvent] its ID is propagated to the SSE stream;
// otherwise the item is sent without an ID.
//
// The type assertion is checked at runtime: if the output value cannot be asserted
// to O the parser returns INTERNAL_SERVER_ERROR rather than panicking.
func OutputParser[O, P any](fn func(O) (P, error)) ProcedureOption {
	oType := reflect.TypeFor[O]()
	return procedureOptionFunc(func(c *procedureConfig) {
		c.parsedOutputType = reflect.TypeFor[P]()
		c.outputParser = func(v any) (any, error) {
			if v == nil {
				var zero O
				if oType != nil && isNilAssignable(oType) {
					return fn(zero)
				}
				return nil, fmt.Errorf("output type mismatch: expected %T, got %T", *new(O), v)
			}
			typed, ok := v.(O)
			if !ok {
				return nil, fmt.Errorf("output type mismatch: expected %T, got %T", *new(O), v)
			}
			return fn(typed)
		}
	})
}

func isNilAssignable(t reflect.Type) bool {
	switch t.Kind() {
	case reflect.Interface, reflect.Pointer, reflect.Map, reflect.Slice, reflect.Func, reflect.Chan:
		return true
	default:
		return false
	}
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

// With returns a new [ProcedureBuilder] with the given options appended.
// Unlike [Use] (middleware-only), With accepts any [ProcedureOption], including
// [OutputParser]. The receiver is not modified.
//
// [trpcgo generate] can discover [OutputParser] calls passed directly to With.
// [WithOutputParser] (untyped) degrades codegen to unknown — use a typed
// [OutputParser] when the output type changes.
func (b *ProcedureBuilder) With(opts ...ProcedureOption) *ProcedureBuilder {
	next := make([]ProcedureOption, len(b.opts)+len(opts))
	copy(next, b.opts)
	copy(next[len(b.opts):], opts)
	return &ProcedureBuilder{opts: next}
}

// WithOutputParser returns a new [ProcedureBuilder] with an untyped output
// parser set. The receiver is not modified. Generated output types fall back to
// unknown unless a typed [OutputParser] is also present.
func (b *ProcedureBuilder) WithOutputParser(fn func(any) (any, error)) *ProcedureBuilder {
	next := make([]ProcedureOption, len(b.opts)+1)
	copy(next, b.opts)
	next[len(b.opts)] = WithOutputParser(fn)
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
