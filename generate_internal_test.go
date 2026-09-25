package trpcgo

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/befabri/trpcgo/internal/typemap"
	"github.com/befabri/trpcgo/internal/typemap/testdata/embedding"
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
