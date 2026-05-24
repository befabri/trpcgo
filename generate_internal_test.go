package trpcgo

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
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

func TestBasicArgToTS(t *testing.T) {
	tests := map[string]string{
		"string":                 "string",
		"bool":                   "boolean",
		"int":                    "number",
		"uint64":                 "number",
		"float32":                "number",
		"example.com/app.User":   "User",
		"github.com/acme.Status": "Status",
		"Custom":                 "Custom",
	}
	for in, want := range tests {
		if got := basicArgToTS(in); got != want {
			t.Errorf("basicArgToTS(%q) = %q, want %q", in, got, want)
		}
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
		{"json number", reflect.TypeFor[json.Number](), "string", "number"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := reflectGoKind(tt.typ); got != tt.kind {
				t.Errorf("reflectGoKind(%v) = %q, want %q", tt.typ, got, tt.kind)
			}
			if got := goTypeToTS(tt.typ, map[string]*reflectDef{}, nil); got != tt.ts {
				t.Errorf("goTypeToTS(%v) = %q, want %q", tt.typ, got, tt.ts)
			}
		})
	}
}
