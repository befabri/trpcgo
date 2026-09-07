package trpcgo_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/befabri/trpcgo"
)

type walkItem struct {
	Name string `json:"name"`
}

// walkText decodes from a JSON string, so a struct validator has no fields to inspect.
type walkText struct{ value string }

func (t *walkText) UnmarshalText(text []byte) error { t.value = string(text); return nil }

type walkError struct{ name string }

func (e walkError) Error() string { return "invalid item " + e.name }

func recordingValidator(calls *[]any) func(any) error {
	return func(input any) error {
		*calls = append(*calls, input)
		var item walkItem
		switch v := input.(type) {
		case walkItem:
			item = v
		case *walkItem:
			item = *v
		default:
			return errors.New("non-struct value reached the struct validator")
		}
		if item.Name == "" {
			return walkError{name: "<empty>"}
		}
		return nil
	}
}

func TestStructValidatorReachesEveryStruct(t *testing.T) {
	good, bad := walkItem{Name: "ok"}, walkItem{Name: ""}
	tests := []struct {
		name  string
		input any
		calls int
		errs  []string // required substrings of the joined error, nil when valid
	}{
		{name: "struct root", input: good, calls: 1},
		{name: "invalid struct root", input: bad, calls: 1, errs: []string{"invalid item"}},
		{name: "pointer root keeps pointer", input: &good, calls: 1},
		{name: "nil pointer", input: (*walkItem)(nil)},
		{name: "nil interface", input: nil},
		{name: "string root", input: "id"},
		{name: "integer root", input: 42},
		{name: "slice of structs", input: []walkItem{good, bad, bad}, calls: 3, errs: []string{"[1]: invalid item", "[2]: invalid item"}},
		{name: "slice of pointers with nil", input: []*walkItem{&good, nil, &bad}, calls: 2, errs: []string{"[2]: invalid item"}},
		{name: "array", input: [2]walkItem{good, bad}, calls: 2, errs: []string{"[1]: invalid item"}},
		{name: "map values", input: map[string]walkItem{"b": bad, "a": good}, calls: 2, errs: []string{`["b"]: invalid item`}},
		{name: "nested containers", input: map[string][]*walkItem{"k": {&good, &bad}}, calls: 2, errs: []string{`["k"][1]: invalid item`}},
		{name: "pointer to slice", input: &[]walkItem{bad}, calls: 1, errs: []string{"[0]: invalid item"}},
		{name: "interface elements", input: []any{good, "text", &bad, nil}, calls: 2, errs: []string{"[2]: invalid item"}},
		{name: "time.Time root", input: time.Now()},
		{name: "slice of time.Time", input: []time.Time{time.Now()}},
		{name: "text unmarshaler struct", input: []walkText{{value: "x"}}},
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
			if !errors.As(err, &typed) {
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
