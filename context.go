package trpcgo

import "context"

type contextKey int

const ctxKeyProcedureMeta contextKey = iota

// ProcedureMeta contains procedure metadata available to middleware via context.
// Use GetProcedureMeta(ctx) to read it inside middleware.
type ProcedureMeta struct {
	Path string
	Type ProcedureType
	Meta any // user-defined metadata from WithMeta()
}

// GetProcedureMeta returns the procedure metadata from the context.
// Returns false if not available (e.g., outside a procedure call).
func GetProcedureMeta(ctx context.Context) (ProcedureMeta, bool) {
	pm, ok := ctx.Value(ctxKeyProcedureMeta).(ProcedureMeta)
	return pm, ok
}

// GetMeta extracts typed metadata from the procedure context.
// Returns the zero value and false if the context has no procedure metadata
// or if the metadata is not of type T.
func GetMeta[T any](ctx context.Context) (T, bool) {
	pm, ok := GetProcedureMeta(ctx)
	if !ok {
		var zero T
		return zero, false
	}
	val, ok := pm.Meta.(T)
	return val, ok
}

// mergeContexts returns a context that carries values from valuesCtx but
// cancels when either cancelCtx or valuesCtx is done (whichever fires first).
// This ensures that request cancellation propagates even when a user-supplied
// createContext function returns a context not derived from the HTTP request.
func mergeContexts(cancelCtx, valuesCtx context.Context) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancelCause(valuesCtx)
	stop := context.AfterFunc(cancelCtx, func() {
		cancel(cancelCtx.Err())
	})
	return ctx, func() {
		stop()
		cancel(nil)
	}
}
