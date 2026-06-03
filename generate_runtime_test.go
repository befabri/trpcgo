package trpcgo_test

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/befabri/trpcgo"
	"github.com/befabri/trpcgo/internal/codegen"
	"github.com/befabri/trpcgo/internal/typemap"
)

func TestGenerateTSTsc(t *testing.T) {
	// Find tsc binary.
	tscPath := filepath.Join("examples", "start-trpc", "web", "node_modules", ".bin", "tsc")
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
	symlinkNodeModules(t, dir)
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

func TestGenerateEnumsTsc(t *testing.T) {
	tscPath := filepath.Join("examples", "start-trpc", "web", "node_modules", ".bin", "tsc")
	if _, err := os.Stat(tscPath); err != nil {
		var lookupErr error
		tscPath, lookupErr = exec.LookPath("tsc")
		if lookupErr != nil {
			t.Skip("tsc not available, skipping TypeScript compilation check")
		}
	}

	// Covers awkward enum values that still need to compile as TypeScript.
	defs := []typemap.TypeDef{
		{Name: "Role", Kind: typemap.TypeDefUnion, UnionMembers: []string{`"viewer"`, `"admin"`}},
		{Name: "Word", Kind: typemap.TypeDefUnion, UnionMembers: []string{`"default"`, `"in"`, `"class"`}},
		{Name: "Weird", Kind: typemap.TypeDefUnion, UnionMembers: []string{`"3d"`, `"a.b"`, `"a-b"`, `""`, `"__proto__"`}},
		{Name: "Escaped", Kind: typemap.TypeDefUnion, UnionMembers: []string{`"a\"b"`, `"c\\d"`, `"e\tf"`}},
		{Name: "Dup", Kind: typemap.TypeDefUnion, UnionMembers: []string{`"x"`, `"x"`, `"y"`}},
		{Name: "Priority", Kind: typemap.TypeDefUnion, UnionMembers: []string{"1", "2"}},
	}

	var buf bytes.Buffer
	if err := codegen.WriteEnums(&buf, defs); err != nil {
		t.Fatal(err)
	}
	enumsTS := buf.String()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "enums.ts"), []byte(enumsTS), 0o644); err != nil {
		t.Fatal(err)
	}
	symlinkNodeModules(t, dir)
	tsconfig := `{
  "compilerOptions": {
    "strict": true,
    "noEmit": true,
    "target": "ES2022",
    "module": "ES2022",
    "moduleResolution": "bundler",
    "skipLibCheck": true
  },
  "include": ["enums.ts"]
}`
	if err := os.WriteFile(filepath.Join(dir, "tsconfig.json"), []byte(tsconfig), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(tscPath, "--noEmit", "--project", dir)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("tsc compilation failed:\n%s\n\nGenerated enums.ts:\n%s", string(output), enumsTS)
	}

	if strings.Contains(enumsTS, "PriorityEnum") {
		t.Errorf("numeric union should be skipped:\n%s", enumsTS)
	}
	if n := strings.Count(enumsTS, `x: "x"`); n != 1 {
		t.Errorf("duplicate value should dedup to one key, found %d:\n%s", n, enumsTS)
	}
}

func TestGenerateTSTscExtended(t *testing.T) {
	// Extended tsc validation covering all new patterns from typescriptify/tygo.
	tscPath := filepath.Join("examples", "start-trpc", "web", "node_modules", ".bin", "tsc")
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
	symlinkNodeModules(t, dir)
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

func TestGenerateTSNodeExecution(t *testing.T) {
	tscPath := filepath.Join("examples", "start-trpc", "web", "node_modules", ".bin", "tsc")
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
	symlinkNodeModules(t, dir)

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
