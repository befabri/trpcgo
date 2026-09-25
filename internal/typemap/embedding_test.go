package typemap

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"reflect"
	"testing"

	"github.com/befabri/trpcgo/internal/typemap/testdata/embedding"
)

func TestEmbeddedFieldShadowedByOuterJSONField(t *testing.T) {
	pkg := types.NewPackage("example.com/input", "input")
	base := types.NewNamed(types.NewTypeName(token.NoPos, pkg, "Base", nil), types.NewStruct(
		[]*types.Var{types.NewField(token.NoPos, pkg, "Value", types.Typ[types.String], false)},
		[]string{`json:"value"`},
	), nil)
	input := types.NewNamed(types.NewTypeName(token.NoPos, pkg, "Input", nil), types.NewStruct(
		[]*types.Var{
			types.NewField(token.NoPos, pkg, "Base", base, true),
			types.NewField(token.NoPos, pkg, "Value", types.Typ[types.Int], false),
		},
		[]string{"", `json:"value"`},
	), nil)

	m := NewMapper(nil)
	m.Convert(input)
	for _, def := range m.Defs() {
		if def.Name != "Input" {
			continue
		}
		if len(def.Fields) != 1 || def.Fields[0].Name != "value" || def.Fields[0].Type != "number" {
			t.Fatalf("fields = %+v; encoding/json selects only the outer numeric value", def.Fields)
		}
		return
	}
	t.Fatal("Input definition was not generated")
}

func TestJSONFieldDominance(t *testing.T) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "testdata/embedding/types.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := new(types.Config).Check("example.com/embedding", fset, []*ast.File{file}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range embedding.Cases {
		name := reflect.TypeOf(value).Name()
		t.Run(name, func(t *testing.T) {
			data, err := json.Marshal(value)
			if err != nil {
				t.Fatal(err)
			}
			var encoded map[string]any
			if err := json.Unmarshal(data, &encoded); err != nil {
				t.Fatal(err)
			}
			m := NewMapper(nil)
			m.Convert(pkg.Scope().Lookup(name).Type())
			for _, def := range m.Defs() {
				if def.Name != name {
					continue
				}
				if len(def.Extends) != 0 || len(def.Fields) != len(encoded) {
					t.Fatalf("fields=%+v extends=%v; JSON=%s", def.Fields, def.Extends, data)
				}
				for _, field := range def.Fields {
					v, ok := encoded[field.Name]
					if !ok {
						t.Fatalf("field %q is absent from JSON=%s", field.Name, data)
					}
					want := map[reflect.Kind]string{reflect.String: "string", reflect.Bool: "boolean", reflect.Float64: "number"}[reflect.TypeOf(v).Kind()]
					if !ok || field.Type != want {
						t.Errorf("field=%+v disagrees with JSON=%s", field, data)
					}
				}
				return
			}
			t.Fatal("type definition missing")
		})
	}
}

func TestBoundFieldReferencesPreserveRulePositionAndMissingTarget(t *testing.T) {
	rules := []ValidateRule{{Tag: "gtfield", Param: "A"}, {Tag: "omitempty"}, {Tag: "gtfield", Param: "Missing"}}
	refs := BindFieldReferences(rules, "2", func(name string) ([]int, bool) { return []int{3}, name == "A" })
	if len(refs) != 2 || refs[0].RuleIndex != 1 || refs[0].OtherPath != "2.3" || refs[1].RuleIndex != 3 || refs[1].OtherPath != "" || refs[1].OtherName != "Missing" {
		t.Fatalf("incorrect bound references: %#v", refs)
	}
	_, _, _, refinements := ResolveFields([]FieldCandidate{
		{Field: Field{Name: "a"}, Path: "2.3"},
		{Field: Field{Name: "b"}, Path: "2.4", References: refs},
	}, nil, false)
	if len(refinements) != 2 || refinements[0].MissingTarget || !refinements[1].MissingTarget || refinements[1].RuleIndex != 3 || refinements[1].OtherField != "Missing" {
		t.Fatalf("incorrect resolved references: %#v", refinements)
	}
}

func TestBoundFieldReferencesPreserveMixedAlternatives(t *testing.T) {
	rule := ValidateRule{Alternatives: []ValidateRule{{Tag: "eqfield", Param: "A"}, {Tag: "eq", Param: "0"}}}
	refs := BindFieldReferences([]ValidateRule{rule}, "", func(name string) ([]int, bool) { return []int{0}, name == "A" })
	_, _, _, resolved := ResolveFields([]FieldCandidate{
		{Field: Field{Name: "a"}, Path: "0"},
		{Field: Field{Name: "value"}, Path: "1", References: refs},
	}, nil, false)
	if len(resolved) != 1 || len(resolved[0].Alternatives) != 2 || resolved[0].Alternatives[0].OtherField != "a" || resolved[0].Alternatives[1].ScalarRule == nil || resolved[0].Alternatives[1].ScalarRule.Param != "0" {
		t.Fatalf("mixed OR binding lost alternatives: %#v", resolved)
	}
	_, _, _, omitted := ResolveFields([]FieldCandidate{
		{Field: Field{Name: "a", ZodOmit: true}, Path: "0"},
		{Field: Field{Name: "value"}, Path: "1", References: refs},
	}, nil, false)
	if len(omitted) != 0 {
		t.Fatalf("OR with an omitted target must be omitted as a whole: %#v", omitted)
	}
}

// A target that exists in Go but never decodes from JSON keeps its zero value,
// so the refinement compares against that instead of being dropped or failing.
func TestResolveFieldsBindsHiddenTargets(t *testing.T) {
	refs := BindFieldReferences([]ValidateRule{{Tag: "gtefield", Param: "max"}}, "", func(name string) ([]int, bool) { return []int{1}, name == "max" })
	_, _, _, resolved := ResolveFields([]FieldCandidate{
		{Field: Field{Name: "count", GoKind: "int"}, Path: "0", References: refs},
		{Field: Field{GoName: "max", GoKind: "int"}, Path: "1", Hidden: true},
	}, nil, false)
	if len(resolved) != 1 || resolved[0].OtherHidden == nil || resolved[0].OtherHidden.GoKind != "int" || resolved[0].OtherField != "max" || resolved[0].MissingTarget {
		t.Fatalf("hidden target was not bound: %#v", resolved)
	}
}
