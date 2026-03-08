package trpcgo

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"reflect"
	"sync"
	"sync/atomic"
)

// Router holds registered procedures and produces an http.Handler
// implementing the tRPC HTTP wire protocol.
type Router struct {
	mu             sync.RWMutex
	procedures     map[string]*procedure
	middleware     []Middleware
	opts           routerOptions
	sseConnections atomic.Int64 // active SSE connection count
	watcherOnce    sync.Once
	closeOnce      sync.Once
	done           chan struct{} // closed by Close() to stop the watcher goroutine
}

// NewRouter creates a new Router with the given options.
func NewRouter(opts ...Option) *Router {
	r := &Router{
		procedures: make(map[string]*procedure),
		done:       make(chan struct{}),
		opts: routerOptions{
			allowBatching:  true,
			maxBodySize:    defaultMaxBodySize,
			maxBatchSize:   defaultMaxBatchSize,
			sseMaxDuration: defaultSSEMaxDuration,
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

func (r *Router) register(path string, typ ProcedureType, handler HandlerFunc, mw []Middleware, meta any, inputType, outputType reflect.Type, outputValidator func(any) error, outputParser func(any) (any, error), route Route) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.procedures[path]; exists {
		return fmt.Errorf("trpcgo: procedure %q already registered", path)
	}
	r.procedures[path] = &procedure{
		typ:             typ,
		handler:         handler,
		middleware:      mw,
		meta:            meta,
		inputType:       inputType,
		outputType:      outputType,
		outputValidator: outputValidator,
		outputParser:    outputParser,
		route:           route,
	}
	return nil
}

// Merge copies all procedures from the source routers into this router.
// Returns an error if any procedure path already exists.
// The operation is atomic: on error, no procedures are added.
// Global middleware and options on source routers are NOT copied.
func (r *Router) Merge(sources ...*Router) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Collect all procedures first, checking for duplicates against both
	// the target router and across sources. This ensures atomicity: either
	// all procedures are added or none are.
	type entry struct {
		path string
		proc *procedure
	}
	var toAdd []entry
	seen := make(map[string]bool)
	for _, src := range sources {
		src.mu.RLock()
		for path, proc := range src.procedures {
			if _, exists := r.procedures[path]; exists {
				src.mu.RUnlock()
				return fmt.Errorf("trpcgo: Merge: duplicate procedure %q", path)
			}
			if seen[path] {
				src.mu.RUnlock()
				return fmt.Errorf("trpcgo: Merge: duplicate procedure %q across sources", path)
			}
			seen[path] = true
			toAdd = append(toAdd, entry{path, proc})
		}
		src.mu.RUnlock()
	}

	// All checks passed — insert.
	for _, e := range toAdd {
		r.procedures[e.path] = &procedure{
			typ:             e.proc.typ,
			handler:         e.proc.handler,
			middleware:      e.proc.middleware,
			meta:            e.proc.meta,
			inputType:       e.proc.inputType,
			outputType:      e.proc.outputType,
			outputValidator: e.proc.outputValidator,
			outputParser:    e.proc.outputParser,
			route:           e.proc.route,
		}
	}
	return nil
}

// MergeRouters creates a new Router combining procedures from all sources.
// Returns an error if any two routers define a procedure at the same path.
// The returned router has default options and no global middleware.
func MergeRouters(routers ...*Router) (*Router, error) {
	merged := NewRouter()
	if err := merged.Merge(routers...); err != nil {
		return nil, err
	}
	return merged, nil
}

// Close stops the file watcher goroutine (if running) and releases resources.
// Safe to call multiple times.
func (r *Router) Close() error {
	r.closeOnce.Do(func() {
		close(r.done)
	})
	return nil
}

// Handler returns an http.Handler that serves all registered procedures
// using the tRPC wire format.
// basePath is stripped from incoming request URLs before procedure lookup.
//
// When WithDev and WithTypeOutput are both set, the TypeScript type file
// is generated and a file watcher is started to regenerate types when
// Go source changes. Call Close() to stop the watcher.
//
// Deprecated: Use [trpc.NewHandler] or [orpc.NewHandler] from the protocol
// sub-packages instead. They provide the same functionality while keeping
// protocol handling decoupled from the core router.
func (r *Router) Handler(basePath string) http.Handler {
	if r.opts.zodOutput != "" && r.opts.typeOutput == "" {
		log.Printf("trpcgo: WithZodOutput is set but WithTypeOutput is not — Zod schemas will not be generated")
	}
	if r.opts.typeOutput != "" && r.opts.isDev {
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
	pm := r.BuildProcedureMap()

	return &httpHandler{router: r, procedures: pm, opts: &r.opts, basePath: basePath}
}

func applyOutputHooks(output any, outputValidator func(any) error, outputParser func(any) (any, error)) (any, error) {
	if outputValidator != nil {
		if err := outputValidator(output); err != nil {
			return nil, fmt.Errorf("output validator: %w", err)
		}
	}
	if outputParser != nil {
		parsed, err := outputParser(output)
		if err != nil {
			return nil, fmt.Errorf("output parser: %w", err)
		}
		output = parsed
	}
	return output, nil
}

// executeProcedure decodes the raw JSON input, validates it, and calls the handler.
// Used by RawCall where the procedure comes from r.procedures (not a snapshot).
func (r *Router) executeProcedure(ctx context.Context, proc *procedure, raw json.RawMessage) (any, error) {
	// Use pre-computed chain if available, otherwise build on the fly.
	handler := proc.wrappedHandler
	if handler == nil {
		handler = applyMiddleware(proc.handler, r.middleware, proc.middleware)
	}
	return r.executeCommon(ctx, handler, proc.inputType, raw, proc.outputValidator, proc.outputParser)
}
