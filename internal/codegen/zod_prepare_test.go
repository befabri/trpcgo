package codegen

import (
	"bytes"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/befabri/trpcgo/internal/typemap"
	"github.com/befabri/trpcgo/zodconfig"
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
				err := WriteZodSchemas(&output, []ProcEntry{{Path: "create", InputTS: tc.input}}, defs, style, ZodOptions{})
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
		if err := WriteZodSchemas(&output, procs, defs, style, ZodOptions{}); err != nil {
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
	if err := WriteZodSchemas(&output, []ProcEntry{{Path: "create", InputTS: "Input", OutputTS: "Output"}}, defs, typemap.ZodStandard, ZodOptions{}); err != nil {
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
	if err := WriteZodSchemas(&output, []ProcEntry{{Path: "create", InputTS: "Box<number>"}}, defs, typemap.ZodStandard, ZodOptions{}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "export const BoxInt8Schema") || strings.Contains(output.String(), "export const BoxSchema") {
		t.Fatalf("unique legacy expression did not resolve: %s", output.String())
	}
}

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

func TestZodScopeErrorsBeforeOutput(t *testing.T) {
	field := func(ts, kind, tag string) typemap.Field {
		return typemap.Field{Name: "value", Type: ts, GoKind: kind, Validate: typemap.ParseValidateTag("validate:" + strconv.Quote(tag))}
	}
	for _, tc := range []struct {
		name    string
		field   typemap.Field
		path    string
		message string
	}{
		{"dive scalar", field("string", "string", "dive,min=1"), "Input.value", "dive requires"},
		{"keys array", field("string[]", "slice", "dive,keys,min=1,endkeys"), "Input.value", "keys/endkeys requires a map"},
		{"unclosed keys", field("Record<string, string>", "map", "dive,keys,min=1"), "Input.value", "endkeys"},
		{"misplaced endkeys", field("string", "string", "endkeys"), "Input.value", "endkeys"},
		{"nested field path", field("string", "string", "eqfield=Parent.Name"), "Input.value", "nested field references"},
		{"indexed field path", field("string", "string", "eqfield=Items[0]"), "Input.value", "nested field references"},
		{"nested path in OR", field("string", "string", "email|eqfield=Parent.Name"), "Input.value", "nested field references"},
		{"empty rule", field("string", "string", "min=1,,max=3"), "Input.value", "empty validation rule"},
		{"empty OR branch", field("string", "string", "email|"), "Input.value", "empty validation rule"},
		{"implicit anonymous element", typemap.Field{Name: "value", Type: "({ bad: string })[]", GoKind: "slice", Element: &typemap.ElementType{
			Type: "{ bad: string }", GoKind: "struct", Inline: &typemap.TypeDef{Kind: typemap.TypeDefInterface, Fields: []typemap.Field{
				{Name: "bad", Type: "string", GoKind: "string", Validate: []typemap.ValidateRule{{Tag: "dive"}}},
			}},
		}}, "Input.value[].bad", "dive requires"},
		{"composite uniqueness", typemap.Field{Name: "value", Type: "Record<string, Item>", GoKind: "map", Validate: []typemap.ValidateRule{{Tag: "unique"}}, Element: &typemap.ElementType{Type: "Item", GoKind: "struct"}}, "Input.value", "unique"},
		{"unsupported float map key", typemap.Field{Name: "value", Type: "Record<number, string>", GoKind: "map", Key: &typemap.ElementType{Type: "number", GoKind: "float64"}}, "Input.value{key}", "only string and integer keys are supported"},
		{"unsupported pointer map key", typemap.Field{Name: "value", Type: "Record<number, string>", GoKind: "map", Key: &typemap.ElementType{Type: "number", GoKind: "int", IsPointer: true}}, "Input.value{key}", "only string and integer keys are supported"},
		{"unsupported nested float map key", typemap.Field{Name: "value", Type: "Record<string, Record<number, string>>", GoKind: "map", Key: &typemap.ElementType{Type: "string", GoKind: "string"}, Element: &typemap.ElementType{
			Type: "Record<number, string>", GoKind: "map", Key: &typemap.ElementType{Type: "number", GoKind: "float32"}, Element: &typemap.ElementType{Type: "string", GoKind: "string"},
		}}, "Input.value[]{key}", "only string and integer keys are supported"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, style := range []typemap.ZodStyle{typemap.ZodStandard, typemap.ZodMini} {
				var out bytes.Buffer
				err := WriteZodSchemas(&out, []ProcEntry{{InputTS: "Input"}}, []typemap.TypeDef{{Name: "Input", Kind: typemap.TypeDefInterface, Fields: []typemap.Field{tc.field}}}, style, ZodOptions{})
				if err == nil || !strings.Contains(err.Error(), tc.path) || !strings.Contains(err.Error(), tc.message) {
					t.Fatalf("error = %v, want path %q and %q", err, tc.path, tc.message)
				}
				if out.Len() != 0 {
					t.Fatalf("invalid schema wrote partial output: %s", out.String())
				}
			}
		})
	}
}

func TestZodScopeTraversalStopsImplicitMetadataCycles(t *testing.T) {
	// Descriptors can be shared or recursive independently of their TS spelling.
	// Traversal must stop a repeated descriptor even when its type is a record.
	recursive := &typemap.ElementType{Type: "Record<string, Recursive>", GoKind: "map", Key: &typemap.ElementType{Type: "string", GoKind: "string"}}
	recursive.Element = recursive
	field := zodElementField(recursive.Type, recursive)
	field.Name = "value"
	defs := map[string]typemap.TypeDef{"Input": {Kind: typemap.TypeDefInterface, Fields: []typemap.Field{field}}}
	if err := validateZodScopes([]string{"Input"}, defs, false); err != nil {
		t.Fatal(err)
	}

	// An explicit dive still advances its finite rule program through a reused
	// descriptor. Do not let the cycle guard conceal an invalid nested key rule.
	field.Validate = typemap.ParseValidateTag(`validate:"dive,dive,keys,dive,endkeys"`)
	defs["Input"] = typemap.TypeDef{Kind: typemap.TypeDefInterface, Fields: []typemap.Field{field}}
	err := validateZodScopes([]string{"Input"}, defs, false)
	if err == nil || !strings.Contains(err.Error(), "Input.value[]{key}") || !strings.Contains(err.Error(), "dive requires") {
		t.Fatalf("explicit nested scope was skipped: %v", err)
	}
}

func TestZodScopeTraversalChecksUnannotatedNestedTypes(t *testing.T) {
	// Nil descriptors do not identify a cycle. All levels of a manually supplied
	// TypeScript container still need checking, even without validate:dive.
	field := typemap.Field{Name: "value", Type: "Record<string, Record<string, Box<string>[]>>", GoKind: "map"}
	defs := map[string]typemap.TypeDef{"Input": {Kind: typemap.TypeDefInterface, Fields: []typemap.Field{field}}}
	err := validateZodScopes([]string{"Input"}, defs, false)
	if err == nil || !strings.Contains(err.Error(), "Input.value[][][]") || !strings.Contains(err.Error(), "requires concrete Go type metadata") {
		t.Fatalf("nested type validation was skipped: %v", err)
	}
}

func TestZodConfigurationErrorsBeforeOutput(t *testing.T) {
	for _, tc := range []struct {
		name   string
		config zodconfig.Config
		tag    string
		want   string
	}{
		{name: "strict unsupported", config: zodconfig.Config{Strict: true}, tag: `validate:"businessRule"`, want: "Input.value: rule \"businessRule\" has no client counterpart"},
		{name: "strict invalid", config: zodconfig.Config{Strict: true}, tag: `validate:"min=no"`, want: "cannot validate Go kind string"},
		{name: "custom kind", config: zodconfig.Config{Rules: map[string]zodconfig.Rule{"even": {Predicate: "v => true", GoKinds: []string{"int"}}}}, tag: `validate:"even"`, want: "cannot validate Go kind string"},
		{name: "alias OR", config: zodconfig.Config{Aliases: map[string]string{"bounded": "min=2"}}, tag: `validate:"bounded|eq=x"`, want: "alias \"bounded\" must be a complete rule"},
		{name: "alias param", config: zodconfig.Config{Aliases: map[string]string{"bounded": "min=2"}}, tag: `validate:"bounded=4"`, want: "alias \"bounded\" must be a complete rule"},
		{name: "unknown struct", config: zodconfig.Config{StructRules: map[string][]zodconfig.StructRule{"Typo": {{Predicate: "v => true"}}}}, want: "must identify one generated Go type"},
		{name: "import collision", config: zodconfig.Config{Imports: map[string]string{"InputSchema": "./validators"}}, want: "conflicts with a generated schema"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			program, err := typemap.CompileValidation(tc.config)
			if err != nil {
				t.Fatal(err)
			}
			field := typemap.Field{Name: "value", Type: "string", GoKind: "string"}
			typemap.ApplyValidation(&field, tc.tag, program)
			var out bytes.Buffer
			err = WriteZodSchemas(&out, []ProcEntry{{InputTS: "Input"}}, []typemap.TypeDef{{Name: "Input", ID: "pkg.Input", PkgPath: "pkg", Kind: typemap.TypeDefInterface, Fields: []typemap.Field{field}}}, typemap.ZodStandard, ZodOptions{Validation: program})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v, want %s", err, tc.want)
			}
			if out.Len() != 0 {
				t.Fatalf("wrote partial schema: %s", out.String())
			}
		})
	}
}

func TestZodExplicitServerOnlyRule(t *testing.T) {
	c := zodconfig.Config{Strict: true, Rules: map[string]zodconfig.Rule{"database": {ServerOnly: true}}}
	p, err := typemap.CompileValidation(c)
	if err != nil {
		t.Fatal(err)
	}
	f := typemap.Field{Name: "value", Type: "string", GoKind: "string"}
	typemap.ApplyValidation(&f, `validate:"database"`, p)
	var out bytes.Buffer
	if err := WriteZodSchemas(&out, []ProcEntry{{InputTS: "Input"}}, []typemap.TypeDef{{Name: "Input", Kind: typemap.TypeDefInterface, Fields: []typemap.Field{f}}}, typemap.ZodStandard, ZodOptions{Validation: p}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "database") {
		t.Fatal("server-only rule lost its diagnostic")
	}
}

func TestZodStructRuleUsesQualifiedIdentity(t *testing.T) {
	config := zodconfig.Config{StructRules: map[string][]zodconfig.StructRule{"example.org/api.Input": {{Predicate: "data => data.value === 'ok'"}}}}
	program, err := typemap.CompileValidation(config)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	err = WriteZodSchemas(&out, []ProcEntry{{InputTS: "ApiInput"}}, []typemap.TypeDef{{Name: "ApiInput", ID: "example.org/api.Input", Kind: typemap.TypeDefInterface, Fields: []typemap.Field{{Name: "value", Type: "string", GoKind: "string"}}}}, typemap.ZodMini, ZodOptions{Validation: program})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "data.value === 'ok'") {
		t.Fatal("qualified struct rule was not emitted")
	}
}

func TestCustomPredicateHoistingClonesMetadata(t *testing.T) {
	config := zodconfig.Config{Rules: map[string]zodconfig.Rule{"safe": {Predicate: "check.safe"}}, Imports: map[string]string{"check": "./rules"}, StructRules: map[string][]zodconfig.StructRule{"Input": {{Predicate: "data.struct", Path: []string{"value"}}}}}
	config.Imports["data"] = "./rules"
	program, err := typemap.CompileValidation(config)
	if err != nil {
		t.Fatal(err)
	}
	field := typemap.Field{Name: "value", Type: "string", GoKind: "string", Optional: true}
	typemap.ApplyValidation(&field, `validate:"safe"`, program)
	defs := []typemap.TypeDef{{Name: "Input", Kind: typemap.TypeDefInterface, Fields: []typemap.Field{field}}}
	before := typemap.ResolveTypeDef(defs[0], nil)
	configBefore := config.Clone()
	var first, second bytes.Buffer
	for _, output := range []*bytes.Buffer{&first, &second} {
		if err := WriteZodSchemas(output, []ProcEntry{{InputTS: "Input"}}, defs, typemap.ZodStandard, ZodOptions{Validation: program}); err != nil {
			t.Fatal(err)
		}
	}
	if !reflect.DeepEqual(defs[0], before) || !reflect.DeepEqual(config, configBefore) {
		t.Fatal("schema generation mutated mapper metadata or caller configuration")
	}
	if first.String() != second.String() {
		t.Fatal("repeated schema generation changed predicate bindings")
	}
	if strings.Count(first.String(), "check.safe") != 1 || strings.Count(first.String(), "data.struct") != 1 {
		t.Fatalf("original predicate expressions were not hoisted once:\n%s", first.String())
	}
	if !strings.Contains(first.String(), "const $goCustomPredicate0: () => (...args: any[]) => unknown = () => (check.safe)") {
		t.Fatalf("predicate did not retain module scope:\n%s", first.String())
	}
}
