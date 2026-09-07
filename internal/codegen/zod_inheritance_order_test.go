package codegen

import (
	"slices"
	"testing"

	"github.com/befabri/trpcgo/internal/typemap"
)

// Inherited fields must sit where their embedded Go field is declared: the
// generated case-insensitive key matcher picks the first field in that order,
// as encoding/json does.
func TestExpandZodInheritanceKeepsGoFieldOrder(t *testing.T) {
	base := typemap.TypeDef{Name: "Emb", Kind: typemap.TypeDefInterface, Fields: []typemap.Field{{Name: "ab", Type: "number", GoKind: "int"}}}
	own := []typemap.Field{{Name: "aB", Type: "number", GoKind: "int"}, {Name: "arr", Type: "number[]", GoKind: "array"}}
	for _, tc := range []struct {
		name      string
		extendsAt []int
		want      []string
	}{
		{name: "declared between own fields", extendsAt: []int{1}, want: []string{"aB", "ab", "arr"}},
		{name: "declared last", extendsAt: []int{2}, want: []string{"aB", "arr", "ab"}},
		{name: "legacy metadata places bases first", want: []string{"ab", "aB", "arr"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			defs := map[string]typemap.TypeDef{
				"Emb":   base,
				"Outer": {Name: "Outer", Kind: typemap.TypeDefInterface, Fields: slices.Clone(own), Extends: []string{"Emb"}, ExtendsAt: tc.extendsAt},
			}
			expanded, err := expandZodInheritance(defs, map[string]bool{"Outer": true, "Emb": true})
			if err != nil {
				t.Fatal(err)
			}
			var got []string
			for _, field := range expanded["Outer"].Fields {
				got = append(got, field.Name)
			}
			if !slices.Equal(got, tc.want) {
				t.Fatalf("field order = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestWritersRejectDuplicateTypeNames(t *testing.T) {
	defs := []typemap.TypeDef{
		{ID: "a/models.User", Name: "User", Kind: typemap.TypeDefInterface, Fields: []typemap.Field{{Name: "name", Type: "string", GoKind: "string"}}},
		{ID: "b/models.User", Name: "User", Kind: typemap.TypeDefInterface, Fields: []typemap.Field{{Name: "email", Type: "string", GoKind: "string"}}},
	}
	procs := []ProcEntry{{Path: "get", ProcType: "query", InputTS: "User", OutputTS: "string"}}
	if err := WriteAppRouter(&discardWriter{}, procs, defs); err == nil {
		t.Fatal("WriteAppRouter accepted two definitions named User")
	}
	if err := WriteZodSchemas(&discardWriter{}, procs, defs, typemap.ZodStandard); err == nil {
		t.Fatal("WriteZodSchemas accepted two definitions named User")
	}
}

type discardWriter struct{}

func (discardWriter) Write(p []byte) (int, error) { return len(p), nil }
