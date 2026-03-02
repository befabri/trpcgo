package trpcgo_test

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
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

// ---------- fixture types for type-mapping edge cases ----------

type WithRawJSON struct {
	Data json.RawMessage `json:"data"`
	Name string          `json:"name"`
}

type WithAnyField struct {
	Payload any    `json:"payload"`
	Label   string `json:"label"`
}

type WithFixedArray struct {
	Coords [3]float64 `json:"coords"`
	Tags   [2]string  `json:"tags"`
}

type WithIntKeyMap struct {
	Lookup map[int]string `json:"lookup"`
}

type WithDoublePtr struct {
	Value **string `json:"value"`
}

type WithBytes struct {
	Photo []byte `json:"photo"`
	Name  string `json:"name"`
}

type CgAddress struct {
	Street string `json:"street"`
	City   string `json:"city"`
}

type WithNested struct {
	Name    string    `json:"name"`
	Address CgAddress `json:"address"`
}

type TreeNode struct {
	Label    string     `json:"label"`
	Children []TreeNode `json:"children"`
}

type WithTSSkip struct {
	Visible string `json:"visible"`
	Hidden  string `json:"hidden" tstype:"-"`
}

type WithTSOverride struct {
	Raw  string `json:"raw" tstype:"Record<string, unknown>"`
	Name string `json:"name"`
}

type WithReadonly struct {
	ID   string `json:"id" tstype:",readonly"`
	Name string `json:"name"`
}

type WithRequired struct {
	Avatar *string `json:"avatar" tstype:",required"`
	Bio    *string `json:"bio"`
}

type WithJSONSkip struct {
	Public string `json:"public"`
	Secret string `json:"-"`
}

type PtrBase struct {
	ID        string `json:"id"`
	CreatedAt string `json:"createdAt"`
}

type WithPtrEmbed struct {
	*PtrBase
	Name string `json:"name"`
}

type WithUnexported struct {
	Public  string `json:"public"`
	private string //nolint:unused // intentionally unexported for test
}

type AllNumerics struct {
	I   int     `json:"i"`
	I8  int8    `json:"i8"`
	I16 int16   `json:"i16"`
	I32 int32   `json:"i32"`
	I64 int64   `json:"i64"`
	U   uint    `json:"u"`
	U8  uint8   `json:"u8"`
	U16 uint16  `json:"u16"`
	U32 uint32  `json:"u32"`
	U64 uint64  `json:"u64"`
	F32 float32 `json:"f32"`
	F64 float64 `json:"f64"`
}

type WithBool struct {
	Active  bool  `json:"active"`
	Deleted *bool `json:"deleted"`
}

type CgInner struct {
	Value string `json:"value"`
}

type CgMiddle struct {
	Inner CgInner `json:"inner"`
	Count int     `json:"count"`
}

type CgOuter struct {
	Middle CgMiddle `json:"middle"`
	Name   string   `json:"name"`
}

// ---------- fixture types from typescriptify/tygo patterns ----------

// Nested maps — from tygo simple/ example.
type NestedMapConfig struct {
	Settings map[string]map[string]int `json:"settings"`
}

// Nested slice of structs — from typescriptify TestArrayOfArrays.
type KeyboardKey struct {
	Key string `json:"key"`
}

type Keyboard struct {
	Keys [][]KeyboardKey `json:"keys"`
}

// Map with struct values as fields — from typescriptify TestMaps.
type Endpoint struct {
	URL    string `json:"url"`
	Method string `json:"method"`
}

type APIConfig struct {
	Endpoints map[string]Endpoint `json:"endpoints"`
}

// Slice of maps — complex nesting.
type BatchResult struct {
	Results []map[string]int `json:"results"`
}

// Map of slices — complex nesting.
type GroupedTags struct {
	Groups map[string][]string `json:"groups"`
}

// Struct with omitzero (Go 1.24+) — from tygo.
type WithOmitzero struct {
	Name      string `json:"name"`
	CreatedAt string `json:"created_at,omitzero"`
}

// Struct with multiple optional strategies combined — from tygo.
type MultiOptional struct {
	Required    string  `json:"required"`
	OmitEmpty   string  `json:"omitEmpty,omitempty"`
	OmitZero    string  `json:"omitZero,omitzero"`
	Pointer     *string `json:"pointer"`
	PtrOmit     *string `json:"ptrOmit,omitempty"`
	PtrRequired *string `json:"ptrRequired" tstype:",required"`
}

// Deeply nested map — from tygo simple/ ComplexType example.
type DeeplyNestedMap struct {
	Data map[string]map[string][]int `json:"data"`
}

// Struct with mixed embedded + regular fields — from typescriptify.
type Timestamps struct {
	CreatedAt string `json:"createdAt"`
	UpdatedAt string `json:"updatedAt"`
}

type Metadata struct {
	Version int    `json:"version"`
	Source  string `json:"source"`
}

type FullEntity struct {
	Timestamps
	Metadata
	Name string `json:"name"`
}

// Field without JSON tag — from typescriptify TestFieldNamesWithoutJSONAnnotation.
type NoJSONTags struct {
	PublicName  string
	PublicCount int
	privateVal  string //nolint:unused
}

// Struct with all optional patterns — pointer, omitempty, omitzero, tstype required.
type OptionalVariants struct {
	Always    string  `json:"always"`
	OmitE     string  `json:"omitE,omitempty"`
	OmitZ     string  `json:"omitZ,omitzero"`
	Ptr       *string `json:"ptr"`
	PtrOmitE  *string `json:"ptrOmitE,omitempty"`
	ForcedReq *string `json:"forcedReq" tstype:",required"`
}

// ---------- fixture types for extends / ts_doc / misc edge cases ----------

// Extends support.
type ExBase struct {
	ID string `json:"id"`
}

type ExUser struct {
	ExBase `tstype:",extends"`
	Name   string `json:"name"`
}

type ExWithPtrExtends struct {
	*ExBase `tstype:",extends"`
	Name    string `json:"name"`
}

type ExWithPtrReqExtends struct {
	*ExBase `tstype:",extends,required"`
	Name    string `json:"name"`
}

type ExAuditFields struct {
	CreatedBy string `json:"createdBy"`
	UpdatedBy string `json:"updatedBy"`
}

type ExMultiExtends struct {
	ExBase        `tstype:",extends"`
	ExAuditFields `tstype:",extends"`
	Title         string `json:"title"`
}

// ts_doc support.
type WithTSDoc struct {
	Host string `json:"host" ts_doc:"The hostname to connect to"`
	Port int    `json:"port" ts_doc:"Port number (1-65535)"`
	Name string `json:"name"`
}

// json.Number support.
type WithJSONNumber struct {
	Value json.Number `json:"value"`
	Name  string      `json:"name"`
}

// ---------- fixture types for Zod generation ----------

type ZodLoginInput struct {
	Email    string `json:"email" validate:"required,email"`
	Password string `json:"password" validate:"required,min=8,max=128"`
}

type ZodCreateItemInput struct {
	Name  string   `json:"name" validate:"required,min=1,max=100"`
	Tags  []string `json:"tags" validate:"max=10,dive,min=1,max=50"`
	Count int      `json:"count" validate:"gte=0,lte=1000"`
}

type ZodOptionalInput struct {
	Query  string  `json:"query,omitempty"`
	Limit  *int    `json:"limit"`
	Offset int     `json:"offset"`
}

// Zod extends fixture types.
type ZodBase struct {
	ID string `json:"id" validate:"required"`
}

type ZodDerived struct {
	ZodBase `tstype:",extends"`
	Name    string `json:"name" validate:"required,min=1"`
}

type ZodMultiBase struct {
	ZodBase       `tstype:",extends"`
	ExAuditFields `tstype:",extends"`
	Title         string `json:"title" validate:"required"`
}

type ZodPtrExtends struct {
	*ZodBase `tstype:",extends"`
	Label    string `json:"label"`
}

type ZodCyclicNode struct {
	ZodBase  `tstype:",extends"`
	Children []ZodCyclicNode `json:"children"`
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

func generateZod(t *testing.T, r *trpcgo.Router) string {
	t.Helper()
	dir := t.TempDir()
	out := filepath.Join(dir, "zod.ts")
	if err := r.GenerateZod(out); err != nil {
		t.Fatalf("GenerateZod: %v", err)
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

// ---------- type-mapping edge case tests ----------

func TestGenerateTSTypeEdgeCases(t *testing.T) {
	t.Run("json.RawMessage to unknown", func(t *testing.T) {
		r := trpcgo.NewRouter()
		trpcgo.VoidQuery(r, "get", func(_ context.Context) (WithRawJSON, error) {
			return WithRawJSON{}, nil
		})
		ts := generateTS(t, r)
		if !strings.Contains(ts, "data: unknown;") {
			t.Errorf("json.RawMessage should map to unknown:\n%s", ts)
		}
		if !strings.Contains(ts, "name: string;") {
			t.Errorf("missing name field:\n%s", ts)
		}
	})

	t.Run("any field to unknown", func(t *testing.T) {
		r := trpcgo.NewRouter()
		trpcgo.VoidQuery(r, "get", func(_ context.Context) (WithAnyField, error) {
			return WithAnyField{}, nil
		})
		ts := generateTS(t, r)
		if !strings.Contains(ts, "payload: unknown;") {
			t.Errorf("any/interface{} should map to unknown:\n%s", ts)
		}
	})

	t.Run("fixed array to T[]", func(t *testing.T) {
		r := trpcgo.NewRouter()
		trpcgo.VoidQuery(r, "get", func(_ context.Context) (WithFixedArray, error) {
			return WithFixedArray{}, nil
		})
		ts := generateTS(t, r)
		if !strings.Contains(ts, "coords: number[];") {
			t.Errorf("[3]float64 should map to number[]:\n%s", ts)
		}
		if !strings.Contains(ts, "tags: string[];") {
			t.Errorf("[2]string should map to string[]:\n%s", ts)
		}
	})

	t.Run("map with int key", func(t *testing.T) {
		r := trpcgo.NewRouter()
		trpcgo.VoidQuery(r, "get", func(_ context.Context) (WithIntKeyMap, error) {
			return WithIntKeyMap{}, nil
		})
		ts := generateTS(t, r)
		if !strings.Contains(ts, "Record<number, string>") {
			t.Errorf("map[int]string should map to Record<number, string>:\n%s", ts)
		}
	})

	t.Run("double pointer unwrapped and optional", func(t *testing.T) {
		r := trpcgo.NewRouter()
		trpcgo.VoidQuery(r, "get", func(_ context.Context) (WithDoublePtr, error) {
			return WithDoublePtr{}, nil
		})
		ts := generateTS(t, r)
		if !strings.Contains(ts, "value?: string;") {
			t.Errorf("**string should unwrap to optional string:\n%s", ts)
		}
	})

	t.Run("byte slice to string", func(t *testing.T) {
		r := trpcgo.NewRouter()
		trpcgo.VoidQuery(r, "get", func(_ context.Context) (WithBytes, error) {
			return WithBytes{}, nil
		})
		ts := generateTS(t, r)
		if !strings.Contains(ts, "photo: string;") {
			t.Errorf("[]byte should map to string:\n%s", ts)
		}
		if strings.Contains(ts, "number[]") {
			t.Errorf("[]byte should NOT be number[]:\n%s", ts)
		}
	})

	t.Run("nested named struct", func(t *testing.T) {
		r := trpcgo.NewRouter()
		trpcgo.VoidQuery(r, "get", func(_ context.Context) (WithNested, error) {
			return WithNested{}, nil
		})
		ts := generateTS(t, r)
		if !strings.Contains(ts, "address: CgAddress;") {
			t.Errorf("nested struct should reference CgAddress:\n%s", ts)
		}
		if !strings.Contains(ts, "export interface CgAddress {") {
			t.Errorf("CgAddress interface should be emitted:\n%s", ts)
		}
		if !strings.Contains(ts, "street: string;") {
			t.Errorf("CgAddress should have street field:\n%s", ts)
		}
		if !strings.Contains(ts, "city: string;") {
			t.Errorf("CgAddress should have city field:\n%s", ts)
		}
	})

	t.Run("recursive struct", func(t *testing.T) {
		r := trpcgo.NewRouter()
		trpcgo.VoidQuery(r, "get", func(_ context.Context) (TreeNode, error) {
			return TreeNode{}, nil
		})
		ts := generateTS(t, r)
		if !strings.Contains(ts, "export interface TreeNode {") {
			t.Errorf("TreeNode interface should be emitted:\n%s", ts)
		}
		if !strings.Contains(ts, "children: TreeNode[];") {
			t.Errorf("recursive field should reference self:\n%s", ts)
		}
		if !strings.Contains(ts, "label: string;") {
			t.Errorf("label field should be present:\n%s", ts)
		}
		if countPattern(ts, `(?m)^export interface TreeNode`) != 1 {
			t.Errorf("TreeNode should appear exactly once:\n%s", ts)
		}
	})

	t.Run("tstype skip", func(t *testing.T) {
		r := trpcgo.NewRouter()
		trpcgo.VoidQuery(r, "get", func(_ context.Context) (WithTSSkip, error) {
			return WithTSSkip{}, nil
		})
		ts := generateTS(t, r)
		if !strings.Contains(ts, "visible: string;") {
			t.Errorf("visible field should be present:\n%s", ts)
		}
		if strings.Contains(ts, "hidden") {
			t.Errorf("tstype:\"-\" field should be excluded:\n%s", ts)
		}
	})

	t.Run("tstype override", func(t *testing.T) {
		r := trpcgo.NewRouter()
		trpcgo.VoidQuery(r, "get", func(_ context.Context) (WithTSOverride, error) {
			return WithTSOverride{}, nil
		})
		ts := generateTS(t, r)
		if !strings.Contains(ts, "raw: Record<string, unknown>;") {
			t.Errorf("tstype override should replace type:\n%s", ts)
		}
	})

	t.Run("tstype readonly", func(t *testing.T) {
		r := trpcgo.NewRouter()
		trpcgo.VoidQuery(r, "get", func(_ context.Context) (WithReadonly, error) {
			return WithReadonly{}, nil
		})
		ts := generateTS(t, r)
		if !strings.Contains(ts, "readonly id: string;") {
			t.Errorf("readonly modifier should be present:\n%s", ts)
		}
		// name should NOT be readonly.
		if strings.Contains(ts, "readonly name") {
			t.Errorf("name should not be readonly:\n%s", ts)
		}
	})

	t.Run("tstype required overrides pointer optional", func(t *testing.T) {
		r := trpcgo.NewRouter()
		trpcgo.VoidQuery(r, "get", func(_ context.Context) (WithRequired, error) {
			return WithRequired{}, nil
		})
		ts := generateTS(t, r)
		if !strings.Contains(ts, "avatar: string;") {
			t.Errorf("required pointer should not be optional:\n%s", ts)
		}
		if strings.Contains(ts, "avatar?:") {
			t.Errorf("avatar should NOT be optional:\n%s", ts)
		}
		if !strings.Contains(ts, "bio?: string;") {
			t.Errorf("bio pointer should be optional:\n%s", ts)
		}
	})

	t.Run("json skip", func(t *testing.T) {
		r := trpcgo.NewRouter()
		trpcgo.VoidQuery(r, "get", func(_ context.Context) (WithJSONSkip, error) {
			return WithJSONSkip{}, nil
		})
		ts := generateTS(t, r)
		if !strings.Contains(ts, "public: string;") {
			t.Errorf("public field should be present:\n%s", ts)
		}
		if strings.Contains(ts, "secret") || strings.Contains(ts, "Secret") {
			t.Errorf("json:\"-\" field should be excluded:\n%s", ts)
		}
	})

	t.Run("embedded pointer struct flattened", func(t *testing.T) {
		r := trpcgo.NewRouter()
		trpcgo.VoidQuery(r, "get", func(_ context.Context) (WithPtrEmbed, error) {
			return WithPtrEmbed{}, nil
		})
		ts := generateTS(t, r)
		if !strings.Contains(ts, "export interface WithPtrEmbed {") {
			t.Errorf("WithPtrEmbed interface should be emitted:\n%s", ts)
		}
		// Base fields should be flattened into WithPtrEmbed.
		if !strings.Contains(ts, "id: string;") {
			t.Errorf("embedded PtrBase.ID should be flattened:\n%s", ts)
		}
		if !strings.Contains(ts, "createdAt: string;") {
			t.Errorf("embedded PtrBase.CreatedAt should be flattened:\n%s", ts)
		}
		if !strings.Contains(ts, "name: string;") {
			t.Errorf("own Name field should be present:\n%s", ts)
		}
		// PtrBase should NOT appear as a separate interface.
		if strings.Contains(ts, "export interface PtrBase") {
			t.Errorf("embedded PtrBase should not be a separate interface:\n%s", ts)
		}
	})

	t.Run("unexported field excluded", func(t *testing.T) {
		r := trpcgo.NewRouter()
		trpcgo.VoidQuery(r, "get", func(_ context.Context) (WithUnexported, error) {
			return WithUnexported{}, nil
		})
		ts := generateTS(t, r)
		if !strings.Contains(ts, "public: string;") {
			t.Errorf("public field should be present:\n%s", ts)
		}
		if strings.Contains(ts, "private") {
			t.Errorf("unexported field should be excluded:\n%s", ts)
		}
	})

	t.Run("all numeric types to number", func(t *testing.T) {
		r := trpcgo.NewRouter()
		trpcgo.VoidQuery(r, "get", func(_ context.Context) (AllNumerics, error) {
			return AllNumerics{}, nil
		})
		ts := generateTS(t, r)
		for _, field := range []string{"i", "i8", "i16", "i32", "i64", "u", "u8", "u16", "u32", "u64", "f32", "f64"} {
			if !strings.Contains(ts, field+": number;") {
				t.Errorf("field %q should be number:\n%s", field, ts)
			}
		}
	})

	t.Run("bool types", func(t *testing.T) {
		r := trpcgo.NewRouter()
		trpcgo.VoidQuery(r, "get", func(_ context.Context) (WithBool, error) {
			return WithBool{}, nil
		})
		ts := generateTS(t, r)
		if !strings.Contains(ts, "active: boolean;") {
			t.Errorf("bool should map to boolean:\n%s", ts)
		}
		if !strings.Contains(ts, "deleted?: boolean;") {
			t.Errorf("*bool should map to optional boolean:\n%s", ts)
		}
	})

	t.Run("deeply nested structs", func(t *testing.T) {
		r := trpcgo.NewRouter()
		trpcgo.VoidQuery(r, "get", func(_ context.Context) (CgOuter, error) {
			return CgOuter{}, nil
		})
		ts := generateTS(t, r)
		for _, iface := range []string{"CgOuter", "CgMiddle", "CgInner"} {
			if !strings.Contains(ts, "export interface "+iface+" {") {
				t.Errorf("missing interface %s:\n%s", iface, ts)
			}
		}
		if !strings.Contains(ts, "middle: CgMiddle;") {
			t.Errorf("CgOuter should reference CgMiddle:\n%s", ts)
		}
		if !strings.Contains(ts, "inner: CgInner;") {
			t.Errorf("CgMiddle should reference CgInner:\n%s", ts)
		}
	})

	t.Run("struct reuse emits single interface", func(t *testing.T) {
		r := trpcgo.NewRouter()
		trpcgo.Query(r, "getAddr", func(_ context.Context, _ CgAddress) (CgAddress, error) {
			return CgAddress{}, nil
		})
		trpcgo.VoidQuery(r, "listAddr", func(_ context.Context) ([]CgAddress, error) {
			return nil, nil
		})
		ts := generateTS(t, r)
		count := countPattern(ts, `(?m)^export interface CgAddress\b`)
		if count != 1 {
			t.Errorf("CgAddress should be emitted exactly once, got %d:\n%s", count, ts)
		}
	})

	t.Run("string type", func(t *testing.T) {
		r := trpcgo.NewRouter()
		trpcgo.VoidQuery(r, "ping", func(_ context.Context) (string, error) {
			return "", nil
		})
		ts := generateTS(t, r)
		if !strings.Contains(ts, "$Query<void, string>") {
			t.Errorf("string output should produce $Query<void, string>:\n%s", ts)
		}
	})

	t.Run("slice of struct", func(t *testing.T) {
		r := trpcgo.NewRouter()
		trpcgo.VoidQuery(r, "list", func(_ context.Context) ([]CgAddress, error) {
			return nil, nil
		})
		ts := generateTS(t, r)
		if !strings.Contains(ts, "$Query<void, CgAddress[]>") {
			t.Errorf("[]CgAddress should produce CgAddress[]:\n%s", ts)
		}
		if !strings.Contains(ts, "export interface CgAddress {") {
			t.Errorf("CgAddress interface should be emitted:\n%s", ts)
		}
	})
}

// ---------- inline struct tests ----------

func TestGenerateTSInlineStructs(t *testing.T) {
	t.Run("mixed field types", func(t *testing.T) {
		r := trpcgo.NewRouter()
		trpcgo.Query(r, "create", func(_ context.Context, input struct {
			Name   string `json:"name"`
			Count  int    `json:"count"`
			Active bool   `json:"active"`
		}) (string, error) {
			return "", nil
		})
		ts := generateTS(t, r)
		// Inline structs are emitted directly in the procedure type.
		if !strings.Contains(ts, "name: string") {
			t.Errorf("inline struct should have name field:\n%s", ts)
		}
		if !strings.Contains(ts, "count: number") {
			t.Errorf("inline struct should have count field:\n%s", ts)
		}
		if !strings.Contains(ts, "active: boolean") {
			t.Errorf("inline struct should have active field:\n%s", ts)
		}
	})

	t.Run("empty struct", func(t *testing.T) {
		r := trpcgo.NewRouter()
		trpcgo.Query(r, "noop", func(_ context.Context, _ struct{}) (string, error) {
			return "", nil
		})
		ts := generateTS(t, r)
		if !strings.Contains(ts, "Record<string, never>") {
			t.Errorf("empty struct should map to Record<string, never>:\n%s", ts)
		}
	})

	t.Run("with pointer field optional", func(t *testing.T) {
		r := trpcgo.NewRouter()
		trpcgo.Query(r, "create", func(_ context.Context, input struct {
			Name  string  `json:"name"`
			Label *string `json:"label"`
		}) (string, error) {
			return "", nil
		})
		ts := generateTS(t, r)
		if !strings.Contains(ts, "label?: string") {
			t.Errorf("pointer field in inline struct should be optional:\n%s", ts)
		}
	})

	t.Run("with json skip", func(t *testing.T) {
		r := trpcgo.NewRouter()
		trpcgo.Query(r, "create", func(_ context.Context, input struct {
			Name   string `json:"name"`
			Secret string `json:"-"`
		}) (string, error) {
			return "", nil
		})
		ts := generateTS(t, r)
		if !strings.Contains(ts, "name: string") {
			t.Errorf("name field should be present:\n%s", ts)
		}
		if strings.Contains(ts, "secret") || strings.Contains(ts, "Secret") {
			t.Errorf("json:\"-\" field should be excluded from inline struct:\n%s", ts)
		}
	})

	t.Run("with omitempty", func(t *testing.T) {
		r := trpcgo.NewRouter()
		trpcgo.Query(r, "create", func(_ context.Context, input struct {
			Name string   `json:"name"`
			Tags []string `json:"tags,omitempty"`
		}) (string, error) {
			return "", nil
		})
		ts := generateTS(t, r)
		if !strings.Contains(ts, "tags?: string[]") {
			t.Errorf("omitempty field should be optional:\n%s", ts)
		}
	})
}

// ---------- all procedure types test ----------

func TestGenerateTSAllProcTypes(t *testing.T) {
	r := trpcgo.NewRouter()

	trpcgo.Query(r, "q", func(_ context.Context, _ CgAddress) (CgAddress, error) {
		return CgAddress{}, nil
	})
	trpcgo.VoidQuery(r, "vq", func(_ context.Context) (string, error) {
		return "", nil
	})
	trpcgo.Mutation(r, "m", func(_ context.Context, _ CgAddress) (CgAddress, error) {
		return CgAddress{}, nil
	})
	trpcgo.VoidMutation(r, "vm", func(_ context.Context) (string, error) {
		return "", nil
	})
	trpcgo.Subscribe(r, "s", func(_ context.Context, _ CgAddress) (<-chan CgAddress, error) {
		return nil, nil
	})
	trpcgo.VoidSubscribe(r, "vs", func(_ context.Context) (<-chan string, error) {
		return nil, nil
	})

	ts := generateTS(t, r)

	// All three helper types should be present.
	for _, helper := range []string{
		`type $Query<TInput, TOutput>`,
		`type $Mutation<TInput, TOutput>`,
		`type $Subscription<TInput, TOutput>`,
	} {
		if !strings.Contains(ts, helper) {
			t.Errorf("missing helper type %q:\n%s", helper, ts)
		}
	}

	// Check each procedure type.
	checks := []string{
		"q: $Query<CgAddress, CgAddress>",
		"vq: $Query<void, string>",
		"m: $Mutation<CgAddress, CgAddress>",
		"vm: $Mutation<void, string>",
		"s: $Subscription<CgAddress, CgAddress>",
		"vs: $Subscription<void, string>",
	}
	for _, c := range checks {
		if !strings.Contains(ts, c) {
			t.Errorf("missing procedure %q:\n%s", c, ts)
		}
	}
}

// ---------- slice variant tests ----------

func TestGenerateTSSliceVariants(t *testing.T) {
	t.Run("[]string", func(t *testing.T) {
		r := trpcgo.NewRouter()
		trpcgo.VoidQuery(r, "get", func(_ context.Context) ([]string, error) { return nil, nil })
		ts := generateTS(t, r)
		if !strings.Contains(ts, "$Query<void, string[]>") {
			t.Errorf("[]string should produce string[]:\n%s", ts)
		}
	})

	t.Run("[]int", func(t *testing.T) {
		r := trpcgo.NewRouter()
		trpcgo.VoidQuery(r, "get", func(_ context.Context) ([]int, error) { return nil, nil })
		ts := generateTS(t, r)
		if !strings.Contains(ts, "$Query<void, number[]>") {
			t.Errorf("[]int should produce number[]:\n%s", ts)
		}
	})

	t.Run("[]bool", func(t *testing.T) {
		r := trpcgo.NewRouter()
		trpcgo.VoidQuery(r, "get", func(_ context.Context) ([]bool, error) { return nil, nil })
		ts := generateTS(t, r)
		if !strings.Contains(ts, "$Query<void, boolean[]>") {
			t.Errorf("[]bool should produce boolean[]:\n%s", ts)
		}
	})

	t.Run("[]CgAddress", func(t *testing.T) {
		r := trpcgo.NewRouter()
		trpcgo.VoidQuery(r, "get", func(_ context.Context) ([]CgAddress, error) { return nil, nil })
		ts := generateTS(t, r)
		if !strings.Contains(ts, "$Query<void, CgAddress[]>") {
			t.Errorf("[]CgAddress should produce CgAddress[]:\n%s", ts)
		}
	})

	t.Run("nested [][]string", func(t *testing.T) {
		r := trpcgo.NewRouter()
		trpcgo.VoidQuery(r, "get", func(_ context.Context) ([][]string, error) { return nil, nil })
		ts := generateTS(t, r)
		if !strings.Contains(ts, "$Query<void, string[][]>") {
			t.Errorf("[][]string should produce string[][]:\n%s", ts)
		}
	})

	t.Run("[3]string fixed array", func(t *testing.T) {
		r := trpcgo.NewRouter()
		trpcgo.VoidQuery(r, "get", func(_ context.Context) ([3]string, error) {
			return [3]string{}, nil
		})
		ts := generateTS(t, r)
		if !strings.Contains(ts, "$Query<void, string[]>") {
			t.Errorf("[3]string should produce string[]:\n%s", ts)
		}
	})

	t.Run("[]*string pointer elements", func(t *testing.T) {
		r := trpcgo.NewRouter()
		trpcgo.VoidQuery(r, "get", func(_ context.Context) ([]*string, error) { return nil, nil })
		ts := generateTS(t, r)
		if !strings.Contains(ts, "$Query<void, string[]>") {
			t.Errorf("[]*string should produce string[]:\n%s", ts)
		}
	})
}

// ---------- map variant tests ----------

func TestGenerateTSMapVariants(t *testing.T) {
	t.Run("map[string]int", func(t *testing.T) {
		r := trpcgo.NewRouter()
		trpcgo.VoidQuery(r, "get", func(_ context.Context) (map[string]int, error) { return nil, nil })
		ts := generateTS(t, r)
		if !strings.Contains(ts, "$Query<void, Record<string, number>>") {
			t.Errorf("map[string]int should produce Record<string, number>:\n%s", ts)
		}
	})

	t.Run("map[int]string", func(t *testing.T) {
		r := trpcgo.NewRouter()
		trpcgo.VoidQuery(r, "get", func(_ context.Context) (map[int]string, error) { return nil, nil })
		ts := generateTS(t, r)
		if !strings.Contains(ts, "$Query<void, Record<number, string>>") {
			t.Errorf("map[int]string should produce Record<number, string>:\n%s", ts)
		}
	})

	t.Run("map[string]CgAddress", func(t *testing.T) {
		r := trpcgo.NewRouter()
		trpcgo.VoidQuery(r, "get", func(_ context.Context) (map[string]CgAddress, error) { return nil, nil })
		ts := generateTS(t, r)
		if !strings.Contains(ts, "Record<string, CgAddress>") {
			t.Errorf("map[string]CgAddress should produce Record<string, CgAddress>:\n%s", ts)
		}
	})

	t.Run("map[string][]string", func(t *testing.T) {
		r := trpcgo.NewRouter()
		trpcgo.VoidQuery(r, "get", func(_ context.Context) (map[string][]string, error) { return nil, nil })
		ts := generateTS(t, r)
		if !strings.Contains(ts, "Record<string, string[]>") {
			t.Errorf("map[string][]string should produce Record<string, string[]>:\n%s", ts)
		}
	})

	t.Run("map[string]any", func(t *testing.T) {
		r := trpcgo.NewRouter()
		trpcgo.VoidQuery(r, "get", func(_ context.Context) (map[string]any, error) { return nil, nil })
		ts := generateTS(t, r)
		if !strings.Contains(ts, "Record<string, unknown>") {
			t.Errorf("map[string]any should produce Record<string, unknown>:\n%s", ts)
		}
	})
}

// ---------- kitchen sink test ----------

func TestGenerateTSKitchenSink(t *testing.T) {
	r := trpcgo.NewRouter()

	trpcgo.Query(r, "addr.get", func(_ context.Context, _ CgAddress) (CgAddress, error) {
		return CgAddress{}, nil
	})
	trpcgo.VoidQuery(r, "nums.get", func(_ context.Context) (AllNumerics, error) {
		return AllNumerics{}, nil
	})
	trpcgo.Mutation(r, "tree.create", func(_ context.Context, _ WithNested) (TreeNode, error) {
		return TreeNode{}, nil
	})
	trpcgo.VoidMutation(r, "bool.toggle", func(_ context.Context) (WithBool, error) {
		return WithBool{}, nil
	})
	trpcgo.Subscribe(r, "bytes.stream", func(_ context.Context, _ WithIntKeyMap) (<-chan WithBytes, error) {
		return nil, nil
	})
	trpcgo.VoidSubscribe(r, "ro.events", func(_ context.Context) (<-chan WithReadonly, error) {
		return nil, nil
	})
	trpcgo.Query(r, "addr.list", func(_ context.Context, _ CgAddress) ([]CgAddress, error) {
		return nil, nil
	})
	trpcgo.Query(r, "raw.get", func(_ context.Context, _ WithRequired) (WithTSOverride, error) {
		return WithTSOverride{}, nil
	})
	trpcgo.VoidQuery(r, "any.get", func(_ context.Context) (WithAnyField, error) {
		return WithAnyField{}, nil
	})
	trpcgo.VoidQuery(r, "deep.get", func(_ context.Context) (CgOuter, error) {
		return CgOuter{}, nil
	})

	ts := generateTS(t, r)

	// No Go package paths should leak.
	if strings.Contains(ts, "github.com/") {
		t.Errorf("output contains Go package paths:\n%s", ts)
	}

	// All named interfaces should be emitted exactly once.
	expectedInterfaces := []string{
		"CgAddress", "AllNumerics", "WithNested", "TreeNode", "WithBool",
		"WithIntKeyMap", "WithBytes", "WithReadonly", "WithRequired",
		"WithTSOverride", "WithAnyField", "CgOuter", "CgMiddle", "CgInner",
	}
	for _, name := range expectedInterfaces {
		count := countPattern(ts, `(?m)^export interface `+name+`\b`)
		if count != 1 {
			t.Errorf("interface %q should appear exactly once, got %d", name, count)
		}
	}

	// All three procedure helper types.
	for _, h := range []string{"$Query", "$Mutation", "$Subscription"} {
		if !strings.Contains(ts, "type "+h+"<") {
			t.Errorf("missing helper type %s:\n%s", h, ts)
		}
	}

	// RouterInputs and RouterOutputs.
	if !strings.Contains(ts, "export type RouterInputs =") {
		t.Errorf("missing RouterInputs:\n%s", ts)
	}
	if !strings.Contains(ts, "export type RouterOutputs =") {
		t.Errorf("missing RouterOutputs:\n%s", ts)
	}

	// AppRouter structure.
	if !strings.Contains(ts, "export type AppRouter =") {
		t.Errorf("missing AppRouter:\n%s", ts)
	}
}

// ---------- tsc validation test ----------

func TestGenerateTSTsc(t *testing.T) {
	// Find tsc binary.
	tscPath := filepath.Join("examples", "tanstack-query", "web", "node_modules", ".bin", "tsc")
	if _, err := os.Stat(tscPath); err != nil {
		// Fallback: check PATH.
		var lookupErr error
		tscPath, lookupErr = exec.LookPath("tsc")
		if lookupErr != nil {
			t.Skip("tsc not available, skipping TypeScript compilation check")
		}
	}

	// Build a router exercising many type paths.
	r := trpcgo.NewRouter()
	trpcgo.Query(r, "addr.get", func(_ context.Context, _ CgAddress) (CgAddress, error) {
		return CgAddress{}, nil
	})
	trpcgo.VoidQuery(r, "nums.get", func(_ context.Context) (AllNumerics, error) {
		return AllNumerics{}, nil
	})
	trpcgo.Mutation(r, "tree.create", func(_ context.Context, _ WithNested) (TreeNode, error) {
		return TreeNode{}, nil
	})
	trpcgo.VoidMutation(r, "bool.toggle", func(_ context.Context) (WithBool, error) {
		return WithBool{}, nil
	})
	trpcgo.Subscribe(r, "bytes.stream", func(_ context.Context, _ WithIntKeyMap) (<-chan WithBytes, error) {
		return nil, nil
	})
	trpcgo.VoidSubscribe(r, "ro.events", func(_ context.Context) (<-chan WithReadonly, error) {
		return nil, nil
	})
	trpcgo.VoidQuery(r, "raw.get", func(_ context.Context) (WithRawJSON, error) {
		return WithRawJSON{}, nil
	})
	trpcgo.VoidQuery(r, "any.get", func(_ context.Context) (WithAnyField, error) {
		return WithAnyField{}, nil
	})
	trpcgo.VoidQuery(r, "deep.get", func(_ context.Context) (CgOuter, error) {
		return CgOuter{}, nil
	})
	trpcgo.Query(r, "skip.get", func(_ context.Context, _ WithRequired) (WithTSOverride, error) {
		return WithTSOverride{}, nil
	})

	ts := generateTS(t, r)

	// Write generated TS and tsconfig to temp dir.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "trpc.ts"), []byte(ts), 0o644); err != nil {
		t.Fatal(err)
	}
	tsconfig := `{
  "compilerOptions": {
    "strict": true,
    "noEmit": true,
    "target": "ES2022",
    "module": "ES2022",
    "moduleResolution": "bundler",
    "skipLibCheck": true
  },
  "include": ["trpc.ts"]
}`
	if err := os.WriteFile(filepath.Join(dir, "tsconfig.json"), []byte(tsconfig), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(tscPath, "--noEmit", "--project", dir)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("tsc compilation failed:\n%s", string(output))
	}
}

// ---------- tests ported from typescriptify / tygo patterns ----------

func TestGenerateTSNestedCollections(t *testing.T) {
	t.Run("nested map map[string]map[string]int", func(t *testing.T) {
		r := trpcgo.NewRouter()
		trpcgo.VoidQuery(r, "get", func(_ context.Context) (NestedMapConfig, error) {
			return NestedMapConfig{}, nil
		})
		ts := generateTS(t, r)
		if !strings.Contains(ts, "settings: Record<string, Record<string, number>>;") {
			t.Errorf("nested map should produce Record<string, Record<string, number>>:\n%s", ts)
		}
	})

	t.Run("nested slice [][]Struct", func(t *testing.T) {
		r := trpcgo.NewRouter()
		trpcgo.VoidQuery(r, "get", func(_ context.Context) (Keyboard, error) {
			return Keyboard{}, nil
		})
		ts := generateTS(t, r)
		if !strings.Contains(ts, "keys: KeyboardKey[][];") {
			t.Errorf("[][]KeyboardKey should produce KeyboardKey[][]:\n%s", ts)
		}
		if !strings.Contains(ts, "export interface KeyboardKey {") {
			t.Errorf("KeyboardKey interface should be emitted:\n%s", ts)
		}
	})

	t.Run("map with struct values as field", func(t *testing.T) {
		r := trpcgo.NewRouter()
		trpcgo.VoidQuery(r, "get", func(_ context.Context) (APIConfig, error) {
			return APIConfig{}, nil
		})
		ts := generateTS(t, r)
		if !strings.Contains(ts, "endpoints: Record<string, Endpoint>;") {
			t.Errorf("map[string]Endpoint field should produce Record<string, Endpoint>:\n%s", ts)
		}
		if !strings.Contains(ts, "export interface Endpoint {") {
			t.Errorf("Endpoint interface should be emitted:\n%s", ts)
		}
		if !strings.Contains(ts, "url: string;") {
			t.Errorf("Endpoint should have url field:\n%s", ts)
		}
	})

	t.Run("slice of maps", func(t *testing.T) {
		r := trpcgo.NewRouter()
		trpcgo.VoidQuery(r, "get", func(_ context.Context) (BatchResult, error) {
			return BatchResult{}, nil
		})
		ts := generateTS(t, r)
		if !strings.Contains(ts, "results: Record<string, number>[];") {
			t.Errorf("[]map[string]int should produce Record<string, number>[]:\n%s", ts)
		}
	})

	t.Run("map of slices", func(t *testing.T) {
		r := trpcgo.NewRouter()
		trpcgo.VoidQuery(r, "get", func(_ context.Context) (GroupedTags, error) {
			return GroupedTags{}, nil
		})
		ts := generateTS(t, r)
		if !strings.Contains(ts, "groups: Record<string, string[]>;") {
			t.Errorf("map[string][]string should produce Record<string, string[]>:\n%s", ts)
		}
	})

	t.Run("deeply nested map[string]map[string][]int", func(t *testing.T) {
		r := trpcgo.NewRouter()
		trpcgo.VoidQuery(r, "get", func(_ context.Context) (DeeplyNestedMap, error) {
			return DeeplyNestedMap{}, nil
		})
		ts := generateTS(t, r)
		if !strings.Contains(ts, "data: Record<string, Record<string, number[]>>;") {
			t.Errorf("deeply nested map should produce Record<string, Record<string, number[]>>:\n%s", ts)
		}
	})

	t.Run("top-level nested slice [][]CgAddress", func(t *testing.T) {
		r := trpcgo.NewRouter()
		trpcgo.VoidQuery(r, "get", func(_ context.Context) ([][]CgAddress, error) {
			return nil, nil
		})
		ts := generateTS(t, r)
		if !strings.Contains(ts, "$Query<void, CgAddress[][]>") {
			t.Errorf("[][]CgAddress should produce CgAddress[][]:\n%s", ts)
		}
	})

	t.Run("top-level slice of maps", func(t *testing.T) {
		r := trpcgo.NewRouter()
		trpcgo.VoidQuery(r, "get", func(_ context.Context) ([]map[string]string, error) {
			return nil, nil
		})
		ts := generateTS(t, r)
		if !strings.Contains(ts, "$Query<void, Record<string, string>[]>") {
			t.Errorf("[]map[string]string should produce Record<string, string>[]:\n%s", ts)
		}
	})
}

func TestGenerateTSOmitzero(t *testing.T) {
	t.Run("omitzero makes field optional", func(t *testing.T) {
		r := trpcgo.NewRouter()
		trpcgo.VoidQuery(r, "get", func(_ context.Context) (WithOmitzero, error) {
			return WithOmitzero{}, nil
		})
		ts := generateTS(t, r)
		if !strings.Contains(ts, "name: string;") {
			t.Errorf("name should be required:\n%s", ts)
		}
		if !strings.Contains(ts, "created_at?: string;") {
			t.Errorf("omitzero field should be optional:\n%s", ts)
		}
	})

	t.Run("all optional strategies", func(t *testing.T) {
		r := trpcgo.NewRouter()
		trpcgo.VoidQuery(r, "get", func(_ context.Context) (OptionalVariants, error) {
			return OptionalVariants{}, nil
		})
		ts := generateTS(t, r)

		// Required: no pointer, no omit*.
		if !strings.Contains(ts, "always: string;") {
			t.Errorf("always should be required:\n%s", ts)
		}
		// omitempty → optional.
		if !strings.Contains(ts, "omitE?: string;") {
			t.Errorf("omitempty should be optional:\n%s", ts)
		}
		// omitzero → optional.
		if !strings.Contains(ts, "omitZ?: string;") {
			t.Errorf("omitzero should be optional:\n%s", ts)
		}
		// pointer → optional.
		if !strings.Contains(ts, "ptr?: string;") {
			t.Errorf("pointer should be optional:\n%s", ts)
		}
		// pointer + omitempty → optional.
		if !strings.Contains(ts, "ptrOmitE?: string;") {
			t.Errorf("pointer+omitempty should be optional:\n%s", ts)
		}
		// pointer + tstype required → required.
		if !strings.Contains(ts, "forcedReq: string;") {
			t.Errorf("tstype required should override pointer optional:\n%s", ts)
		}
		if strings.Contains(ts, "forcedReq?:") {
			t.Errorf("forcedReq should NOT be optional:\n%s", ts)
		}
	})
}

func TestGenerateTSMultipleEmbedded(t *testing.T) {
	t.Run("multiple embedded structs flattened", func(t *testing.T) {
		r := trpcgo.NewRouter()
		trpcgo.VoidQuery(r, "get", func(_ context.Context) (FullEntity, error) {
			return FullEntity{}, nil
		})
		ts := generateTS(t, r)
		if !strings.Contains(ts, "export interface FullEntity {") {
			t.Errorf("FullEntity interface should be emitted:\n%s", ts)
		}
		// Fields from Timestamps should be flattened.
		if !strings.Contains(ts, "createdAt: string;") {
			t.Errorf("Timestamps.CreatedAt should be flattened:\n%s", ts)
		}
		if !strings.Contains(ts, "updatedAt: string;") {
			t.Errorf("Timestamps.UpdatedAt should be flattened:\n%s", ts)
		}
		// Fields from Metadata should be flattened.
		if !strings.Contains(ts, "version: number;") {
			t.Errorf("Metadata.Version should be flattened:\n%s", ts)
		}
		if !strings.Contains(ts, "source: string;") {
			t.Errorf("Metadata.Source should be flattened:\n%s", ts)
		}
		// Own field.
		if !strings.Contains(ts, "name: string;") {
			t.Errorf("own Name field should be present:\n%s", ts)
		}
		// Embedded types should NOT appear as separate interfaces.
		if strings.Contains(ts, "export interface Timestamps") {
			t.Errorf("Timestamps should not be a separate interface:\n%s", ts)
		}
		if strings.Contains(ts, "export interface Metadata") {
			t.Errorf("Metadata should not be a separate interface:\n%s", ts)
		}
	})
}

func TestGenerateTSFieldsWithoutJSONTag(t *testing.T) {
	t.Run("fields without json tag use Go name", func(t *testing.T) {
		r := trpcgo.NewRouter()
		trpcgo.VoidQuery(r, "get", func(_ context.Context) (NoJSONTags, error) {
			return NoJSONTags{}, nil
		})
		ts := generateTS(t, r)
		// Exported fields should use their Go name.
		if !strings.Contains(ts, "PublicName: string;") {
			t.Errorf("exported field without json tag should use Go name:\n%s", ts)
		}
		if !strings.Contains(ts, "PublicCount: number;") {
			t.Errorf("exported field without json tag should use Go name:\n%s", ts)
		}
		// Unexported field should be excluded.
		if strings.Contains(ts, "privateVal") || strings.Contains(ts, "private") {
			t.Errorf("unexported field should be excluded:\n%s", ts)
		}
	})
}

func TestGenerateTSTscExtended(t *testing.T) {
	// Extended tsc validation covering all new patterns from typescriptify/tygo.
	tscPath := filepath.Join("examples", "tanstack-query", "web", "node_modules", ".bin", "tsc")
	if _, err := os.Stat(tscPath); err != nil {
		var lookupErr error
		tscPath, lookupErr = exec.LookPath("tsc")
		if lookupErr != nil {
			t.Skip("tsc not available, skipping TypeScript compilation check")
		}
	}

	r := trpcgo.NewRouter()

	// Nested collections.
	trpcgo.VoidQuery(r, "nested.mapmap", func(_ context.Context) (NestedMapConfig, error) {
		return NestedMapConfig{}, nil
	})
	trpcgo.VoidQuery(r, "nested.keyboard", func(_ context.Context) (Keyboard, error) {
		return Keyboard{}, nil
	})
	trpcgo.VoidQuery(r, "nested.apiconfig", func(_ context.Context) (APIConfig, error) {
		return APIConfig{}, nil
	})
	trpcgo.VoidQuery(r, "nested.batch", func(_ context.Context) (BatchResult, error) {
		return BatchResult{}, nil
	})
	trpcgo.VoidQuery(r, "nested.grouped", func(_ context.Context) (GroupedTags, error) {
		return GroupedTags{}, nil
	})
	trpcgo.VoidQuery(r, "nested.deep", func(_ context.Context) (DeeplyNestedMap, error) {
		return DeeplyNestedMap{}, nil
	})

	// Optional strategies.
	trpcgo.VoidQuery(r, "opt.omitzero", func(_ context.Context) (WithOmitzero, error) {
		return WithOmitzero{}, nil
	})
	trpcgo.VoidQuery(r, "opt.variants", func(_ context.Context) (OptionalVariants, error) {
		return OptionalVariants{}, nil
	})

	// Multiple embedded.
	trpcgo.VoidQuery(r, "entity.full", func(_ context.Context) (FullEntity, error) {
		return FullEntity{}, nil
	})

	// Fields without JSON tags.
	trpcgo.VoidQuery(r, "raw.nojson", func(_ context.Context) (NoJSONTags, error) {
		return NoJSONTags{}, nil
	})

	// Recursive.
	trpcgo.VoidQuery(r, "tree.get", func(_ context.Context) (TreeNode, error) {
		return TreeNode{}, nil
	})

	// Top-level complex types.
	trpcgo.VoidQuery(r, "complex.sliceslice", func(_ context.Context) ([][]CgAddress, error) {
		return nil, nil
	})
	trpcgo.VoidQuery(r, "complex.slicemap", func(_ context.Context) ([]map[string]string, error) {
		return nil, nil
	})

	// Tags: readonly, required, override, skip.
	trpcgo.VoidQuery(r, "tags.readonly", func(_ context.Context) (WithReadonly, error) {
		return WithReadonly{}, nil
	})
	trpcgo.VoidQuery(r, "tags.required", func(_ context.Context) (WithRequired, error) {
		return WithRequired{}, nil
	})
	trpcgo.VoidQuery(r, "tags.override", func(_ context.Context) (WithTSOverride, error) {
		return WithTSOverride{}, nil
	})
	trpcgo.VoidQuery(r, "tags.skip", func(_ context.Context) (WithTSSkip, error) {
		return WithTSSkip{}, nil
	})

	ts := generateTS(t, r)

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "trpc.ts"), []byte(ts), 0o644); err != nil {
		t.Fatal(err)
	}
	tsconfig := `{
  "compilerOptions": {
    "strict": true,
    "noEmit": true,
    "target": "ES2022",
    "module": "ES2022",
    "moduleResolution": "bundler",
    "skipLibCheck": true
  },
  "include": ["trpc.ts"]
}`
	if err := os.WriteFile(filepath.Join(dir, "tsconfig.json"), []byte(tsconfig), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(tscPath, "--noEmit", "--project", dir)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("tsc compilation failed:\n%s\n\nGenerated TypeScript:\n%s", string(output), ts)
	}
}

// TestGenerateZodBasic verifies that GenerateZod produces Zod schemas
// with correct validate tag constraints from reflect-based type info.
func TestGenerateZodBasic(t *testing.T) {
	r := trpcgo.NewRouter()

	trpcgo.Mutation(r, "auth.login", func(_ context.Context, input ZodLoginInput) (struct {
		Token string `json:"token"`
	}, error) {
		return struct {
			Token string `json:"token"`
		}{Token: "tok"}, nil
	})

	trpcgo.Mutation(r, "item.create", func(_ context.Context, input ZodCreateItemInput) (struct {
		ID string `json:"id"`
	}, error) {
		return struct {
			ID string `json:"id"`
		}{ID: "1"}, nil
	})

	zod := generateZod(t, r)
	t.Log("Generated Zod:\n" + zod)

	// Must have import.
	if !strings.Contains(zod, `import { z } from "zod"`) {
		t.Error("expected zod import")
	}

	// ZodLoginInput should have email() and password constraints.
	if !strings.Contains(zod, "ZodLoginInputSchema") {
		t.Error("expected ZodLoginInputSchema")
	}
	if !strings.Contains(zod, "z.email()") {
		t.Error("expected z.email() for email field")
	}
	if !strings.Contains(zod, ".min(8)") {
		t.Error("expected .min(8) for password field")
	}
	if !strings.Contains(zod, ".max(128)") {
		t.Error("expected .max(128) for password field")
	}

	// ZodCreateItemInput should have constraints.
	if !strings.Contains(zod, "ZodCreateItemInputSchema") {
		t.Error("expected ZodCreateItemInputSchema")
	}
	if !strings.Contains(zod, ".gte(0)") {
		t.Error("expected .gte(0) for count field")
	}
	if !strings.Contains(zod, ".lte(1000)") {
		t.Error("expected .lte(1000) for count field")
	}
}

// TestGenerateZodOptional verifies that optional fields (pointer, omitempty)
// get .optional() in Zod output.
func TestGenerateZodOptional(t *testing.T) {
	r := trpcgo.NewRouter()

	trpcgo.Query(r, "search", func(_ context.Context, input ZodOptionalInput) ([]string, error) {
		return nil, nil
	})

	zod := generateZod(t, r)
	t.Log("Generated Zod:\n" + zod)

	if !strings.Contains(zod, "ZodOptionalInputSchema") {
		t.Error("expected ZodOptionalInputSchema")
	}

	// query has omitempty → optional
	if !strings.Contains(zod, ".optional()") {
		t.Error("expected .optional() for optional fields")
	}
}

// TestGenerateZodGoKind verifies that Go kind metadata is used for correct
// Zod base types (z.int() vs z.number() vs z.float64()).
func TestGenerateZodGoKind(t *testing.T) {
	r := trpcgo.NewRouter()

	type NumInput struct {
		IntVal   int     `json:"intVal"`
		FloatVal float64 `json:"floatVal"`
		StrVal   string  `json:"strVal"`
		BoolVal  bool    `json:"boolVal"`
	}

	trpcgo.Query(r, "nums", func(_ context.Context, input NumInput) (string, error) {
		return "", nil
	})

	zod := generateZod(t, r)
	t.Log("Generated Zod:\n" + zod)

	if !strings.Contains(zod, "z.int()") {
		t.Error("expected z.int() for int field")
	}
	if !strings.Contains(zod, "z.float64()") {
		t.Error("expected z.float64() for float64 field")
	}
	if !strings.Contains(zod, "z.string()") {
		t.Error("expected z.string() for string field")
	}
	if !strings.Contains(zod, "z.boolean()") {
		t.Error("expected z.boolean() for bool field")
	}
}

// TestGenerateZodVoidInputProducesNothing verifies that procedures
// with nil input types (registered without input) produce no Zod schemas.
func TestGenerateZodVoidInputProducesNothing(t *testing.T) {
	r := trpcgo.NewRouter()

	// VoidQuery registers with nil inputType → InputTS = "void".
	trpcgo.VoidQuery(r, "hello", func(_ context.Context) (string, error) {
		return "hi", nil
	})

	dir := t.TempDir()
	out := filepath.Join(dir, "zod.ts")
	if err := r.GenerateZod(out); err != nil {
		t.Fatalf("GenerateZod: %v", err)
	}

	// File should not exist since WriteZodSchemas returns nil for no input types.
	if _, err := os.Stat(out); err == nil {
		data, _ := os.ReadFile(out)
		t.Logf("Unexpected zod output:\n%s", string(data))
		t.Error("expected no Zod file when all inputs are void")
	}
}

// TestGenerateZodMini verifies that GenerateZod with zodMini produces
// zod/mini functional syntax (z.optional(), z.check()).
func TestGenerateZodMini(t *testing.T) {
	r := trpcgo.NewRouter(trpcgo.WithZodMini(true))

	trpcgo.Mutation(r, "auth.login", func(_ context.Context, input ZodLoginInput) (struct {
		Token string `json:"token"`
	}, error) {
		return struct {
			Token string `json:"token"`
		}{Token: "tok"}, nil
	})

	trpcgo.Query(r, "search", func(_ context.Context, input ZodOptionalInput) ([]string, error) {
		return nil, nil
	})

	zod := generateZod(t, r)
	t.Log("Generated Zod (mini):\n" + zod)

	// Must use zod/mini import.
	if !strings.Contains(zod, `import * as z from "zod/mini"`) {
		t.Error("expected zod/mini import")
	}

	// Optional fields should use z.optional() wrapper style.
	if !strings.Contains(zod, "z.optional(") {
		t.Error("expected z.optional() wrapper for mini style")
	}

	// Constraints should use z.check() style.
	if !strings.Contains(zod, ".check(") {
		t.Error("expected .check() for mini style constraints")
	}

	// Should NOT have .optional() chain style.
	if strings.Contains(zod, ".optional()") {
		t.Error("mini style should not use .optional() chaining")
	}
}

// ---------- misc edge case tests ----------

func TestGenerateTSMiscEdgeCases(t *testing.T) {
	t.Run("inline struct in map value", func(t *testing.T) {
		r := trpcgo.NewRouter()
		trpcgo.VoidQuery(r, "get", func(_ context.Context) (map[string]struct {
			Foo uint32 `json:"foo"`
		}, error) {
			return nil, nil
		})
		ts := generateTS(t, r)
		if !strings.Contains(ts, "Record<string, { foo: number }>") {
			t.Errorf("map[string]struct should produce Record<string, { foo: number }>:\n%s", ts)
		}
	})

	t.Run("inline struct in slice", func(t *testing.T) {
		r := trpcgo.NewRouter()
		trpcgo.VoidQuery(r, "get", func(_ context.Context) ([]struct {
			Name string `json:"name"`
		}, error) {
			return nil, nil
		})
		ts := generateTS(t, r)
		if !strings.Contains(ts, "{ name: string }[]") {
			t.Errorf("[]struct should produce { name: string }[]:\n%s", ts)
		}
	})

	t.Run("rune returns number", func(t *testing.T) {
		r := trpcgo.NewRouter()
		trpcgo.VoidQuery(r, "get", func(_ context.Context) (rune, error) { return 0, nil })
		ts := generateTS(t, r)
		if !strings.Contains(ts, "$Query<void, number>") {
			t.Errorf("rune should produce number:\n%s", ts)
		}
	})

	t.Run("byte returns number", func(t *testing.T) {
		r := trpcgo.NewRouter()
		trpcgo.VoidQuery(r, "get", func(_ context.Context) (byte, error) { return 0, nil })
		ts := generateTS(t, r)
		if !strings.Contains(ts, "$Query<void, number>") {
			t.Errorf("byte should produce number:\n%s", ts)
		}
	})

	t.Run("json.Number returns number", func(t *testing.T) {
		r := trpcgo.NewRouter()
		trpcgo.VoidQuery(r, "get", func(_ context.Context) (WithJSONNumber, error) {
			return WithJSONNumber{}, nil
		})
		ts := generateTS(t, r)
		if !strings.Contains(ts, "value: number;") {
			t.Errorf("json.Number should map to number (marshals as unquoted JSON number):\n%s", ts)
		}
	})
}

// ---------- extends tests ----------

func TestGenerateTSExtends(t *testing.T) {
	t.Run("basic extends", func(t *testing.T) {
		r := trpcgo.NewRouter()
		trpcgo.VoidQuery(r, "get", func(_ context.Context) (ExUser, error) {
			return ExUser{}, nil
		})
		ts := generateTS(t, r)
		t.Log(ts)
		// ExUser should extend ExBase.
		if !strings.Contains(ts, "export interface ExUser extends ExBase {") {
			t.Errorf("should produce extends clause:\n%s", ts)
		}
		// ExBase should be emitted as its own interface.
		if !strings.Contains(ts, "export interface ExBase {") {
			t.Errorf("ExBase should be emitted as separate interface:\n%s", ts)
		}
		if !strings.Contains(ts, "id: string;") {
			t.Errorf("ExBase should have id field:\n%s", ts)
		}
		// ExUser's own fields should be present but not ExBase's.
		if !strings.Contains(ts, "name: string;") {
			t.Errorf("ExUser should have name field:\n%s", ts)
		}
	})

	t.Run("pointer extends wraps with Partial", func(t *testing.T) {
		r := trpcgo.NewRouter()
		trpcgo.VoidQuery(r, "get", func(_ context.Context) (ExWithPtrExtends, error) {
			return ExWithPtrExtends{}, nil
		})
		ts := generateTS(t, r)
		t.Log(ts)
		if !strings.Contains(ts, "export interface ExWithPtrExtends extends Partial<ExBase> {") {
			t.Errorf("pointer extends should wrap with Partial:\n%s", ts)
		}
	})

	t.Run("pointer extends with required skips Partial", func(t *testing.T) {
		r := trpcgo.NewRouter()
		trpcgo.VoidQuery(r, "get", func(_ context.Context) (ExWithPtrReqExtends, error) {
			return ExWithPtrReqExtends{}, nil
		})
		ts := generateTS(t, r)
		t.Log(ts)
		if !strings.Contains(ts, "export interface ExWithPtrReqExtends extends ExBase {") {
			t.Errorf("required pointer extends should not wrap with Partial:\n%s", ts)
		}
	})

	t.Run("multiple extends", func(t *testing.T) {
		r := trpcgo.NewRouter()
		trpcgo.VoidQuery(r, "get", func(_ context.Context) (ExMultiExtends, error) {
			return ExMultiExtends{}, nil
		})
		ts := generateTS(t, r)
		t.Log(ts)
		if !strings.Contains(ts, "extends ExBase, ExAuditFields") {
			t.Errorf("should extend both bases:\n%s", ts)
		}
		// Own field should be present.
		if !strings.Contains(ts, "title: string;") {
			t.Errorf("own field should be present:\n%s", ts)
		}
		// Both base interfaces should be emitted.
		if !strings.Contains(ts, "export interface ExBase {") {
			t.Errorf("ExBase should be emitted:\n%s", ts)
		}
		if !strings.Contains(ts, "export interface ExAuditFields {") {
			t.Errorf("ExAuditFields should be emitted:\n%s", ts)
		}
	})
}

// ---------- ts_doc tests ----------

func TestGenerateTSTsDoc(t *testing.T) {
	r := trpcgo.NewRouter()
	trpcgo.VoidQuery(r, "get", func(_ context.Context) (WithTSDoc, error) {
		return WithTSDoc{}, nil
	})
	ts := generateTS(t, r)
	t.Log(ts)

	// Fields with ts_doc should have JSDoc comments.
	if !strings.Contains(ts, "/** The hostname to connect to */") {
		t.Errorf("host field should have JSDoc:\n%s", ts)
	}
	if !strings.Contains(ts, "/** Port number (1-65535) */") {
		t.Errorf("port field should have JSDoc:\n%s", ts)
	}
	// Field without ts_doc should have no JSDoc.
	if strings.Contains(ts, "/** name") {
		t.Errorf("name field should not have JSDoc:\n%s", ts)
	}
}

// ---------- structural tsc validation with satisfies ----------

func TestGenerateTSNodeExecution(t *testing.T) {
	tscPath := filepath.Join("examples", "tanstack-query", "web", "node_modules", ".bin", "tsc")
	if _, err := os.Stat(tscPath); err != nil {
		var lookupErr error
		tscPath, lookupErr = exec.LookPath("tsc")
		if lookupErr != nil {
			t.Skip("tsc not available, skipping TypeScript structural check")
		}
	}

	r := trpcgo.NewRouter()
	trpcgo.Query(r, "addr.get", func(_ context.Context, _ CgAddress) (CgAddress, error) {
		return CgAddress{}, nil
	})
	trpcgo.VoidQuery(r, "nums.get", func(_ context.Context) (AllNumerics, error) {
		return AllNumerics{}, nil
	})
	trpcgo.VoidQuery(r, "tree.get", func(_ context.Context) (TreeNode, error) {
		return TreeNode{}, nil
	})
	trpcgo.VoidQuery(r, "bool.get", func(_ context.Context) (WithBool, error) {
		return WithBool{}, nil
	})
	trpcgo.VoidQuery(r, "ext.get", func(_ context.Context) (ExUser, error) {
		return ExUser{}, nil
	})
	trpcgo.VoidQuery(r, "doc.get", func(_ context.Context) (WithTSDoc, error) {
		return WithTSDoc{}, nil
	})

	ts := generateTS(t, r)

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "trpc.ts"), []byte(ts), 0o644); err != nil {
		t.Fatal(err)
	}

	// Write a structural check file that validates types via satisfies.
	check := `import type { CgAddress, AllNumerics, TreeNode, WithBool, ExUser, ExBase, WithTSDoc } from "./trpc.js";

// Structural validation — these fail at compile time if types are wrong.
const addr = { street: "Main St", city: "NYC" } satisfies CgAddress;
const nums = {
  i: 1, i8: 2, i16: 3, i32: 4, i64: 5,
  u: 6, u8: 7, u16: 8, u32: 9, u64: 10,
  f32: 1.1, f64: 2.2,
} satisfies AllNumerics;
const tree: TreeNode = { label: "root", children: [{ label: "child", children: [] }] };
const bools = { active: true } satisfies WithBool;
const base = { id: "1" } satisfies ExBase;
const user = { id: "1", name: "Alice" } satisfies ExUser;
const doc = { host: "localhost", port: 8080, name: "test" } satisfies WithTSDoc;

// Suppress unused variable warnings.
void addr; void nums; void tree; void bools; void base; void user; void doc;
`
	if err := os.WriteFile(filepath.Join(dir, "check.ts"), []byte(check), 0o644); err != nil {
		t.Fatal(err)
	}

	tsconfig := `{
  "compilerOptions": {
    "strict": true,
    "noEmit": true,
    "target": "ES2022",
    "module": "ES2022",
    "moduleResolution": "bundler",
    "skipLibCheck": true
  },
  "include": ["trpc.ts", "check.ts"]
}`
	if err := os.WriteFile(filepath.Join(dir, "tsconfig.json"), []byte(tsconfig), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(tscPath, "--noEmit", "--project", dir)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("tsc structural check failed:\n%s\n\nGenerated TypeScript:\n%s", string(output), ts)
	}
}

// ---------- Zod extends tests ----------

func TestGenerateZodExtends(t *testing.T) {
	t.Run("basic extends uses .extend()", func(t *testing.T) {
		r := trpcgo.NewRouter()
		trpcgo.Mutation(r, "create", func(_ context.Context, input ZodDerived) (string, error) {
			return "", nil
		})
		zod := generateZod(t, r)
		t.Log(zod)

		// Base schema must be emitted.
		if !strings.Contains(zod, "export const ZodBaseSchema = z.object({") {
			t.Error("missing ZodBaseSchema")
		}
		if !strings.Contains(zod, "id: z.string(),") {
			t.Error("ZodBaseSchema missing id field")
		}

		// Derived must use .extend(), not z.object().
		if !strings.Contains(zod, "export const ZodDerivedSchema = ZodBaseSchema.extend({") {
			t.Errorf("ZodDerivedSchema should use ZodBaseSchema.extend(), got:\n%s", zod)
		}
		if !strings.Contains(zod, "name: z.string().min(1),") {
			t.Error("ZodDerivedSchema missing name field")
		}

		// Base schema must appear before derived (topo order).
		baseIdx := strings.Index(zod, "ZodBaseSchema")
		derivedIdx := strings.Index(zod, "ZodDerivedSchema")
		if baseIdx > derivedIdx {
			t.Error("ZodBaseSchema must appear before ZodDerivedSchema (topo order)")
		}
	})

	t.Run("multiple extends uses .merge().extend()", func(t *testing.T) {
		r := trpcgo.NewRouter()
		trpcgo.Mutation(r, "create", func(_ context.Context, input ZodMultiBase) (string, error) {
			return "", nil
		})
		zod := generateZod(t, r)
		t.Log(zod)

		// Must emit both base schemas.
		if !strings.Contains(zod, "ZodBaseSchema") {
			t.Error("missing ZodBaseSchema")
		}
		if !strings.Contains(zod, "ExAuditFieldsSchema") {
			t.Error("missing ExAuditFieldsSchema")
		}

		// Derived uses merge+extend.
		if !strings.Contains(zod, "ZodBaseSchema.merge(ExAuditFieldsSchema).extend({") {
			t.Errorf("ZodMultiBaseSchema should use .merge().extend(), got:\n%s", zod)
		}
	})

	t.Run("pointer extends uses .partial()", func(t *testing.T) {
		r := trpcgo.NewRouter()
		trpcgo.Mutation(r, "create", func(_ context.Context, input ZodPtrExtends) (string, error) {
			return "", nil
		})
		zod := generateZod(t, r)
		t.Log(zod)

		// Pointer extends without required → Partial<ZodBase> → .partial().extend()
		if !strings.Contains(zod, "ZodBaseSchema.partial().extend({") {
			t.Errorf("pointer extends should use .partial().extend(), got:\n%s", zod)
		}
		if !strings.Contains(zod, "label: z.string(),") {
			t.Error("missing own field 'label'")
		}
	})

	t.Run("base type reachable only through extends", func(t *testing.T) {
		// ZodDerived.Fields has no reference to ZodBase — the ONLY
		// link is through Extends. If transitiveReachable doesn't
		// walk Extends, ZodBaseSchema won't be emitted.
		r := trpcgo.NewRouter()
		trpcgo.Mutation(r, "create", func(_ context.Context, input ZodDerived) (string, error) {
			return "", nil
		})
		zod := generateZod(t, r)

		if !strings.Contains(zod, "export const ZodBaseSchema") {
			t.Errorf("ZodBase should be reachable through extends, but missing from output:\n%s", zod)
		}
	})

	t.Run("cyclic type with extends uses z.lazy + .extend()", func(t *testing.T) {
		r := trpcgo.NewRouter()
		trpcgo.Mutation(r, "create", func(_ context.Context, input ZodCyclicNode) (string, error) {
			return "", nil
		})
		zod := generateZod(t, r)
		t.Log(zod)

		// The exact expected output for the cyclic+extends case.
		// z.lazy wraps the whole expression for the cycle; .extend chains the base.
		want := `export const ZodCyclicNodeSchema: z.ZodType<ZodCyclicNode> = z.lazy(() => ZodBaseSchema.extend({
  children: z.array(ZodCyclicNodeSchema),
}));`
		if !strings.Contains(zod, want) {
			t.Errorf("cyclic+extends output mismatch.\nwant:\n%s\n\ngot:\n%s", want, zod)
		}

		// Base must appear before derived (topo order).
		baseIdx := strings.Index(zod, "ZodBaseSchema =")
		derivedIdx := strings.Index(zod, "ZodCyclicNodeSchema")
		if baseIdx < 0 || derivedIdx < 0 || baseIdx > derivedIdx {
			t.Errorf("ZodBaseSchema must be emitted before ZodCyclicNodeSchema:\n%s", zod)
		}
	})
}

func TestGenerateZodStaleFileCleanup(t *testing.T) {
	dir := t.TempDir()
	zodOut := filepath.Join(dir, "zod.ts")

	// Step 1: Generate with input types → file exists.
	r1 := trpcgo.NewRouter()
	trpcgo.Mutation(r1, "login", func(_ context.Context, input ZodLoginInput) (string, error) {
		return "", nil
	})
	if err := r1.GenerateZod(zodOut); err != nil {
		t.Fatalf("GenerateZod (with inputs): %v", err)
	}
	if _, err := os.ReadFile(zodOut); err != nil {
		t.Fatalf("Zod file should exist after generation with inputs: %v", err)
	}

	// Step 2: Generate with NO input types → stale file should be removed.
	r2 := trpcgo.NewRouter()
	trpcgo.VoidQuery(r2, "ping", func(_ context.Context) (string, error) {
		return "pong", nil
	})
	if err := r2.GenerateZod(zodOut); err != nil {
		t.Fatalf("GenerateZod (void inputs): %v", err)
	}

	if _, err := os.ReadFile(zodOut); err == nil {
		t.Error("stale Zod file should be removed when all inputs are void")
	}
}
