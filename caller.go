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
	proc, ok := r.BuildProcedureMap().Lookup(path)

	if !ok {
		return nil, NewError(CodeNotFound, "procedure not found")
	}

	if proc.typ == ProcedureSubscription {
		return nil, NewError(CodeBadRequest, "subscriptions are not supported via RawCall")
	}

	ctx = WithProcedureMeta(ctx, ProcedureMeta{
		Path: path,
		Type: proc.typ,
		Meta: proc.meta,
	})
	// Response metadata is injected so handlers can still SetCookie and
	// SetResponseHeader under RawCall.
	if getResponseMetadata(ctx) == nil {
		ctx = WithResponseMetadata(ctx)
	}

	result, err := r.ExecuteEntry(ctx, proc, input)
	if err != nil {
		return nil, SanitizeError(err)
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

	// A direct assertion skips the JSON round-trip when the handler already
	// returned an O.
	if typed, ok := result.(O); ok {
		return typed, nil
	}

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
