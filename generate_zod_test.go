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

	// ZodLoginInput should have Go-compatible email and password constraints.
	if !strings.Contains(zod, "ZodLoginInputSchema") {
		t.Error("expected ZodLoginInputSchema")
	}
	if !strings.Contains(zod, "email: z.string().check(z.refine(") {
		t.Error("expected Go-compatible validation for email field")
	}
	if !strings.Contains(zod, "Array.from(value).length >= 8)") {
		t.Error("expected a rune-count minimum of 8 for password field")
	}
	if !strings.Contains(zod, "Array.from(value).length <= 128)") {
		t.Error("expected a rune-count maximum of 128 for password field")
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
	t.Run("basic extends retains inherited fields", func(t *testing.T) {
		r := trpcgo.NewRouter()
		trpcgo.Mutation(r, "create", func(_ context.Context, input ZodDerived) (string, error) {
			return "", nil
		})
		zod := generateZod(t, r)
		t.Log(zod)

		// Base schema must be emitted.
		if !strings.Contains(zod, "export const ZodBaseSchema = z.strictObject({") {
			t.Error("missing ZodBaseSchema")
		}
		if !strings.Contains(zod, "id: z.string().min(1),") {
			t.Error("ZodBaseSchema missing id field")
		}

		if !strings.Contains(zod, "export const ZodDerivedSchema = z.strictObject({\n  id: z.string().min(1),") {
			t.Errorf("ZodDerivedSchema should include inherited fields, got:\n%s", zod)
		}
		if !strings.Contains(zod, "name: z.string().min(1),") {
			t.Error("ZodDerivedSchema missing name field")
		}

	})

	t.Run("multiple extends retains all base fields", func(t *testing.T) {
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

		if !strings.Contains(zod, "export const ZodMultiBaseSchema = z.strictObject({\n  id: z.string().min(1),\n  createdBy: z.string(),\n  updatedBy: z.string(),") {
			t.Errorf("ZodMultiBaseSchema should include both bases, got:\n%s", zod)
		}
	})

	t.Run("pointer extends makes inherited fields optional", func(t *testing.T) {
		r := trpcgo.NewRouter()
		trpcgo.Mutation(r, "create", func(_ context.Context, input ZodPtrExtends) (string, error) {
			return "", nil
		})
		zod := generateZod(t, r)
		t.Log(zod)

		// A nil embedded pointer omits its fields, so inherited fields are optional.
		if !strings.Contains(zod, "export const ZodPtrExtendsSchema = z.strictObject({\n  id: z.string().min(1).optional(),") {
			t.Errorf("pointer extends should make inherited fields optional, got:\n%s", zod)
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

	t.Run("cyclic type with extends defers recursive fields", func(t *testing.T) {
		r := trpcgo.NewRouter()
		trpcgo.Mutation(r, "create", func(_ context.Context, input ZodCyclicNode) (string, error) {
			return "", nil
		})
		zod := generateZod(t, r)
		t.Log(zod)

		// The exact expected output for the cyclic+extends case. Children has no
		// dive, so validator never enters the elements and they use the private
		// rule-free variant of the same recursive schema.
		want := `export const ZodCyclicNodeSchema = z.strictObject({
  id: z.string().min(1),
  children: z.lazy((): z.ZodType<$ZodCyclicNode["children"], $ZodCyclicNode["children"]> => z.array($goUnvalidatedZodCyclicNodeSchema)),
}).meta({ id: "ZodCyclicNode" });`
		if !strings.Contains(zod, want) {
			t.Errorf("cyclic+extends output mismatch.\nwant:\n%s\n\ngot:\n%s", want, zod)
		}
		variant := `export const $goUnvalidatedZodCyclicNodeSchema = z.strictObject({
  id: z.string(),
  children: z.lazy((): z.ZodType<$$goUnvalidatedZodCyclicNode["children"], $$goUnvalidatedZodCyclicNode["children"]> => z.array($goUnvalidatedZodCyclicNodeSchema)),
}).meta({ id: "$goUnvalidatedZodCyclicNode" });`
		if !strings.Contains(zod, variant) {
			t.Errorf("rule-free element variant mismatch.\nwant:\n%s\n\ngot:\n%s", variant, zod)
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
	for _, mini := range []bool{false, true} {
		t.Run(zodStyleName(mini), func(t *testing.T) {
			r := trpcgo.NewRouter(trpcgo.WithZodMini(mini))
			t.Cleanup(func() { _ = r.Close() })
			trpcgo.MustMutation(r, "big", func(_ context.Context, input ZodInt64Input) (string, error) { return "", nil })
			checkGeneratedZod(t, r, `import { ZodInt64InputSchema as schema } from './schemas';
const valid = { bigSigned: 1, bigUnsigned: 1, normalInt: 1 };
for (const input of [valid, { ...valid, bigSigned: -(2 ** 62), bigUnsigned: 2 ** 63 }]) {
  const parsed = schema.parse(input);
  const signed: number = parsed.bigSigned;
  const unsigned: number = parsed.bigUnsigned;
  if (signed !== input.bigSigned || unsigned !== input.bigUnsigned) throw new Error('integer values changed');
}
for (const input of [
  { ...valid, bigSigned: 1.5 }, { ...valid, bigUnsigned: 1.5 },
  { ...valid, bigSigned: 2 ** 63 }, { ...valid, bigSigned: -(2 ** 64) },
  { ...valid, bigUnsigned: -1 }, { ...valid, bigUnsigned: 2 ** 64 },
  { ...valid, bigSigned: Infinity }, { ...valid, normalInt: 2 ** 31 },
  { ...valid, bigSigned: '1' }, { ...valid, bigUnsigned: 1n },
]) {
  if (schema.safeParse(input).success) throw new Error('invalid Go integer accepted');
}
`)
		})
	}
}

func TestGenerateZodNewTags(t *testing.T) {
	for _, mini := range []bool{false, true} {
		t.Run(zodStyleName(mini), func(t *testing.T) {
			r := trpcgo.NewRouter(trpcgo.WithZodMini(mini))
			t.Cleanup(func() { _ = r.Close() })
			trpcgo.MustMutation(r, "create", func(_ context.Context, input ZodNewTagsInput) (string, error) { return "", nil })
			checkGeneratedZod(t, r, `import { ZodNewTagsInputSchema as schema } from './schemas';
const valid = {
  host: 'example.com', token: 'YQ==', hash: 'a'.repeat(64),
  id: '01ARZ3NDEKTSV4RRFFQ69G5FAV', mac: '00:00:5e:00:53:01',
  subnet: '192.168.1.0/24', code: 'ABC', website: 'https://example.com',
  file: 'config.json', path: '/api/items',
};
schema.parse(valid);
const invalid: Record<string, string[]> = {
  host: ['-example.com'], token: ['a', 'YQ='], hash: ['x'.repeat(64), 'a'.repeat(63), 'a'.repeat(65)],
  id: ['invalid'], mac: ['invalid'], subnet: ['192.168.1.1/24'], code: ['abc'],
  website: ['http://example.com', 'https://a'], file: ['config.txt'], path: ['/v1/items'],
};
for (const [field, values] of Object.entries(invalid)) {
  for (const value of values) {
    if (schema.safeParse({ ...valid, [field]: value }).success) throw new Error(field + ' accepted ' + value);
  }
}
`)
		})
	}
}

func TestZodRuntimeValidation(t *testing.T) {
	_, tsx := zodRuntime(t)
	runValidation := func(t *testing.T, zodCode, script string) {
		t.Helper()
		dir := t.TempDir()
		symlinkNodeModules(t, dir)
		writeTestSources(t, dir, map[string]string{"schemas.ts": zodCode, "validate.ts": script})
		assertTypeScriptCompiles(t, dir, "schemas.ts", "validate.ts")
		cmd := exec.CommandContext(t.Context(), tsx, "validate.ts")
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
  catch (e) { failed++; console.error("FAIL:", name, e instanceof Error ? e.message : e); }
}
function mustReject(name: string, schema: z.ZodType, data: unknown) {
  test(name, () => {
    const r = schema.safeParse(data);
    if (r.success) throw new Error("expected rejection, got success");
  });
}

// ---- Dynamic fuzz: auto-discover and fuzz every exported schema ----
//
// This checks schema self-consistency, not agreement with Go validation:
// omitted constraints also disappear from the generated test data. The shared
// cases in TestZodValidationContract check that independent contract.
//
const fuzzOpts = {};
const fuzzOverrides: zxTest.fuzz.Overrides = {
  // This fuzzer treats Zod's strict-object catchall z.never() as undefined and
  // generates unknown properties. Build valid objects from their known shape;
  // independent Go/Zod contracts cover unknown-key rejection separately.
  object: (node) => fc.record(node._zod.def.shape),
};

// Schemas with .refine() can't be fuzzed (fast-check can't satisfy arbitrary
// JS predicates). In Zod 4, refinements live in _zod.def.checks as entries
// with _zod.def.check === "custom".
function hasRefine(value: unknown, seen = new Set<object>()): boolean {
  if (value === null || typeof value !== 'object' || seen.has(value)) return false;
  seen.add(value);
  if ('_zod' in value) {
    const internals = value._zod;
    if (internals !== null && typeof internals === 'object' && 'def' in internals) {
      const def = internals.def;
      if (def !== null && typeof def === 'object') {
        if ('check' in def && def.check === 'custom') return true;
        // The fuzzer cannot construct recursive arbitrary values either.
        if ('type' in def && def.type === 'lazy') return true;
        return Object.values(def).some(child => hasRefine(child, seen));
      }
    }
    return false;
  }
  return Object.values(value).some(child => hasRefine(child, seen));
}

// Generated objects are strict, but the fuzzer emits catchall keys for strict
// objects. Values are therefore generated from a structural twin whose objects
// strip unknown keys and are validated against the real schema. The twin
// shares every field schema and check, so it generates the same value space.
function loosen<T extends z.ZodType>(schema: T): T {
  const def: Record<string, unknown> = { ...schema._zod.def };
  for (const [key, child] of Object.entries(def)) {
    if (child instanceof z.ZodType) def[key] = loosen(child);
    else if (Array.isArray(child) && child.length > 0 && child.every(item => item instanceof z.ZodType)) def[key] = child.map(item => loosen(item as z.ZodType));
    else if (key === 'shape' && child !== null && typeof child === 'object') {
      def[key] = Object.fromEntries(Object.entries(child as Record<string, z.ZodType>).map(([field, fieldSchema]) => [field, loosen(fieldSchema)]));
    }
  }
  if (def['type'] === 'object') def['catchall'] = undefined;
  return z.clone(schema, def as unknown as T['_zod']['def']);
}

for (const [name, value] of Object.entries(schemas)) {
  // Skip non-schema exports.
  if (!(value instanceof z.ZodType)) continue;

  // Skip schemas with .refine() — cross-field constraints are arbitrary JS.
  if (hasRefine(value)) {
    console.log("SKIP-REFINE:", name);
    continue;
  }

  test("fuzz " + name, () => {
    const arb = zxTest.fuzz<unknown>(loosen(value), fuzzOpts, fuzzOverrides);
    fc.assert(fc.property(arb, (d) => { value.parse(d); }), { numRuns: 100 });
    fuzzed++;
  });
}

// ---- Manual valid data ----

const S = schemas;

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
  throw new Error("Zod runtime validation failed");
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
  catch (e) { failed++; console.error("FAIL:", name, e instanceof Error ? e.message : e); }
}
function mustReject(name: string, schema: z.ZodMiniType, data: unknown) {
  test(name, () => {
    const r = schema.safeParse(data);
    if (r.success) throw new Error("expected rejection, got success");
  });
}

const S = schemas;

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
  throw new Error("Zod Mini runtime validation failed");
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
	t.Run("field unvalidated in Zod but kept in TS", func(t *testing.T) {
		r := trpcgo.NewRouter()
		trpcgo.Mutation(r, "update", func(_ context.Context, input ZodOmitInput) (string, error) {
			return "", nil
		})

		// TS interface should have all fields including id.
		ts := generateTS(t, r)
		if !strings.Contains(ts, "id: string") {
			t.Errorf("TS interface should include omitted field.\nOutput:\n%s", ts)
		}

		// Known omitted fields are accepted without client validation.
		zod := generateZod(t, r)
		t.Log(zod)
		if !strings.Contains(zod, "id: z.custom<string>().optional(),") {
			t.Errorf("omitted field 'id' should be optional and unvalidated.\nOutput:\n%s", zod)
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

		// id remains an optional field without validation.
		if !strings.Contains(zod, "id: z.custom<number>().optional(),") {
			t.Errorf("omitted field 'id' should be optional and unvalidated.\nOutput:\n%s", zod)
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
