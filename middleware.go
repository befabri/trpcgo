package trpcgo

// Middleware wraps a procedure handler, enabling cross-cutting concerns
// like logging, authentication, and error handling.
type Middleware func(next HandlerFunc) HandlerFunc

// Chain composes multiple middleware into one, applied left-to-right.
func Chain(mws ...Middleware) Middleware {
	return func(next HandlerFunc) HandlerFunc {
		for i := len(mws) - 1; i >= 0; i-- {
			next = mws[i](next)
		}
		return next
	}
}

func applyMiddleware(handler HandlerFunc, global []Middleware, perProc []Middleware) HandlerFunc {
	for i := len(perProc) - 1; i >= 0; i-- {
		handler = perProc[i](handler)
	}
	for i := len(global) - 1; i >= 0; i-- {
		handler = global[i](handler)
	}
	return handler
}
