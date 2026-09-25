package codegen

import (
	"bytes"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/befabri/trpcgo/internal/typemap"
)

func TestFieldToZodComplexTypes(t *testing.T) {
	tests := []struct {
		name  string
		field typemap.Field
		style typemap.ZodStyle
		want  string
	}{
		{
			name:  "optional named reference standard",
			field: typemap.Field{Type: "User", Optional: true},
			style: typemap.ZodStandard,
			want:  "UserSchema.optional()",
		},
		{
			name:  "optional named reference mini",
			field: typemap.Field{Type: "User", Optional: true},
			style: typemap.ZodMini,
			want:  "z.optional(UserSchema)",
		},
		{
			name:  "record with concrete generic schema metadata",
			field: typemap.Field{Type: "Record<string, Box<User>>", Element: &typemap.ElementType{Type: "BoxUser"}},
			style: typemap.ZodStandard,
			want:  "z.record(z.string(), BoxUserSchema)",
		},
		{
			name:  "optional record mini",
			field: typemap.Field{Type: "Record<string, User>", Optional: true},
			style: typemap.ZodMini,
			want:  "z.optional(z.record(z.string(), UserSchema))",
		},
		{
			name: "array constraints mini",
			field: typemap.Field{Type: "number[]", Element: &typemap.ElementType{GoKind: "int"}, Validate: []typemap.ValidateRule{
				{Tag: "min", Param: "1"},
				{Tag: "max", Param: "3"},
				{Tag: "len", Param: "2"},
			}, Optional: true},
			style: typemap.ZodMini,
			want:  "(((([]).length >= 1) && (([]).length <= 3) && (([]).length === 2)) ? z.optional(z.array(z.int()).check(z.minLength(1), z.maxLength(3), z.length(2))) : z.array(z.int()).check(z.minLength(1), z.maxLength(3), z.length(2)))",
		},
		{
			name: "array constraints normalize validator length params",
			field: typemap.Field{Type: "string[]", Element: &typemap.ElementType{GoKind: "string"}, Validate: []typemap.ValidateRule{
				{Tag: "min", Param: "0x10"},
			}},
			style: typemap.ZodStandard,
			want:  "z.array(z.string()).check(z.minLength(16))",
		},
		{
			name: "pointer string element required does not imply non-empty",
			field: typemap.Field{Type: "string[]", Element: &typemap.ElementType{GoKind: "string", IsPointer: true}, ElementValidate: []typemap.ValidateRule{
				{Tag: "required"},
			}},
			style: typemap.ZodStandard,
			want:  "z.array(z.string())",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := fieldToZod(tt.field, tt.style); got != tt.want {
				t.Errorf("fieldToZod() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSplitTopLevelAndExtractTypeRefs(t *testing.T) {
	parts := splitTopLevel("string, Record<string, User>, Box<A, B>", ',')
	wantParts := []string{"string", " Record<string, User>", " Box<A, B>"}
	if !reflect.DeepEqual(parts, wantParts) {
		t.Errorf("splitTopLevel = %#v, want %#v", parts, wantParts)
	}

	tests := []struct {
		typ  string
		want []string
	}{
		{"string", nil},
		{"User[]", []string{"User"}},
		{"(User)[]", []string{"User"}},
		{"Record<string, Box<User>>", []string{"Box", "User"}},
		{"Page<User, Account>", []string{"Page", "User", "Account"}},
		{"{ id: string }", nil},
		{"User | Account | string", []string{"User", "Account"}},
	}
	for _, tt := range tests {
		if got := extractTypeRefs(tt.typ); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("extractTypeRefs(%q) = %#v, want %#v", tt.typ, got, tt.want)
		}
	}
}

func TestWriteZodAliasAndExtendedObjectPaths(t *testing.T) {
	procs := []ProcEntry{{Path: "create", ProcType: "mutation", InputTS: "Child", OutputTS: "void"}}
	defs := []typemap.TypeDef{
		{Name: "ID", Kind: typemap.TypeDefAlias, AliasOf: "string"},
		{Name: "Base", Kind: typemap.TypeDefInterface, Fields: []typemap.Field{{Name: "id", Type: "ID"}}},
		{Name: "Audit", Kind: typemap.TypeDefInterface, Fields: []typemap.Field{{Name: "createdAt", Type: "string", GoKind: "string"}}},
		{Name: "Child", Kind: typemap.TypeDefInterface, Extends: []string{"Base", "Partial<Audit>"}, Fields: []typemap.Field{{Name: "name", Type: "string", GoKind: "string"}}},
	}

	var buf bytes.Buffer
	if err := WriteZodSchemas(&buf, procs, defs, typemap.ZodStandard, ZodOptions{}); err != nil {
		t.Fatal(err)
	}
	output := buf.String()
	for _, want := range []string{
		"export const IDSchema = z.string().meta({ id: \"ID\" });",
		"export const ChildSchema = z.strictObject({\n  id: IDSchema,\n  createdAt: z.string().optional(),\n  name: z.string(),",
		"id: IDSchema",
		"name: z.string()",
	} {
		if !strings.Contains(output, want) {
			t.Errorf("output missing %q:\n%s", want, output)
		}
	}
}

func TestWriteZodCrossFieldUsesSafePropertyAccess(t *testing.T) {
	procs := []ProcEntry{{Path: "create", ProcType: "mutation", InputTS: "Window", OutputTS: "void"}}
	defs := []typemap.TypeDef{
		{
			Name: "Window",
			Kind: typemap.TypeDefInterface,
			Fields: []typemap.Field{
				{Name: "start-date", Type: "number", GoKind: "int"},
				{Name: "end-date", Type: "number", GoKind: "int"},
			},
			Refinements: []typemap.Refinement{
				{Field: "end-date", Op: ">=", OtherField: "start-date", Tag: "gtefield"},
			},
		},
	}

	var buf bytes.Buffer
	if err := WriteZodSchemas(&buf, procs, defs, typemap.ZodStandard, ZodOptions{}); err != nil {
		t.Fatal(err)
	}
	output := buf.String()
	if !strings.Contains(output, `data["end-date"] >= data["start-date"]`) {
		t.Fatalf("cross-field refinement should use bracket property access:\n%s", output)
	}
	if strings.Contains(output, `data."end-date"`) {
		t.Fatalf("cross-field refinement emitted invalid dot access:\n%s", output)
	}
	if !strings.Contains(output, `path: ["end-date"]`) {
		t.Fatalf("cross-field refinement path should be quoted safely:\n%s", output)
	}
}

func TestUnsupportedCommentEscapesCommentTerminators(t *testing.T) {
	comment := unsupportedComment([]typemap.ValidateRule{{Tag: "custom", Param: "x */ alert(1)"}})
	if strings.Contains(comment[:len(comment)-2], "*/") {
		t.Fatalf("unsupported comment contains embedded terminator: %q", comment)
	}
	if !strings.Contains(comment, "x * / alert(1)") {
		t.Fatalf("unsupported comment did not preserve sanitized text: %q", comment)
	}
}

func TestWriteZodSingleNumericConstantKeepsInputOpen(t *testing.T) {
	procs := []ProcEntry{{Path: "create", ProcType: "mutation", InputTS: "Input", OutputTS: "void"}}
	defs := []typemap.TypeDef{
		{
			Name:   "Input",
			Kind:   typemap.TypeDefInterface,
			Fields: []typemap.Field{{Name: "priority", Type: "Priority"}},
		},
		{
			Name:         "Priority",
			Kind:         typemap.TypeDefUnion,
			UnionMembers: []string{"1"},
		},
	}

	var buf bytes.Buffer
	if err := WriteZodSchemas(&buf, procs, defs, typemap.ZodStandard, ZodOptions{}); err != nil {
		t.Fatal(err)
	}
	output := buf.String()
	if !strings.Contains(output, "export const PrioritySchema = z.number()") {
		t.Fatalf("single Go constant must not restrict the numeric input, got:\n%s", output)
	}
	if strings.Contains(output, "z.union([z.literal(1)])") {
		t.Fatalf("single numeric union emitted invalid one-option union:\n%s", output)
	}
}

func TestWriteZodRejectsUnresolvedGenericWithoutPartialOutput(t *testing.T) {
	for _, style := range []typemap.ZodStyle{typemap.ZodStandard, typemap.ZodMini} {
		defs := []typemap.TypeDef{{Name: "Input", Kind: typemap.TypeDefInterface, Fields: []typemap.Field{{Name: "values", Type: "Record<string, Box<User>>"}}}}
		var output bytes.Buffer
		err := WriteZodSchemas(&output, []ProcEntry{{InputTS: "Input"}}, defs, style, ZodOptions{})
		if err == nil || !strings.Contains(err.Error(), "Input.values[]") || !strings.Contains(err.Error(), "requires concrete Go type metadata") {
			t.Fatalf("expected contextual unresolved generic error, got %v", err)
		}
		if output.Len() != 0 {
			t.Fatalf("generation wrote a partial module before returning %v: %s", err, output.String())
		}
	}
}

func TestObjectUnknownFieldPolicyPreservesObjectAPI(t *testing.T) {
	for _, style := range []typemap.ZodStyle{typemap.ZodStandard, typemap.ZodMini} {
		for _, allow := range []bool{false, true} {
			t.Run(fmt.Sprintf("style%d/allow%v", style, allow), func(t *testing.T) {
				def := typemap.TypeDef{Name: "Input", Kind: typemap.TypeDefInterface, Fields: []typemap.Field{
					{Name: "value", Type: "string", GoKind: "string"},
					{Name: "supplied", Type: "string", GoKind: "string", ZodOmit: true},
					{Name: "nested", Type: "{ value: string }", GoKind: "struct", Inline: &typemap.TypeDef{Fields: []typemap.Field{{Name: "value", Type: "string", GoKind: "string"}}}},
				}}
				var output bytes.Buffer
				emitter := zodSchemaEmitter{style: style, allowUnknownFields: allow}
				emitter.writeObject(newErrWriter(&output), def, nil)
				imported := "zod"
				if style == typemap.ZodMini {
					imported = "zod/mini"
				}
				program := fmt.Sprintf(`import * as z from %q;
%s
const valid = {value:'ok',nested:{value:'ok'}};
const supplied = InputSchema.parse({...valid,supplied:{by:'transport'}});
if (!supplied.supplied || !InputSchema.shape.supplied) throw new Error('known omitted field was discarded');
for (const input of [{...valid,extra:1},{...valid,nested:{value:'ok',extra:1}}]) {
 const result = z.safeParse(InputSchema,input);
 if(result.success !== Boolean(%t)) throw new Error('unknown field policy mismatch: '+JSON.stringify(input));
 if(result.success && JSON.stringify(result.data)!==JSON.stringify(input)) throw new Error('unknown fields were discarded');
}
if (!InputSchema.shape.value) throw new Error('object shape API lost');
`, imported, output.String(), allow)
				if style == typemap.ZodStandard {
					program += `if(!InputSchema.safeExtend({additional:z.string()}).safeParse({...valid,additional:'ok'}).success) throw new Error('safeExtend unavailable');`
				}
				runJSONHelperProgram(t, map[string]string{"main.ts": program})
			})
		}
	}
}

func TestPublicGoConstantsRemainOpen(t *testing.T) {
	for _, tc := range []struct{ kind, ts, literal string }{{"string", "string", `"known"`}, {"int8", "number", "1"}} {
		def := typemap.TypeDef{Name: "Value", Kind: typemap.TypeDefUnion, UnionMembers: []string{tc.literal}, Underlying: &typemap.Field{Type: tc.ts, GoKind: tc.kind}}
		var output bytes.Buffer
		writeTypeDef(newErrWriter(&output), def)
		open := tc.ts
		if open == "string" {
			open = "(string & {})"
		}
		expected := "export type Value = " + tc.literal + " | " + open + ";"
		if !strings.Contains(output.String(), expected) {
			t.Fatalf("public type must admit unnamed Go values: %s", output.String())
		}
	}
}

func TestZodUnvalidatedTypesFollowValidatorTraversal(t *testing.T) {
	profile := typemap.TypeDef{Name: "Profile", Kind: typemap.TypeDefInterface, Fields: []typemap.Field{
		{Name: "name", Type: "string", GoKind: "string", Validate: []typemap.ValidateRule{{Tag: "required"}}},
		{Name: "address", Type: "Address", GoKind: "struct"},
	}}
	address := typemap.TypeDef{Name: "Address", Kind: typemap.TypeDefInterface, Fields: []typemap.Field{{Name: "city", Type: "string", GoKind: "string"}}}
	element := &typemap.ElementType{Type: "Profile", GoKind: "struct"}
	for _, tc := range []struct {
		name  string
		field typemap.Field
		want  map[string]bool
	}{
		{name: "slice without dive", field: typemap.Field{Name: "items", Type: "Profile[]", GoKind: "slice", Element: element, Validate: []typemap.ValidateRule{{Tag: "min", Param: "1"}}}, want: map[string]bool{"Profile": true, "Address": true}},
		{name: "slice with dive", field: typemap.Field{Name: "items", Type: "Profile[]", GoKind: "slice", Element: element, ElementValidate: []typemap.ValidateRule{}}, want: map[string]bool{}},
		{name: "map without dive", field: typemap.Field{Name: "items", Type: "Record<string, Profile>", GoKind: "map", Element: element, Key: &typemap.ElementType{Type: "string", GoKind: "string"}}, want: map[string]bool{"Profile": true, "Address": true}},
		{name: "structonly field", field: typemap.Field{Name: "profile", Type: "Profile", GoKind: "struct", Validate: []typemap.ValidateRule{{Tag: "structonly"}}}, want: map[string]bool{"Profile": true, "Address": true}},
		{name: "plain struct field", field: typemap.Field{Name: "profile", Type: "Profile", GoKind: "struct"}, want: map[string]bool{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			defs := map[string]typemap.TypeDef{
				"Input":   {Name: "Input", Kind: typemap.TypeDefInterface, Fields: []typemap.Field{tc.field}},
				"Profile": profile,
				"Address": address,
			}
			reachable := map[string]bool{"Input": true, "Profile": true, "Address": true}
			got := zodUnvalidatedTypes(defs, reachable, zodRootCollections(defs, reachable))
			if len(got) != len(tc.want) {
				t.Fatalf("unvalidated = %v, want %v", got, tc.want)
			}
			for name := range tc.want {
				if !got[name] {
					t.Fatalf("unvalidated = %v, want %v", got, tc.want)
				}
			}
		})
	}
}
