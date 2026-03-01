package trpcgo

import (
	"context"
	"encoding/json"
	"reflect"
)

// ProcedureType distinguishes queries, mutations, and subscriptions.
type ProcedureType string

const (
	ProcedureQuery        ProcedureType = "query"
	ProcedureMutation     ProcedureType = "mutation"
	ProcedureSubscription ProcedureType = "subscription"
)

// HandlerFunc is the raw procedure handler signature, used in Middleware.
type HandlerFunc func(ctx context.Context, rawInput json.RawMessage) (any, error)

// procedure is an internal registration entry.
type procedure struct {
	typ            ProcedureType
	handler        HandlerFunc
	wrappedHandler HandlerFunc
	middleware     []Middleware
	inputType      reflect.Type
	outputType     reflect.Type
}

func wrapHandler[I any, O any](fn func(ctx context.Context, input I) (O, error)) HandlerFunc {
	return func(ctx context.Context, rawInput json.RawMessage) (any, error) {
		var input I
		if len(rawInput) > 0 {
			if err := json.Unmarshal(rawInput, &input); err != nil {
				return nil, NewError(CodeParseError, "failed to parse input")
			}
		}
		return fn(ctx, input)
	}
}

func wrapVoidHandler[O any](fn func(ctx context.Context) (O, error)) HandlerFunc {
	return func(ctx context.Context, _ json.RawMessage) (any, error) {
		return fn(ctx)
	}
}
