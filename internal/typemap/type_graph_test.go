package typemap

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"slices"
	"strings"
	"testing"
)

func graphTypes(t *testing.T, source string) *types.Package {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "graph.go", "package graph\n"+source, 0)
	if err != nil {
		t.Fatal(err)
	}
	p, err := new(types.Config).Check("example.com/graph", fset, []*ast.File{f}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestRecursiveNamedContainersRetainIdentity(t *testing.T) {
	pkg := graphTypes(t, "type Tree map[string]Tree\ntype List []List\ntype Nodes map[string]Edges\ntype Edges []Nodes")
	mapper := NewMapper(nil)
	for _, name := range []string{"Tree", "List", "Nodes", "Edges"} {
		if got := mapper.Resolve(mapper.Convert(pkg.Scope().Lookup(name).Type())); got != name {
			t.Fatalf("%s mapped to %s", name, got)
		}
	}
	defs := mapper.Defs()
	expected := map[string]string{"Tree": "Record<string, Tree>", "List": "List[]", "Nodes": "Record<string, Edges>", "Edges": "Nodes[]"}
	for _, def := range defs {
		if def.Kind != TypeDefAlias || def.AliasOf != expected[def.Name] || def.Underlying == nil || def.Underlying.Element == nil {
			t.Errorf("recursive definition lost metadata: %#v", def)
		}
	}
	if len(defs) != len(expected) {
		t.Fatalf("got %d definitions, want %d", len(defs), len(expected))
	}
}

func TestConcreteGenericMetadataPreservesKindsAndDeclarations(t *testing.T) {
	pkg := graphTypes(t, "type Box[T any] struct { Value T; Next *Box[T] }; type Values[T any] []T; type Input struct { Text Box[string]; Number Box[int8]; Values Values[int8] }")
	mapper := NewMapper(nil)
	mapper.Convert(pkg.Scope().Lookup("Input").Type())
	defs := mapper.Defs()
	var boxes, values int
	for _, def := range defs {
		if def.Name == "Input" {
			if def.Fields[0].Type != "Box<string>" || def.Fields[1].Type != "Box<number>" || def.Fields[2].Type != "Values<number>" {
				t.Fatalf("generic TS contract changed: %#v", def.Fields)
			}
		}
		if def.Name == "Box" {
			if len(def.TypeParams) != 1 || def.TypeParams[0] != "T" || def.Fields[0].Type != "T" {
				t.Fatalf("generic declaration was specialized: %#v", def)
			}
			for _, instance := range def.Specializations {
				boxes++
				if len(instance.TypeParams) != 0 || instance.Fields[0].GoKind != "string" && instance.Fields[0].GoKind != "int8" {
					t.Fatalf("invalid concrete schema metadata: %#v", instance)
				}
			}
		}
		if def.Name == "Values" {
			for _, instance := range def.Specializations {
				values++
				if instance.Underlying == nil || instance.Underlying.Element.GoKind != "int8" {
					t.Fatalf("generic container lost element kind: %#v", instance)
				}
			}
		}
	}
	if boxes != 2 || values != 1 {
		t.Fatalf("specializations: boxes=%d, values=%d", boxes, values)
	}
}

func TestAnonymousFieldMetadataAndTokenResolution(t *testing.T) {
	pkg := graphTypes(t, "type Node struct { Inline struct { Count int8 `json:\"count\" validate:\"min=1\"`; Secret string `json:\"secret\" zod_omit:\"true\"`; Next *Node }; Items []struct { Count int8 `validate:\"min=1\"` } }")
	mapper := NewMapper(nil)
	mapper.Convert(pkg.Scope().Lookup("Node").Type())
	first := mapper.Defs()[0]
	inline := first.Fields[0].Inline
	if inline == nil || inline.Fields[0].GoKind != "int8" || len(inline.Fields[0].Validate) != 1 || !inline.Fields[1].ZodOmit || inline.Fields[2].Type != "Node" {
		t.Fatalf("anonymous metadata lost: %#v", inline)
	}
	if first.Fields[1].Element.Inline == nil || len(first.Fields[1].Element.Inline.Fields[0].Validate) != 1 {
		t.Fatalf("element metadata lost: %#v", first.Fields[1])
	}
	// Defs must not leak mutable metadata back into the mapper's graph.
	first.Fields[0].Inline.Fields[0].Type = "corrupted"
	if got := mapper.Defs()[0].Fields[0].Inline.Fields[0].Type; got != "number" {
		t.Fatalf("Defs mutated mapper metadata: %s", got)
	}
}

func TestEnumMapKeysAreSparse(t *testing.T) {
	pkg := graphTypes(t, "type State string; type Input struct { State State; States map[State]int8 }")
	mapper := NewMapper(map[string]TypeMeta{"example.com/graph.State": {ConstValues: []string{`"ready"`, `"done"`}}})
	mapper.Convert(pkg.Scope().Lookup("Input").Type())
	for _, def := range mapper.Defs() {
		if def.Name != "Input" {
			continue
		}
		if def.Fields[0].Type != "State" || def.Fields[1].Type != "Partial<Record<State, number>>" || def.Fields[1].Key.Type != "State" {
			t.Fatalf("enum references or sparse map type lost: %#v", def.Fields)
		}
	}
}

func TestPointerAliasesAndNamedBytesPreserveWireKinds(t *testing.T) {
	pkg := graphTypes(t, "type P = *int8; type Byte uint8; type Phantom[T any] int8; type Input struct { Pointer P; Bytes []Byte; Number Phantom[string] }")
	mapper := NewMapper(nil)
	mapper.Convert(pkg.Scope().Lookup("Input").Type())
	var input TypeDef
	for _, def := range mapper.Defs() {
		if def.Name == "Input" {
			input = def
		}
	}
	if len(input.Fields) != 3 {
		t.Fatalf("unexpected fields: %#v", input.Fields)
	}
	if f := input.Fields[0]; f.GoKind != "int8" || !f.IsPointer || !f.Optional {
		t.Fatalf("pointer alias metadata lost: %#v", f)
	}
	if f := input.Fields[1]; f.GoKind != "[]byte" || f.Type != "string" {
		t.Fatalf("named byte slice should use base64 wire representation: %#v", f)
	}
	if f := input.Fields[2]; f.Type != "number" || f.ZodType != "number" || f.GoKind != "int8" {
		t.Fatalf("phantom generic scalar should retain its underlying kind: %#v", f)
	}
}

func TestGenericAliasUsesConcreteSchemaType(t *testing.T) {
	pkg := graphTypes(t, "type Numeric[T any] = int8; type Input struct { Number Numeric[string] }")
	mapper := NewMapper(map[string]TypeMeta{"example.com/graph.Numeric": {IsAlias: true}})
	mapper.Convert(pkg.Scope().Lookup("Input").Type())
	for _, def := range mapper.Defs() {
		switch def.Name {
		case "Input":
			f := def.Fields[0]
			if f.Type != "Numeric<string>" || f.ZodType != "number" || f.GoKind != "int8" {
				t.Fatalf("generic alias lost scalar schema: %#v", f)
			}
		case "Numeric":
			if len(def.TypeParams) != 1 || def.TypeParams[0] != "T" || def.AliasOf != "number" {
				t.Fatalf("generic alias declaration lost parameters: %#v", def)
			}
		}
	}
}

// Every Go type cycle passes through a named type. A cycle through an
// anonymous struct passes no struct definition that could stop it, so the
// named type itself must be recognised instead of being expanded again.
func TestRecursionThroughAnonymousStructsTerminates(t *testing.T) {
	for _, tc := range []struct {
		name, source, root, def, alias, field string
	}{
		{"slice", "type Menu []struct { Label string `json:\"label\"`; Children Menu `json:\"children\"` }", "Menu", "Menu", "{ label: string; children: Menu }[]", "children"},
		{"map", "type Tree map[string]struct { Kids Tree `json:\"kids\"` }", "Tree", "Tree", "Record<string, { kids: Tree }>", "kids"},
		{"pointer elements", "type Chain []*struct { Next Chain `json:\"next\"` }", "Chain", "Chain", "{ next: Chain }[]", "next"},
		// Schemas use the concrete instance, whose own name stops its body.
		{"generic", "type List[T any] []struct { Value T `json:\"value\"`; Next List[T] `json:\"next\"` }; type Input struct { List List[int8] `json:\"list\"` }", "Input", "List", "{ value: number; next: List<number> }[]", "next"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mapper := NewMapper(nil)
			mapper.Convert(graphTypes(t, tc.source).Scope().Lookup(tc.root).Type())
			var def TypeDef
			for _, candidate := range mapper.Defs() {
				if candidate.Name != tc.def {
					continue
				}
				def = candidate
				if len(candidate.Specializations) == 1 {
					def = candidate.Specializations[0]
				}
			}
			if def.AliasOf != tc.alias || def.Underlying == nil {
				t.Fatalf("recursive definition = %#v, want alias %s", def, tc.alias)
			}
			// The element keeps its anonymous metadata; the recursive field
			// inside it is a reference to the named type, where schemas stop.
			field := inlineField(t, def.Underlying.Element, tc.field)
			if !strings.HasPrefix(field.Type, tc.def) || field.Element != nil || field.Inline != nil {
				t.Fatalf("recursive field was expanded past its named reference: %#v", field)
			}
		})
	}
}

// A named pointer has no definition that a recursive occurrence could name,
// so that occurrence is left untyped rather than expanded forever.
func TestRecursiveNamedPointerIsUntyped(t *testing.T) {
	pkg := graphTypes(t, "type Link *struct { Next Link `json:\"next\"` }; type Input struct { Link Link `json:\"link\"` }")
	mapper := NewMapper(nil)
	mapper.Convert(pkg.Scope().Lookup("Input").Type())
	defs := mapper.Defs()
	if len(defs) != 1 || len(defs[0].Fields) != 1 || defs[0].Fields[0].Type != "{ next: unknown }" {
		t.Fatalf("recursive named pointer = %#v", defs)
	}
}

func inlineField(t *testing.T, element *ElementType, name string) Field {
	t.Helper()
	if element == nil || element.Inline == nil {
		t.Fatalf("element %#v has no anonymous metadata", element)
	}
	for _, f := range element.Inline.Fields {
		if f.Name == name {
			return f
		}
	}
	t.Fatalf("anonymous object %#v has no field %s", element.Inline, name)
	return Field{}
}

// Each parameter carries the TypeScript constraint its Go constraint needs.
// An unrestricted parameter is constrained only where the declaration uses
// it as a map key, directly or through an instantiation.
func TestTypeParamBounds(t *testing.T) {
	pkg := graphTypes(t, `
type Ints interface{ ~int8 | ~int64 }
type Named string
type Text[K ~string, V any] map[K]V
type Numbers[K Ints, V any] map[K]V
type Mixed[K ~string | Ints] []K
type Exact[K string] map[K]bool
type Flags[K ~bool] map[K]bool
type Keyed[K comparable, V any] map[K]V
type Nested[K comparable] struct {
	Inner Keyed[K, int]
	Next  *Nested[K]
}
type Listed[T comparable] []T
type Wide[T ~string | ~[]byte] map[string]T
type Both[K interface{ comparable; ~string | ~int }] map[K]int
type Second[A comparable, B comparable] struct{ Pairs []Keyed[B, A] }
`)
	tests := []struct {
		name string
		want []string
	}{
		{"Text", []string{"string", ""}},
		{"Numbers", []string{"number", ""}},
		{"Mixed", []string{"string | number"}},
		{"Exact", []string{"string"}},
		{"Flags", []string{"boolean"}},
		{"Keyed", []string{"string | number", ""}},
		{"Nested", []string{"string | number"}},
		{"Listed", []string{""}},
		{"Wide", []string{""}},
		{"Both", []string{"string | number"}},
		{"Second", []string{"", "string | number"}},
	}
	for _, tt := range tests {
		named := pkg.Scope().Lookup(tt.name).Type().(*types.Named)
		if got := typeParamBounds(named.TypeParams(), named.Underlying()); !slices.Equal(got, tt.want) {
			t.Errorf("%s bounds = %q, want %q", tt.name, got, tt.want)
		}
	}
}

// The mapper records the bounds on generic declarations, for both struct and
// alias kinds, while instantiations carry none.
func TestMapperRecordsTypeParamBounds(t *testing.T) {
	pkg := graphTypes(t, `
type Dict[K ~string, V any] map[K]V
type Box[K comparable] struct {
	Items Dict[string, K]
	Index map[K]int
}
type Input struct {
	D Dict[string, int]
	B Box[int]
}
`)
	mapper := NewMapper(nil)
	mapper.Convert(pkg.Scope().Lookup("Input").Type())
	want := map[string][]string{"Dict": {"string", ""}, "Box": {"string | number"}}
	for _, def := range mapper.Defs() {
		if len(def.TypeParams) == 0 {
			if def.TypeParamBounds != nil {
				t.Errorf("%s: instantiation carries bounds %q", def.Name, def.TypeParamBounds)
			}
			continue
		}
		if bounds, ok := want[def.Name]; !ok {
			t.Errorf("unexpected generic declaration %s", def.Name)
		} else if !slices.Equal(def.TypeParamBounds, bounds) {
			t.Errorf("%s bounds = %q, want %q", def.Name, def.TypeParamBounds, bounds)
		}
		delete(want, def.Name)
	}
	if len(want) > 0 {
		t.Fatalf("generic declarations missing from defs: %v", want)
	}
}
