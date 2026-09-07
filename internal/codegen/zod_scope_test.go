package codegen

import (
	"bytes"
	"strconv"
	"strings"
	"testing"

	"github.com/befabri/trpcgo/internal/typemap"
)

func TestZodScopeErrorsBeforeOutput(t *testing.T) {
	field := func(ts, kind, tag string) typemap.Field {
		return typemap.Field{Name: "value", Type: ts, GoKind: kind, Validate: typemap.ParseValidateTag("validate:" + strconv.Quote(tag))}
	}
	for _, tc := range []struct {
		name    string
		field   typemap.Field
		path    string
		message string
	}{
		{"dive scalar", field("string", "string", "dive,min=1"), "Input.value", "dive requires"},
		{"keys array", field("string[]", "slice", "dive,keys,min=1,endkeys"), "Input.value", "keys/endkeys requires a map"},
		{"unclosed keys", field("Record<string, string>", "map", "dive,keys,min=1"), "Input.value", "endkeys"},
		{"misplaced endkeys", field("string", "string", "endkeys"), "Input.value", "endkeys"},
		{"nested field path", field("string", "string", "eqfield=Parent.Name"), "Input.value", "nested field references"},
		{"indexed field path", field("string", "string", "eqfield=Items[0]"), "Input.value", "nested field references"},
		{"nested path in OR", field("string", "string", "email|eqfield=Parent.Name"), "Input.value", "nested field references"},
		{"empty rule", field("string", "string", "min=1,,max=3"), "Input.value", "empty validation rule"},
		{"empty OR branch", field("string", "string", "email|"), "Input.value", "empty validation rule"},
		{"implicit anonymous element", typemap.Field{Name: "value", Type: "({ bad: string })[]", GoKind: "slice", Element: &typemap.ElementType{
			Type: "{ bad: string }", GoKind: "struct", Inline: &typemap.TypeDef{Kind: typemap.TypeDefInterface, Fields: []typemap.Field{
				{Name: "bad", Type: "string", GoKind: "string", Validate: []typemap.ValidateRule{{Tag: "dive"}}},
			}},
		}}, "Input.value[].bad", "dive requires"},
		{"composite uniqueness", typemap.Field{Name: "value", Type: "Record<string, Item>", GoKind: "map", Validate: []typemap.ValidateRule{{Tag: "unique"}}, Element: &typemap.ElementType{Type: "Item", GoKind: "struct"}}, "Input.value", "unique"},
		{"unsupported float map key", typemap.Field{Name: "value", Type: "Record<number, string>", GoKind: "map", Key: &typemap.ElementType{Type: "number", GoKind: "float64"}}, "Input.value{key}", "only string and integer keys are supported"},
		{"unsupported pointer map key", typemap.Field{Name: "value", Type: "Record<number, string>", GoKind: "map", Key: &typemap.ElementType{Type: "number", GoKind: "int", IsPointer: true}}, "Input.value{key}", "only string and integer keys are supported"},
		{"unsupported nested float map key", typemap.Field{Name: "value", Type: "Record<string, Record<number, string>>", GoKind: "map", Key: &typemap.ElementType{Type: "string", GoKind: "string"}, Element: &typemap.ElementType{
			Type: "Record<number, string>", GoKind: "map", Key: &typemap.ElementType{Type: "number", GoKind: "float32"}, Element: &typemap.ElementType{Type: "string", GoKind: "string"},
		}}, "Input.value[]{key}", "only string and integer keys are supported"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, style := range []typemap.ZodStyle{typemap.ZodStandard, typemap.ZodMini} {
				var out bytes.Buffer
				err := WriteZodSchemas(&out, []ProcEntry{{InputTS: "Input"}}, []typemap.TypeDef{{Name: "Input", Kind: typemap.TypeDefInterface, Fields: []typemap.Field{tc.field}}}, style)
				if err == nil || !strings.Contains(err.Error(), tc.path) || !strings.Contains(err.Error(), tc.message) {
					t.Fatalf("error = %v, want path %q and %q", err, tc.path, tc.message)
				}
				if out.Len() != 0 {
					t.Fatalf("invalid schema wrote partial output: %s", out.String())
				}
			}
		})
	}
}

func TestZodScopeTraversalStopsImplicitMetadataCycles(t *testing.T) {
	// Descriptors can be shared or recursive independently of their TS spelling.
	// Traversal must stop a repeated descriptor even when its type is a record.
	recursive := &typemap.ElementType{Type: "Record<string, Recursive>", GoKind: "map", Key: &typemap.ElementType{Type: "string", GoKind: "string"}}
	recursive.Element = recursive
	field := zodElementField(recursive.Type, recursive)
	field.Name = "value"
	defs := map[string]typemap.TypeDef{"Input": {Kind: typemap.TypeDefInterface, Fields: []typemap.Field{field}}}
	if err := validateZodScopes([]string{"Input"}, defs); err != nil {
		t.Fatal(err)
	}

	// An explicit dive still advances its finite rule program through a reused
	// descriptor. Do not let the cycle guard conceal an invalid nested key rule.
	field.Validate = typemap.ParseValidateTag(`validate:"dive,dive,keys,dive,endkeys"`)
	defs["Input"] = typemap.TypeDef{Kind: typemap.TypeDefInterface, Fields: []typemap.Field{field}}
	err := validateZodScopes([]string{"Input"}, defs)
	if err == nil || !strings.Contains(err.Error(), "Input.value[]{key}") || !strings.Contains(err.Error(), "dive requires") {
		t.Fatalf("explicit nested scope was skipped: %v", err)
	}
}

func TestZodScopeTraversalChecksUnannotatedNestedTypes(t *testing.T) {
	// Nil descriptors do not identify a cycle. All levels of a manually supplied
	// TypeScript container still need checking, even without validate:dive.
	field := typemap.Field{Name: "value", Type: "Record<string, Record<string, Box<string>[]>>", GoKind: "map"}
	defs := map[string]typemap.TypeDef{"Input": {Kind: typemap.TypeDefInterface, Fields: []typemap.Field{field}}}
	err := validateZodScopes([]string{"Input"}, defs)
	if err == nil || !strings.Contains(err.Error(), "Input.value[][][]") || !strings.Contains(err.Error(), "requires concrete Go type metadata") {
		t.Fatalf("nested type validation was skipped: %v", err)
	}
}
