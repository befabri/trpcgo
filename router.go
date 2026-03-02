package trpcgo

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"reflect"
	"sync"
)

// Router holds registered procedures and produces an http.Handler
// implementing the tRPC HTTP wire protocol.
type Router struct {
	mu          sync.RWMutex
	procedures  map[string]*procedure
	middleware  []Middleware
	opts        routerOptions
	watcherOnce sync.Once
}

// NewRouter creates a new Router with the given options.
func NewRouter(opts ...Option) *Router {
	r := &Router{
		procedures: make(map[string]*procedure),
		opts: routerOptions{
			allowBatching: true,
			maxBodySize:   defaultMaxBodySize,
			maxBatchSize:  defaultMaxBatchSize,
		},
	}
	for _, opt := range opts {
		opt(&r.opts)
	}
	return r
}

// Use adds global middleware that applies to all procedures.
func (r *Router) Use(mw ...Middleware) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.middleware = append(r.middleware, mw...)
}

func (r *Router) register(path string, typ ProcedureType, handler HandlerFunc, mw []Middleware, meta any, inputType, outputType reflect.Type) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.procedures[path]; exists {
		panic(fmt.Sprintf("trpcgo: procedure %q already registered", path))
	}
	r.procedures[path] = &procedure{
		typ:        typ,
		handler:    handler,
		middleware: mw,
		meta:       meta,
		inputType:  inputType,
		outputType: outputType,
	}
}

// Merge copies all procedures from the source routers into this router.
// Panics if any procedure path already exists.
// Global middleware and options on source routers are NOT copied.
func (r *Router) Merge(sources ...*Router) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, src := range sources {
		src.mu.RLock()
		for path, proc := range src.procedures {
			if _, exists := r.procedures[path]; exists {
				src.mu.RUnlock()
				panic(fmt.Sprintf("trpcgo: Merge: duplicate procedure %q", path))
			}
			r.procedures[path] = &procedure{
				typ:        proc.typ,
				handler:    proc.handler,
				middleware: proc.middleware,
				meta:       proc.meta,
				inputType:  proc.inputType,
				outputType: proc.outputType,
			}
		}
		src.mu.RUnlock()
	}
}

// MergeRouters creates a new Router combining procedures from all sources.
// Panics if any two routers define a procedure at the same path.
// The returned router has default options and no global middleware.
func MergeRouters(routers ...*Router) *Router {
	merged := NewRouter()
	merged.Merge(routers...)
	return merged
}

// Handler returns an http.Handler that serves all registered procedures.
// basePath is stripped from incoming request URLs before procedure lookup.
//
// If WithTypeOutput was configured, the TypeScript type file is written
// and a file watcher is started to regenerate types when Go source changes.
func (r *Router) Handler(basePath string) http.Handler {
	if r.opts.zodOutput != "" && r.opts.typeOutput == "" {
		log.Printf("trpcgo: WithZodOutput is set but WithTypeOutput is not — Zod schemas will not be generated")
	}
	if r.opts.typeOutput != "" {
		if err := r.GenerateTS(r.opts.typeOutput); err != nil {
			log.Printf("trpcgo: failed to generate TypeScript types: %v", err)
		}
		if r.opts.zodOutput != "" {
			if err := r.GenerateZod(r.opts.zodOutput); err != nil {
				log.Printf("trpcgo: failed to generate Zod schemas: %v", err)
			}
		}
		r.watcherOnce.Do(r.startWatcher)
	}

	// Pre-compute middleware chains and snapshot the procedures map so
	// the HTTP handler needs no locking on the hot path.
	// IMPORTANT: create new procedure copies — do NOT mutate the originals.
	// r.procedures entries are shared with RawCall(), which reads them
	// concurrently under RLock. Writing wrappedHandler on the original
	// would race with executeProcedure reading it.
	r.mu.RLock()
	snapshot := make(map[string]*procedure, len(r.procedures))
	for path, proc := range r.procedures {
		snap := *proc // shallow copy
		snap.wrappedHandler = applyMiddleware(proc.handler, r.middleware, proc.middleware)
		snapshot[path] = &snap
	}
	r.mu.RUnlock()

	return &httpHandler{router: r, procedures: snapshot, opts: &r.opts, basePath: basePath}
}

// executeProcedure decodes the raw JSON input, validates it, and calls the handler.
// This is the single execution path shared by Handler() and RawCall.
func (r *Router) executeProcedure(ctx context.Context, proc *procedure, raw json.RawMessage) (any, error) {
	// Decode the raw input into the registered type.
	var input any
	if proc.inputType != nil {
		ptr := reflect.New(proc.inputType)
		if len(raw) > 0 {
			if r.opts.strictInput {
				dec := json.NewDecoder(bytes.NewReader(raw))
				dec.DisallowUnknownFields()
				if err := dec.Decode(ptr.Interface()); err != nil {
					return nil, NewError(CodeBadRequest, "unknown field in input")
				}
			} else if err := json.Unmarshal(raw, ptr.Interface()); err != nil {
				return nil, NewError(CodeParseError, "failed to parse input")
			}
		}
		input = ptr.Elem().Interface()
	}

	// Validate the decoded struct.
	if r.opts.validator != nil && proc.inputType != nil && input != nil {
		t := proc.inputType
		for t.Kind() == reflect.Pointer {
			t = t.Elem()
		}
		if t.Kind() == reflect.Struct {
			if err := r.opts.validator(input); err != nil {
				return nil, WrapError(CodeBadRequest, "input validation failed", err)
			}
		}
	}

	// Use pre-computed chain if available, otherwise build on the fly.
	handler := proc.wrappedHandler
	if handler == nil {
		handler = applyMiddleware(proc.handler, r.middleware, proc.middleware)
	}

	return handler(ctx, input)
}
