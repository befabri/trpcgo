package trpcgo

import (
	"context"
	"encoding/json"
)

// RawCall invokes a procedure by path, running the full middleware chain.
// This is the server-side equivalent of an HTTP call — no network involved.
//
// Subscriptions are not supported via RawCall; use the subscription handler directly.
func (r *Router) RawCall(ctx context.Context, path string, input json.RawMessage) (any, error) {
	r.mu.RLock()
	proc, ok := r.procedures[path]
	r.mu.RUnlock()

	if !ok {
		return nil, NewError(CodeNotFound, "procedure not found")
	}

	if proc.typ == ProcedureSubscription {
		return nil, NewError(CodeBadRequest, "subscriptions are not supported via RawCall")
	}

	// Inject procedure metadata and response metadata into context.
	// Response metadata allows handlers to call SetCookie/SetResponseHeader
	// even via RawCall (callers can retrieve it with GetResponseMetadata).
	ctx = withProcedureMeta(ctx, ProcedureMeta{
		Path: path,
		Type: proc.typ,
		Meta: proc.meta,
	})
	if getResponseMetadata(ctx) == nil {
		ctx = WithResponseMetadata(ctx)
	}

	result, err := r.executeProcedure(ctx, proc, input)
	if err != nil {
		return nil, sanitizeReturnedError(err)
	}
	return result, nil
}

// Call invokes a typed procedure by path, running the full middleware chain.
// Input is marshaled to JSON and the result is unmarshaled to the output type.
func Call[I any, O any](r *Router, ctx context.Context, path string, input I) (O, error) {
	var zero O

	rawInput, err := json.Marshal(input)
	if err != nil {
		return zero, NewError(CodeParseError, "failed to marshal input")
	}

	result, err := r.RawCall(ctx, path, rawInput)
	if err != nil {
		return zero, err
	}

	// Try direct type assertion first (avoids JSON round-trip).
	if typed, ok := result.(O); ok {
		return typed, nil
	}

	// Fallback: JSON round-trip for type conversion.
	data, err := json.Marshal(result)
	if err != nil {
		return zero, NewError(CodeInternalServerError, "failed to serialize result")
	}

	var output O
	if err := json.Unmarshal(data, &output); err != nil {
		return zero, NewError(CodeInternalServerError, "failed to deserialize result")
	}

	return output, nil
}
