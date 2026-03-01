package trpcgo

import (
	"context"
	"encoding/json"
	"reflect"
)

// withValidation wraps a HandlerFunc to validate the deserialized input
// before calling the inner handler. Only struct-typed inputs are validated.
//
// This causes a double-unmarshal (once here, once in the handler).
// TODO(v2): unify the decode path to avoid double-unmarshal.
func withValidation(next HandlerFunc, validate func(any) error, inputType reflect.Type) HandlerFunc {
	t := inputType
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return next
	}

	return func(ctx context.Context, rawInput json.RawMessage) (any, error) {
		if len(rawInput) > 0 {
			ptr := reflect.New(t)
			if err := json.Unmarshal(rawInput, ptr.Interface()); err != nil {
				// Let the inner handler report parse errors consistently.
				return next(ctx, rawInput)
			}
			if err := validate(ptr.Elem().Interface()); err != nil {
				return nil, WrapError(CodeBadRequest, "input validation failed", err)
			}
		}
		return next(ctx, rawInput)
	}
}
