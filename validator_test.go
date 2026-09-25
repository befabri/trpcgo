package trpcgo_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/befabri/trpcgo"
)

type walkItem struct {
	Name string `json:"name"`
}

// walkDecoded decodes itself from a JSON object and still carries a tagged
// field, so a struct validator has rules to apply.
type walkDecoded struct {
	Name string `json:"name" validate:"required"`
}

func (d *walkDecoded) UnmarshalJSON(data []byte) error {
	var raw struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	d.Name = raw.Name
	return nil
}

// walkText decodes from a JSON string and has no exported fields to validate.
type walkText struct{ value string }

func (t *walkText) UnmarshalText(text []byte) error { t.value = string(text); return nil }

// walkStamp is convertible to time.Time, which validator refuses to validate.
type walkStamp time.Time

type walkError struct{ name string }

func (e walkError) Error() string { return "invalid item " + e.name }

// walkInvalid stands for validator's InvalidValidationError, returned for a
// root that is not a struct, such as a nil pointer.
type walkInvalid struct{ input any }

func (e walkInvalid) Error() string { return fmt.Sprintf("validator: (nil %T)", e.input) }

// recordingValidator behaves like validate.Struct: it accepts a struct or a
// non-nil pointer to one, rejects every other root, and fails a struct whose
// Name field is empty.
func recordingValidator(calls *[]any) func(any) error {
	return func(input any) error {
		*calls = append(*calls, input)
		v := reflect.ValueOf(input)
		if v.Kind() == reflect.Pointer && !v.IsNil() {
			v = v.Elem()
		}
		if v.Kind() != reflect.Struct {
			return walkInvalid{input: input}
		}
		if name := v.FieldByName("Name"); name.IsValid() && name.String() == "" {
			return walkError{name: "<empty>"}
		}
		return nil
	}
}

func TestStructValidatorReachesEveryStruct(t *testing.T) {
	good, bad := walkItem{Name: "ok"}, walkItem{Name: ""}
	tests := []struct {
		name    string
		input   any
		calls   int
		errs    []string // required substrings of the joined error, nil when valid
		invalid bool     // the error is the validator's rejection of the root itself
	}{
		{name: "struct root", input: good, calls: 1},
		{name: "invalid struct root", input: bad, calls: 1, errs: []string{"invalid item"}},
		{name: "pointer root keeps pointer", input: &good, calls: 1},
		{name: "nil pointer root reaches validate", input: (*walkItem)(nil), calls: 1, errs: []string{"validator: (nil *trpcgo_test.walkItem)"}, invalid: true},
		{name: "nil interface", input: nil},
		{name: "string root", input: "id"},
		{name: "integer root", input: 42},
		{name: "slice of structs", input: []walkItem{good, bad, bad}, calls: 3, errs: []string{"[1]: invalid item", "[2]: invalid item"}},
		{name: "slice of pointers with nil", input: []*walkItem{&good, nil, &bad}, calls: 2, errs: []string{"[2]: invalid item"}},
		{name: "array", input: [2]walkItem{good, bad}, calls: 2, errs: []string{"[1]: invalid item"}},
		{name: "map values", input: map[string]walkItem{"b": bad, "a": good}, calls: 2, errs: []string{`["b"]: invalid item`}},
		{name: "map of pointers with nil", input: map[string]*walkItem{"a": nil, "b": &bad}, calls: 1, errs: []string{`["b"]: invalid item`}},
		{name: "nested containers", input: map[string][]*walkItem{"k": {&good, &bad}}, calls: 2, errs: []string{`["k"][1]: invalid item`}},
		{name: "pointer to slice", input: &[]walkItem{bad}, calls: 1, errs: []string{"[0]: invalid item"}},
		{name: "nil pointer to slice", input: (*[]walkItem)(nil)},
		{name: "interface elements", input: []any{good, "text", &bad, nil}, calls: 2, errs: []string{"[2]: invalid item"}},
		{name: "json unmarshaler root", input: walkDecoded{Name: "ok"}, calls: 1},
		{name: "invalid json unmarshaler root", input: walkDecoded{}, calls: 1, errs: []string{"invalid item"}},
		{name: "json unmarshaler pointer root", input: &walkDecoded{}, calls: 1, errs: []string{"invalid item"}},
		{name: "slice of json unmarshalers", input: []walkDecoded{{Name: "ok"}, {}}, calls: 2, errs: []string{"[1]: invalid item"}},
		{name: "text unmarshaler struct reaches validate", input: []walkText{{value: "x"}}, calls: 1},
		{name: "time.Time root", input: time.Now()},
		{name: "pointer to time.Time", input: new(time.Time)},
		{name: "nil pointer to time.Time", input: (*time.Time)(nil)},
		{name: "slice of time.Time", input: []time.Time{time.Now()}},
		{name: "type convertible to time.Time", input: []walkStamp{walkStamp(time.Now())}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var calls []any
			err := trpcgo.StructValidator(recordingValidator(&calls))(tt.input)
			if len(calls) != tt.calls {
				t.Fatalf("validator calls=%d, want %d", len(calls), tt.calls)
			}
			if tt.errs == nil {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("expected an error")
			}
			for _, want := range tt.errs {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q lacks %q", err, want)
				}
			}
			var typed walkError
			var invalid walkInvalid
			if tt.invalid {
				if !errors.As(err, &invalid) {
					t.Errorf("errors.As lost the validator's root rejection through %q", err)
				}
			} else if !errors.As(err, &typed) {
				t.Errorf("errors.As lost the validator's error type through %q", err)
			}
		})
	}
}

func TestStructValidatorKeepsPointerIdentity(t *testing.T) {
	item := &walkItem{Name: "ok"}
	var calls []any
	if err := trpcgo.StructValidator(recordingValidator(&calls))(item); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 || calls[0] != item {
		t.Fatalf("validator received %#v, want the original pointer", calls)
	}
}

func TestStructValidatorMapErrorsAreOrdered(t *testing.T) {
	bad := walkItem{}
	input := map[string]walkItem{"c": bad, "a": bad, "b": bad}
	first := trpcgo.StructValidator(recordingValidator(new([]any)))(input).Error()
	for range 5 {
		if again := trpcgo.StructValidator(recordingValidator(new([]any)))(input).Error(); again != first {
			t.Fatalf("map error order changed between runs:\n%s\n%s", first, again)
		}
	}
	if !strings.HasPrefix(first, `["a"]`) {
		t.Fatalf("errors are not sorted by key: %s", first)
	}
}

func TestStructValidatorStopsOnCycles(t *testing.T) {
	cyclic := []any{nil}
	cyclic[0] = cyclic
	self := map[string]any{}
	self["self"] = self
	var calls []any
	validate := trpcgo.StructValidator(recordingValidator(&calls))
	done := make(chan error, 2)
	go func() { done <- validate(cyclic) }()
	go func() { done <- validate(self) }()
	for range 2 {
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("cyclic input did not terminate")
		}
	}
	if len(calls) != 0 {
		t.Fatalf("cyclic inputs reached the validator %d times", len(calls))
	}
}

func TestStructValidatorValidatesSliceRootsThroughRouter(t *testing.T) {
	structOnly := func(input any) error {
		item, ok := input.(walkItem)
		if !ok {
			t.Fatalf("struct validator received %T", input)
		}
		if item.Name == "" {
			return walkError{name: "<empty>"}
		}
		return nil
	}
	r := trpcgo.NewRouter(trpcgo.WithValidator(trpcgo.StructValidator(structOnly)))
	t.Cleanup(func() { _ = r.Close() })
	trpcgo.MustMutation(r, "items.create", func(_ context.Context, items []walkItem) (int, error) { return len(items), nil })
	trpcgo.MustQuery(r, "items.count", func(_ context.Context, prefix string) (int, error) { return len(prefix), nil })

	if _, err := r.RawCall(t.Context(), "items.create", []byte(`[{"name":"a"},{"name":"b"}]`)); err != nil {
		t.Fatalf("valid slice root rejected: %v", err)
	}
	_, err := r.RawCall(t.Context(), "items.create", []byte(`[{"name":"a"},{"name":""}]`))
	var rpcError *trpcgo.Error
	var typed walkError
	if !errors.As(err, &rpcError) || rpcError.Code != trpcgo.CodeBadRequest || !errors.As(err, &typed) {
		t.Fatalf("invalid slice element: err=%v, want BAD_REQUEST wrapping the validator error", err)
	}
	if _, err := r.RawCall(t.Context(), "items.count", []byte(`"abc"`)); err != nil {
		t.Fatalf("scalar root must bypass the struct validator: %v", err)
	}
}

// The generated TypeScript types a pointer root as the struct itself and its
// Zod schema is the struct's object schema, so a JSON null or an absent input
// is outside the client contract and must not reach the handler. A struct
// with its own JSON decoder keeps its rules.
func TestStructValidatorRootsThroughRouter(t *testing.T) {
	r := trpcgo.NewRouter(trpcgo.WithValidator(trpcgo.StructValidator(recordingValidator(new([]any)))))
	t.Cleanup(func() { _ = r.Close() })
	var received []any
	trpcgo.MustMutation(r, "pointer", func(_ context.Context, in *walkItem) (bool, error) {
		received = append(received, in)
		return in != nil, nil
	})
	trpcgo.MustMutation(r, "decoded", func(_ context.Context, in walkDecoded) (string, error) { return in.Name, nil })
	trpcgo.MustMutation(r, "decoded.list", func(_ context.Context, in []walkDecoded) (int, error) { return len(in), nil })

	tests := []struct {
		name, proc, raw string
		reject          error // the validator error a BAD_REQUEST must wrap, nil when accepted
	}{
		{name: "pointer root value", proc: "pointer", raw: `{"name":"a"}`},
		{name: "pointer root null", proc: "pointer", raw: `null`, reject: walkInvalid{}},
		{name: "pointer root absent", proc: "pointer", raw: ``, reject: walkInvalid{}},
		{name: "decoder root value", proc: "decoded", raw: `{"name":"a"}`},
		{name: "decoder root empty name", proc: "decoded", raw: `{"name":""}`, reject: walkError{}},
		{name: "decoder element empty name", proc: "decoded.list", raw: `[{"name":"a"},{"name":""}]`, reject: walkError{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := r.RawCall(t.Context(), tt.proc, []byte(tt.raw))
			if tt.reject == nil {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			var rpcError *trpcgo.Error
			if !errors.As(err, &rpcError) || rpcError.Code != trpcgo.CodeBadRequest {
				t.Fatalf("err=%v, want BAD_REQUEST", err)
			}
			target := reflect.New(reflect.TypeOf(tt.reject))
			if !errors.As(err, target.Interface()) {
				t.Fatalf("err=%v does not wrap %T", err, tt.reject)
			}
		})
	}
	if len(received) != 1 {
		t.Fatalf("the pointer handler ran %d times, want once for the valid input", len(received))
	}
}
