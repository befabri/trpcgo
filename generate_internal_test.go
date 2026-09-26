package trpcgo

import (
	"bytes"
	"encoding/json"
	"go/importer"
	"go/token"
	"go/types"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/befabri/trpcgo/internal/codegen"
	"github.com/befabri/trpcgo/internal/typemap"
	"github.com/befabri/trpcgo/internal/typemap/testdata/embedding"
	"github.com/befabri/trpcgo/zodconfig"
)

func TestReflectGoKindAndTypeScriptMapping(t *testing.T) {
	tests := []struct {
		name string
		typ  reflect.Type
		kind string
		ts   string
	}{
		{"string", reflect.TypeFor[string](), "string", "string"},
		{"bool pointer", reflect.TypeFor[*bool](), "bool", "boolean"},
		{"int8", reflect.TypeFor[int8](), "int8", "number"},
		{"uint32", reflect.TypeFor[uint32](), "uint32", "number"},
		{"float64", reflect.TypeFor[float64](), "float64", "number"},
		{"bytes", reflect.TypeFor[[]byte](), "[]byte", "string"},
		{"slice", reflect.TypeFor[[]string](), "slice", "string[]"},
		{"array", reflect.TypeFor[[2]int](), "array", "number[]"},
		{"map", reflect.TypeFor[map[string]int](), "map", "Record<string, number>"},
		{"interface", reflect.TypeFor[any](), "interface", "unknown"},
		{"raw message", reflect.TypeFor[json.RawMessage](), "json.RawMessage", "unknown"},
		{"json number", reflect.TypeFor[json.Number](), "json.Number", "number"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := reflectGoKind(tt.typ); got != tt.kind {
				t.Errorf("reflectGoKind(%v) = %q, want %q", tt.typ, got, tt.kind)
			}
			if got := goTypeToTS(tt.typ, newReflectDefs(nil)); got != tt.ts {
				t.Errorf("goTypeToTS(%v) = %q, want %q", tt.typ, got, tt.ts)
			}
		})
	}
}

func TestReflectedJSONFieldDominance(t *testing.T) {
	for _, value := range embedding.Cases {
		typ := reflect.TypeOf(value)
		t.Run(typ.Name(), func(t *testing.T) {
			data, err := json.Marshal(value)
			if err != nil {
				t.Fatal(err)
			}
			var encoded map[string]any
			if err := json.Unmarshal(data, &encoded); err != nil {
				t.Fatal(err)
			}
			fields, extends, _, _ := collectFieldsTS(typ, newReflectDefs(nil))
			if len(extends) != 0 || len(fields) != len(encoded) {
				t.Fatalf("fields=%+v extends=%v; JSON=%s", fields, extends, data)
			}
			for _, field := range fields {
				v, ok := encoded[field.Name]
				if !ok || goTypeToTS(reflect.TypeOf(v), newReflectDefs(nil)) != field.Type {
					t.Errorf("field=%+v disagrees with JSON=%s", field, data)
				}
			}
		})
	}
}

type recursiveMenu []struct {
	Label    string        `json:"label"`
	Children recursiveMenu `json:"children"`
}

type recursiveTree map[string]struct {
	Kids recursiveTree `json:"kids"`
}

type recursiveChain []*struct {
	Next recursiveChain `json:"next"`
}

type recursiveList[T any] []struct {
	Value T                `json:"value"`
	Next  recursiveList[T] `json:"next"`
}

type recursiveLink *struct {
	Next recursiveLink `json:"next"`
}

// Reflection mirrors the static mapper: a cycle through an anonymous struct
// stops at the named type, which the recursive field then references.
func TestReflectRecursionThroughAnonymousStructsTerminates(t *testing.T) {
	for _, tc := range []struct {
		name  string
		typ   reflect.Type
		alias string
		field string
	}{
		{"slice", reflect.TypeFor[recursiveMenu](), "{ label: string; children: recursiveMenu }[]", "children"},
		{"map", reflect.TypeFor[recursiveTree](), "Record<string, { kids: recursiveTree }>", "kids"},
		{"pointer elements", reflect.TypeFor[recursiveChain](), "{ next: recursiveChain }[]", "next"},
		{"generic", reflect.TypeFor[recursiveList[int8]](), "{ value: number; next: %s }[]", "next"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			defs := newReflectDefs(nil)
			goTypeToTS(tc.typ, defs)
			key := tc.typ.PkgPath() + "." + tc.typ.Name()
			def := defs.byKey[key]
			if def == nil || def.underlying == nil {
				t.Fatalf("no definition for %s in %v", key, defs.byKey)
			}
			display := resolveDisplayNames(defs)
			alias := strings.Replace(tc.alias, "%s", display[key], 1)
			if got := typemap.ResolveTokens(def.aliasOf, display); got != alias {
				t.Fatalf("alias = %q, want %q", got, alias)
			}
			if field := reflectInlineField(t, def.underlying.Element, tc.field); field.Element != nil || field.Inline != nil {
				t.Fatalf("recursive field was expanded past its named reference: %#v", field)
			}
		})
	}
	t.Run("named pointer", func(t *testing.T) {
		if got := goTypeToTS(reflect.TypeFor[recursiveLink](), newReflectDefs(nil)); !strings.Contains(got, "next") || !strings.HasSuffix(got, ": unknown }") {
			t.Fatalf("recursive named pointer = %q, want its recursive field untyped", got)
		}
	})
}

func reflectInlineField(t *testing.T, element *typemap.ElementType, name string) typemap.Field {
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
	return typemap.Field{}
}

func TestZodRejectsValidatorKindPanics(t *testing.T) {
	timePkg, err := importer.Default().Import("time")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		kind   string
		value  any
		static types.Type
		rules  []string
	}{
		{"float32", float32(1), types.Typ[types.Float32], []string{"oneof=1 2"}},
		{"float64", float64(1), types.Typ[types.Float64], []string{"oneof=1 2"}},
		{"bool", true, types.Typ[types.Bool], []string{"oneof=1", "min=1", "max=1", "len=1", "gt=1", "gte=1", "lt=1", "lte=1", "lowercase", "uppercase", "url"}},
		{"struct", struct{}{}, types.NewStruct(nil, nil), []string{"oneof=1", "min=1", "max=1", "len=1", "gt=1", "gte=1", "lt=1", "lte=1", "eq=1", "ne=1", "lowercase", "uppercase", "url"}},
		{"time.Time", time.Time{}, timePkg.Scope().Lookup("Time").Type(), []string{"oneof=1", "len=1", "eq=1", "ne=1", "lowercase", "uppercase", "url"}},
		{"slice", []int{1}, types.NewSlice(types.Typ[types.Int]), []string{"oneof=1", "lowercase", "uppercase", "url"}},
		{"array", [1]int{1}, types.NewArray(types.Typ[types.Int], 1), []string{"oneof=1", "lowercase", "uppercase", "url"}},
		{"map", map[string]int{"a": 1}, types.NewMap(types.Typ[types.String], types.Typ[types.Int]), []string{"oneof=1", "lowercase", "uppercase", "url"}},
		{"[]byte", []byte{1}, types.NewSlice(types.Typ[types.Byte]), []string{"oneof=1", "lowercase", "uppercase", "url"}},
	} {
		for _, rule := range tc.rules {
			t.Run(tc.kind+"/"+rule, func(t *testing.T) {
				for _, scope := range []string{"field", "pointer", "dive", "nested dive", "OR", "omission", "alias"} {
					t.Run(scope, func(t *testing.T) {
						rt, st := reflect.TypeOf(tc.value), tc.static
						tag, path := rule, "Input.value"
						config := zodconfig.Config{}
						switch scope {
						case "pointer":
							rt, st = reflect.PointerTo(reflect.PointerTo(rt)), types.NewPointer(types.NewPointer(st))
						case "dive":
							rt, st = reflect.SliceOf(reflect.PointerTo(rt)), types.NewSlice(types.NewPointer(st))
							tag, path = "dive,"+rule, path+"[]"
						case "nested dive":
							rt = reflect.MapOf(reflect.TypeFor[string](), reflect.SliceOf(rt))
							st = types.NewMap(types.Typ[types.String], types.NewSlice(st))
							tag, path = "dive,dive,"+rule, path+"[][]"
						case "OR":
							tag = "required|" + rule
						case "omission":
							tag = "omitempty," + rule
						case "alias":
							config.Aliases = map[string]string{"choice": rule}
							tag = "choice"
						}
						kind := tc.kind
						if kind == "[]byte" {
							kind = "slice"
						}
						name, _, _ := strings.Cut(rule, "=")
						testValidatorKindGeneration(t, rt, st, tag, path+": rule "+strconv.Quote(name)+" panics in validator on Go kind "+kind, config)
					})
				}
			})
		}
	}
	// Key rules must use the key kind rather than the map or value kind.
	testValidatorKindGeneration(t, reflect.TypeFor[map[int]string](), types.NewMap(types.Typ[types.Int], types.Typ[types.String]),
		"dive,keys,lowercase,endkeys", `Input.value{key}: rule "lowercase" panics in validator on Go kind int`, zodconfig.Config{})
}

// Safe Go rules without a Zod translation must retain the default-mode comment
// and strict-mode error. In particular time comparisons must not be mistaken
// for ordinary struct panics, nor json.Number for a numeric oneof.
func TestZodKeepsSafeUntranslatedKinds(t *testing.T) {
	type namedTime time.Time
	for _, tc := range []struct {
		value any
		tag   string
	}{
		{[]int{1}, "eq=1"},
		{map[string]int{"a": 1}, "ne=1"},
		{time.Time{}, "gt"},
		{time.Time{}, "min=1"},
		{namedTime{}, "max=1"},
		{1, "email"},
	} {
		for _, strict := range []bool{false, true} {
			program, err := typemap.CompileValidation(zodconfig.Config{Strict: strict})
			if err != nil {
				t.Fatal(err)
			}
			field := reflectMappedField(reflect.StructField{Name: "Value", Type: reflect.TypeOf(tc.value), Tag: reflect.StructTag("validate:" + strconv.Quote(tc.tag))}, newReflectDefs(program), "value", false, typemap.TSTypeTag{}, false)
			var out bytes.Buffer
			err = codegen.WriteZodSchemas(&out, []codegen.ProcEntry{{InputTS: "Input"}}, []typemap.TypeDef{{Name: "Input", Kind: typemap.TypeDefInterface, Fields: []typemap.Field{field}}}, typemap.ZodStandard, codegen.ZodOptions{Validation: program})
			if strict {
				if err == nil || !strings.Contains(err.Error(), "cannot validate Go kind") || out.Len() != 0 {
					t.Errorf("strict %s on %T: error=%v, output length=%d", tc.tag, tc.value, err, out.Len())
				}
			} else if err != nil || !strings.Contains(out.String(), "invalid zod params:") {
				t.Errorf("default %s on %T: error=%v; missing unsupported translation comment", tc.tag, tc.value, err)
			}
		}
	}
	for _, value := range []any{1, "1", json.Number("1")} {
		field := reflectMappedField(reflect.StructField{Name: "Value", Type: reflect.TypeOf(value), Tag: `validate:"oneof=1 2"`}, newReflectDefs(nil), "value", false, typemap.TSTypeTag{}, false)
		var out bytes.Buffer
		if err := codegen.WriteZodSchemas(&out, []codegen.ProcEntry{{InputTS: "Input"}}, []typemap.TypeDef{{Name: "Input", Kind: typemap.TypeDefInterface, Fields: []typemap.Field{field}}}, typemap.ZodStandard, codegen.ZodOptions{}); err != nil {
			t.Errorf("valid oneof on %T: %v", value, err)
		}
	}
}

func testValidatorKindGeneration(t *testing.T, rt reflect.Type, st types.Type, tag, want string, config zodconfig.Config) {
	t.Helper()
	for _, strict := range []bool{false, true} {
		config.Strict = strict
		program, err := typemap.CompileValidation(config)
		if err != nil {
			t.Fatal(err)
		}
		structTag := "json:\"value\" validate:" + strconv.Quote(tag)
		fields, _, _, _ := collectFieldsTS(reflect.StructOf([]reflect.StructField{{Name: "Value", Type: rt, Tag: reflect.StructTag(structTag)}}), newReflectDefs(program))
		reflected := []typemap.TypeDef{{Name: "Input", Kind: typemap.TypeDefInterface, Fields: fields}}
		m := typemap.NewMapper(nil)
		m.SetValidation(program)
		input := types.NewNamed(types.NewTypeName(token.NoPos, types.NewPackage("fixture", "fixture"), "Input", nil), types.NewStruct([]*types.Var{types.NewField(token.NoPos, nil, "Value", st, false)}, []string{structTag}), nil)
		m.Convert(input)
		for _, defs := range [][]typemap.TypeDef{reflected, m.Defs()} {
			for _, style := range []typemap.ZodStyle{typemap.ZodStandard, typemap.ZodMini} {
				var out bytes.Buffer
				err := codegen.WriteZodSchemas(&out, []codegen.ProcEntry{{InputTS: "Input"}}, defs, style, codegen.ZodOptions{Validation: program})
				if err == nil || !strings.Contains(err.Error(), want) {
					t.Fatalf("strict=%v style=%v: error=%v, want %q", strict, style, err, want)
				}
				if out.Len() != 0 {
					t.Fatalf("generation error wrote partial output: %s", out.String())
				}
			}
		}
	}
}
