package trpcgo_test

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/befabri/trpcgo/internal/codegen"
	"github.com/befabri/trpcgo/internal/typemap"
	"github.com/befabri/trpcgo/testdata/typegraph"
	"github.com/befabri/trpcgo/testdata/validationcontract"
)

func TestZodIntegerMapDecodingContract(t *testing.T) {
	runTypeGraphContract(t, []string{"IntegerMapInput"}, integerMapContractScript)
}

const integerMapContractScript = `
import type * as z from 'zod';
import {IntegerMapInputSchema, RecursiveIntegerMapSchema, parseGoJSON} from './schemas';
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

const decoded = IntegerMapInputSchema.parse(parseGoJSON('{"values":{"01":7,"1":2},"nested":{},"recursive":{}}'));
equal(decoded.values, {1:2}, 'source order was not used for colliding aliases');
const duplicate = IntegerMapInputSchema.parse(parseGoJSON('{"values":{"1":1,"\\u0031":2,"1":3},"nested":{},"recursive":{}}'));
equal(duplicate.values, {1:3}, 'exact or escaped duplicate keys lost source order');
for (const raw of [
 '{"values":{"01":"bad","1":2},"nested":{},"recursive":{}}',
 '{"values":{"01":128,"1":2},"nested":{},"recursive":{}}',
 '{"values":{"128":1,"1":2},"nested":{},"recursive":{}}',
]) assert(!IntegerMapInputSchema.safeParse(parseGoJSON(raw)).success, 'overwritten decoder failure was ignored: '+raw);

const stale = parseGoJSON('{"values":{"01":1,"1":2},"nested":{},"recursive":{}}') as {values: {[key: string]: number}};
stale.values['01'] = 3;
assert(!IntegerMapInputSchema.safeParse(stale).success, 'ambiguous object reused stale raw JSON metadata');
const changedDistinct = parseGoJSON('{"values":{"01":1,"2":2},"nested":{},"recursive":{}}') as {values: {[key: string]: number}};
changedDistinct.values['01'] = 3;
equal(IntegerMapInputSchema.parse(changedDistinct).values, {1:3,2:2}, 'unambiguous mutation did not use current values');

const nested = IntegerMapInputSchema.parse(parseGoJSON('{"values":{},"nested":{"01":{"01":1,"1":2},"1":{"002":3}},"recursive":{"01":{"01":{},"1":{}},"1":{"2":{}}}}'));
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
		writeStaticContract(t, dir, mini, procs, mapper.Defs())
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
  assertMap(result.error.issues.some(issue => issue.message.includes('parseGoJSON') && issue.path[0] === 'values'), 'ambiguous aliases need an actionable field error');
}
const distinct = schemas.MapRoundtripSchema.parse({ values: { '01': 7, '2': 2 } });
assertMap(JSON.stringify(distinct) === '{"values":{"1":7,"2":2}}', 'unambiguous object did not normalize Go keys');
const raw = schemas.parseGoJSON('{"values":{"01":7,"1":2}}');
assertMap(JSON.stringify(schemas.MapRoundtripSchema.parse(raw)) === '{"values":{"1":2}}', 'raw source order was lost');
assertMap(JSON.stringify(schemas.MapRoundtripSchema.parse(raw)) === '{"values":{"1":2}}', 'parsing mutated the source or consumed its metadata');
for (const values of [null, [], 'wrong', 1, { 'invalid': 1 }, { '1': 'wrong' }]) {
  assertMap(!schemas.MapRoundtripSchema.safeParse({ values }).success, 'normalization bypassed the map schema');
}
`
