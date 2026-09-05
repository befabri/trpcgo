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
