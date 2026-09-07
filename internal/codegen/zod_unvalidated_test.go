package codegen

import (
	"testing"

	"github.com/befabri/trpcgo/internal/typemap"
)

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
			got := zodUnvalidatedTypes(defs, map[string]bool{"Input": true, "Profile": true, "Address": true})
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
