package typemap

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
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
