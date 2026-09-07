package codegen

import (
	"bytes"
	"reflect"
	"strings"
	"testing"

	"github.com/befabri/trpcgo/internal/typemap"
	"github.com/befabri/trpcgo/zodconfig"
)

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
		if err := WriteZodSchemas(output, []ProcEntry{{InputTS: "Input"}}, defs, typemap.ZodStandard, ZodOptions{Validation: config}); err != nil {
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
