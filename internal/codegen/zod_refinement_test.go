package codegen

import (
	"bytes"
	"strings"
	"testing"

	"github.com/befabri/trpcgo/internal/typemap"
)

func TestRefinementUsesGoComparisonSemantics(t *testing.T) {
	for _, tc := range []struct {
		name, op, kind, want string
	}{
		{"string ordering", ">", "string", "new TextEncoder().encode(data.b).length > new TextEncoder().encode(data.a).length"},
		{"string equality", "===", "string", "$goString(data.b) === $goString(data.a)"},
		{"slice equality", "===", "slice", "(data.b?.length ?? 0) === (data.a?.length ?? 0)"},
		{"map inequality", "!==", "map", "Object.keys(data.b ?? {}).length !== Object.keys(data.a ?? {}).length"},
		{"time ordering", ">", "time.Time", "$goTimeCompare(data.b, data.a) > 0"},
		{"float32 decoding", "===", "float32", "Math.fround(data.b) === Math.fround(data.a)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fields := map[string]typemap.Field{"a": {GoKind: tc.kind}, "b": {GoKind: tc.kind}}
			got := zodRefinementPredicate(typemap.Refinement{Field: "b", OtherField: "a", Op: tc.op}, fields)
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestRefinementOmissionRespectsTagOrder(t *testing.T) {
	fields := map[string]typemap.Field{
		"a": {GoKind: "int"},
		"b": {GoKind: "int", ValidateOmitempty: true, Validate: []typemap.ValidateRule{
			{Tag: "gtfield", Param: "A"}, {Tag: "omitempty"}, {Tag: "gtfield", Param: "A"},
		}},
	}
	before := zodRefinementPredicate(typemap.Refinement{RuleIndex: 1, Field: "b", OtherField: "a", Op: ">"}, fields)
	after := zodRefinementPredicate(typemap.Refinement{RuleIndex: 3, Field: "b", OtherField: "a", Op: ">"}, fields)
	if before != "(data.b ?? 0) > data.a" {
		t.Fatalf("omitempty incorrectly skipped an earlier validator: %s", before)
	}
	if after != "((data.b ?? 0) === 0 || ((data.b ?? 0) > data.a))" {
		t.Fatalf("omitempty did not skip a later validator: %s", after)
	}
}

func TestRefinementMissingTargetDoesNotBindJSONNames(t *testing.T) {
	fields := map[string]typemap.Field{"a": {GoKind: "int"}, "b": {GoKind: "int"}}
	for _, op := range []string{"===", "!==", ">", ">=", "<", "<="} {
		ref := typemap.Refinement{Field: "b", OtherField: "a", Op: op, MissingTarget: true}
		want := "false"
		if op == "!==" {
			want = "true"
		}
		if got := zodRefinementPredicate(ref, fields); got != want {
			t.Errorf("missing target %s: got %q, want %q", op, got, want)
		}
	}
}

func TestRefinementHelpersIncludeNestedTimesOnce(t *testing.T) {
	inner := &typemap.TypeDef{Fields: []typemap.Field{{Name: "a", GoKind: "time.Time"}, {Name: "b", GoKind: "time.Time"}},
		Refinements: []typemap.Refinement{{Field: "a", OtherField: "b", Op: "==="}}}
	defs := map[string]typemap.TypeDef{"Input": {Fields: []typemap.Field{{Name: "one", Inline: inner}, {Name: "many", Element: &typemap.ElementType{Inline: inner}}}}}
	var output bytes.Buffer
	writeZodRefinementHelpers(newErrWriter(&output), defs, map[string]bool{"Input": true})
	if count := strings.Count(output.String(), "function $goTimeCompare("); count != 1 {
		t.Fatalf("time helper defined %d times:\n%s", count, &output)
	}
	output.Reset()
	writeZodRefinementHelpers(newErrWriter(&output), defs, nil)
	if output.Len() != 0 {
		t.Fatal("unreachable schemas should not emit runtime helpers")
	}
}

func TestRefinementGroupKeepsSinglePredicateAndInheritedScope(t *testing.T) {
	fields := map[string]typemap.Field{"a": {GoKind: "int", Optional: true}, "b": {GoKind: "int", Optional: true}}
	ref := typemap.Refinement{Field: "b", WhenAnyPresent: []string{"a", "b"}, Alternatives: []typemap.Refinement{
		{Field: "b", OtherField: "a", Op: "==="},
		{Field: "b", ScalarRule: &typemap.ValidateRule{Tag: "eq", Param: "0"}},
	}}
	predicate := zodRefinementPredicate(ref, fields)
	if !strings.HasPrefix(predicate, "!((data.a !== undefined || data.b !== undefined)) || ") || !strings.Contains(predicate, " || ") {
		t.Fatalf("group lost inherited presence guard or OR structure: %s", predicate)
	}
	if message := zodRefinementMessage(ref); message != "b must be === a or b must satisfy eq=0" {
		t.Fatalf("unclear grouped rule message: %s", message)
	}
}

func TestRefinementJSONStringUsesPreciseGuardedBigInt(t *testing.T) {
	fields := map[string]typemap.Field{"a": {GoKind: "int64", JSONString: true}, "b": {GoKind: "int64", JSONString: true}}
	got := zodRefinementPredicate(typemap.Refinement{Field: "b", OtherField: "a", Op: "==="}, fields)
	if !strings.Contains(got, `BigInt((data.b === "null" ? "0" : data.b)) === BigInt((data.a === "null" ? "0" : data.a))`) || !strings.Contains(got, "catch { return false; }") {
		t.Fatalf("comparison must preserve precision and never throw for malformed wire values: %s", got)
	}
}
