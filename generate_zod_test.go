package trpcgo_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/befabri/trpcgo"
	"github.com/befabri/trpcgo/internal/codegen"
	"github.com/befabri/trpcgo/internal/typemap"
	"github.com/befabri/trpcgo/testdata/fieldcomposition"
	"github.com/befabri/trpcgo/testdata/schemaissues"
	fixture "github.com/befabri/trpcgo/testdata/validationconfig"
	"github.com/befabri/trpcgo/testdata/validationcontract"
	"github.com/befabri/trpcgo/zodconfig"
)

func TestZodRecursiveBaseExtension(t *testing.T) {
	for _, mini := range []bool{false, true} {
		t.Run(zodStyleName(mini), func(t *testing.T) {
			r := trpcgo.NewRouter(trpcgo.WithZodMini(mini))
			trpcgo.MustQuery(r, "check", func(context.Context, CompositionRecursiveDerived) (string, error) { return "ok", nil })
			trpcgo.MustQuery(r, "pointer", func(context.Context, CompositionRecursivePointer) (string, error) { return "ok", nil })
			trpcgo.MustQuery(r, "multi", func(context.Context, CompositionRecursiveMulti) (string, error) { return "ok", nil })
			trpcgo.MustQuery(r, "forward", func(context.Context, CompositionForwardDerived) (string, error) { return "ok", nil })
			checkGeneratedZod(t, r, `
import { CompositionRecursiveDerivedSchema as schema, CompositionRecursivePointerSchema as pointer, CompositionRecursiveMultiSchema as multi, CompositionForwardDerivedSchema as forward } from './schemas.ts';
schema.parse({ value: 'x', extra: 'y' });
const parsed = schema.parse({ value: 'x', extra: 'y', next: { value: 'z' } });
const value: string = parsed.next!.value;
// @ts-expect-error recursive leaves retain their string type
const wrong: number = parsed.next!.value;
void [value, wrong];
pointer.parse({ extra: 'x' });
multi.parse({ value: 'x', extra: 'y', audit: 1 });
forward.parse({ value: 'x', children: [{ value: 'y', children: [] }] });
for (const s of [schema, pointer, multi]) {
  if (s.safeParse({ value: 'x', extra: 'y', audit: 1, next: { value: 123 } }).success) throw new Error('invalid recursive leaf accepted');
}
if (forward.safeParse({ value: 'x', children: [{ value: 1, children: [] }] }).success) throw new Error('invalid forward reference accepted');
`)
		})
	}
}

func TestZodFlattenedBaseRefinements(t *testing.T) {
	for _, mini := range []bool{false, true} {
		t.Run(zodStyleName(mini), func(t *testing.T) {
			r := trpcgo.NewRouter(trpcgo.WithZodMini(mini))
			trpcgo.MustQuery(r, "check", func(context.Context, CompositionRangeDerived) (string, error) { return "ok", nil })
			trpcgo.MustQuery(r, "shadow", func(context.Context, CompositionRangeShadowedTarget) (string, error) { return "ok", nil })
			trpcgo.MustQuery(r, "nested", func(context.Context, CompositionNestedRange) (string, error) { return "ok", nil })
			checkGeneratedZod(t, r, `
import { CompositionRangeDerivedSchema as schema, CompositionRangeShadowedTargetSchema as shadow, CompositionNestedRangeSchema as nested } from './schemas.ts';
schema.parse({ min: 1, max: 10, label: 2 });
if (schema.safeParse({ min: 10, max: 1, label: 2 }).success) throw new Error('inherited gtefield constraint was dropped');
// The shadowing min is not the original field read by the base's validator.
shadow.parse({ min: 'separate outer field', max: 1, label: 2 });
nested.parse({ 'lower-bound': 1, 'upper-bound': 10, label: 2 });
if (nested.safeParse({ 'lower-bound': 10, 'upper-bound': 1, label: 2 }).success) throw new Error('promoted reference lost its original scope');
`)
		})
	}
}

func TestZodEnumConstraintComposition(t *testing.T) {
	for _, mini := range []bool{false, true} {
		t.Run(zodStyleName(mini), func(t *testing.T) {
			r := trpcgo.NewRouter(trpcgo.WithZodMini(mini))
			trpcgo.MustQuery(r, "check", func(context.Context, CompositionCaseEnum) (string, error) { return "ok", nil })
			checkGeneratedZod(t, r, `
import { CompositionCaseEnumSchema as schema } from './schemas.ts';
const valid = { value: 'good', upper: 'GOOD', bounded: 'good', values: ['good'], both: '123' };
const parsed = schema.parse(valid);
const value: 'good' | 'BAD' = parsed.value;
// @ts-expect-error enum checks preserve the literal output type
const wrong: 'unlisted' = parsed.value;
void [value, wrong];
schema.parse({ ...valid, optional: '' });
schema.parse({ ...valid, optional: 'good' });
for (const invalid of [{ value: 'BAD' }, { value: 'unlisted' }, { upper: 'bad' }, { bounded: 'go' }, { bounded: 'toolong' }, { optional: 'BAD' }, { values: ['BAD'] }, { both: 'good' }, { both: 'BAD' }]) {
  if (schema.safeParse({ ...valid, ...invalid }).success) throw new Error('enum constraint was dropped: ' + JSON.stringify(invalid));
}
`)
		})
	}
}

type InheritanceOrderBase struct {
	Ab int `json:"ab"`
}

type InheritanceOrderOuter struct {
	AB                   int `json:"aB"`
	InheritanceOrderBase `tstype:",extends"`
	Arr                  [2]int `json:"arr"`
}

// encoding/json resolves a case-insensitive key against fields in declaration
// order, including promoted fields at their embedded position. The flattened
// schema must present the same order to its key matcher.
func TestZodInheritanceKeepsGoKeyMatchOrder(t *testing.T) {
	r := trpcgo.NewRouter()
	trpcgo.MustQuery(r, "outer", func(context.Context, InheritanceOrderOuter) (string, error) { return "", nil })
	checkGeneratedZod(t, r, `
import { InheritanceOrderOuterSchema as schema, decodeGoJSON } from './schemas.ts';
const result = decodeGoJSON(schema, '{"Ab":7,"ab":1,"arr":[1,2]}');
if (!result.success) throw new Error('folded key must select the first Go field: ' + JSON.stringify(result.error.issues));
const data = result.data as { aB: number; ab: number };
if (data.aB !== 7 || data.ab !== 1) throw new Error('folded key landed on the wrong field: ' + JSON.stringify(data));
`)
}

type GoDecodingProfile struct {
	Name  string `json:"name" validate:"min=1"`
	Email string `json:"email" validate:"email"`
}

type GoDecodingInput struct {
	Profile GoDecodingProfile `json:"profile"`
	Scores  map[int]int       `json:"scores"`
	Slots   [2]string         `json:"slots"`
	Extra   any               `json:"extra,omitempty"`
}

// goDecodingNested nests arrays inside the extra field, depth levels deep in
// total, as the TypeScript contract below does.
func goDecodingNested(depth int) string {
	return `{"profile":{"name":"x","email":"a@example.com"},"scores":{},"slots":[],"extra":` + strings.Repeat("[", depth-1) + strings.Repeat("]", depth-1) + "}"
}

// Integer maps and fixed arrays need Go decoding, but only for their own
// fields. Exported object schemas stay plain objects with the full object API,
// and raw JSON is decoded by decodeGoJSON before any schema sees it.
func TestZodGoDecodingKeepsPlainObjectSchemas(t *testing.T) {
	// The contract's nesting limit is Go's: 10000 levels decode, 10001 do not.
	for depth, valid := range map[int]bool{10000: true, 10001: false} {
		var input GoDecodingInput
		if err := json.Unmarshal([]byte(goDecodingNested(depth)), &input); (err == nil) != valid {
			t.Fatalf("encoding/json nesting depth %d: err = %v, want valid=%v", depth, err, valid)
		}
	}
	forEachStyle(t, func(t *testing.T, mini bool) {
		r := trpcgo.NewRouter(trpcgo.WithZodMini(mini))
		trpcgo.MustQuery(r, "decoding", func(context.Context, GoDecodingInput) (string, error) { return "", nil })
		imports, derive := `import { z } from 'zod';`, `
const extended = profile.extend({ nickname: z.string() });
const picked = profile.pick({ name: true });`
		if mini {
			imports, derive = `import * as z from 'zod/mini';`, `
const extended = z.extend(profile, { nickname: z.string() });
const picked = z.pick(profile, { name: true });`
		}
		checkGeneratedZod(t, r, imports+`
import { GoDecodingProfileSchema as profile, GoDecodingInputSchema as input, decodeGoJSON } from './schemas.ts';
function assert(condition: unknown, message: string): asserts condition {
  if (!condition) throw new Error(message);
}
function throwsTypeError(run: () => unknown): boolean {
  try { run(); return false; } catch (error) { return error instanceof TypeError; }
}

for (const schema of [profile, input]) assert(schema._zod.def.type === 'object', 'exported schema is not a plain object');
const name: typeof profile.shape.name = profile.shape.name;
assert(Object.keys(input.shape).join() === 'profile,scores,slots,extra', 'shape lost its fields');
const json = JSON.stringify(z.toJSONSchema(input));
assert(json.includes('"required":["profile","scores","slots"]') && json.includes('"minLength":1'), 'JSON Schema lost fields or constraints: ' + json);
`+derive+`
assert(extended.parse({ name: 'x', email: 'a@example.com', nickname: 'n' }).nickname === 'n', 'extend lost the added field');
assert(!extended.safeParse({ name: '', email: 'a@example.com', nickname: 'n' }).success, 'extend lost field rules');
assert(JSON.stringify(picked.parse({ name: 'x' })) === '{"name":"x"}', 'pick changed the schema');
void name;

// Ordinary objects keep the per-field Go decoding without decodeGoJSON.
const ordinary = input.parse({ profile: { name: 'x', email: 'a@example.com' }, scores: { '01': 1 }, slots: ['a'] });
assert(JSON.stringify(ordinary) === '{"profile":{"name":"x","email":"a@example.com"},"scores":{"1":1},"slots":["a",""]}', 'ordinary object lost field decoding: ' + JSON.stringify(ordinary));

const folded = decodeGoJSON(input, '{"PROFILE":{"Name":"x","EMAIL":"a@example.com"},"scores":{"01":1,"1":2},"slots":["a"]}');
assert(folded.success, 'raw JSON with Go field matching was rejected');
const data: { profile: { name: string } } = folded.data;
assert(JSON.stringify(data) === '{"profile":{"name":"x","email":"a@example.com"},"scores":{"1":2},"slots":["a",""]}', 'raw JSON did not decode as Go: ' + JSON.stringify(data));
const invalid = decodeGoJSON(input, '{"profile":{"name":"","email":"a@example.com"},"scores":{},"slots":[]}');
assert(!invalid.success && invalid.error.issues.length === 1, 'invalid final value was accepted or reported twice');
assert(invalid.error.issues[0]!.code === 'too_small' && invalid.error.issues[0]!.path.join('.') === 'profile.name', 'final value lost its schema issue');
// A value Go fails to decode reports Zod's issue for it at its raw JSON path,
// even when a later occurrence overwrites it with a valid value.
function failure(raw: string) {
  const result = decodeGoJSON(input, raw);
  assert(!result.success, 'raw JSON was accepted: ' + raw);
  assert(result.error.issues.length === 1, 'raw JSON reported several issues: ' + JSON.stringify(result.error.issues));
  const issue = result.error.issues[0] as { code: string; path: PropertyKey[]; format?: string; message: string };
  return { code: issue.code, path: issue.path.join('.'), format: issue.format, message: issue.message };
}
const overwritten = failure('{"profile":{"name":1,"NAME":"x","email":"a@example.com"},"scores":{},"slots":[]}');
assert(overwritten.code === 'invalid_type' && overwritten.path === 'profile.name', 'overwritten field lost its issue: ' + JSON.stringify(overwritten));
const entry = failure('{"profile":{"name":"x","email":"a@example.com"},"scores":{"1":"bad","01":2},"slots":[]}');
assert(entry.code === 'invalid_type' && entry.path === 'scores.1', 'overwritten map entry lost its issue: ' + JSON.stringify(entry));
const key = failure('{"profile":{"name":"x","email":"a@example.com"},"scores":{"x":1},"slots":[]}');
assert(key.code === 'invalid_key' && key.path === 'scores.x', 'invalid map key lost its issue: ' + JSON.stringify(key));
const malformed = failure('{"profile":');
assert(malformed.code === 'invalid_format' && malformed.format === 'json_string' && malformed.path === '', 'malformed JSON did not fail as a JSON string');
// encoding/json decodes JSON nested 10000 levels deep, here into an any
// field, and rejects deeper JSON as malformed.
const nested = (depth: number) => '{"profile":{"name":"x","email":"a@example.com"},"scores":{},"slots":[],"extra":' + '['.repeat(depth - 1) + ']'.repeat(depth - 1) + '}';
assert(decodeGoJSON(input, nested(10000)).success, 'JSON nested as deep as Go allows was rejected');
const deep = failure(nested(10001));
assert(deep.code === 'invalid_format' && deep.format === 'json_string', 'JSON nested deeper than Go allows did not fail as a JSON string');
// @ts-expect-error decodeGoJSON only accepts schemas exported by this module
assert(throwsTypeError(() => decodeGoJSON(z.string(), '""')), 'a foreign schema was accepted');
`)
	})
}

type ZodCaseInput struct {
	Name  string `json:"name" validate:"lowercase,min=1,max=12"`
	Code  string `json:"code" validate:"uppercase,len=3"`
	Email string `json:"email" validate:"email,lowercase"`
}

type NestedTrackedInput struct {
	Text  trpcgo.TrackedEvent[string] `json:"text"`
	Count trpcgo.TrackedEvent[int]    `json:"count"`
}

func TestZodNestedTrackedInput(t *testing.T) {
	for _, mini := range []bool{false, true} {
		t.Run(zodStyleName(mini), func(t *testing.T) {
			r := trpcgo.NewRouter(trpcgo.WithZodMini(mini))
			trpcgo.MustQuery(r, "check", func(context.Context, NestedTrackedInput) (string, error) { return "", nil })
			checkGeneratedZod(t, r, `
import { NestedTrackedInputSchema as schema } from './schemas.ts';
const valid = { text: { ID: 'one', Data: 'hello', Retry: 0 }, count: { ID: 'two', Data: 2, Retry: 0 } };
schema.parse(valid);
if (schema.safeParse({ ...valid, count: { ...valid.count, Data: 'wrong' } }).success) throw new Error('tracked payload lost its concrete type');
`)
		})
	}
}

type ZodOmitEmailInput struct {
	Email string `json:"email,omitempty" validate:"omitempty,email"`
	Code  string `json:"code,omitempty" validate:"omitempty,len=3"`
	Count int    `json:"count" validate:"omitempty,gte=2"`
}

type ZodRecursiveInput struct {
	Label    string              `json:"label"`
	Children []ZodRecursiveInput `json:"children"`
	Secret   string              `json:"secret" zod_omit:"true"`
}

type ZodMutualA struct {
	Name string       `json:"name"`
	Bs   []ZodMutualB `json:"bs"`
}

type ZodMutualB struct {
	Value int          `json:"value"`
	As    []ZodMutualA `json:"as"`
}

type ZodNestedContainersInput struct {
	Matrix  [][]string                             `json:"matrix" validate:"min=1,dive,min=1,dive,min=2"`
	Groups  map[string][]string                    `json:"groups"`
	Buckets []map[string][]ZodContainerLeaf        `json:"buckets" validate:"dive,dive,dive"`
	Index   map[string]map[string]ZodContainerLeaf `json:"index" validate:"dive,dive"`
}

type ZodContainerLeaf struct {
	Value string `json:"value" validate:"min=1"`
}

func TestZodCaseChecksRuntime(t *testing.T) {
	for _, mini := range []bool{false, true} {
		t.Run(zodStyleName(mini), func(t *testing.T) {
			r := trpcgo.NewRouter(trpcgo.WithZodMini(mini))
			trpcgo.MustQuery(r, "check", func(_ context.Context, input ZodCaseInput) (string, error) {
				return "ok", nil
			})
			checkGeneratedZod(t, r, `
import { ZodCaseInputSchema as schema } from './schemas.ts';
const valid = { name: 'alice', code: 'ABC', email: 'alice@example.com' };
if (!schema.safeParse(valid).success) {
  throw new Error('valid lowercase and uppercase strings were rejected');
}
for (const input of [{ name: 'ALICE' }, { code: 'abc' }, { name: '' }, { code: 'ABCD' }, { email: 'ALICE@example.com' }, { email: 'invalid' }]) {
  if (schema.safeParse({ ...valid, ...input }).success) throw new Error('string constraint was ignored');
}
`)
		})
	}
}

func TestZodMiniOmitemptyEmailRuntime(t *testing.T) {
	r := trpcgo.NewRouter(trpcgo.WithZodMini(true))
	trpcgo.MustQuery(r, "check", func(_ context.Context, input ZodOmitEmailInput) (string, error) {
		return "ok", nil
	})
	checkGeneratedZod(t, r, `
import { ZodOmitEmailInputSchema as schema } from './schemas.ts';
for (const input of [{}, { email: '' }, { email: 'alice@example.com' }, { code: '' }, { code: 'ABC' }, { count: 2 }]) {
  if (!schema.safeParse({ count: 0, ...input }).success) throw new Error('valid zero or nonzero value was rejected');
}
for (const input of [{ email: 'invalid' }, { code: 'AB' }, { count: 1 }, { count: '0' }]) {
  if (schema.safeParse({ count: 0, ...input }).success) throw new Error('invalid value was accepted');
}
// The TypeScript type requires count; omitempty only admits its zero value.
if (schema.safeParse({}).success) throw new Error('missing required count was accepted');
`)
}

func TestZodRecursiveSchemaModule(t *testing.T) {
	for _, mini := range []bool{false, true} {
		t.Run(zodStyleName(mini), func(t *testing.T) {
			r := trpcgo.NewRouter(trpcgo.WithZodMini(mini))
			trpcgo.MustQuery(r, "check", func(_ context.Context, input ZodRecursiveInput) (string, error) {
				return "ok", nil
			})
			checkGeneratedZod(t, r, `
import { ZodRecursiveInputSchema as schema } from './schemas.ts';
const input = { label: 'root', children: [{ label: 'child', children: [] }] };
if (!schema.safeParse(input).success) throw new Error('valid recursive input was rejected');
if (schema.safeParse({ label: 'root', children: [{ label: 12, children: [] }] }).success) {
  throw new Error('invalid nested label was accepted');
}
const parsed = schema.parse(input);
const label: string = parsed.children[0].label;
// @ts-expect-error recursive output must retain its string type
const wrong: number = parsed.children[0].label;
const secret: string | undefined = parsed.secret;
// @ts-expect-error unvalidated fields keep their public type and may be absent
const secretString: string = parsed.secret;
if ('secret' in parsed) throw new Error('absent unvalidated field was materialized');
const opaque = { arbitrary: true };
if ((schema.parse({ ...input, secret: opaque } as unknown).secret as unknown) !== opaque) throw new Error('unvalidated field was not preserved');
void [label, wrong, secret];
`)
		})
	}
}

func TestZodMutualRecursionAndExtendsModule(t *testing.T) {
	for _, mini := range []bool{false, true} {
		t.Run(zodStyleName(mini), func(t *testing.T) {
			r := trpcgo.NewRouter(trpcgo.WithZodMini(mini))
			trpcgo.MustQuery(r, "mutual", func(context.Context, ZodMutualA) (string, error) { return "ok", nil })
			trpcgo.MustQuery(r, "extended", func(context.Context, ZodCyclicNode) (string, error) { return "ok", nil })
			checkGeneratedZod(t, r, `
import { ZodMutualASchema as mutual, ZodCyclicNodeSchema as extended } from './schemas.ts';
const input = { name: 'a', bs: [{ value: 1, as: [{ name: 'b', bs: [] }] }] };
const parsed = mutual.parse(input);
const name: string = parsed.bs[0].as[0].name;
// @ts-expect-error mutual recursion must retain leaf types
const wrong: number = parsed.bs[0].as[0].name;
void [name, wrong];
if (mutual.safeParse({ name: 'a', bs: [{ value: 'bad', as: [] }] }).success) throw new Error('invalid recursive leaf accepted');
extended.parse({ id: 'root', children: [{ id: 'child', children: [] }] });
if (extended.safeParse({ id: 'root', children: [{ id: 12, children: [] }] }).success) throw new Error('invalid inherited leaf accepted');
`)
		})
	}
}

func TestZodNestedContainersRuntime(t *testing.T) {
	for _, mini := range []bool{false, true} {
		t.Run(zodStyleName(mini), func(t *testing.T) {
			r := trpcgo.NewRouter(trpcgo.WithZodMini(mini))
			trpcgo.MustQuery(r, "check", func(_ context.Context, input ZodNestedContainersInput) (string, error) {
				return "ok", nil
			})
			checkGeneratedZod(t, r, `
import { ZodNestedContainersInputSchema as schema } from './schemas.ts';
const valid = { matrix: [['ab']], groups: { first: ['b'] }, buckets: [{ first: [{ value: 'x' }] }], index: { a: { b: { value: 'y' } } } };
if (!schema.safeParse(valid).success) {
  throw new Error('valid nested containers were rejected');
}
for (const input of [
  { matrix: [[12]] }, { matrix: [] }, { matrix: [[]] }, { matrix: [['a']] },
  { groups: { first: [12] } }, { buckets: [{ first: [{ value: 12 }] }] },
  { index: { a: { b: { value: '' } } } },
]) {
  if (schema.safeParse({ ...valid, ...input }).success) throw new Error('invalid nested value was accepted');
}
`)
		})
	}
}

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

	checkGeneratedZod(t, r, `
import { ZodLoginInputSchema as login, ZodCreateItemInputSchema as item } from './schemas';
type Issue = { code: string; path: PropertyKey[] };
const raises = (result: { success: boolean; error?: { issues: Issue[] } }, code: string, path: string) => {
  if (result.success || !result.error!.issues.some((issue) => issue.code === code && issue.path.join('.') === path)) throw new Error('want ' + code + ' at ' + path + ': ' + JSON.stringify(result));
};
login.parse({ email: 'ada@example.com', password: '😀'.repeat(8) });
raises(login.safeParse({ email: 'ada', password: 'password' }), 'invalid_format', 'email');
raises(login.safeParse({ email: 'ada@example.com', password: '😀'.repeat(7) }), 'too_small', 'password');
raises(login.safeParse({ email: 'ada@example.com', password: 'x'.repeat(129) }), 'too_big', 'password');
item.parse({ name: 'a', tags: ['x'], count: 0 });
raises(item.safeParse({ name: 'a', tags: [''], count: 0 }), 'too_small', 'tags.0');
raises(item.safeParse({ name: 'a', tags: [], count: -1 }), 'too_small', 'count');
raises(item.safeParse({ name: 'a', tags: [], count: 1001 }), 'too_big', 'count');
`)
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

// Unnamed scalar inputs have no schema, so a router with only those exports
// no module rather than a decoder over an empty schema union.
func TestGenerateZodScalarInputsProduceNothing(t *testing.T) {
	r := trpcgo.NewRouter()
	trpcgo.MustQuery(r, "echo", func(_ context.Context, input string) (string, error) { return input, nil })
	trpcgo.MustQuery(r, "count", func(_ context.Context, input int) (int, error) { return input, nil })

	out := filepath.Join(t.TempDir(), "zod.ts")
	if err := r.GenerateZod(out); err != nil {
		t.Fatalf("GenerateZod: %v", err)
	}
	if data, err := os.ReadFile(out); err == nil {
		t.Fatalf("expected no Zod file when no input has a schema, got:\n%s", data)
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
  // Number bounds come from Zod's public JSON Schema. The fuzzer reads them
  // from Zod's internal metadata, which Zod 4.6 stopped filling for numbers.
  number: (node) => {
    const json = z.toJSONSchema(node as unknown as z.ZodType) as { type?: string; minimum?: number; maximum?: number; exclusiveMinimum?: number; exclusiveMaximum?: number };
    if (json.type === 'integer') {
      const low = json.minimum ?? (json.exclusiveMinimum === undefined ? Number.MIN_SAFE_INTEGER : Math.floor(json.exclusiveMinimum) + 1);
      const high = json.maximum ?? (json.exclusiveMaximum === undefined ? Number.MAX_SAFE_INTEGER : Math.ceil(json.exclusiveMaximum) - 1);
      return fc.bigInt({ min: BigInt(Math.ceil(low)), max: BigInt(Math.floor(high)) }).map(Number);
    }
    return fc.double({
      min: json.minimum ?? json.exclusiveMinimum,
      max: json.maximum ?? json.exclusiveMaximum,
      minExcluded: json.minimum === undefined && json.exclusiveMinimum !== undefined,
      maxExcluded: json.maximum === undefined && json.exclusiveMaximum !== undefined,
      noNaN: true,
      noDefaultInfinity: true,
    });
  },
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

func TestZodConfigurationAppliedBeforeFieldBinding(t *testing.T) {
	config := zodconfig.Config{TagName: "binding", Aliases: map[string]string{
		"requiredText": "required,min=2", "afterStart": "gtfield=Start", "nonemptyList": "min=1,dive,required",
	}}
	forEachGeneration(t, func(t *testing.T, mini, static bool) {
		dir := t.TempDir()
		symlinkNodeModules(t, dir)
		if static {
			pkg := parseFixture(t, "github.com/befabri/trpcgo/testdata/validationconfig", "testdata/validationconfig/types.go")
			program, err := typemap.CompileValidation(config)
			if err != nil {
				t.Fatal(err)
			}
			mapper := typemap.NewMapper(nil)
			mapper.SetValidation(program)
			input := mapper.Resolve(mapper.Convert(pkg.Scope().Lookup("Input").Type()))
			procs := []codegen.ProcEntry{{Path: "input", ProcType: "query", InputTS: input, OutputTS: "boolean"}}
			writeStaticContract(t, dir, mini, procs, mapper.Defs(), codegen.ZodOptions{Validation: program})
		} else {
			router := trpcgo.NewRouter(trpcgo.WithZodMini(mini), trpcgo.WithZodValidation(config))
			trpcgo.MustQuery(router, "input", func(context.Context, fixture.Input) (bool, error) { return true, nil })
			if err := router.GenerateZod(filepath.Join(dir, "schemas.ts")); err != nil {
				t.Fatal(err)
			}
			if err := router.GenerateTS(filepath.Join(dir, "trpc.ts")); err != nil {
				t.Fatal(err)
			}
		}
		program := `import { InputSchema } from './schemas';
import type { Input } from './trpc';
const valid: Input={start:1,end:2,child:{name:'ok'},anonymous:{name:'ok'},values:['ok']};
// @ts-expect-error alias required must remove JSON omitempty optionality
const missing: Input={...valid,child:{}};
void missing;
InputSchema.parse(valid);
for(const input of [
 {...valid,end:1}, {...valid,child:{}}, {...valid,child:{name:'x'}},
 {...valid,anonymous:{name:''}}, {...valid,values:[]}, {...valid,values:['']}
]) {if(InputSchema.safeParse(input).success) throw new Error('configured rule lost: '+JSON.stringify(input));}
`
		writeTestSources(t, dir, map[string]string{"main.ts": program})
		assertTypeScriptCompiles(t, dir, "trpc.ts", "schemas.ts", "main.ts")
		runTypeScript(t, dir, "main.ts")
	})
}

func TestGenerateZodUsesRouterUnknownFieldPolicy(t *testing.T) {
	for _, strict := range []bool{false, true} {
		r := trpcgo.NewRouter(trpcgo.WithStrictInput(strict))
		trpcgo.MustQuery(r, "input", func(context.Context, fixture.Child) (bool, error) { return true, nil })
		output := generateZod(t, r)
		expected := "z.looseObject("
		if strict {
			expected = "z.strictObject("
		}
		if !strings.Contains(output, expected) {
			t.Fatalf("strict=%v lost object policy:\n%s", strict, output)
		}
	}
}

func TestInvalidZodConfigurationPreservesFiles(t *testing.T) {
	r := trpcgo.NewRouter(trpcgo.WithZodValidation(zodconfig.Config{Aliases: map[string]string{"first": "second", "second": "first"}}))
	trpcgo.MustQuery(r, "input", func(context.Context, fixture.Input) (bool, error) { return true, nil })
	for _, generate := range []func(string) error{r.GenerateTS, r.GenerateZod} {
		path := filepath.Join(t.TempDir(), "out.ts")
		if err := os.WriteFile(path, []byte("previous output"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := generate(path); err == nil {
			t.Fatal("cyclic configuration accepted")
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if string(data) != "previous output" {
			t.Fatalf("invalid configuration overwrote output: %s", data)
		}
	}
}

func TestZodCustomValidationContract(t *testing.T) {
	assertions := `
import { setMinimum } from './custom-validators';
setMinimum(0);
if (!schemas.CustomDynamicInputSchema.safeParse({}).success) throw new Error('missing custom rule was cached during schema construction');
setMinimum(1);
if (schemas.CustomDynamicInputSchema.safeParse({}).success) throw new Error('missing custom rule was not evaluated at parse time');
`
	validators := map[string]string{"custom-validators.ts": validationcontract.CustomValidatorsTS}
	// Custom rules must also work in a module that emits no array helpers.
	t.Run("objects", func(t *testing.T) {
		var cases []validationcontract.Case
		for _, tc := range validationcontract.CustomCases {
			if !strings.HasPrefix(tc.Type, "CustomOptionalArray") {
				cases = append(cases, tc)
			}
		}
		runZodContract(t, zodContract{cases: cases, config: validationcontract.CustomValidation(), files: validators, assertions: []string{assertions}})
	})
	t.Run("with_arrays", func(t *testing.T) {
		runZodContract(t, zodContract{cases: validationcontract.CustomCases, config: validationcontract.CustomValidation(), files: validators, assertions: []string{assertions}})
	})
}

// Every alias is a valid module import and also a local in some generated
// callback or guard, so the original expressions must bind to the import.
// Hoisting happens in the schema writer, which both mappers share.
func TestZodCustomPredicateImportHygiene(t *testing.T) {
	for _, alias := range []string{"check", "value", "result", "data", "integer"} {
		t.Run(alias, func(t *testing.T) {
			config := validationcontract.CustomValidation()
			config.Imports[alias] = "./custom-validators"
			safe := config.Rules["safe"]
			safe.Predicate = alias + ".safe"
			config.Rules["safe"] = safe
			large := config.Rules["large"]
			large.Predicate = alias + ".large"
			config.Rules["large"] = large
			config.StructRules["CustomStructInput"][0].Predicate = alias + ".struct"
			// A live exported function can change between parses. Hoisting its lexical
			// scope must not cache its current value during module construction.
			source := strings.Replace(validationcontract.CustomValidatorsTS, "export function safe(value: string): unknown {", "export let safe = (value: string): unknown => {", 1)
			source = strings.Replace(source, "}\nlet minimum", "};\nlet minimum", 1)
			source += `
export function large(value: bigint): boolean { return value === 9223372036854775807n; }
export function struct(data: {start:number;end:number}): boolean { return data.end >= data.start; }
export function changeSafe(): void { safe = (value: string) => value === 'changed'; }
`
			assertions := `
import {changeSafe} from './custom-validators';
changeSafe();
if (schemas.CustomSafeInputSchema.safeParse({value:'ok'}).success) throw new Error('hoisting cached an imported function');
if (!schemas.CustomSafeInputSchema.safeParse({value:'changed'}).success) throw new Error('hoisting did not preserve live module binding');
`
			runZodContract(t, zodContract{
				cases: validationcontract.CustomCases, config: config,
				files: map[string]string{"custom-validators.ts": source}, assertions: []string{assertions},
				reflectionOnly: true,
			})
		})
	}
}

func TestZodEmbeddedRefinementSemantics(t *testing.T) {
	checkCompositionZod(t, fieldcomposition.RefinementCases, func(r *trpcgo.Router) {
		trpcgo.MustQuery(r, "optional", func(context.Context, fieldcomposition.OptionalBoundsInput) (string, error) { return "", nil })
		trpcgo.MustQuery(r, "strict", func(context.Context, fieldcomposition.OptionalStrictBoundsInput) (string, error) { return "", nil })
		trpcgo.MustQuery(r, "partial", func(context.Context, fieldcomposition.OptionalBoundsWithoutCollision) (string, error) { return "", nil })
		trpcgo.MustQuery(r, "multi", func(context.Context, fieldcomposition.MultipleRefinedBases) (string, error) { return "", nil })
		trpcgo.MustQuery(r, "optionalMulti", func(context.Context, fieldcomposition.MultipleOptionalRefinedBases) (string, error) { return "", nil })
		trpcgo.MustQuery(r, "omitted", func(context.Context, fieldcomposition.InheritedOmittedBoundInput) (string, error) { return "", nil })
		trpcgo.MustQuery(r, "nestedOmitted", func(context.Context, fieldcomposition.NestedOmittedBoundInput) (string, error) { return "", nil })
		trpcgo.MustQuery(r, "nestedOptional", func(context.Context, fieldcomposition.OptionalNestedBoundsInput) (string, error) { return "", nil })
		trpcgo.MustQuery(r, "optionalTarget", func(context.Context, fieldcomposition.OptionalTargetInput) (string, error) { return "", nil })
	}, `
const parsed = schemas.OptionalBoundsInputSchema.parse({ label: 1, max: 2 });
if ('min' in parsed) throw new Error('refinement default leaked into parsed output');
if (schemas.OptionalTargetInputSchema.safeParse({ ceiling: 1 }).success) throw new Error('missing comparison target accepted');
`)
}

func TestZodJSONPromotionSemantics(t *testing.T) {
	encoded, err := json.Marshal(fieldcomposition.PromotionInput{})
	if err != nil {
		t.Fatal(err)
	}
	if string(encoded) != `{"x":"","base":{"y":0}}` {
		t.Fatalf("unexpected JSON reference: %s", encoded)
	}
	checkCompositionZod(t, []fieldcomposition.RefinementCase{
		{Type: "PromotionInput", JSON: string(encoded), Valid: true},
		{Type: "PromotionInput", JSON: `{"x":1,"base":{"y":0}}`, Valid: false},
		{Type: "PromotionInput", JSON: `{"x":"","base":{"y":"wrong"}}`, Valid: false},
	}, func(r *trpcgo.Router) {
		trpcgo.MustQuery(r, "promotion", func(context.Context, fieldcomposition.PromotionInput) (string, error) { return "", nil })
	}, `const parsed=schemas.PromotionInputSchema.parse({x:'ok',base:{y:1}}); const x:string=parsed.x; const y:number=parsed.base.y; void [x,y];`)
}

func TestZodElementOmitemptySemantics(t *testing.T) {
	checkCompositionZod(t, fieldcomposition.ElementValidationCases, func(r *trpcgo.Router) {
		trpcgo.MustQuery(r, "elements", func(context.Context, fieldcomposition.ElementOmitemptyInput) (string, error) { return "", nil })
		trpcgo.MustQuery(r, "pointerElements", func(context.Context, fieldcomposition.PointerElementOmitemptyInput) (string, error) { return "", nil })
	}, `
for (const input of [
  { emails: { a: 1 }, list: [], nested: {} },
  { emails: {}, list: [null], nested: {} },
  { list: [], nested: {} },
]) {
  if (schemas.ElementOmitemptyInputSchema.safeParse(input).success) throw new Error('omitempty weakened element types or parent presence');
}
`)
}

func TestZodNumericEnumOmitemptySemantics(t *testing.T) {
	checkCompositionZod(t, fieldcomposition.NumericEnumValidationCases, func(r *trpcgo.Router) {
		trpcgo.MustQuery(r, "numericEnums", func(context.Context, fieldcomposition.NumericEnumOmitemptyInput) (string, error) { return "", nil })
	}, `
for (const input of [{}, { values: { a: '0' } }, { values: { a: 1.5 } }, { values: { a: null } }]) {
  if (schemas.NumericEnumOmitemptyInputSchema.safeParse(input).success) throw new Error('omitempty weakened numeric types or parent presence');
}
const parsed = schemas.NumericEnumOmitemptyInputSchema.parse({ values: { a: 0 } });
const value: 0 | 1 | 2 = parsed.values.a;
// @ts-expect-error numeric enum output includes zero
const withoutZero: 1 | 2 = parsed.values.a;
void [value, withoutZero];
`)
}

func checkCompositionZod(t *testing.T, inputs []fieldcomposition.RefinementCase, register func(*trpcgo.Router), checks string) {
	t.Helper()
	cases, err := json.Marshal(inputs)
	if err != nil {
		t.Fatal(err)
	}
	script := `
import * as schemas from './schemas.ts';
type Schema = { safeParse(input: unknown): { success: boolean } };
const byName: Record<string, unknown> = schemas;
const cases = ` + string(cases) + `;
for (const test of cases) {
  const result = (byName[test.type + 'Schema'] as Schema).safeParse(JSON.parse(test.json));
  if (result.success !== test.valid) throw new Error(test.type + ': ' + test.json + ', expected valid=' + test.valid);
}
` + checks
	forEachGeneration(t, func(t *testing.T, mini, static bool) {
		if !static {
			r := trpcgo.NewRouter(trpcgo.WithZodMini(mini))
			register(r)
			checkGeneratedZod(t, r, script)
			return
		}
		pkg := parseFixture(t, "example.com/fieldcomposition", "testdata/fieldcomposition/types.go")
		m := typemap.NewMapper(nil)
		var procs []codegen.ProcEntry
		seen := map[string]bool{}
		for _, test := range inputs {
			if seen[test.Type] {
				continue
			}
			seen[test.Type] = true
			input := m.Convert(pkg.Scope().Lookup(test.Type).Type())
			procs = append(procs, codegen.ProcEntry{Path: test.Type, ProcType: "query", InputTS: input, OutputTS: "string"})
		}
		for i := range procs {
			procs[i].InputTS = m.Resolve(procs[i].InputTS)
		}
		checkGeneratedZodFile(t, func(path string) error {
			var out bytes.Buffer
			if err := codegen.WriteZodSchemas(&out, procs, m.Defs(), zodStyle(mini), codegen.ZodOptions{}); err != nil {
				return err
			}
			return os.WriteFile(path, out.Bytes(), 0o644)
		}, script)
	})
}

func TestZodSchemaSpecificRecursiveTypes(t *testing.T) {
	forEachGeneration(t, func(t *testing.T, mini, static bool) {
		dir := t.TempDir()
		symlinkNodeModules(t, dir)
		if static {
			pkg := parseFixture(t, "schemaissues", "testdata/schemaissues/types.go")
			m := typemap.NewMapper(map[string]typemap.TypeMeta{"schemaissues.Tags": {IsAlias: true}, "schemaissues.Links": {IsAlias: true}})
			input := m.Convert(pkg.Scope().Lookup("Node").Type())
			procs := []codegen.ProcEntry{{Path: "node", ProcType: "query", InputTS: m.Resolve(input), OutputTS: "string"}}
			writeStaticContract(t, dir, mini, procs, m.Defs(), codegen.ZodOptions{})
		} else {
			r := trpcgo.NewRouter(trpcgo.WithZodMini(mini))
			trpcgo.MustQuery(r, "node", func(context.Context, schemaissues.Node) (string, error) { return "", nil })
			if err := r.GenerateZod(filepath.Join(dir, "schemas.ts")); err != nil {
				t.Fatal(err)
			}
			if err := r.GenerateTS(filepath.Join(dir, "trpc.ts")); err != nil {
				t.Fatal(err)
			}
		}
		script := `
import type * as z from 'zod';
import { NodeSchema } from './schemas';
import type { Node } from './barrel';
const leaf: z.input<typeof NodeSchema> = { date: 'today', tags: [{ name: 'ok' }], inline: { z: 1 } };
const tree: z.input<typeof NodeSchema> = { ...leaf, child: leaf, links: { next: leaf }, inline: { z: 2, next: leaf } };
const parsed = NodeSchema.parse(tree);
const date: string = parsed.child!.date;
const name: string = parsed.tags[0].name;
declare const full: Node;
type Secret = Node['secret'];
const secret: unknown = parsed.secret;
// @ts-expect-error unvalidated fields require narrowing before use as strings
const secretString: string = parsed.secret;
if ('secret' in parsed) throw new Error('absent unvalidated field was materialized');
const opaque = { arbitrary: true };
if ((NodeSchema.parse({ ...tree, secret: opaque } as unknown).secret as unknown) !== opaque) throw new Error('unvalidated field was not preserved');
// @ts-expect-error schema types must not be exported as domain types
type Private = import('./schemas').Node;
void [date, name];
for (const input of [{ ...leaf, tags: [{ name: '' }] }, { ...leaf, inline: { z: 'bad' } }, { ...leaf, links: { next: { ...leaf, date: 1 } } }]) {
  if (NodeSchema.safeParse(input).success) throw new Error('invalid nested value accepted');
}
`
		writeTestSources(t, dir, map[string]string{"barrel.ts": "export * from './trpc';\nexport * from './schemas';\n", "validate.ts": script})
		assertTypeScriptCompiles(t, dir, "barrel.ts", "validate.ts")
		runTypeScript(t, dir, "validate.ts")
	})
}

func TestZodNonStructExtendsKeepsOtherSchemas(t *testing.T) {
	forEachStyle(t, func(t *testing.T, mini bool) {
		r := trpcgo.NewRouter(trpcgo.WithZodMini(mini))
		trpcgo.MustQuery(r, "time", func(context.Context, schemaissues.NonStructBase) (string, error) { return "", nil })
		trpcgo.MustQuery(r, "tag", func(context.Context, schemaissues.Tag) (string, error) { return "", nil })
		checkGeneratedZod(t, r, `import {TagSchema} from './schemas'; TagSchema.parse({name:'ok'}); if(TagSchema.safeParse({name:''}).success) throw new Error('unrelated validation lost');`)
	})
}

type malformedDiveParam struct {
	Value []string `json:"value" validate:"dive=1,required"`
}

type malformedDiveEmptyParam struct {
	Value []string `json:"value" validate:"dive=,required"`
}

type malformedDiveAlternative struct {
	Value []string `json:"value" validate:"required|dive"`
}

type malformedOmitAlternative struct {
	Value []string `json:"value" validate:"omitempty|required"`
}

type malformedOmitEmptyParam struct {
	Value []string `json:"value" validate:"omitempty=,required"`
}

func registerMalformedScope[T any](r *trpcgo.Router) {
	trpcgo.MustQuery(r, "scope", func(context.Context, T) (bool, error) { return true, nil })
}

// The invalid directive must survive both mappers until grammar validation. In
// particular, a malformed dive must not disappear at the first split.
func TestZodMalformedDirectiveContract(t *testing.T) {
	for _, tc := range []struct {
		tag, message string
		register     func(*trpcgo.Router)
	}{
		{"dive=1,required", "dive does not accept a parameter", registerMalformedScope[malformedDiveParam]},
		{"dive=,required", "dive does not accept a parameter", registerMalformedScope[malformedDiveEmptyParam]},
		{"required|dive", "dive cannot be used in an OR group", registerMalformedScope[malformedDiveAlternative]},
		{"omitempty|required", "omitempty cannot be used in an OR group", registerMalformedScope[malformedOmitAlternative]},
		{"omitempty=,required", "omitempty does not accept a parameter", registerMalformedScope[malformedOmitEmptyParam]},
	} {
		t.Run(tc.tag, func(t *testing.T) {
			fset := token.NewFileSet()
			source := fmt.Sprintf("package contract; type Input struct { Value []string `json:\"value\" validate:%q` }", tc.tag)
			syntax, err := parser.ParseFile(fset, "input.go", source, 0)
			if err != nil {
				t.Fatal(err)
			}
			pkg := typeCheck(t, "contract", fset, []*ast.File{syntax})
			forEachStyle(t, func(t *testing.T, mini bool) {
				router := trpcgo.NewRouter(trpcgo.WithZodMini(mini))
				t.Cleanup(func() { _ = router.Close() })
				tc.register(router)
				err := router.GenerateZod(filepath.Join(t.TempDir(), "schemas.ts"))
				if err == nil || !strings.Contains(err.Error(), ".value") || !strings.Contains(err.Error(), tc.message) {
					t.Fatalf("reflection diagnostic=%v; want field path and %q", err, tc.message)
				}
				mapper := typemap.NewMapper(nil)
				input := mapper.Resolve(mapper.Convert(pkg.Scope().Lookup("Input").Type()))
				var out bytes.Buffer
				err = codegen.WriteZodSchemas(&out, []codegen.ProcEntry{{InputTS: input}}, mapper.Defs(), zodStyle(mini), codegen.ZodOptions{})
				if err == nil || !strings.Contains(err.Error(), "Input.value") || !strings.Contains(err.Error(), tc.message) {
					t.Fatalf("static diagnostic=%v; want field path and %q", err, tc.message)
				}
				if out.Len() != 0 {
					t.Fatalf("invalid scope wrote a partial schema: %s", &out)
				}
			})
		})
	}
}

type ZodVersionInput struct {
	Name string `json:"name" validate:"min=1"`
}

// An older Zod counts UTF-16 code units in string length checks, so a
// generated module must fail to type-check against one. The stale compilation
// maps zod to copies of the installed package whose core reports a version
// below the floor; the assertion is a type, so the module gains no runtime code.
func TestZodModuleRejectsOlderZodAtCompileTime(t *testing.T) {
	forEachStyle(t, func(t *testing.T, mini bool) {
		r := trpcgo.NewRouter(trpcgo.WithZodMini(mini))
		trpcgo.Mutation(r, "user.create", func(_ context.Context, input ZodVersionInput) (string, error) {
			return "", nil
		})
		dir := t.TempDir()
		symlinkNodeModules(t, dir)
		if err := r.GenerateZod(filepath.Join(dir, "schemas.ts")); err != nil {
			t.Fatal(err)
		}
		assertTypeScriptCompiles(t, dir, "schemas.ts")
		minimum := codegen.ZodMinimumVersion
		writeTestSources(t, dir, map[string]string{
			"stale-zod.ts": `export * from "./node_modules/zod/index.js";
export * as z from "./stale-classic.js";
`,
			"stale-classic.ts": `export * from "./node_modules/zod/v4/classic/external.js";
export * as core from "./stale-core.js";
`,
			"stale-mini.ts": `export * from "./node_modules/zod/v4/mini/external.js";
export * as core from "./stale-core.js";
`,
			"stale-core.ts": fmt.Sprintf(`export * from "./node_modules/zod/v4/core/index.js";
export const version = { major: %d, minor: %d, patch: 0 } as const;
`, minimum[0], minimum[1]-1),
			"tsconfig.stale.json": `{
  "compilerOptions": {
    "noEmit": true, "strict": true, "skipLibCheck": true, "target": "ES2022",
    "allowImportingTsExtensions": true, "module": "ES2022", "moduleResolution": "bundler",
    "noUnusedLocals": true,
    "paths": { "zod": ["./stale-zod.ts"], "zod/mini": ["./stale-mini.ts"] }
  },
  "files": ["schemas.ts"]
}
`,
		})
		cmd := exec.CommandContext(t.Context(), typeScriptCompiler(t), "-p", "tsconfig.stale.json")
		cmd.Dir = dir
		output, err := cmd.CombinedOutput()
		if err == nil {
			t.Fatalf("generated module type-checked against zod %d.%d", minimum[0], minimum[1]-1)
		}
		expected := fmt.Sprintf("need zod %d.%d.%d or newer", minimum[0], minimum[1], minimum[2])
		if !strings.Contains(string(output), "TS2344") || !strings.Contains(string(output), expected) {
			t.Fatalf("unexpected compiler output:\n%s", output)
		}
	})
}

// The floor is promised in several places; each must state the one the
// generator enforces, so bumping ZodMinimumVersion fails until they agree.
func TestZodMinimumVersionIsStatedConsistently(t *testing.T) {
	m := codegen.ZodMinimumVersion
	floor := fmt.Sprintf("%d.%d.%d", m[0], m[1], m[2])
	for file, want := range map[string]string{
		".github/workflows/ci.yml":                         "zod@" + floor,
		"README.md":                                        "zod@^" + floor,
		"docs/src/content/docs/install.md":                 "zod@^" + floor,
		"docs/src/content/docs/quick-start.md":             "zod@^" + floor,
		"docs/src/content/docs/zod-schemas.md":             "zod@^" + floor,
		"docs/src/content/docs/reference/compatibility.md": "Zod " + floor + " or newer",
		"testdata/zodruntime/package.json":                 `"zod": "^` + floor + `"`,
		"examples/start-trpc/web/package.json":             `"zod": "^` + floor + `"`,
	} {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(data), want) {
			t.Errorf("%s does not state the Zod floor %q", file, want)
		}
	}
}
