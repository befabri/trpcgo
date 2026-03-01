package trpcgo

import (
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

func (r *Router) register(path string, typ ProcedureType, handler HandlerFunc, mw []Middleware, inputType, outputType reflect.Type) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.procedures[path]; exists {
		panic(fmt.Sprintf("trpcgo: procedure %q already registered", path))
	}
	r.procedures[path] = &procedure{
		typ:        typ,
		handler:    handler,
		middleware: mw,
		inputType:  inputType,
		outputType: outputType,
	}
}

func (r *Router) lookup(path string) (*procedure, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.procedures[path]
	return p, ok
}

// Handler returns an http.Handler that serves all registered procedures.
// basePath is stripped from incoming request URLs before procedure lookup.
//
// If WithTypeOutput was configured, the TypeScript type file is written
// and a file watcher is started to regenerate types when Go source changes.
func (r *Router) Handler(basePath string) http.Handler {
	if r.opts.typeOutput != "" {
		if err := r.GenerateTS(r.opts.typeOutput); err != nil {
			log.Printf("trpcgo: failed to generate TypeScript types: %v", err)
		}
		r.watcherOnce.Do(r.startWatcher)
	}

	// Pre-compute middleware chains for each procedure.
	r.mu.Lock()
	for _, proc := range r.procedures {
		proc.wrappedHandler = applyMiddleware(proc.handler, r.middleware, proc.middleware)
	}
	r.mu.Unlock()

	return &httpHandler{router: r, basePath: basePath}
}
