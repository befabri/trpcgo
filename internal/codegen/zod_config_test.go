package codegen

import (
	"bytes"
	"strings"
	"testing"

	"github.com/befabri/trpcgo/internal/typemap"
	"github.com/befabri/trpcgo/zodconfig"
)

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
			err = WriteZodSchemas(&out, []ProcEntry{{InputTS: "Input"}}, []typemap.TypeDef{{Name: "Input", ID: "pkg.Input", PkgPath: "pkg", Kind: typemap.TypeDefInterface, Fields: []typemap.Field{field}}}, typemap.ZodStandard, ZodOptions{Validation: tc.config})
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
	if err := WriteZodSchemas(&out, []ProcEntry{{InputTS: "Input"}}, []typemap.TypeDef{{Name: "Input", Kind: typemap.TypeDefInterface, Fields: []typemap.Field{f}}}, typemap.ZodStandard, ZodOptions{Validation: c}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "database") {
		t.Fatal("server-only rule lost its diagnostic")
	}
}

func TestZodStructRuleUsesQualifiedIdentity(t *testing.T) {
	config := zodconfig.Config{StructRules: map[string][]zodconfig.StructRule{"example.org/api.Input": {{Predicate: "data => data.value === 'ok'"}}}}
	var out bytes.Buffer
	err := WriteZodSchemas(&out, []ProcEntry{{InputTS: "ApiInput"}}, []typemap.TypeDef{{Name: "ApiInput", ID: "example.org/api.Input", Kind: typemap.TypeDefInterface, Fields: []typemap.Field{{Name: "value", Type: "string", GoKind: "string"}}}}, typemap.ZodMini, ZodOptions{Validation: config})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "data.value === 'ok'") {
		t.Fatal("qualified struct rule was not emitted")
	}
}
