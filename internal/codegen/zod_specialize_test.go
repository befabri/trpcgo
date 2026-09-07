package codegen

import (
	"bytes"
	"strings"
	"testing"

	"github.com/befabri/trpcgo/internal/typemap"
)

func numericGenericDefinitions() []typemap.TypeDef {
	return []typemap.TypeDef{{
		Name: "Box", Kind: typemap.TypeDefInterface, TypeParams: []string{"T"},
		Fields: []typemap.Field{{Name: "value", Type: "T"}},
		Specializations: []typemap.TypeDef{
			{Name: "BoxInt8", Kind: typemap.TypeDefInterface, InstanceOf: "Box<number>", Fields: []typemap.Field{{Name: "value", Type: "number", GoKind: "int8"}}},
			{Name: "BoxInt16", Kind: typemap.TypeDefInterface, InstanceOf: "Box<number>", Fields: []typemap.Field{{Name: "value", Type: "number", GoKind: "int16"}}},
		},
	}}
}

func TestZodAmbiguousGenericIdentityRequiresMetadata(t *testing.T) {
	cases := []struct {
		name, input, path string
		definition        typemap.TypeDef
	}{
		{name: "procedure", input: "Box<number>", path: `procedure "create" input`},
		{name: "field", input: "Input", path: "Input.box", definition: typemap.TypeDef{Name: "Input", Kind: typemap.TypeDefInterface, Fields: []typemap.Field{{Name: "box", Type: "Box<number>"}}}},
		{name: "alias", input: "Input", path: "Input", definition: typemap.TypeDef{Name: "Input", Kind: typemap.TypeDefAlias, AliasOf: "Box<number>"}},
		{name: "inheritance", input: "Input", path: "Input base", definition: typemap.TypeDef{Name: "Input", Kind: typemap.TypeDefInterface, Extends: []string{"Box<number>"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, style := range []typemap.ZodStyle{typemap.ZodStandard, typemap.ZodMini} {
				defs := numericGenericDefinitions()
				if tc.definition.Name != "" {
					defs = append(defs, tc.definition)
				}
				var output bytes.Buffer
				err := WriteZodSchemas(&output, []ProcEntry{{Path: "create", InputTS: tc.input}}, defs, style)
				if err == nil || !strings.Contains(err.Error(), tc.path) || !strings.Contains(err.Error(), "Box<number> matches multiple Go instantiations") {
					t.Fatalf("expected a field-specific ambiguity error, got %v", err)
				}
				if output.Len() != 0 {
					t.Fatalf("ambiguous generation wrote a partial module: %s", output.String())
				}
			}
		})
	}
}

func TestZodSpecializationUsesConcreteNestedMetadata(t *testing.T) {
	defs := append(numericGenericDefinitions(), typemap.TypeDef{
		Name: "Input", Kind: typemap.TypeDefInterface,
		Fields: []typemap.Field{{Name: "details", Type: "{ narrow: Box<number>; wide: Box<number> }", Inline: &typemap.TypeDef{
			Kind: typemap.TypeDefInterface,
			Fields: []typemap.Field{
				{Name: "narrow", Type: "Box<number>", ZodType: "BoxInt8"},
				{Name: "wide", Type: "Box<number>", ZodType: "BoxInt16"},
			},
		}}},
	})
	procs := []ProcEntry{{Path: "input", InputTS: "Input"}, {Path: "narrow", InputTS: "Box<number>", InputZod: "BoxInt8"}, {Path: "wide", InputTS: "Box<number>", InputZod: "BoxInt16"}}
	for _, style := range []typemap.ZodStyle{typemap.ZodStandard, typemap.ZodMini} {
		var output bytes.Buffer
		if err := WriteZodSchemas(&output, procs, defs, style); err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{"narrow: BoxInt8Schema", "wide: BoxInt16Schema", "z.gte(-128)", "z.gte(-32768)"} {
			if !strings.Contains(output.String(), want) {
				t.Errorf("concrete metadata lost %q:\n%s", want, output.String())
			}
		}
	}
	if procs[1].InputTS != "Box<number>" || defs[1].Fields[0].Inline.Fields[0].Type != "Box<number>" {
		t.Fatal("schema specialization mutated the public TypeScript definitions")
	}
}

func TestZodAmbiguousOutputTypesDoNotBlockInputs(t *testing.T) {
	defs := append(numericGenericDefinitions(),
		typemap.TypeDef{Name: "Input", Kind: typemap.TypeDefInterface, Fields: []typemap.Field{{Name: "value", Type: "string"}}},
		typemap.TypeDef{Name: "Output", Kind: typemap.TypeDefInterface, Fields: []typemap.Field{{Name: "box", Type: "Box<number>"}}},
	)
	var output bytes.Buffer
	if err := WriteZodSchemas(&output, []ProcEntry{{Path: "create", InputTS: "Input", OutputTS: "Output"}}, defs, typemap.ZodStandard); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output.String(), "BoxInt8Schema") || strings.Contains(output.String(), "BoxInt16Schema") || !strings.Contains(output.String(), "InputSchema") {
		t.Fatalf("unexpected schema reachability: %s", output.String())
	}
}

func TestZodUniqueGenericIdentityCompatibilityFallback(t *testing.T) {
	defs := numericGenericDefinitions()
	defs[0].Specializations = defs[0].Specializations[:1]
	var output bytes.Buffer
	if err := WriteZodSchemas(&output, []ProcEntry{{Path: "create", InputTS: "Box<number>"}}, defs, typemap.ZodStandard); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "export const BoxInt8Schema") || strings.Contains(output.String(), "export const BoxSchema") {
		t.Fatalf("unique legacy expression did not resolve: %s", output.String())
	}
}
