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

func withProcedureMeta(ctx context.Context, pm ProcedureMeta) context.Context {
	return context.WithValue(ctx, ctxKeyProcedureMeta, pm)
}
