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
