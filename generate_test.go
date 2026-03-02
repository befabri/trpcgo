package trpcgo_test

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/befabri/trpcgo"
)

// ---------- fixture types for codegen tests ----------

// GenPage is a generic paginated response type.
// The reflection-based codegen must emit this as a TypeScript generic interface.
type GenPage[T any] struct {
	Items []T `json:"items"`
	Total int `json:"total"`
	Page  int `json:"page"`
}

// GenPair is a multi-parameter generic type.
type GenPair[A any, B any] struct {
	First  A `json:"first"`
	Second B `json:"second"`
}

// Concrete types used as type arguments.

type GenAlpha struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type GenBeta struct {
	ID    string `json:"id"`
	Score int    `json:"score"`
}

type GenGamma struct {
	ID   string `json:"id"`
	Flag bool   `json:"flag"`
}

// ---------- helpers ----------

func generateTS(t *testing.T, r *trpcgo.Router) string {
	t.Helper()
	dir := t.TempDir()
	out := filepath.Join(dir, "trpc.ts")
	if err := r.GenerateTS(out); err != nil {
		t.Fatalf("GenerateTS: %v", err)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("reading output: %v", err)
	}
	return string(data)
}

// countPattern counts non-overlapping occurrences of a regex in s.
func countPattern(s, pattern string) int {
	re := regexp.MustCompile(pattern)
	return len(re.FindAllStringIndex(s, -1))
}

// ---------- tests ----------

// TestGenerateTSGenericInstantiation verifies that generic Go types are emitted
// as valid TypeScript generics (using <> syntax, not Go's [] syntax) and that
// Go package paths do not leak into the output.
func TestGenerateTSGenericInstantiation(t *testing.T) {
	r := trpcgo.NewRouter()

	trpcgo.Query(r, "a.list", func(_ context.Context, _ struct{}) (GenPage[GenAlpha], error) {
		return GenPage[GenAlpha]{}, nil
	})
	trpcgo.Query(r, "b.list", func(_ context.Context, _ struct{}) (GenPage[GenBeta], error) {
		return GenPage[GenBeta]{}, nil
	})

	ts := generateTS(t, r)
	t.Log("Generated TypeScript:\n" + ts)

	// Must NOT contain Go-style generic bracket syntax in type names.
	// (Array notation like "string[]" is fine — we check specifically for
	// the pattern TypeName[...pkg...] which is the Go reflect name.)
	if regexp.MustCompile(`GenPage\[`).MatchString(ts) {
		t.Error("output contains Go-style generic syntax 'GenPage[...]'; expected TypeScript '<>' syntax")
	}

	// Must NOT contain Go package paths anywhere in the output.
	if strings.Contains(ts, "github.com/") {
		t.Error("output contains Go package paths (github.com/...)")
	}
	if strings.Contains(ts, "trpcgo_test.") {
		t.Error("output contains test package path (trpcgo_test.)")
	}

	// Must contain valid TypeScript generic references in the procedure types.
	if !strings.Contains(ts, "GenPage<GenAlpha>") {
		t.Error("expected 'GenPage<GenAlpha>' in procedure output type")
	}
	if !strings.Contains(ts, "GenPage<GenBeta>") {
		t.Error("expected 'GenPage<GenBeta>' in procedure output type")
	}
}

// TestGenerateTSGenericInterfaceDeduplication verifies that multiple
// instantiations of the same generic type produce a single generic interface
// declaration with a type parameter, not N duplicate concrete interfaces.
func TestGenerateTSGenericInterfaceDeduplication(t *testing.T) {
	r := trpcgo.NewRouter()

	// Three different instantiations of GenPage.
	trpcgo.Query(r, "a.list", func(_ context.Context, _ struct{}) (GenPage[GenAlpha], error) {
		return GenPage[GenAlpha]{}, nil
	})
	trpcgo.Query(r, "b.list", func(_ context.Context, _ struct{}) (GenPage[GenBeta], error) {
		return GenPage[GenBeta]{}, nil
	})
	trpcgo.Query(r, "c.list", func(_ context.Context, _ struct{}) (GenPage[GenGamma], error) {
		return GenPage[GenGamma]{}, nil
	})

	ts := generateTS(t, r)
	t.Log("Generated TypeScript:\n" + ts)

	// There must be exactly ONE interface declaration for GenPage.
	count := countPattern(ts, `(?m)^export interface GenPage`)
	if count != 1 {
		t.Errorf("expected exactly 1 'export interface GenPage' declaration, got %d", count)
	}

	// The single interface must be generic (have a type parameter).
	if !regexp.MustCompile(`export interface GenPage<\w+>`).MatchString(ts) {
		t.Error("expected generic interface 'export interface GenPage<T>' (or similar type param name)")
	}

	// Each concrete type should still have its own interface.
	for _, name := range []string{"GenAlpha", "GenBeta", "GenGamma"} {
		if countPattern(ts, `(?m)^export interface `+name+`\b`) != 1 {
			t.Errorf("expected exactly 1 interface declaration for %s", name)
		}
	}
}

// TestGenerateTSMultiParamGeneric verifies that generic types with multiple
// type parameters are correctly handled.
func TestGenerateTSMultiParamGeneric(t *testing.T) {
	r := trpcgo.NewRouter()

	trpcgo.Query(r, "getPair", func(_ context.Context, _ struct{}) (GenPair[GenAlpha, GenBeta], error) {
		return GenPair[GenAlpha, GenBeta]{}, nil
	})

	ts := generateTS(t, r)
	t.Log("Generated TypeScript:\n" + ts)

	// Must have TypeScript generic reference with two args.
	if !strings.Contains(ts, "GenPair<GenAlpha, GenBeta>") {
		t.Error("expected 'GenPair<GenAlpha, GenBeta>' in output")
	}

	// Must NOT contain Go bracket syntax.
	if regexp.MustCompile(`GenPair\[`).MatchString(ts) {
		t.Error("output contains Go-style generic syntax 'GenPair[...]'")
	}

	// Interface should have two type parameters.
	if !regexp.MustCompile(`export interface GenPair<\w+, \w+>`).MatchString(ts) {
		t.Error("expected generic interface with two type parameters like 'GenPair<A, B>'")
	}
}

// TestGenerateTSNoDuplicateInterfaceNames verifies that every interface
// name in the generated output is unique (no duplicate declarations).
func TestGenerateTSNoDuplicateInterfaceNames(t *testing.T) {
	r := trpcgo.NewRouter()

	// Register several procedures with different types.
	trpcgo.Query(r, "a.list", func(_ context.Context, _ struct{}) (GenPage[GenAlpha], error) {
		return GenPage[GenAlpha]{}, nil
	})
	trpcgo.Query(r, "b.list", func(_ context.Context, _ struct{}) (GenPage[GenBeta], error) {
		return GenPage[GenBeta]{}, nil
	})
	trpcgo.Query(r, "b.get", func(_ context.Context, _ GenBeta) (GenBeta, error) {
		return GenBeta{}, nil
	})

	ts := generateTS(t, r)

	// Extract all interface names.
	re := regexp.MustCompile(`(?m)^export interface (\w+)`)
	matches := re.FindAllStringSubmatch(ts, -1)

	seen := map[string]int{}
	for _, m := range matches {
		seen[m[1]]++
	}
	for name, count := range seen {
		if count > 1 {
			t.Errorf("interface %q declared %d times (must be unique)", name, count)
		}
	}
}

// TestGenerateTSGenericFieldTypes verifies that the generic interface's field
// types correctly use type parameters rather than concrete types.
// For example, GenPage<T> should have `items: T[]`, not `items: GenAlpha[]`.
func TestGenerateTSGenericFieldTypes(t *testing.T) {
	r := trpcgo.NewRouter()

	trpcgo.Query(r, "a.list", func(_ context.Context, _ struct{}) (GenPage[GenAlpha], error) {
		return GenPage[GenAlpha]{}, nil
	})

	ts := generateTS(t, r)
	t.Log("Generated TypeScript:\n" + ts)

	// Extract the GenPage interface body.
	re := regexp.MustCompile(`(?s)export interface GenPage<(\w+)> \{(.+?)\}`)
	m := re.FindStringSubmatch(ts)
	if m == nil {
		t.Fatal("could not find generic GenPage interface in output")
	}

	paramName := m[1] // e.g., "T"
	body := m[2]

	// The items field should reference the type parameter, not a concrete type.
	expectedItems := paramName + "[]"
	if !strings.Contains(body, "items: "+expectedItems) {
		t.Errorf("expected 'items: %s' in GenPage body, got:\n%s", expectedItems, body)
	}

	// Non-generic fields should remain concrete.
	if !strings.Contains(body, "total: number") {
		t.Errorf("expected 'total: number' in GenPage body, got:\n%s", body)
	}
	if !strings.Contains(body, "page: number") {
		t.Errorf("expected 'page: number' in GenPage body, got:\n%s", body)
	}
}
