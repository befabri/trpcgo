package codegen

import (
	"bytes"
	"reflect"
	"strings"
	"testing"

	"github.com/befabri/trpcgo/internal/typemap"
)

func TestExtendsBaseExprStandardAndMini(t *testing.T) {
	standard := extendsBaseExpr([]string{"Base", "Partial<Audit>", "Extra"}, typemap.ZodStandard)
	if standard != "BaseSchema.merge(AuditSchema.partial()).merge(ExtraSchema).extend(" {
		t.Errorf("standard extends = %q", standard)
	}

	mini := extendsBaseExpr([]string{"Base", "Partial<Audit>", "Extra"}, typemap.ZodMini)
	if mini != "z.extend(z.merge(z.merge(BaseSchema, z.partial(AuditSchema)), ExtraSchema), " {
		t.Errorf("mini extends = %q", mini)
	}

	if got := extendsBaseExpr([]string{"Base"}, typemap.ZodStandard); got != "BaseSchema.extend(" {
		t.Errorf("single standard extends = %q", got)
	}
	if got := extendsBaseExpr([]string{"Base"}, typemap.ZodMini); got != "z.extend(BaseSchema, " {
		t.Errorf("single mini extends = %q", got)
	}
}

func TestFieldToZodComplexTypes(t *testing.T) {
	tests := []struct {
		name   string
		field  typemap.Field
		style  typemap.ZodStyle
		cycles map[string]bool
		want   string
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
			name:  "record with nested generic value",
			field: typemap.Field{Type: "Record<string, Box<User>>"},
			style: typemap.ZodStandard,
			want:  "z.record(z.string(), BoxSchema)",
		},
		{
			name:  "optional record mini",
			field: typemap.Field{Type: "Record<string, User>", Optional: true},
			style: typemap.ZodMini,
			want:  "z.optional(z.record(z.string(), UserSchema))",
		},
		{
			name: "array constraints mini",
			field: typemap.Field{Type: "number[]", ElementGoKind: "int", Validate: []typemap.ValidateRule{
				{Tag: "min", Param: "1"},
				{Tag: "max", Param: "3"},
				{Tag: "len", Param: "2"},
			}, Optional: true},
			style: typemap.ZodMini,
			want:  "z.optional(z.array(z.int()).check(z.minLength(1), z.maxLength(3), z.length(2)))",
		},
		{
			name: "array constraints normalize validator length params",
			field: typemap.Field{Type: "string[]", ElementGoKind: "string", Validate: []typemap.ValidateRule{
				{Tag: "min", Param: "0x10"},
			}},
			style: typemap.ZodStandard,
			want:  "z.array(z.string()).min(16)",
		},
		{
			name: "pointer string element required does not imply non-empty",
			field: typemap.Field{Type: "string[]", ElementGoKind: "string", ElementIsPointer: true, ElementValidate: []typemap.ValidateRule{
				{Tag: "required"},
			}},
			style: typemap.ZodStandard,
			want:  "z.array(z.string())",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := fieldToZod(tt.field, tt.cycles, tt.style); got != tt.want {
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
	if err := WriteZodSchemas(&buf, procs, defs, typemap.ZodStandard); err != nil {
		t.Fatal(err)
	}
	output := buf.String()
	for _, want := range []string{
		"export const IDSchema = z.string().meta({ id: \"ID\" });",
		"BaseSchema.merge(AuditSchema.partial()).extend({",
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
	if err := WriteZodSchemas(&buf, procs, defs, typemap.ZodStandard); err != nil {
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

func TestWriteZodSingleNumericUnionUsesLiteral(t *testing.T) {
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
	if err := WriteZodSchemas(&buf, procs, defs, typemap.ZodStandard); err != nil {
		t.Fatal(err)
	}
	output := buf.String()
	if !strings.Contains(output, "export const PrioritySchema = z.literal(1)") {
		t.Fatalf("single numeric union should use z.literal, got:\n%s", output)
	}
	if strings.Contains(output, "z.union([z.literal(1)])") {
		t.Fatalf("single numeric union emitted invalid one-option union:\n%s", output)
	}
}
