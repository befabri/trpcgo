package trpcgo_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/befabri/trpcgo"
)

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
		if !strings.Contains(zod, "id: z.string().min(1),") {
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
})).meta({ id: "ZodCyclicNode" });`
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

func TestGenerateZodUnsupportedComment(t *testing.T) {
	r := trpcgo.NewRouter()
	trpcgo.Mutation(r, "spawn", func(_ context.Context, input ZodUnsupportedInput) (string, error) {
		return "", nil
	})
	zod := generateZod(t, r)
	t.Log(zod)

	// minVal has only supported tags — no comment.
	if strings.Contains(zod, "minVal: z.float64().gte(0), /*") {
		t.Error("minVal should not have unsupported comment")
	}

	// maxVal should NOT have unsupported comment — gtefield is now supported via .refine().
	if strings.Contains(zod, "/* unsupported: gtefield") {
		t.Error("gtefield should not be flagged as unsupported (it generates .refine())")
	}

	// maxVal should generate .refine() for gtefield.
	if !strings.Contains(zod, ".refine(") {
		t.Errorf("expected .refine() for gtefield.\nOutput:\n%s", zod)
	}
	if !strings.Contains(zod, "data.maxVal >= data.minVal") {
		t.Errorf("expected refine callback with correct field names.\nOutput:\n%s", zod)
	}

	// label should flag custom_check (truly unsupported custom tag).
	if !strings.Contains(zod, "/* unsupported: custom_check */") {
		t.Errorf("label should have unsupported comment.\nOutput:\n%s", zod)
	}
}

func TestGenerateZodCrossField(t *testing.T) {
	r := trpcgo.NewRouter()
	trpcgo.Mutation(r, "update", func(_ context.Context, input ZodCrossFieldInput) (string, error) {
		return "", nil
	})
	zod := generateZod(t, r)
	t.Log(zod)

	// Should have two .refine() calls for gtefield.
	if strings.Count(zod, ".refine(") != 2 {
		t.Errorf("expected exactly 2 .refine() calls, got %d.\nOutput:\n%s", strings.Count(zod, ".refine("), zod)
	}

	// Check correct JSON field names (snake_case, not Go PascalCase).
	if !strings.Contains(zod, "data.max_val >= data.min_val") {
		t.Errorf("expected refine with JSON field names max_val >= min_val.\nOutput:\n%s", zod)
	}
	if !strings.Contains(zod, "data.end_val >= data.start_val") {
		t.Errorf("expected refine with JSON field names end_val >= start_val.\nOutput:\n%s", zod)
	}

	// No unsupported comments for gtefield.
	if strings.Contains(zod, "/* unsupported") {
		t.Errorf("no unsupported comments expected.\nOutput:\n%s", zod)
	}
}

func TestGenerateZodCrossFieldAllOps(t *testing.T) {
	r := trpcgo.NewRouter()
	trpcgo.Mutation(r, "check", func(_ context.Context, input ZodCrossFieldAllOps) (string, error) {
		return "", nil
	})
	zod := generateZod(t, r)
	t.Log(zod)

	tests := []struct {
		field string
		op    string
	}{
		{"b", ">="},  // gtefield
		{"c", "<="},  // ltefield
		{"d", ">"},   // gtfield
		{"e", "<"},   // ltfield
		{"f", "==="}, // eqfield
		{"g", "!=="}, // nefield
	}

	for _, tc := range tests {
		expected := fmt.Sprintf("data.%s %s data.a", tc.field, tc.op)
		if !strings.Contains(zod, expected) {
			t.Errorf("expected %q in .refine() callback.\nOutput:\n%s", expected, zod)
		}
	}

	if strings.Count(zod, ".refine(") != 6 {
		t.Errorf("expected 6 .refine() calls, got %d.\nOutput:\n%s", strings.Count(zod, ".refine("), zod)
	}
}

func TestGenerateZodInt64Number(t *testing.T) {
	r := trpcgo.NewRouter()
	trpcgo.Mutation(r, "big", func(_ context.Context, input ZodInt64Input) (string, error) {
		return "", nil
	})
	zod := generateZod(t, r)
	t.Log(zod)

	// int64 and uint64 should map to z.number() since JSON encodes them as numbers,
	// not z.int64()/z.uint64() which are ZodBigInt schemas in Zod 4.
	if strings.Contains(zod, "z.int64()") {
		t.Errorf("int64 should map to z.number(), not z.int64().\nOutput:\n%s", zod)
	}
	if strings.Contains(zod, "z.uint64()") {
		t.Errorf("uint64 should map to z.number(), not z.uint64().\nOutput:\n%s", zod)
	}

	// bigSigned and bigUnsigned should be z.number().
	if !strings.Contains(zod, "bigSigned: z.number(),") {
		t.Errorf("expected bigSigned: z.number().\nOutput:\n%s", zod)
	}
	if !strings.Contains(zod, "bigUnsigned: z.number(),") {
		t.Errorf("expected bigUnsigned: z.number().\nOutput:\n%s", zod)
	}

	// int32 should remain z.int32().
	if !strings.Contains(zod, "normalInt: z.int32(),") {
		t.Errorf("expected normalInt: z.int32().\nOutput:\n%s", zod)
	}
}

func TestGenerateZodNewTags(t *testing.T) {
	r := trpcgo.NewRouter()
	trpcgo.Mutation(r, "create", func(_ context.Context, input ZodNewTagsInput) (string, error) {
		return "", nil
	})
	zod := generateZod(t, r)
	t.Log(zod)

	// Format tags → base types.
	checks := map[string]string{
		"host":   "z.hostname()",
		"token":  "z.base64url()",
		"id":     "z.ulid()",
		"mac":    "z.mac()",
		"subnet": "z.cidrv4()",
		"code":   "z.uppercase()",
	}
	for field, base := range checks {
		if !strings.Contains(zod, field+": "+base) {
			t.Errorf("expected %s: %s.\nOutput:\n%s", field, base, zod)
		}
	}

	// Format + constraint combo: hex with .min(64).max(64).
	if !strings.Contains(zod, "hash: z.hex().min(64).max(64),") {
		t.Errorf("expected hash: z.hex().min(64).max(64).\nOutput:\n%s", zod)
	}

	// Constraint tags on string fields.
	if !strings.Contains(zod, `website: z.string().startsWith("https://").min(10),`) {
		t.Errorf("expected website with startsWith + min.\nOutput:\n%s", zod)
	}
	if !strings.Contains(zod, `file: z.string().endsWith(".json"),`) {
		t.Errorf("expected file with endsWith.\nOutput:\n%s", zod)
	}
	if !strings.Contains(zod, `path: z.string().includes("/api/"),`) {
		t.Errorf("expected path with includes.\nOutput:\n%s", zod)
	}

	// No unsupported comments — all tags are now recognized.
	if strings.Contains(zod, "/* unsupported") {
		t.Errorf("no unsupported comments expected.\nOutput:\n%s", zod)
	}
}

func TestZodRuntimeValidation(t *testing.T) {
	zodRuntimeDir, err := filepath.Abs("testdata/zodruntime")
	if err != nil {
		t.Fatal(err)
	}
	tsxPath := filepath.Join(zodRuntimeDir, "node_modules", ".bin", "tsx")
	if _, err := os.Stat(tsxPath); err != nil {
		t.Skip("zodruntime node_modules not installed, run: npm install --prefix testdata/zodruntime")
	}

	tscPath := filepath.Join(zodRuntimeDir, "node_modules", ".bin", "tsc")

	runValidation := func(t *testing.T, zodCode, script string) {
		t.Helper()
		dir := t.TempDir()

		// Symlink node_modules so imports resolve.
		if err := os.Symlink(filepath.Join(zodRuntimeDir, "node_modules"), filepath.Join(dir, "node_modules")); err != nil {
			t.Fatalf("symlink node_modules: %v", err)
		}
		if err := os.WriteFile(filepath.Join(dir, "schemas.ts"), []byte(zodCode), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "validate.ts"), []byte(script), 0o644); err != nil {
			t.Fatal(err)
		}

		// Type-check generated schemas with tsc --noEmit.
		tsconfig := `{
  "compilerOptions": {
    "strict": true,
    "noEmit": true,
    "target": "ES2022",
    "module": "ES2022",
    "moduleResolution": "bundler",
    "skipLibCheck": true
  },
  "include": ["schemas.ts"]
}`
		if err := os.WriteFile(filepath.Join(dir, "tsconfig.json"), []byte(tsconfig), 0o644); err != nil {
			t.Fatal(err)
		}
		tsc := exec.Command(tscPath, "--noEmit", "--project", dir)
		tsc.Dir = dir
		if tscOut, err := tsc.CombinedOutput(); err != nil {
			t.Fatalf("tsc type-check failed:\n%s\n\nGenerated schemas:\n%s", string(tscOut), zodCode)
		}

		cmd := exec.Command(tsxPath, filepath.Join(dir, "validate.ts"))
		cmd.Dir = dir
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("runtime validation failed:\n%s\n\nGenerated schemas:\n%s", string(output), zodCode)
		}
		t.Log(string(output))
	}

	t.Run("standard", func(t *testing.T) {
		r := trpcgo.NewRouter()
		// Register diverse types exercising all constraint categories.
		trpcgo.Mutation(r, "auth.login", func(_ context.Context, input ZodLoginInput) (string, error) {
			return "", nil
		})
		trpcgo.Mutation(r, "items.create", func(_ context.Context, input ZodCreateItemInput) (string, error) {
			return "", nil
		})
		trpcgo.Query(r, "search", func(_ context.Context, input ZodOptionalInput) (string, error) {
			return "", nil
		})
		trpcgo.Mutation(r, "derived.create", func(_ context.Context, input ZodDerived) (string, error) {
			return "", nil
		})
		trpcgo.Mutation(r, "multi.create", func(_ context.Context, input ZodMultiBase) (string, error) {
			return "", nil
		})
		trpcgo.Mutation(r, "ptr.create", func(_ context.Context, input ZodPtrExtends) (string, error) {
			return "", nil
		})
		trpcgo.Mutation(r, "cross.create", func(_ context.Context, input ZodCrossFieldInput) (string, error) {
			return "", nil
		})
		trpcgo.Mutation(r, "omit.create", func(_ context.Context, input ZodOmitInput) (string, error) {
			return "", nil
		})
		trpcgo.Mutation(r, "nums.create", func(_ context.Context, input ZodInt64Input) (string, error) {
			return "", nil
		})

		zod := generateZod(t, r)
		t.Log(zod)

		script := `import { z } from "zod";
import { zxTest } from "@traversable/zod-test";
import * as fc from "fast-check";
import * as schemas from "./schemas.js";

let passed = 0;
let failed = 0;
let fuzzed = 0;
function test(name: string, fn: () => void) {
  try { fn(); passed++; console.log("PASS:", name); }
  catch (e: any) { failed++; console.error("FAIL:", name, e?.message ?? e); }
}
function mustReject(name: string, schema: z.ZodType, data: unknown) {
  test(name, () => {
    const r = schema.safeParse(data);
    if (r.success) throw new Error("expected rejection, got success");
  });
}

// ---- Dynamic fuzz: auto-discover and fuzz every exported schema ----
//
// The string override handles the z.email()/fast-check incompatibility:
// fuzz()'s built-in applyStringFormat calls fc.emailAddress() for z.email(),
// but fast-check generates RFC emails that fail Zod 4's stricter regex.
// We intercept format:"email" and filter; return null for other strings
// to fall through to the built-in (which respects min/max constraints).
const zodEmailRe = /^(?!\.)(?!.*\.\.)([A-Za-z0-9_'+\-\.]*)[A-Za-z0-9_+-]@([A-Za-z0-9][A-Za-z0-9\-]*\.)+[A-Za-z]{2,}$/;
const fuzzOpts = {};
const fuzzOverrides = {
  string: (x: any, constraints: any) => {
    if (x.format === "email") return fc.emailAddress().filter((e: string) => zodEmailRe.test(e));
    return null; // fall through to built-in (respects min/max/length)
  },
};

// Schemas with .refine() can't be fuzzed (fast-check can't satisfy arbitrary
// JS predicates). In Zod 4, refinements live in _zod.def.checks as entries
// with _zod.def.check === "custom".
function hasRefine(v: any): boolean {
  const checks = v?._zod?.def?.checks;
  if (!Array.isArray(checks) || checks.length === 0) return false;
  return checks.some((ch: any) => ch?._zod?.def?.check === "custom");
}

for (const [name, value] of Object.entries(schemas)) {
  // Skip non-schema exports.
  if (typeof (value as any)?._zod?.def?.type !== "string") continue;

  // Skip schemas with .refine() — cross-field constraints are arbitrary JS.
  if (hasRefine(value)) {
    console.log("SKIP-REFINE:", name);
    continue;
  }

  test("fuzz " + name, () => {
    const arb = zxTest.fuzz(value as any, fuzzOpts, fuzzOverrides);
    fc.assert(fc.property(arb, (d) => { (value as any).parse(d); }), { numRuns: 100 });
    fuzzed++;
  });
}

// ---- Manual valid data ----

const S = schemas as any;

test("LoginInput valid", () => {
  S.ZodLoginInputSchema.parse({ email: "user@example.com", password: "securepass123" });
});

test("CreateItemInput valid", () => {
  S.ZodCreateItemInputSchema.parse({ name: "Widget", tags: ["electronics", "sale"], count: 42 });
});

test("CreateItemInput boundary", () => {
  S.ZodCreateItemInputSchema.parse({ name: "x", tags: [], count: 0 });
  S.ZodCreateItemInputSchema.parse({ name: "x".repeat(100), tags: Array(10).fill("a".repeat(50)), count: 1000 });
});

test("OptionalInput with all fields", () => {
  S.ZodOptionalInputSchema.parse({ query: "test", limit: 10, offset: 0 });
});

test("OptionalInput minimal", () => {
  S.ZodOptionalInputSchema.parse({ offset: 0 });
});

test("OptionalInput omitempty empty string", () => {
  S.ZodOptionalInputSchema.parse({ query: "", offset: 5 });
});

test("DerivedSchema inherits base", () => {
  S.ZodDerivedSchema.parse({ id: "abc-123", name: "Alice" });
});

test("MultiBase merges two bases", () => {
  S.ZodMultiBaseSchema.parse({ id: "1", createdBy: "a", updatedBy: "b", title: "T" });
});

test("PtrExtends partial base", () => {
  S.ZodPtrExtendsSchema.parse({ label: "x" });
  S.ZodPtrExtendsSchema.parse({ id: "1", label: "x" });
});

test("OmitInput skips id", () => {
  S.ZodOmitInputSchema.parse({ name: "Alice", active: true });
});

test("Int64Input numeric mapping", () => {
  S.ZodInt64InputSchema.parse({ bigSigned: 999999999, bigUnsigned: 999999999, normalInt: 42 });
});

test("CrossFieldInput valid", () => {
  S.ZodCrossFieldInputSchema.parse({ min_val: 1, max_val: 5, start_val: 1, end_val: 10 });
});

// ---- Constraint rejection ----

mustReject("LoginInput: invalid email", S.ZodLoginInputSchema, { email: "not-email", password: "securepass123" });
mustReject("LoginInput: password too short", S.ZodLoginInputSchema, { email: "a@b.com", password: "short" });
mustReject("LoginInput: password too long", S.ZodLoginInputSchema, { email: "a@b.com", password: "x".repeat(129) });
mustReject("LoginInput: missing email", S.ZodLoginInputSchema, { password: "securepass123" });
mustReject("LoginInput: missing password", S.ZodLoginInputSchema, { email: "a@b.com" });

mustReject("CreateItemInput: name too long", S.ZodCreateItemInputSchema, { name: "x".repeat(101), tags: [], count: 0 });
mustReject("CreateItemInput: count below min", S.ZodCreateItemInputSchema, { name: "x", tags: [], count: -1 });
mustReject("CreateItemInput: count above max", S.ZodCreateItemInputSchema, { name: "x", tags: [], count: 1001 });
mustReject("CreateItemInput: array too long", S.ZodCreateItemInputSchema, { name: "x", tags: Array(11).fill("a"), count: 0 });
mustReject("CreateItemInput: element too long", S.ZodCreateItemInputSchema, { name: "x", tags: ["a".repeat(51)], count: 0 });

mustReject("DerivedSchema: missing base id", S.ZodDerivedSchema, { name: "Alice" });
mustReject("DerivedSchema: base id empty", S.ZodDerivedSchema, { id: "", name: "Alice" });
mustReject("DerivedSchema: name too short", S.ZodDerivedSchema, { id: "1", name: "" });

mustReject("CrossField: max < min", S.ZodCrossFieldInputSchema, { min_val: 10, max_val: 5, start_val: 1, end_val: 10 });
mustReject("CrossField: end < start", S.ZodCrossFieldInputSchema, { min_val: 1, max_val: 5, start_val: 10, end_val: 1 });

mustReject("OmitInput: name too short", S.ZodOmitInputSchema, { name: "", active: false });

// ---- Summary ----
console.log("Fuzzed " + fuzzed + " schemas dynamically");
if (failed > 0) {
  console.error(failed + " test(s) FAILED out of " + (passed + failed));
  process.exit(1);
}
console.log("All " + passed + " tests passed");
`
		runValidation(t, zod, script)
	})

	t.Run("mini", func(t *testing.T) {
		r := trpcgo.NewRouter(trpcgo.WithZodMini(true))
		trpcgo.Mutation(r, "auth.login", func(_ context.Context, input ZodLoginInput) (string, error) {
			return "", nil
		})
		trpcgo.Mutation(r, "items.create", func(_ context.Context, input ZodCreateItemInput) (string, error) {
			return "", nil
		})
		trpcgo.Query(r, "search", func(_ context.Context, input ZodOptionalInput) (string, error) {
			return "", nil
		})
		trpcgo.Mutation(r, "derived.create", func(_ context.Context, input ZodDerived) (string, error) {
			return "", nil
		})
		trpcgo.Mutation(r, "cross.create", func(_ context.Context, input ZodCrossFieldInput) (string, error) {
			return "", nil
		})

		zod := generateZod(t, r)
		t.Log(zod)

		// zod/mini is not compatible with @traversable/zod-test fuzz
		// (fuzz imports from "zod" internally), so manual tests only.
		script := `import * as z from "zod/mini";
import * as schemas from "./schemas.js";

let passed = 0;
let failed = 0;
function test(name: string, fn: () => void) {
  try { fn(); passed++; console.log("PASS:", name); }
  catch (e: any) { failed++; console.error("FAIL:", name, e?.message ?? e); }
}
function mustReject(name: string, schema: z.ZodMiniType, data: unknown) {
  test(name, () => {
    const r = schema.safeParse(data);
    if (r.success) throw new Error("expected rejection, got success");
  });
}

const S = schemas as any;

// ---- Manual valid data ----

test("LoginInput valid", () => {
  S.ZodLoginInputSchema.parse({ email: "user@example.com", password: "securepass123" });
});

test("CreateItemInput valid", () => {
  S.ZodCreateItemInputSchema.parse({ name: "Widget", tags: ["electronics"], count: 42 });
});

test("CreateItemInput boundary", () => {
  S.ZodCreateItemInputSchema.parse({ name: "x", tags: [], count: 0 });
  S.ZodCreateItemInputSchema.parse({ name: "x".repeat(100), tags: Array(10).fill("a".repeat(50)), count: 1000 });
});

test("OptionalInput minimal", () => {
  S.ZodOptionalInputSchema.parse({ offset: 0 });
});

test("OptionalInput with optionals", () => {
  S.ZodOptionalInputSchema.parse({ query: "q", limit: 5, offset: 0 });
});

test("DerivedSchema inherits base", () => {
  S.ZodDerivedSchema.parse({ id: "1", name: "Alice" });
});

test("CrossFieldInput valid", () => {
  S.ZodCrossFieldInputSchema.parse({ min_val: 1, max_val: 5, start_val: 1, end_val: 10 });
});

// ---- Constraint rejection ----

mustReject("LoginInput: invalid email", S.ZodLoginInputSchema, { email: "bad", password: "securepass123" });
mustReject("LoginInput: password too short", S.ZodLoginInputSchema, { email: "a@b.com", password: "short" });
mustReject("CreateItemInput: count below min", S.ZodCreateItemInputSchema, { name: "x", tags: [], count: -1 });
mustReject("CreateItemInput: count above max", S.ZodCreateItemInputSchema, { name: "x", tags: [], count: 1001 });
mustReject("CreateItemInput: name too long", S.ZodCreateItemInputSchema, { name: "x".repeat(101), tags: [], count: 0 });
mustReject("CreateItemInput: element too long", S.ZodCreateItemInputSchema, { name: "x", tags: ["a".repeat(51)], count: 0 });
mustReject("DerivedSchema: missing base id", S.ZodDerivedSchema, { name: "Alice" });
mustReject("DerivedSchema: base id empty", S.ZodDerivedSchema, { id: "", name: "Alice" });
mustReject("CrossField: max < min", S.ZodCrossFieldInputSchema, { min_val: 10, max_val: 5, start_val: 1, end_val: 10 });

// ---- Summary ----
if (failed > 0) {
  console.error(failed + " test(s) FAILED out of " + (passed + failed));
  process.exit(1);
}
console.log("All " + passed + " tests passed");
`
		runValidation(t, zod, script)
	})
}

func TestGenerateZodDescribeAndMeta(t *testing.T) {
	r := trpcgo.NewRouter()
	trpcgo.Mutation(r, "configure", func(_ context.Context, input WithTSDoc) (string, error) {
		return "", nil
	})
	zod := generateZod(t, r)
	t.Log(zod)

	// Fields with ts_doc should have .describe().
	if !strings.Contains(zod, `.describe("The hostname to connect to")`) {
		t.Errorf("host field should have .describe().\nOutput:\n%s", zod)
	}
	if !strings.Contains(zod, `.describe("Port number (1-65535)")`) {
		t.Errorf("port field should have .describe().\nOutput:\n%s", zod)
	}

	// Field without ts_doc should NOT have .describe().
	// "name" has no ts_doc, so its line should be just "z.string()," with no .describe.
	if strings.Contains(zod, "name: z.string().describe(") {
		t.Error("name field should not have .describe()")
	}

	// Schema should have .meta({ id: "..." }).
	if !strings.Contains(zod, `.meta({ id: "WithTSDoc" })`) {
		t.Errorf("schema should have .meta() with type name.\nOutput:\n%s", zod)
	}
}

func TestGenerateZodOmit(t *testing.T) {
	t.Run("field excluded from zod but kept in TS", func(t *testing.T) {
		r := trpcgo.NewRouter()
		trpcgo.Mutation(r, "update", func(_ context.Context, input ZodOmitInput) (string, error) {
			return "", nil
		})

		// TS interface should have all fields including id.
		ts := generateTS(t, r)
		if !strings.Contains(ts, "id: string") {
			t.Errorf("TS interface should include omitted field.\nOutput:\n%s", ts)
		}

		// Zod schema should NOT have id.
		zod := generateZod(t, r)
		t.Log(zod)
		if strings.Contains(zod, "id: z.") {
			t.Errorf("omitted field 'id' should not appear in Zod schema.\nOutput:\n%s", zod)
		}
		if !strings.Contains(zod, "name: z.string().min(1),") {
			t.Errorf("non-omitted field 'name' should appear.\nOutput:\n%s", zod)
		}
		if !strings.Contains(zod, "active: z.boolean(),") {
			t.Errorf("non-omitted field 'active' should appear.\nOutput:\n%s", zod)
		}
	})

	t.Run("refinement referencing omitted field is skipped", func(t *testing.T) {
		r := trpcgo.NewRouter()
		trpcgo.Mutation(r, "update", func(_ context.Context, input ZodOmitWithRefine) (string, error) {
			return "", nil
		})
		zod := generateZod(t, r)
		t.Log(zod)

		// id should not appear.
		if strings.Contains(zod, "id: z.") {
			t.Errorf("omitted field 'id' should not appear.\nOutput:\n%s", zod)
		}

		// Refinement for max_val >= min_val should still exist.
		if !strings.Contains(zod, "data.max_val >= data.min_val") {
			t.Errorf("expected refinement for non-omitted fields.\nOutput:\n%s", zod)
		}

		// Only 1 refine (no refinement referencing id).
		if strings.Count(zod, ".refine(") != 1 {
			t.Errorf("expected exactly 1 .refine(), got %d.\nOutput:\n%s",
				strings.Count(zod, ".refine("), zod)
		}
	})
}
