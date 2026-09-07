package trpcgo

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/befabri/trpcgo/internal/typemap/testdata/embedding"
)

func analysisFixtureDir(t *testing.T, name string) string {
	t.Helper()
	_, thisFile, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(thisFile), "internal", "analysis", "testdata", name)
}

func TestRegenerateFromSourceWritesTypesAndZod(t *testing.T) {
	dir := t.TempDir()
	typesOut := filepath.Join(dir, "router.ts")
	zodOut := filepath.Join(dir, "schemas.ts")

	regenerateFromSource(watchOpts{
		dir:       analysisFixtureDir(t, "enhanced"),
		patterns:  []string{"."},
		output:    typesOut,
		zodOutput: zodOut,
		zodStyle:  0,
	})

	typesData, err := os.ReadFile(typesOut)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(typesData), "export type AppRouter") {
		t.Fatalf("types output missing AppRouter:\n%s", typesData)
	}
	zodData, err := os.ReadFile(zodOut)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(zodData), "Schema") {
		t.Fatalf("zod output missing schemas:\n%s", zodData)
	}
}

func TestRegenerateFromSourcePreservesExistingOnAnalyzeError(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "router.ts")
	old := []byte("old content")
	if err := os.WriteFile(out, old, 0o644); err != nil {
		t.Fatal(err)
	}

	regenerateFromSource(watchOpts{
		dir:      filepath.Join(dir, "missing"),
		patterns: []string{"."},
		output:   out,
	})

	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(old) {
		t.Fatalf("output changed on analyze error: %q", got)
	}
}

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
			if got := goTypeToTS(tt.typ, map[string]*reflectDef{}); got != tt.ts {
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
			fields, extends, _, _ := collectFieldsTS(typ, map[string]*reflectDef{})
			if len(extends) != 0 || len(fields) != len(encoded) {
				t.Fatalf("fields=%+v extends=%v; JSON=%s", fields, extends, data)
			}
			for _, field := range fields {
				v, ok := encoded[field.Name]
				if !ok || goTypeToTS(reflect.TypeOf(v), map[string]*reflectDef{}) != field.Type {
					t.Errorf("field=%+v disagrees with JSON=%s", field, data)
				}
			}
		})
	}
}
