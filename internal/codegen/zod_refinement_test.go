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
			got := zodRefinementPredicate(typemap.Refinement{Field: "b", OtherField: "a", Op: tc.op}, fields, newZodSchemaChecks())
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestRefinementOmissionRespectsTagOrder(t *testing.T) {
	fields := map[string]typemap.Field{
		"a": {GoKind: "int"},
		"b": {GoKind: "int", Optional: true, ValidateOmitempty: true, Validate: []typemap.ValidateRule{
			{Tag: "gtfield", Param: "A"}, {Tag: "omitempty"}, {Tag: "gtfield", Param: "A"},
		}},
	}
	before := zodRefinementPredicate(typemap.Refinement{RuleIndex: 1, Field: "b", OtherField: "a", Op: ">"}, fields, newZodSchemaChecks())
	after := zodRefinementPredicate(typemap.Refinement{RuleIndex: 3, Field: "b", OtherField: "a", Op: ">"}, fields, newZodSchemaChecks())
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
		if got := zodRefinementPredicate(ref, fields, newZodSchemaChecks()); got != want {
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
	predicate := zodRefinementPredicate(ref, fields, newZodSchemaChecks())
	if !strings.HasPrefix(predicate, "!((data.a !== undefined || data.b !== undefined)) || ") || !strings.Contains(predicate, " || ") {
		t.Fatalf("group lost inherited presence guard or OR structure: %s", predicate)
	}
	if message := zodRefinementMessage(ref); message != "b must be === a or b must satisfy eq=0" {
		t.Fatalf("unclear grouped rule message: %s", message)
	}
}

func TestRefinementJSONStringUsesPreciseGuardedBigInt(t *testing.T) {
	fields := map[string]typemap.Field{"a": {GoKind: "int64", JSONString: true}, "b": {GoKind: "int64", JSONString: true}}
	got := zodRefinementPredicate(typemap.Refinement{Field: "b", OtherField: "a", Op: "==="}, fields, newZodSchemaChecks())
	if !strings.Contains(got, `BigInt((data.b === "null" ? "0" : data.b)) === BigInt((data.a === "null" ? "0" : data.a))`) || !strings.Contains(got, "catch { return false; }") {
		t.Fatalf("comparison must preserve precision and never throw for malformed wire values: %s", got)
	}
}

// Only a predicate that embeds an operand able to throw is wrapped in
// try/catch. The operand builders declare that, so a decoder whose text
// changes, or is hoisted into a helper, keeps its protection.
func TestRefinementWrapsOnlyThrowingOperands(t *testing.T) {
	wrapped := func(predicate string) bool { return strings.HasPrefix(predicate, "(() => { try { return ") }
	for _, tc := range []struct {
		name        string
		left, right typemap.Field
		want        bool
	}{
		{"plain integers", typemap.Field{GoKind: "int"}, typemap.Field{GoKind: "int"}, false},
		{"quoted integer decodes with BigInt", typemap.Field{GoKind: "int64", JSONString: true}, typemap.Field{GoKind: "int64", JSONString: true}, true},
		{"plain integer converted to meet a quoted one", typemap.Field{GoKind: "int64"}, typemap.Field{GoKind: "int64", JSONString: true}, true},
		{"quoted string decodes with JSON.parse", typemap.Field{GoKind: "string", JSONString: true}, typemap.Field{GoKind: "string", JSONString: true}, true},
		{"quoted float decoder throws on invalid syntax", typemap.Field{GoKind: "float64", JSONString: true}, typemap.Field{GoKind: "float64", JSONString: true}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fields := map[string]typemap.Field{"a": tc.right, "b": tc.left}
			got := zodRefinementPredicate(typemap.Refinement{Field: "b", OtherField: "a", Op: "==="}, fields, newZodSchemaChecks())
			if wrapped(got) != tc.want {
				t.Fatalf("wrapped = %v, want %v: %s", !tc.want, tc.want, got)
			}
		})
	}
	// A skipped quoted pointer decodes its value for the zero test, so the
	// omission alone needs the wrapper.
	quoted := typemap.Field{GoKind: "int64", JSONString: true, IsPointer: true, Optional: true, Validate: []typemap.ValidateRule{{Tag: "omitzero"}, {Tag: "required"}}}
	fields := map[string]typemap.Field{"b": quoted}
	got := zodRefinementPredicate(typemap.Refinement{RuleIndex: 2, Field: "b", ScalarRule: &typemap.ValidateRule{Tag: "required"}}, fields, newZodSchemaChecks())
	if !wrapped(got) {
		t.Fatalf("omitzero on a quoted pointer must catch its decoder: %s", got)
	}
}

// omitzero skips the zero value a pointer points to, tested as reflect does
// on the decoded Go value; omitempty only skips a nil pointer.
func TestRefinementOmitzeroDereferencesPointers(t *testing.T) {
	two := int64(2)
	for _, tc := range []struct {
		name  string
		field typemap.Field
		want  string
	}{
		{"quoted integer compares as bigint", typemap.Field{GoKind: "int64", JSONString: true, IsPointer: true}, `data.b == null || data.b === "null" || BigInt((data.b === "null" ? "0" : data.b)) === 0n`},
		{"fixed array tests every element", typemap.Field{GoKind: "array", Type: "number[]", ArrayLen: &two, IsPointer: true}, "data.b == null || ((value: unknown) => { try { return "},
		{"slice tests its length", typemap.Field{GoKind: "slice", Type: "number[]", IsPointer: true}, "data.b == null || (data.b?.length ?? 0) === 0"},
		{"map tests its size", typemap.Field{GoKind: "map", Type: "Record<string, number>", IsPointer: true}, "data.b == null || Object.keys(data.b ?? {}).length === 0"},
		{"time tests Go's zero spelling", typemap.Field{GoKind: "time.Time", IsPointer: true}, `text.endsWith("Z")`},
		{"string compares with its zero", typemap.Field{GoKind: "string", IsPointer: true}, `data.b == null || data.b === ""`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			field := tc.field
			field.Validate = []typemap.ValidateRule{{Tag: "omitzero"}, {Tag: "eqfield", Param: "A"}}
			operand := zodRefinementOperand("b", field)
			got := zodRefinementSkip(typemap.Refinement{RuleIndex: 2, Field: "b"}, field, operand.value)
			if !strings.Contains(got, tc.want) {
				t.Fatalf("omitzero skip = %s, want it to contain %s", got, tc.want)
			}
			field.Validate[0].Tag = "omitempty"
			nilOnly := "data.b == null"
			if field.JSONString {
				nilOnly += ` || data.b === "null"`
			}
			if got := zodRefinementSkip(typemap.Refinement{RuleIndex: 2, Field: "b"}, field, operand.value); got != nilOnly {
				t.Fatalf("omitempty must only skip a nil pointer: %s", got)
			}
		})
	}
	// A non-pointer time is zero only in Go's zero UTC spelling; an equal
	// instant with an explicit offset records a Location and is validated.
	field := typemap.Field{GoKind: "time.Time", Validate: []typemap.ValidateRule{{Tag: "omitempty"}, {Tag: "eqfield", Param: "A"}}}
	got := zodRefinementSkip(typemap.Refinement{RuleIndex: 2, Field: "b"}, field, "data.b")
	if strings.Contains(got, "$goTimeCompare") || !strings.Contains(got, `text.endsWith("Z")`) {
		t.Fatalf("time omission must test reflect's zero, not the instant: %s", got)
	}
}
