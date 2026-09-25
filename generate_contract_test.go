package trpcgo_test

import (
	"bytes"
	"context"
	"encoding/json"
	"go/types"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/befabri/trpcgo"
	"github.com/befabri/trpcgo/internal/analysis"
	"github.com/befabri/trpcgo/internal/codegen"
	"github.com/befabri/trpcgo/internal/typemap"
	"github.com/befabri/trpcgo/testdata/typegraph"
	"github.com/befabri/trpcgo/testdata/validationcontract"
)

// The complete corpus runs in one module to expose interactions between helpers.
func TestZodValidationContract(t *testing.T) {
	testZodValidationCases(t, validationcontract.Cases(), inputPolicyAssertions)
}

// This module has no integer maps or fixed arrays. Quoted decoding and scalar
// refinements must work without those helpers being pulled in by another input.
func TestZodQuotedWireContract(t *testing.T) {
	cases := slices.Concat(validationcontract.WireCases, validationcontract.ScalarBoundaryCases, validationcontract.CrossFieldOmissionCases)
	runZodContract(t, zodContract{cases: cases, absent: []string{"function $goIntegerMap", "function $goFixedArray"}})
}

type NumericTagNumberInput struct {
	Value int `json:"value" validate:"numeric"`
}

// numeric on a Go number must compile and keep rejecting nonnumeric object input.
func TestZodNumericTagContract(t *testing.T) {
	for _, mini := range []bool{false, true} {
		t.Run(zodStyleName(mini), func(t *testing.T) {
			r := trpcgo.NewRouter(trpcgo.WithZodMini(mini))
			t.Cleanup(func() { _ = r.Close() })
			trpcgo.MustQuery(r, "numericTag", func(context.Context, NumericTagNumberInput) (string, error) { return "ok", nil })
			checkGeneratedZod(t, r, `import { NumericTagNumberInputSchema as schema } from './schemas';
for (const value of [42, -42, 0]) schema.parse({value});
for (const value of [1.5, "42", true, null]) {
 if (schema.safeParse({value}).success) throw new Error('numeric accepted ' + JSON.stringify(value));
}`)
		})
	}
}

// Every tag the generator supports must be exercised by the shared corpus in
// both directions. The generator's table is the source of truth, so adding a
// tag without fixtures fails here; the Go oracle in examples/start-trpc/server
// additionally requires that the validator itself rejects a case for the tag.
func TestValidationContractCoversSupportedTags(t *testing.T) {
	cases := validationcontract.Cases()
	coverage, err := validationcontract.Coverage(cases, nil)
	if err != nil {
		t.Fatal(err)
	}
	problems := validationcontract.Uncovered(coverage, false)
	for _, problem := range problems {
		t.Error(problem)
	}
	if len(problems) > 0 {
		t.Log("add accepted and rejected cases to testdata/validationcontract for each tag above")
	}
}

const inputPolicyAssertions = `
import type { core } from 'zod';
const customRole: core.output<typeof schemas.PolicyEnumInputSchema>['role'] = 'application-defined';
const customStatus: core.output<typeof schemas.PolicyEnumInputSchema>['status'] = 2;
void customRole; void customStatus;
const provided = schemas.PolicyOmitInputSchema.parse({name:'ok',id:'supplied',keys:{}});
if (provided.id !== 'supplied') throw new Error('known omitted field was discarded');
const unvalidated = schemas.PolicyOmitInputSchema.parse({name:'ok',id:{supplied:true},keys:{}});
if (typeof unvalidated.id !== 'object') throw new Error('omitted field unexpectedly validated');
`

func TestZodIntegerMapDecodingContract(t *testing.T) {
	runTypeGraphContract(t, []string{"IntegerMapInput"}, integerMapContractScript)
}

const integerMapContractScript = `
import type * as z from 'zod';
import {IntegerMapInputSchema, RecursiveIntegerMapSchema, decodeGoJSON} from './schemas';
import type {IntegerMapInput, RecursiveIntegerMap} from './trpc';

function assert(condition: unknown, message: string): asserts condition {
 if (!condition) throw new Error(message);
}
function equal(actual: unknown, expected: unknown, message: string): void {
 assert(JSON.stringify(actual) === JSON.stringify(expected), message + ': ' + JSON.stringify(actual));
}
const empty: IntegerMapInput = {values:{},nested:{},recursive:{}};
const recursive: RecursiveIntegerMap = {1:{2:{}}};
const recursiveInput: z.input<typeof RecursiveIntegerMapSchema> = recursive;
const recursiveOutput: RecursiveIntegerMap = RecursiveIntegerMapSchema.parse(recursiveInput);
const schemaInput: z.input<typeof IntegerMapInputSchema> = {...empty,recursive,next:empty};
const schemaOutput: IntegerMapInput = IntegerMapInputSchema.parse(schemaInput);
const nestedNumber: number = schemaOutput.next!.values[1]!;
// @ts-expect-error recursive maps keep their recursive object leaf type
const invalidLeaf: z.input<typeof RecursiveIntegerMapSchema> = {1:2};
void [recursiveOutput,nestedNumber,invalidLeaf];

const original = {values:{'01':1,'+2':2,'-00':3},nested:{'01':{'002':4}},recursive:{'01':{'002':{}}}};
const before = JSON.stringify(original);
Object.freeze(original.values);
const normalized = IntegerMapInputSchema.parse(original);
equal(normalized, {values:{0:3,1:1,2:2},nested:{1:{2:4}},recursive:{1:{2:{}}}}, 'noncolliding keys were not canonicalized');
assert(JSON.stringify(original) === before, 'normalization mutated the input');
assert(normalized.values !== original.values && normalized.nested !== original.nested, 'normalization reused mutable input maps');

// Every pair of distinct spellings is ambiguous after ordinary JSON.parse,
// even when both values happen to be equal.
for (const aliases of [['1','01','+1','+01','001'],['0','-0','+0','00','-00','+00'],['-1','-01','-001']]) {
 for (let i = 0; i < aliases.length; i++) for (let j = i + 1; j < aliases.length; j++) {
  for (const values of [[1,1],[1,2]]) {
   const map = Object.fromEntries([[aliases[i]!,values[0]!],[aliases[j]!,values[1]!]]);
   assert(!IntegerMapInputSchema.safeParse({...empty,values:map}).success, 'plain ambiguous aliases accepted: '+JSON.stringify(map));
  }
 }
}

function decode(raw: string): IntegerMapInput {
 const result = decodeGoJSON(IntegerMapInputSchema, raw);
 if (!result.success) throw new Error('raw JSON rejected: ' + raw + ': ' + JSON.stringify(result.error.issues));
 return result.data;
}
equal(decode('{"values":{"01":7,"1":2},"nested":{},"recursive":{}}').values, {1:2}, 'source order was not used for colliding aliases');
equal(decode('{"values":{"1":1,"\\u0031":2,"1":3},"nested":{},"recursive":{}}').values, {1:3}, 'exact or escaped duplicate keys lost source order');
for (const raw of [
 '{"values":{"01":"bad","1":2},"nested":{},"recursive":{}}',
 '{"values":{"01":128,"1":2},"nested":{},"recursive":{}}',
 '{"values":{"128":1,"1":2},"nested":{},"recursive":{}}',
]) assert(!decodeGoJSON(IntegerMapInputSchema, raw).success, 'overwritten decoder failure was ignored: '+raw);

// Only decodeGoJSON knows source order. An ordinary object stays ambiguous,
// even when it carries metadata under the formerly global symbol.
const pair = (name: string, value: unknown) => Object.freeze([name, value] as const);
const forged = {...empty, values: {'01':1,'1':2}};
Object.defineProperty(forged.values, Symbol.for('trpcgo.go-json.entries.v1'), {
 value: Object.freeze({entries: Object.freeze([pair('01',1), pair('1',2)]), snapshot: Object.freeze([pair('1',2), pair('01',1)])}),
});
assert(!IntegerMapInputSchema.safeParse(forged).success, 'ordinary object supplied trusted source order');

const nested = decode('{"values":{},"nested":{"01":{"01":1,"1":2},"1":{"002":3}},"recursive":{"01":{"01":{},"1":{}},"1":{"2":{}}}}');
equal(nested.nested, {1:{2:3}}, 'nested map alias order lost');
equal(nested.recursive, {1:{2:{}}}, 'recursive map alias order lost');
assert(!IntegerMapInputSchema.safeParse({...empty,nested:{1:{'01':1,'1':2}}}).success, 'nested plain aliases accepted');
assert(!IntegerMapInputSchema.safeParse({...empty,recursive:{1:{'01':{},'1':{}}}}).success, 'recursive plain aliases accepted');
`

// Named constants come from static analysis only, so this contract has no
// reflection leg. Accepted values must decode to the same Go value as the input.
func TestZodDecodedNamedEnumContract(t *testing.T) {
	pkg := typeGraphPackage(t)
	forEachStyle(t, func(t *testing.T, mini bool) {
		mapper := typemap.NewMapper(map[string]typemap.TypeMeta{
			pkg.Path() + ".Replacement":  {ConstValues: []string{`"�"`, `"ok"`}},
			pkg.Path() + ".FloatChoice":  {ConstValues: []string{"1", "2"}},
			pkg.Path() + ".NarrowChoice": {ConstValues: []string{"0.1", "1.0000001192092896"}},
		})
		input := mapper.Resolve(mapper.Convert(pkg.Scope().Lookup("DecodedEnumInput").Type()))
		procs := []codegen.ProcEntry{{Path: "input", ProcType: "query", InputTS: input, OutputTS: "string"}}
		dir := t.TempDir()
		symlinkNodeModules(t, dir)
		writeStaticContract(t, dir, mini, procs, mapper.Defs(), codegen.ZodOptions{})
		writeTestSources(t, dir, map[string]string{"validate.ts": decodedEnumScript})
		assertTypeScriptCompiles(t, dir, "schemas.ts", "trpc.ts", "validate.ts")
		out := runTypeScript(t, dir, "validate.ts")
		var results []struct {
			Input  json.RawMessage `json:"input"`
			Output json.RawMessage `json:"output"`
		}
		if err := json.Unmarshal(out, &results); err != nil || len(results) != 9 {
			t.Fatalf("invalid runtime results: %v\n%s", err, out)
		}
		for _, result := range results {
			var before, after typegraph.DecodedEnumInput
			if err := json.Unmarshal(result.Input, &before); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(result.Output, &after); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(before, after) {
				t.Fatalf("open scalar validation changed decoded Go values: before=%#v after=%#v", before, after)
			}
		}
	})
}

// A oneof narrows a field of a named scalar type exactly as it narrows an
// unnamed scalar, whether constants make the type a union or not. The named
// type itself stays open. Static analysis alone knows the constants.
func TestZodOneofNarrowsNamedScalars(t *testing.T) {
	pkg := typeGraphPackage(t)
	forEachStyle(t, func(t *testing.T, mini bool) {
		mapper := typemap.NewMapper(map[string]typemap.TypeMeta{
			pkg.Path() + ".OneofRole":  {ConstValues: []string{`"admin"`, `"editor"`}},
			pkg.Path() + ".OneofLevel": {ConstValues: []string{"1", "2"}},
		})
		input := mapper.Resolve(mapper.Convert(pkg.Scope().Lookup("OneofNamedInput").Type()))
		procs := []codegen.ProcEntry{{Path: "input", ProcType: "query", InputTS: input, OutputTS: "string"}}
		dir := t.TempDir()
		symlinkNodeModules(t, dir)
		writeStaticContract(t, dir, mini, procs, mapper.Defs(), codegen.ZodOptions{})
		writeTestSources(t, dir, map[string]string{"validate.ts": oneofNamedScript})
		assertTypeScriptCompiles(t, dir, "schemas.ts", "trpc.ts", "validate.ts")
		runTypeScript(t, dir, "validate.ts")
	})
}

const oneofNamedScript = `import type { core } from 'zod';
import { OneofNamedInputSchema as schema, OneofRoleSchema, OneofLevelSchema } from './schemas';
import type { OneofNamedInput } from './trpc';
type Equal<A, B> = (<T>() => T extends A ? 1 : 2) extends (<T>() => T extends B ? 1 : 2) ? true : false;
type Output = core.output<typeof schema>;
const exact: [
  Equal<Output['role'], 'admin' | 'editor'>,
  Equal<Output['optional'], '' | 'admin' | 'editor' | undefined>,
  Equal<Output['level'], 1 | 2>,
  Equal<Output['code'], 'x' | 'y'>,
  Equal<Output['open'], string>,
] = [true, true, true, true, true];
const valid = { role: 'admin', level: 1, code: 'x', open: 'unlisted' } as const;
// Parsed values remain valid procedure inputs.
const parsed: OneofNamedInput = schema.parse(valid);
void [exact, parsed];
schema.parse({ ...valid, optional: '' });
schema.parse({ ...valid, optional: 'editor' });
OneofRoleSchema.parse('unlisted');
OneofLevelSchema.parse(100);
for (const [invalid, path] of [[{ role: 'viewer' }, 'role'], [{ optional: 'viewer' }, 'optional'], [{ level: 3 }, 'level'], [{ level: 128 }, 'level'], [{ code: 'z' }, 'code']] as const) {
  const result = schema.safeParse({ ...valid, ...invalid });
  if (result.success || !result.error.issues.some(issue => issue.code === 'invalid_value' && issue.path.join('.') === path)) throw new Error('oneof did not reject ' + JSON.stringify(invalid) + ' with invalid_value');
}
`

const decodedEnumScript = `import { DecodedEnumInputSchema, ReplacementSchema } from './schemas';
import type { DecodedEnumInput, Replacement } from './trpc';
const baseline: DecodedEnumInput = { value: '�', quoted: '"�"', float: '1', narrow: '0.1', sparse: {} };
const parsed = DecodedEnumInputSchema.parse(baseline);
const literal: string = parsed.value;
const named: Replacement = ReplacementSchema.parse('\ud800');
// Go constants do not close the underlying scalar type.
const arbitrary: typeof parsed.value = 'unknown';
void literal; void named; void arbitrary;
const valid = [
  baseline,
  {...baseline, value: '\ud800', quoted: '"\\udc00"', float: '0x1p1', narrow: '0.100000001'},
  {...baseline, value: 'ok', quoted: '"ok"', float: '0x_1p0', narrow: '1.0000000596046447753906250000000000001'},
  {...baseline, sparse: {'\ud800': 1}},
  {...baseline, optional: 'null'},
  {...baseline, optional: '0x1p1'},
  {...baseline, value: '😀', quoted: '"\\ud83d\\ude00"'},
  {...baseline, value: 'unknown', quoted: '"unknown"', sparse: {'unknown': 1}},
  {...baseline, float: '1_0', narrow: '1.000000059604644775390625'},
];
const results = valid.map(input => {
  const output = DecodedEnumInputSchema.parse(input);
  return { input, output };
});
for (const input of [
  {...baseline, quoted: 'unknown'},
  {...baseline, float: '.1'},
  {...baseline, float: '1e999'},
  {...baseline, narrow: '3.5e38'},
  {...baseline, sparse: {'unknown': 128}},
  {...baseline, optional: '0'},
]) { if (DecodedEnumInputSchema.safeParse(input).success) throw new Error('underlying scalar constraint lost: ' + JSON.stringify(input)); }
console.log(JSON.stringify(results));
`

// Ordinary object parsing and map normalization must work in a module without
// fixed arrays. All other regression cases also run in the combined corpus.
func TestZodIntegerMapObjectContract(t *testing.T) {
	testZodValidationCases(t, validationcontract.MapDecodingCases, mapObjectAssertions)
}

// Exercise the ordinary object API separately from the raw JSON oracle. These
// assertions also ensure preprocessing preserves precise public schema types.
const mapObjectAssertions = `
import type { core } from 'zod';
type MapInput = core.input<typeof schemas.MapRoundtripSchema>;
type MapOutput = core.output<typeof schemas.MapRoundtripSchema>;
type InputIsExact = Assert<Equal<MapInput, { values: Record<string, number> }>>;
type OutputIsExact = Assert<Equal<MapOutput, { values: Record<string, number> }>>;
// @ts-expect-error map values remain numeric through preprocessing
const invalidInput: MapInput = { values: { '1': 'wrong' } };
// @ts-expect-error map output cannot become any or unknown
const invalidOutput: MapOutput = { values: { '1': 'wrong' } };
void [invalidInput, invalidOutput];

function assertMap(condition: unknown, message: string): asserts condition {
  if (!condition) throw new Error(message);
}
for (const values of [{ '1': 1, '01': 2 }, { '+1': 1, '1': 2 }, { '-0': 1, '0': 2 }]) {
  const result = schemas.MapRoundtripSchema.safeParse({ values });
  assertMap(!result.success, 'ordinary object accepted ambiguous aliases');
  assertMap(result.error.issues.some(issue => issue.message.includes('decodeGoJSON') && issue.path[0] === 'values'), 'ambiguous aliases need an actionable field error');
}
const distinct = schemas.MapRoundtripSchema.parse({ values: { '01': 7, '2': 2 } });
assertMap(JSON.stringify(distinct) === '{"values":{"1":7,"2":2}}', 'unambiguous object did not normalize Go keys');
const raw = schemas.decodeGoJSON(schemas.MapRoundtripSchema, '{"values":{"01":7,"1":2}}');
assertMap(raw.success && JSON.stringify(raw.data) === '{"values":{"1":2}}', 'raw source order was lost');
for (const values of [null, [], 'wrong', 1, { 'invalid': 1 }, { '1': 'wrong' }]) {
  assertMap(!schemas.MapRoundtripSchema.safeParse({ values }).success, 'normalization bypassed the map schema');
}
`

// Array padding and zero-value omission also run without integer-map schemas.
func TestZodFixedArrayContract(t *testing.T) {
	testZodValidationCases(t, slices.Concat(validationcontract.FixedArrayCases, validationcontract.ArrayZeroCases), fixedArrayContextAssertions, arrayZeroContextAssertions)
}

// The decoder's synthesized nils must be visible in schema output types, while
// the ordinary named schemas retain their independent nullability policy.
const fixedArrayContextAssertions = `
import type { core } from 'zod';
type PointerOutput = core.output<typeof schemas.FixedArrayNilPointersSchema>;
type PointerOutputExact = Assert<Equal<PointerOutput, { values: (number | null)[] }>>;
type RequiredPointerOutput = core.output<typeof schemas.FixedArrayRequiredPointersSchema>;
type RequiredPointerOutputExact = Assert<Equal<RequiredPointerOutput, { values: number[] }>>;
type RecursiveArrayOutput = core.output<typeof schemas.FixedArrayNilRecursiveSchema>;
type RecursiveElement = RecursiveArrayOutput['values'][number];
const nilRecursiveElement: RecursiveElement = { value: 0, next: null, children: null };
// @ts-expect-error contextual recursive values must retain numeric scalar types
const badRecursiveElement: RecursiveElement = { value: 'wrong', next: null, children: null };
void [nilRecursiveElement, badRecursiveElement];
function assertArray(condition: unknown, message: string): asserts condition {
  if (!condition) throw new Error(message);
}
const zeros = schemas.FixedArrayNilStructsSchema.parse({ values: [] });
assertArray(JSON.stringify(zeros) === '{"values":[{"pointer":null,"items":null,"lookup":null,"bytes":null},{"pointer":null,"items":null,"lookup":null,"bytes":null}]}', 'zero struct did not preserve all nil fields');
assertArray(!schemas.FixedArrayNilItemSchema.safeParse({ pointer: null, items: null, lookup: null, bytes: null }).success, 'context changed ordinary named null policy');
assertArray(schemas.FixedArrayNilItemSchema.safeParse({ pointer: 1, items: [], lookup: {}, bytes: '' }).success, 'ordinary non-null control failed');
const nested = schemas.FixedArrayNilRecursiveSchema.parse({ values: [{ next: { value: 1 } }] });
assertArray(nested.values[0]?.next?.value === 1 && nested.values[0]?.next?.children === null, 'new pointer target did not receive Go zero fields');
const original = { values: [{ pointer: 1 }] };
schemas.FixedArrayNilStructsSchema.parse(original);
assertArray(JSON.stringify(original) === '{"values":[{"pointer":1}]}', 'array decoding mutated its object input');
const nilEmbedded = schemas.FixedArrayEmbeddedPointersSchema.parse({ values: [] });
assertArray(JSON.stringify(nilEmbedded) === '{"values":[{"label":""}]}', 'padding allocated a nil embedded pointer');
const allocatedEmbedded = schemas.FixedArrayEmbeddedPointersSchema.parse({ values: [{ name: 'ok' }] });
assertArray(allocatedEmbedded.values[0]?.items === null && allocatedEmbedded.values[0]?.count === 0, 'allocating an embedded parent did not initialize its siblings');
const allocatedOuter = schemas.FixedArrayNestedEmbeddedPointersSchema.parse({ values: [{ marker: 'x' }] });
assertArray(allocatedOuter.values[0]?.name === undefined && allocatedOuter.values[0]?.marker === 'x', 'allocating an outer embedded parent allocated its nested pointer');
`

func TestZodFixedArrayClientContract(t *testing.T) {
	pkg := validationContractTypes(t)
	forEachGeneration(t, func(t *testing.T, mini, static bool) {
		dir := t.TempDir()
		symlinkNodeModules(t, dir)
		copyTypeScriptAssertions(t, dir)
		if static {
			seen := map[string]bool{}
			var sourceProcs []analysis.Procedure
			for _, tc := range validationcontract.FixedArrayCases {
				if seen[tc.Type] {
					continue
				}
				seen[tc.Type] = true
				typ := pkg.Scope().Lookup(tc.Type).Type()
				sourceProcs = append(sourceProcs, analysis.Procedure{Path: tc.Type, Type: "query", InputType: typ, OutputType: typ})
			}
			generic := pkg.Scope().Lookup("ArrayClientGenericInput").Type()
			genericBase := pkg.Scope().Lookup("ArrayClientBox").Type().(*types.Named)
			pointerBox, err := types.Instantiate(nil, genericBase, []types.Type{types.NewPointer(types.Typ[types.Int])}, true)
			if err != nil {
				t.Fatal(err)
			}
			for path, typ := range map[string]types.Type{"generic": generic, "genericPointer": pointerBox, "override": pkg.Scope().Lookup("ArrayClientOverride").Type(), "pointerEcho": pkg.Scope().Lookup("FixedArrayNilPointers").Type(), "structEcho": pkg.Scope().Lookup("FixedArrayNilStructs").Type(), "recursiveEcho": pkg.Scope().Lookup("FixedArrayNilRecursive").Type()} {
				sourceProcs = append(sourceProcs, analysis.Procedure{Path: path, Type: "query", InputType: typ, OutputType: typ})
			}
			list := types.NewSlice(pointerBox)
			sourceProcs = append(sourceProcs, analysis.Procedure{Path: "genericPointerList", Type: "query", OutputType: list})
			gen := codegen.Prepare(&analysis.Result{Procedures: sourceProcs}, nil, nil)
			writeStaticContract(t, dir, mini, gen.Procs, gen.Defs, codegen.ZodOptions{})
		} else {
			r := trpcgo.NewRouter(trpcgo.WithZodMini(mini))
			t.Cleanup(func() { _ = r.Close() })
			validationcontract.RegisterCases(r, validationcontract.FixedArrayCases)
			trpcgo.MustQuery(r, "pointerEcho", func(_ context.Context, input validationcontract.FixedArrayNilPointers) (validationcontract.FixedArrayNilPointers, error) {
				return input, nil
			})
			trpcgo.MustQuery(r, "structEcho", func(_ context.Context, input validationcontract.FixedArrayNilStructs) (validationcontract.FixedArrayNilStructs, error) {
				return input, nil
			})
			trpcgo.MustQuery(r, "recursiveEcho", func(_ context.Context, input validationcontract.FixedArrayNilRecursive) (validationcontract.FixedArrayNilRecursive, error) {
				return input, nil
			})
			trpcgo.MustVoidQuery(r, "genericPointerList", func(context.Context) ([]validationcontract.ArrayClientBox[*int], error) { return nil, nil })
			trpcgo.MustQuery(r, "generic", func(_ context.Context, input validationcontract.ArrayClientGenericInput) (validationcontract.ArrayClientGenericInput, error) {
				return input, nil
			})
			trpcgo.MustQuery(r, "genericPointer", func(_ context.Context, input validationcontract.ArrayClientBox[*int]) (validationcontract.ArrayClientBox[*int], error) {
				return input, nil
			})
			trpcgo.MustQuery(r, "override", func(_ context.Context, input validationcontract.ArrayClientOverride) (validationcontract.ArrayClientOverride, error) {
				return input, nil
			})
			if err := r.GenerateZod(filepath.Join(dir, "schemas.ts")); err != nil {
				t.Fatal(err)
			}
			if err := r.GenerateTS(filepath.Join(dir, "trpc.ts")); err != nil {
				t.Fatal(err)
			}
		}
		writeTestSources(t, dir, map[string]string{"client.ts": fixedArrayClientAssertions + fixedArrayOutputAssertions})
		assertTypeScriptCompiles(t, dir, "trpc.ts", "schemas.ts", "client.ts")
	})
}

const fixedArrayClientAssertions = `
import type {RouterInputs, RouterOutputs, FixedArrayNilItem, FixedArrayNilNode, ArrayClientOverride} from './trpc';
import * as schemas from './schemas';
import type {Assert, Equal} from './assertions';
const generic = schemas.ArrayClientGenericInputSchema.parse({pointer:{values:[]},scalar:{values:[]},embedded:{values:[]}});
type GenericKeys = Assert<Equal<keyof typeof generic, 'pointer' | 'scalar' | 'embedded'>>;
type PointerElements = Assert<Equal<typeof generic.pointer.values, (number | null)[]>>;
type ScalarElements = Assert<Equal<typeof generic.scalar.values, number[]>>;
type EmbeddedElements = Assert<Equal<typeof generic.embedded.values, (number | null)[]>>;
type PointerInputElements = Assert<Equal<RouterInputs['genericPointer']['values'], (number | null)[]>>;
type PointerOutputElements = Assert<Equal<RouterOutputs['genericPointer']['values'], (number | null)[]>>;
type GenericInputElements = Assert<Equal<RouterInputs['generic']['pointer']['values'], (number | null)[]>>;
type GenericOutputElements = Assert<Equal<RouterOutputs['generic']['scalar']['values'], number[]>>;
// @ts-expect-error synthesized nullable elements retain their numeric type
const badPointer: typeof generic.pointer = {values:['wrong']};
// @ts-expect-error scalar instantiation cannot inherit pointer nullability
const badScalar: typeof generic.scalar = {values:[null]};
void [badPointer,badScalar];
const genericInput: RouterInputs['generic'] = generic;
const genericPointer: RouterInputs['genericPointer'] = generic.pointer;
const genericList: RouterOutputs['genericPointerList'] = [generic.pointer];
const genericOutput: RouterOutputs['genericPointer'] = generic.pointer;
const override: ArrayClientOverride = {values:'explicit override'};
// @ts-expect-error explicit tstype still wins over the Go array representation
const badOverride: ArrayClientOverride = {values:[null]};
void [genericInput,genericPointer,genericOutput,override,badOverride];
const pointers: RouterInputs['FixedArrayNilPointers'] = schemas.FixedArrayNilPointersSchema.parse({values:[]});
const maps: RouterInputs['FixedArrayNilMaps'] = schemas.FixedArrayNilMapsSchema.parse({values:[]});
const slices: RouterInputs['FixedArrayNilSlices'] = schemas.FixedArrayNilSlicesSchema.parse({values:[]});
const structs: RouterInputs['FixedArrayNilStructs'] = schemas.FixedArrayNilStructsSchema.parse({values:[]});
const recursive: RouterInputs['FixedArrayNilRecursive'] = schemas.FixedArrayNilRecursiveSchema.parse({values:[]});
const aliases: RouterInputs['FixedArrayNilAliases'] = schemas.FixedArrayNilAliasesSchema.parse({values:[]});
// @ts-expect-error ordinary structs retain their existing null policy
const ordinary: FixedArrayNilItem = {pointer:null,items:null,lookup:null,bytes:null};
// @ts-expect-error ordinary recursive nodes retain their existing null policy
const ordinaryNode: FixedArrayNilNode = {value:0,next:null,children:null};
void [pointers,maps,slices,structs,recursive,aliases,ordinary,ordinaryNode];
`

const fixedArrayOutputAssertions = `
const pointerOutput: RouterOutputs['pointerEcho'] = schemas.FixedArrayNilPointersSchema.parse({values:[]});
const structOutput: RouterOutputs['structEcho'] = schemas.FixedArrayNilStructsSchema.parse({values:[]});
const recursiveOutput: RouterOutputs['recursiveEcho'] = schemas.FixedArrayNilRecursiveSchema.parse({values:[]});
type PointerOutputExact = Assert<Equal<typeof pointerOutput, {values: (number | null)[]}>>;
type MapInputElements = Assert<Equal<RouterInputs['FixedArrayNilMaps']['values'][number], Record<string, number> | null>>;
type SliceInputElements = Assert<Equal<RouterInputs['FixedArrayNilSlices']['values'][number], number[] | null>>;
type StructPointer = Assert<Equal<typeof structOutput.values[number]['pointer'], number | null | undefined>>;
type RecursiveValue = Assert<Equal<typeof recursiveOutput.values[number]['value'], number>>;
void [pointerOutput,structOutput,recursiveOutput];
`

const arrayZeroContextAssertions = `
const omittedPointers = schemas.ArrayZeroOmitRequiredPointersSchema.parse({values:[]});
const nullable: number | null | undefined = omittedPointers.values?.[0];
const nilElement: NonNullable<typeof omittedPointers.values>[number] = null;
void nullable; void nilElement;
`

// Previously these inputs crashed before the writer could emit z.lazy. The
// contract now verifies successful TS compilation, inferred recursive types,
// and validation of nested values with both mappers and Zod variants.
func TestZodRecursiveContainerContract(t *testing.T) {
	script := `
import { RecursiveInputSchema, RecursiveMapSchema, RecursiveSliceSchema } from './schemas';
import type { RecursiveInput } from './trpc';
const input: RecursiveInput = { map: { a: { b: {} } }, slice: [[], [[]]], mutual: { a: [{ b: [] }] } };
const parsed = RecursiveInputSchema.parse(input);
const map: typeof parsed.map = { a: {} };
const slice: typeof parsed.slice = [[], [[]]];
// @ts-expect-error recursive map leaves must be maps
const wrongMap: typeof parsed.map = { a: 1 };
// @ts-expect-error recursive slice leaves must be arrays
const wrongSlice: typeof parsed.slice = [1];
for (const invalid of [
 {...input,map:{a:1}}, {...input,slice:[1]}, {...input,mutual:{a:[{b:1}]}}
]) { if (RecursiveInputSchema.safeParse(invalid).success) throw new Error('invalid recursive leaf accepted'); }
RecursiveMapSchema.parse({a:{b:{}}});
RecursiveSliceSchema.parse([[],[[]]]);
void [map,slice,wrongMap,wrongSlice];
`
	runTypeGraphContract(t, []string{"RecursiveInput", "RecursiveMap", "RecursiveSlice"}, script)
}

// Cycles through anonymous structs used to overflow the stack in both
// mappers. Types stay recursive, and validation follows Go at every depth:
// the root menu enters its elements and dive re-enters nested menus, while a
// struct field without dive never enters its collection.
func TestZodAnonymousRecursionContract(t *testing.T) {
	script := `
import { AnonymousMenuSchema, AnonymousRecursionInputSchema } from './schemas';
import type { AnonymousMenu, AnonymousRecursionInput } from './trpc';
const input: AnonymousRecursionInput = {
 menu: [{ label: 'a', children: [{ label: 'b', children: [] }] }],
 tree: { a: { size: 1, kids: { b: { size: 1, kids: {} } } } },
 chain: [{ next: [{}] }],
 list: [{ value: 1, next: [{ value: -128, next: [] }] }],
 link: { next: {} },
};
const parsed: AnonymousRecursionInput = AnonymousRecursionInputSchema.parse(input);
const menu: AnonymousMenu = AnonymousMenuSchema.parse(input.menu);
const label: string = menu[0]!.children[0]!.label;
// @ts-expect-error recursive elements keep their structure
const wrong: number = parsed.menu[0]!.children[0]!.label;
for (const invalid of [[{ label: '', children: [] }], [{ label: 'a', children: [{ label: '', children: [] }] }]]) {
 if (AnonymousMenuSchema.safeParse(invalid).success) throw new Error('menu element accepted: ' + JSON.stringify(invalid));
}
AnonymousRecursionInputSchema.parse({ ...input, menu: [{ label: '', children: [] }], tree: { a: { size: 0, kids: {} } } });
if (AnonymousRecursionInputSchema.safeParse({ ...input, list: [{ value: 1, next: [{ value: 128, next: [] }] }] }).success) throw new Error('int8 overflow accepted in a recursive list');
void [label, wrong];
`
	runTypeGraphContract(t, []string{"AnonymousMenu", "AnonymousRecursionInput"}, script)
}

func TestZodConcreteGenericContract(t *testing.T) {
	script := `
import { GenericInputSchema } from './schemas';
import type { GenericInput } from './trpc';
const input: GenericInput = { text: { value:'ok', values:['yes'], next:{value:'nested',values:[]} }, number:{value:1,values:[2]}, wide:{value:128,values:[256]}, items:[1,2] };
const parsed = GenericInputSchema.parse(input);
const text: string = parsed.text.next!.value;
const number: number = parsed.number.value;
// @ts-expect-error string generic retains its concrete type
const wrong: number = parsed.text.value;
for (const invalid of [
 {...input,text:{value:1,values:[]}}, {...input,text:{value:'ok',values:[1]}},
 {...input,text:{value:'ok',values:[],next:{value:1,values:[]}}},
 {...input,number:{value:128,values:[]}}, {...input,number:{value:1,values:[128]}},
 {...input,number:{value:0,values:[]}}, {...input,wide:{value:32768,values:[]}}, {...input,items:[0]}, {...input,items:[128]}
]) { if (GenericInputSchema.safeParse(invalid).success) throw new Error('invalid concrete generic value accepted: '+JSON.stringify(invalid)); }
void [text,number,wrong];
`
	runTypeGraphContract(t, []string{"GenericInput"}, script)
}

func TestZodAnonymousMetadataContract(t *testing.T) {
	script := `
import { InlineInputSchema } from './schemas';
const input = { optionalName:'',details:{count:1,name:'ok',low:1,high:2},items:[{name:'ok'}],map:{one:{count:1}} };
const parsed = InlineInputSchema.parse({...input,next:input});
const name: string = parsed.next!.details.name;
// Unvalidated fields keep their TypeScript type in recursive positions too.
const secret: string | undefined = parsed.next!.details.secret;
// @ts-expect-error the field is a string, not an arbitrary value
const secretNumber: number | undefined = parsed.next!.details.secret;
if ('secret' in parsed.next!.details) throw new Error('absent recursive unvalidated field was materialized');
const opaque = { arbitrary: true };
const withSecret = InlineInputSchema.parse({ ...input, next: { ...input, details: { ...input.details, secret: opaque } } });
if ((withSecret.next!.details.secret as unknown) !== opaque) throw new Error('recursive unvalidated field was not preserved');
for (const invalid of [
 {...input,details:{...input.details,count:0}}, {...input,details:{...input.details,count:128}},
 {...input,details:{...input.details,name:'x'}}, {...input,details:{...input.details,high:0}},
 {...input,items:[{name:'x'}]}, {...input,map:{one:{count:0}}},
 {...input,next:{...input,details:{...input.details,name:'x'}}}
]) { if(InlineInputSchema.safeParse(invalid).success) throw new Error('anonymous field metadata lost: '+JSON.stringify(invalid)); }
void [name,secret];
`
	runTypeGraphContract(t, []string{"InlineInput"}, script)
}

func TestZodEnumReferencesAndSparseKeysContract(t *testing.T) {
	forEachStyle(t, func(t *testing.T, mini bool) {
		pkg := typeGraphPackage(t)
		mapper := typemap.NewMapper(map[string]typemap.TypeMeta{
			pkg.Path() + ".State":    {ConstValues: []string{`"ready"`, `"done"`}},
			pkg.Path() + ".Level":    {ConstValues: []string{"1", "2"}},
			pkg.Path() + ".Fraction": {ConstValues: []string{"0.1", "0.5"}},
			pkg.Path() + ".Ratio":    {ConstValues: []string{"0.125", "1"}},
		})
		input := mapper.Resolve(mapper.Convert(pkg.Scope().Lookup("EnumInput").Type()))
		procs := []codegen.ProcEntry{{Path: "enum", ProcType: "query", InputTS: input, OutputTS: "string"}}
		dir := t.TempDir()
		symlinkNodeModules(t, dir)
		writeStaticContract(t, dir, mini, procs, mapper.Defs(), codegen.ZodOptions{})
		script := `
import { EnumInputSchema } from './schemas';
import type { EnumInput } from './trpc';
const input: EnumInput = {state:'ready',sparse:{ready:1},numeric:{1:'ok'}};
EnumInputSchema.parse(input);
EnumInputSchema.parse({state:'done',sparse:{},numeric:{}});
for (const fraction of ['0.1', '0.10000000001', '0x1p-1']) {
 const wire = {...input, fraction, ratio:'0x1p0'};
 const parsed = EnumInputSchema.parse(wire);
 if (parsed.fraction !== fraction || parsed.ratio !== wire.ratio) throw new Error('quoted enum lost its wire value');
}
const open: EnumInput = {state:'unknown',sparse:{other:1},numeric:{3:'ok'}};
for (const value of [
 open, {...input,state:'unknown'}, {...input,sparse:{unknown:1}},
 {...input,numeric:{3:'ok'}}, {...input,fraction:'0.2'}, {...input,ratio:'0x1p1'}
]) { EnumInputSchema.parse(value); }
for (const value of [
 {...input,sparse:{ready:128}}, {...input,sparse:{other:-129}},
 {...input,fraction:'.1'}, {...input,ratio:'1e999'}
]) { if(EnumInputSchema.safeParse(value).success) throw new Error('underlying scalar constraint lost: '+JSON.stringify(value)); }
void open;
`
		writeTestSources(t, dir, map[string]string{"validate.ts": script})
		assertTypeScriptCompiles(t, dir, "trpc.ts", "schemas.ts", "validate.ts")
		runTypeScript(t, dir, "validate.ts")
	})
}

// A named constant type's raw JSON check follows its Go kind: a boolean
// constant type accepts JSON booleans, and an int8 one keeps its bounds.
func TestZodNamedConstantWireKinds(t *testing.T) {
	forEachStyle(t, func(t *testing.T, mini bool) {
		pkg := typeGraphPackage(t)
		mapper := typemap.NewMapper(map[string]typemap.TypeMeta{
			pkg.Path() + ".Flag":  {ConstValues: []string{"true", "false"}},
			pkg.Path() + ".Level": {ConstValues: []string{"1", "2"}},
		})
		input := mapper.Resolve(mapper.Convert(pkg.Scope().Lookup("FlagInput").Type()))
		procs := []codegen.ProcEntry{{Path: "flag", ProcType: "query", InputTS: input, OutputTS: "string"}}
		dir := t.TempDir()
		symlinkNodeModules(t, dir)
		writeStaticContract(t, dir, mini, procs, mapper.Defs(), codegen.ZodOptions{})
		script := `
import { FlagInputSchema, FlagSchema, decodeGoJSON } from './schemas';
for (const raw of ['{"flag":true,"level":1}', '{"flag":false,"level":-128}']) {
 if (!FlagInputSchema.safeParse(JSON.parse(raw)).success) throw new Error('named boolean rejected: ' + raw);
 if (!decodeGoJSON(FlagInputSchema, raw).success) throw new Error('raw named boolean rejected: ' + raw);
}
if (!decodeGoJSON(FlagSchema, 'true').success) throw new Error('raw named boolean schema rejected true');
for (const raw of ['{"flag":1,"level":1}', '{"flag":"true","level":1}', '{"flag":null,"level":128}', '{"flag":true,"level":1.5}', 'null']) {
 if (raw === 'null' ? decodeGoJSON(FlagSchema, '1').success : decodeGoJSON(FlagInputSchema, raw).success) throw new Error('wrong wire kind accepted: ' + raw);
}
`
		writeTestSources(t, dir, map[string]string{"validate.ts": script})
		assertTypeScriptCompiles(t, dir, "trpc.ts", "schemas.ts", "validate.ts")
		runTypeScript(t, dir, "validate.ts")
	})
}

func TestZodGenericProcedureInputIdentity(t *testing.T) {
	forEachStyle(t, func(t *testing.T, mini bool) {
		pkg := typeGraphPackage(t)
		mapper := typemap.NewMapper(nil)
		var procs []codegen.ProcEntry
		for _, name := range []string{"NarrowInput", "WideInput"} {
			typ := pkg.Scope().Lookup(name).Type()
			procs = append(procs, codegen.ProcEntry{Path: name, ProcType: "query", InputTS: mapper.Convert(typ), InputZod: mapper.ConvertZod(typ), OutputTS: "string"})
		}
		for i := range procs {
			procs[i].InputTS = mapper.Resolve(procs[i].InputTS)
			procs[i].InputZod = mapper.Resolve(procs[i].InputZod)
		}
		if procs[0].InputTS != procs[1].InputTS || procs[0].InputZod == procs[1].InputZod {
			t.Fatalf("Go identity must survive TypeScript erasure: %#v", procs)
		}
		var out bytes.Buffer
		if err := codegen.WriteZodSchemas(&out, procs, mapper.Defs(), zodStyle(mini), codegen.ZodOptions{}); err != nil {
			t.Fatal(err)
		}
		dir := t.TempDir()
		symlinkNodeModules(t, dir)
		script := "import {" + procs[0].InputZod + "Schema as narrow," + procs[1].InputZod + "Schema as wide} from './schemas';\n" + `
const input = {value:128,values:[256],next:{value:512,values:[]}};
wide.parse(input);
if(narrow.safeParse(input).success) throw new Error('narrow numeric generic selected wide schema');
if(wide.safeParse({...input,value:32768}).success) throw new Error('wide generic ignored int16 bounds');
`
		for name, data := range map[string][]byte{"schemas.ts": out.Bytes(), "validate.ts": []byte(script)} {
			if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
				t.Fatal(err)
			}
		}
		assertTypeScriptCompiles(t, dir, "schemas.ts", "validate.ts")
		runTypeScript(t, dir, "validate.ts")
	})
}

func TestZodGenericInheritanceIdentity(t *testing.T) {
	script := `
import {GenericExtendedInputSchema} from './schemas';
import type {GenericExtendedInput} from './trpc';
const input: GenericExtendedInput = {narrow:{value:1,label:'narrow'},wide:{value:128,label:'wide'}};
GenericExtendedInputSchema.parse(input);
for(const invalid of [{...input,narrow:{value:128,label:'bad'}},{...input,wide:{value:32768,label:'bad'}}]) {
 if(GenericExtendedInputSchema.safeParse(invalid).success) throw new Error('generic inheritance lost concrete Go kind');
}
`
	runTypeGraphContract(t, []string{"GenericExtendedInput"}, script)
}

func TestZodExplicitRequiredOverridesOmission(t *testing.T) {
	script := `
import {RequiredOmitInputSchema} from './schemas';
import type {RequiredOmitInput} from './trpc';
const input: RequiredOmitInput = {text:'',pointer:'ok@example.com',map:{},details:{value:''}};
const parsed = RequiredOmitInputSchema.parse({...input,next:input});
const text: string = parsed.text;
const nestedText: string = parsed.next!.text;
const inlineText: string = parsed.next!.details.value;
// @ts-expect-error explicit required fields remain mandatory in TypeScript
const missing: RequiredOmitInput = {pointer:'ok@example.com',map:{},details:{value:''}};
for (const key of ['text','pointer','map']) {
 const invalid: Record<string,unknown> = {...input}; delete invalid[key];
 if(RequiredOmitInputSchema.safeParse(invalid).success) throw new Error('tstype required lost to omission: '+key);
}
for(const invalid of [{...input,details:{}},{...input,pointer:''},{...input,text:'x'},{...input,next:{pointer:'ok@example.com',map:{},details:{value:''}}}]) {
 if(RequiredOmitInputSchema.safeParse(invalid).success) throw new Error('explicit required or supplied-value validation lost');
}
void [text,nestedText,inlineText,missing];
`
	runTypeGraphContract(t, []string{"RequiredOmitInput"}, script)
}

// The two Go instances share GenericInlineNode<number> in TypeScript. Their
// anonymous recursive fields must still select their own lazy schema and
// retain their distinct integer bounds at every depth.
func TestZodGenericAnonymousRecursionIdentity(t *testing.T) {
	script := `
import {GenericInlineRecursiveInputSchema} from './schemas';
import type {GenericInlineRecursiveInput} from './trpc';
const input: GenericInlineRecursiveInput = {
 narrow:{value:127,inline:{next:{value:-128,inline:{next:{value:0,inline:{}}}}},children:[{next:{value:-128,inline:{}}}],byName:{one:{next:{value:127,inline:{}}}}},
 wide:{value:32767,inline:{next:{value:-32768,inline:{next:{value:128,inline:{}}}}},children:[{next:{value:32767,inline:{}}}],byName:{one:{next:{value:-32768,inline:{}}}}},
};
const parsed = GenericInlineRecursiveInputSchema.parse(input);
const narrowValue: number = parsed.narrow.inline.next!.inline.next!.value;
const wideValue: number = parsed.wide.inline.next!.inline.next!.value;
const childValue: number = parsed.narrow.children![0]!.next!.value;
const mapValue: number = parsed.wide.byName!.one!.next!.value;
// @ts-expect-error recursive generic values retain their number type
const wrong: string = parsed.wide.inline.next!.value;
for (const invalid of [
 {...input,narrow:{value:128,inline:{}}},
 {...input,wide:{value:32768,inline:{}}},
 {...input,narrow:{value:1,inline:{next:{value:128,inline:{}}}}},
 {...input,narrow:{value:1,inline:{next:{value:1,inline:{next:{value:-129,inline:{}}}}}}},
 {...input,wide:{value:1,inline:{next:{value:32768,inline:{}}}}},
 {...input,wide:{value:1,inline:{next:{value:1,inline:{next:{value:-32769,inline:{}}}}}}},
 {...input,wide:{value:1,inline:{next:{value:'invalid',inline:{}}}}},
 {...input,narrow:{value:1,inline:{},children:[{next:{value:128,inline:{}}}]}},
 {...input,wide:{value:1,inline:{},children:[{next:{value:32768,inline:{}}}]}},
 {...input,narrow:{value:1,inline:{},byName:{one:{next:{value:-129,inline:{}}}}}},
 {...input,wide:{value:1,inline:{},byName:{one:{next:{value:-32769,inline:{}}}}}},
]) {
 if (GenericInlineRecursiveInputSchema.safeParse(invalid).success) {
  throw new Error('anonymous recursive generic lost its concrete Go type: '+JSON.stringify(invalid));
 }
}
void [narrowValue,wideValue,childValue,mapValue,wrong];
`
	runTypeGraphContract(t, []string{"GenericInlineRecursiveInput"}, script)
}

func TestZodValidationDiagnosticsAgreeAcrossMappers(t *testing.T) {
	forEachStyle(t, func(t *testing.T, mini bool) {
		pkg := typeGraphPackage(t)
		mapper := typemap.NewMapper(nil)
		input := mapper.Resolve(mapper.Convert(pkg.Scope().Lookup("DiagnosticInput").Type()))
		var static bytes.Buffer
		if err := codegen.WriteZodSchemas(&static, []codegen.ProcEntry{{InputTS: input}}, mapper.Defs(), zodStyle(mini), codegen.ZodOptions{}); err != nil {
			t.Fatal(err)
		}
		r := trpcgo.NewRouter(trpcgo.WithZodMini(mini))
		t.Cleanup(func() { _ = r.Close() })
		trpcgo.MustQuery(r, "diagnostic", func(context.Context, typegraph.DiagnosticInput) (string, error) { return "", nil })
		path := filepath.Join(t.TempDir(), "schemas.ts")
		if err := r.GenerateZod(path); err != nil {
			t.Fatal(err)
		}
		reflection, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if string(reflection) != static.String() {
			t.Fatalf("mapper diagnostics differ\nreflection:\n%s\nstatic:\n%s", reflection, static.String())
		}
		output := static.String()
		if strings.Count(output, "invalid zod params: email") != 1 {
			t.Fatalf("email on int must have one invalid-kind diagnostic; valid map keys and nested values must not be flagged:\n%s", output)
		}
		for _, name := range []string{"validMap:", "nestedMaps:"} {
			found := false
			for line := range strings.SplitSeq(output, "\n") {
				if strings.Contains(line, name) {
					found = true
					if strings.Contains(line, "invalid zod params") {
						t.Fatalf("valid typed map scope gained a false diagnostic: %s", line)
					}
				}
			}
			if !found {
				t.Fatalf("missing field %s:\n%s", name, output)
			}
		}
		// The typed map scopes still validate their keys and values.
		checkGeneratedZodFile(t, r.GenerateZod, `
import { DiagnosticInputSchema as schema } from './schemas';
const valid = { invalid: 1, validMap: { 'a@b.co': 1 }, nestedMaps: [{ 'a@b.co': 1 }] };
schema.parse(valid);
for (const invalid of [{ ...valid, validMap: { bad: 1 } }, { ...valid, validMap: { 'a@b.co': 0 } }, { ...valid, nestedMaps: [{ bad: 1 }] }]) {
  if (schema.safeParse(invalid).success) throw new Error('map scope lost validation: ' + JSON.stringify(invalid));
}
`)
	})
}
