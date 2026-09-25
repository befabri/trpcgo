package codegen

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/befabri/trpcgo/internal/typemap"
)

func TestGoJSONHelpersRuntime(t *testing.T) {
	var helper bytes.Buffer
	writeZodJSONHelpers(newErrWriter(&helper))
	// decodeGoJSON is the module's only raw JSON entry point; the parser it
	// uses is private, so its output cannot bypass Go's decoding.
	if strings.Contains(helper.String(), "export ") {
		t.Fatal("the JSON helpers must not export anything")
	}
	runJSONHelperProgram(t, map[string]string{
		"main.ts":  helper.String() + goJSONRuntimeAssertions,
		"other.ts": helper.String() + "export { $goParseJSON as parseFromOtherModule };\n",
	})
}

func runJSONHelperProgram(t *testing.T, sources map[string]string) {
	t.Helper()
	compiler := os.Getenv("TRPCGO_TSC")
	if compiler == "" {
		compiler = filepath.Join("..", "..", "testdata", "zodruntime", "node_modules", "typescript", "bin", "tsc")
	} else if !filepath.IsAbs(compiler) {
		compiler = filepath.Join("..", "..", compiler)
	}
	compiler, err := filepath.Abs(compiler)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(compiler); err != nil {
		if os.Getenv("CI") != "" || os.Getenv("TRPCGO_TSC") != "" || !os.IsNotExist(err) {
			t.Fatalf("TypeScript compiler unavailable: %v", err)
		}
		t.Skip("run npm ci --prefix testdata/zodruntime for JSON helper contracts")
	}
	dir := t.TempDir()
	modules, err := filepath.Abs(filepath.Join("..", "..", "testdata", "zodruntime", "node_modules"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(modules, filepath.Join(dir, "node_modules")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"type":"module"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	var files []string
	for name, source := range sources {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(source), 0o644); err != nil {
			t.Fatal(err)
		}
		files = append(files, name)
	}
	slices.Sort(files)
	args := append([]string{"--strict", "--skipLibCheck", "--target", "ES2022", "--module", "ES2022", "--moduleResolution", "bundler"}, files...)
	compile := exec.CommandContext(t.Context(), compiler, args...)
	compile.Dir = dir
	if output, err := compile.CombinedOutput(); err != nil {
		t.Fatalf("JSON helper does not compile: %v\n%s", err, output)
	}
	run := exec.CommandContext(t.Context(), "node", "main.js")
	run.Dir = dir
	if output, err := run.CombinedOutput(); err != nil {
		t.Fatalf("JSON helper runtime contract: %v\n%s", err, output)
	}
}

const goJSONRuntimeAssertions = `
import {parseFromOtherModule} from './other.js';

function assert(condition: unknown, message: string): asserts condition {
  if (!condition) throw new Error(message);
}
function object(value: unknown): { [key: string]: unknown } {
  assert(value !== null && typeof value === "object" && !Array.isArray(value), "expected an object");
  return value as { [key: string]: unknown };
}
function equal(actual: unknown, expected: unknown, message: string): void {
  assert(JSON.stringify(actual) === JSON.stringify(expected), message + ": " + JSON.stringify(actual));
}

const duplicate = object($goParseJSON('{"1":"first","\\u0031":{"nested":1,"nested":2},"1":"last"}'));
equal(duplicate, {1:"last"}, "native object value changed");
const entries = $goJSONEntries(duplicate);
assert(entries?.length === 3, "duplicate or escaped property name lost");
equal(entries.map(entry => entry[0]), ["1","1","1"], "escaped names were not decoded");
assert(entries[0]![1] === "first" && entries[2]![1] === "last", "duplicate order changed");
const overwritten = object(entries[1]![1]);
equal(overwritten, {nested:2}, "overwritten object was not retained");
equal($goJSONEntries(overwritten), [["nested",1],["nested",2]], "nested duplicates lost");

const reordered = object($goParseJSON('{"01":"a","1":"b","+1":"c","2":"d","0":"e"}'));
equal(Object.keys(reordered), ["0","1","2","01","+1"], "native property enumeration changed");
equal($goJSONEntries(reordered)?.map(entry => entry[0]), ["01","1","+1","2","0"], "source order changed");
// Only this module's decodeGoJSON output may carry source order: another
// module's parser output and objects built with the former global symbol are
// ordinary objects.
const foreign = object(parseFromOtherModule('{"x":1,"x":2}'));
assert($goJSONEntries(foreign) === undefined, "another module's metadata was trusted");
const forged = { x: 2 };
const pair = (name: string, value: unknown) => Object.freeze([name, value] as const);
Object.defineProperty(forged, Symbol.for("trpcgo.go-json.entries.v1"), {
  value: Object.freeze({ entries: Object.freeze([pair("x", 1), pair("x", 2)]), snapshot: Object.freeze([pair("x", 2)]) }),
});
assert($goJSONEntries(forged) === undefined, "an ordinary object forged source metadata");

const array = $goParseJSON('[1,{"x":false,"x":true},null,"9223372036854775807"]');
assert(Array.isArray(array), "array became an object");
equal(array, [1,{x:true},null,"9223372036854775807"], "array/scalar wire values changed");
equal($goJSONEntries(object(array[1])), [["x",false],["x",true]], "array child duplicates lost");
equal($goJSONEntries(object($goParseJSON('{}'))), [], "empty object metadata missing");
equal($goParseJSON('[]'), [], "empty array changed");
for (const raw of ['null','true','false','0','-0','1.25','1e3','1e400','"001"','"9223372036854775807"','"emoji: \\ud83d\\ude00"']) {
  assert(Object.is($goParseJSON(raw), JSON.parse(raw)), "scalar representation changed: " + raw);
}
const escaped = { 'quote"slash\\': 'brace } ], quote" and slash\\ and newline\n and nul\u0000' };
equal($goParseJSON(JSON.stringify(escaped)), escaped, "string scanner mishandled escapes");
equal($goParseJSON(' \n\t { "a" : [ 1 , 2 ] } \r '), {a:[1,2]}, "whitespace changed parsing");

const prototype = object($goParseJSON('{"__proto__":{"polluted":true},"constructor":"own","__proto__":{"safe":true}}'));
assert(Object.getPrototypeOf(prototype) === Object.prototype, "__proto__ changed object prototype");
assert(!Object.hasOwn(Object.prototype, "polluted"), "prototype was polluted");
equal(Object.getOwnPropertyDescriptor(prototype,"__proto__")?.value, {safe:true}, "__proto__ own value lost");
assert($goJSONEntries(prototype)?.length === 3, "__proto__ duplicate lost");

const modified = object($goParseJSON('{"x":1,"x":2}'));
const metadataDescriptor = Object.getOwnPropertyDescriptor(modified, $goJSONMetadataKey);
assert(metadataDescriptor && !metadataDescriptor.enumerable && !metadataDescriptor.writable && !metadataDescriptor.configurable, "metadata must be hidden and immutable");
assert(Object.isFrozen(entries) && Object.isFrozen(entries[0]), "retained entries must be immutable");
modified.x = 3;
assert($goJSONEntries(modified) === undefined, "changed value retained stale entries");
const deleted = object($goParseJSON('{"x":1}'));
delete deleted.x;
assert($goJSONEntries(deleted) === undefined, "deleted key retained stale entries");
const added = object($goParseJSON('{"x":1}'));
added.y = 2;
assert($goJSONEntries(added) === undefined, "added key retained stale entries");
const accessor = object($goParseJSON('{"x":1}'));
Object.defineProperty(accessor, "x", {get() { throw new Error("must not invoke object getters"); }, enumerable:true});
assert($goJSONEntries(accessor) === undefined, "accessor retained stale entries");
const nested = object($goParseJSON('{"nested":{"1":1,"01":2}}'));
const nestedMap = object(nested.nested);
nestedMap["1"] = 4;
assert($goJSONEntries(nestedMap) === undefined, "nested mutation retained stale child entries");
assert($goJSONEntries(nested) !== undefined, "unchanged parent lost its direct-property snapshot");

for (const malformed of [null,1,{},Object.freeze({entries:[],snapshot:[]}),Object.freeze({entries:Object.freeze(["bad"]),snapshot:Object.freeze([])}),Object.freeze({get entries() { throw new Error("bad metadata getter"); }})]) {
  const ordinary = {x:1};
  Object.defineProperty(ordinary,$goJSONMetadataKey,{value:malformed});
  assert($goJSONEntries(ordinary) === undefined, "malformed symbol metadata was accepted");
}
assert($goJSONEntries(new Proxy({}, {getOwnPropertyDescriptor() { throw new Error("hostile proxy"); }})) === undefined, "metadata lookup threw for a proxy");
assert($goJSONEntries({x:1}) === undefined, "ordinary objects gained source metadata");

for (const raw of ['', '{', '[', '{"a":1,}', '[1,]', '{"a" 1}', '01', '+1', 'NaN', 'true false', '"\\x41"', '"line\nbreak"']) {
  let failed = false;
  try { $goParseJSON(raw); } catch { failed = true; }
  assert(failed, "invalid JSON was accepted: " + raw);
}
const deep = '{"child":'.repeat(256) + '0' + '}'.repeat(256);
equal($goParseJSON(deep), JSON.parse(deep), "nested scanner lost an object level");
// encoding/json accepts 10000 nested arrays and objects and rejects 10001.
// The scanner reads that deep without recursion.
let level: unknown = $goParseJSON('[{"a":'.repeat(5000) + '0' + '}]'.repeat(5000));
for (let depth = 0; depth < 5000; depth++) {
  assert(Array.isArray(level) && level.length === 1, "deep array level lost at " + depth);
  const inner = object(level[0]);
  assert($goJSONEntries(inner)?.length === 1, "deep object lost its entries at " + depth);
  level = inner.a;
}
assert(level === 0, "deepest value lost");
let tooDeep: unknown;
try { $goParseJSON('['.repeat(10001) + ']'.repeat(10001)); } catch (error) { tooDeep = error; }
assert(tooDeep instanceof SyntaxError, "JSON nested deeper than Go allows was not a syntax error");
const brackets = JSON.stringify({ text: '['.repeat(10001) + '"\\"{' });
equal($goParseJSON(brackets), JSON.parse(brackets), "brackets inside strings counted as nesting");
`

func TestGoJSONMergeRuntime(t *testing.T) {
	for _, style := range []typemap.ZodStyle{typemap.ZodStandard, typemap.ZodMini} {
		var source bytes.Buffer
		ew := newErrWriter(&source)
		if style == typemap.ZodMini {
			ew.println(`import * as z from "zod/mini";`)
		} else {
			ew.println(`import {z} from "zod";`)
		}
		writeZodJSONHelpers(ew)
		writeZodMergeHelpers(ew, nil, nil, style)
		writeZodWireChecks(ew, nil, nil, style, newZodSchemaChecks(), false)
		ew.println(goJSONMergeAssertions)
		runJSONHelperProgram(t, map[string]string{"main.ts": source.String()})
	}
}

// Check the field-name compatibility inputs against Go itself. The JS helper
// assertions below exercise the same cases rather than duplicating a regex.
func TestGoJSONFieldNameOracle(t *testing.T) {
	var result struct {
		Kelvin int `json:"K"`
		LongS  int `json:"S"`
		Sigma  int `json:"Σ"`
		SharpS int `json:"ß"`
		Upper  int `json:"X"`
		Lower  int `json:"x"`
	}
	if err := json.Unmarshal([]byte(`{"K":1,"ſ":2,"ς":3,"ẞ":4,"X":5,"x":6,"ss":7}`), &result); err != nil {
		t.Fatal(err)
	}
	if result.Kelvin != 1 || result.LongS != 2 || result.Sigma != 3 || result.SharpS != 4 || result.Upper != 5 || result.Lower != 6 {
		t.Fatalf("unexpected Go field folding: %+v", result)
	}
}

// The map-entry assertions in goJSONMergeAssertions rely on these results.
func TestGoJSONMapEntryOracle(t *testing.T) {
	type entry struct{ A, B int }
	var result struct {
		Values map[string]entry `json:"values"`
	}
	if err := json.Unmarshal([]byte(`{"values":{"k":{"A":1},"k":{"b":2,"B":3}},"values":{"j":{"a":4}}}`), &result); err != nil {
		t.Fatal(err)
	}
	want := map[string]entry{"k": {B: 3}, "j": {A: 4}}
	if !reflect.DeepEqual(result.Values, want) {
		t.Fatalf("unexpected map entry decoding: %+v", result.Values)
	}
}

func TestGoJSONRepeatedSliceOracle(t *testing.T) {
	var structs struct {
		Items []struct{ A, B int } `json:"items"`
	}
	if err := json.Unmarshal([]byte(`{"items":[{"A":1,"B":2}],"items":[{"A":3}]}`), &structs); err != nil {
		t.Fatal(err)
	}
	if len(structs.Items) != 1 || structs.Items[0].A != 3 || structs.Items[0].B != 2 {
		t.Fatalf("unexpected struct element reuse: %+v", structs)
	}
	for _, tc := range []struct {
		name, raw string
		want      []int
	}{
		{"truncate then expand", `{"items":[1,2],"items":[null],"items":[null,null]}`, []int{1, 2}},
		{"empty clears storage", `{"items":[1,2],"items":[],"items":[null,null]}`, []int{0, 0}},
		{"null clears storage", `{"items":[1,2],"items":null,"items":[null,null]}`, []int{0, 0}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var integers struct {
				Items []int `json:"items"`
			}
			if err := json.Unmarshal([]byte(tc.raw), &integers); err != nil {
				t.Fatal(err)
			}
			if len(integers.Items) != len(tc.want) {
				t.Fatalf("unexpected length: %+v", integers)
			}
			for i := range tc.want {
				if integers.Items[i] != tc.want[i] {
					t.Fatalf("unexpected slice storage reuse: %+v", integers)
				}
			}
		})
	}
}

const goJSONMergeAssertions = `
function assert(condition: unknown, message: string): asserts condition {
  if (!condition) throw new Error(message);
}
function equal(actual: unknown, expected: unknown, message: string): void {
  assert(JSON.stringify(actual) === JSON.stringify(expected), message + ': ' + JSON.stringify(actual));
}
const scalar: $GoJSONDecoder = (value, previous) => value === null && previous !== undefined ? previous : value;
const quotedPointer: $GoJSONDecoder = (value) => value;
const details: readonly (readonly [string,$GoJSONDecoder])[] = [['values',$goMergeMap],['label',scalar]];
const fields: readonly (readonly [string,$GoJSONDecoder])[] = [
 ['details',(value,previous) => value === null && previous !== undefined ? previous : $goMergeObject(value,previous,details)],
 ['quotedPointer',quotedPointer],
];
const raw = $goParseJSON('{"details":{"values":{"01":1},"label":"kept"},"DETAILS":{"values":{"2":2},"label":null},"quotedPointer":"1","quotedPointer":"null"}');
const merged = $goMergeObject(raw,undefined,fields) as {details:{values:object,label:string},quotedPointer:string};
equal(merged.details,{values:{'2':2,'01':1},label:'kept'},'repeated struct/map fields were not merged');
equal($goJSONEntries(merged.details.values),[['01',1],['2',2]],'map entry history lost during field merge');
assert(merged.quotedPointer === 'null','quoted nil changed its wire representation');
equal(raw,{details:{values:{'01':1},label:'kept'},DETAILS:{values:{'2':2},label:null},quotedPointer:'null'},'merging mutated the raw input');

const cleared = $goMergeObject($goParseJSON('{"values":{"1":1},"values":null,"values":{"2":2}}'),undefined,[['values',$goMergeMap]]) as {values:object};
equal(cleared.values,{2:2},'null did not clear the previous map');
equal($goJSONEntries(cleared.values),[['2',2]],'cleared map retained previous entries');
const mapValues = $goMergeMap($goParseJSON('{"1":{"a":1},"1":{"b":2}}'),undefined) as object;
equal(mapValues,{1:{b:2}},'map values were merged instead of replaced');
assert($goJSONEntries(mapValues)?.length === 2,'map replacement dropped source entries');
// Each entry value decodes into a fresh value: it never merges with an earlier
// entry for its key, but its own repeated and case-folded fields still merge.
const entryFields: readonly (readonly [string,$GoJSONDecoder])[] = [['A',scalar],['B',scalar]];
const decodedEntries = $goMergeMap($goParseJSON('{"k":{"A":1},"k":{"b":2,"B":3}}'),undefined,item => $goMergeObject(item,undefined,entryFields)) as object;
equal(decodedEntries,{k:{B:3}},'map entry merged with an earlier entry or skipped its own decoding');
equal($goJSONEntries(decodedEntries),[['k',{A:1}],['k',{B:3}]],'decoded entries lost their source order');
const appended = $goMergeMap($goParseJSON('{"j":{"a":4}}'),decodedEntries,item => $goMergeObject(item,undefined,entryFields)) as object;
equal(appended,{k:{B:3},j:{A:4}},'repeated map field lost earlier entries or skipped entry decoding');

const structItems = $goMergeObject($goParseJSON('{"items":[{"a":1,"b":2}],"items":[{"a":3}]}'),undefined,
 [['items',(value,previous) => $goMergeArray(value,previous,(item,prior) => $goMergeObject(item,prior,[['a',scalar],['b',scalar]]))]]);
equal(structItems,{items:[{a:3,b:2}]},'repeated slice lost existing struct element fields');
const integers: $GoJSONDecoder = (value,previous) => $goMergeArray(value,previous,scalar);
const backing = $goMergeObject($goParseJSON('{"items":[1,2],"items":[null],"items":[null,null]}'),undefined,[['items',integers]]);
equal(backing,{items:[1,2]},'truncation lost existing slice storage');
const clearedBacking = $goMergeObject($goParseJSON('{"items":[1,2],"items":[],"items":[null,null]}'),undefined,[['items',integers]]);
equal(clearedBacking,{items:[null,null]},'empty slice incorrectly retained previous storage');
const nullBacking = $goMergeObject($goParseJSON('{"items":[1,2],"items":null,"items":[null,null]}'),undefined,[['items',integers]]);
equal(nullBacking,{items:[null,null]},'null slice incorrectly retained previous storage');
const sourceArray = $goParseJSON('[{"a":1,"b":2}]') as unknown[];
const firstArray = $goMergeArray(sourceArray,undefined,(item,prior) => $goMergeObject(item,prior,[['a',scalar],['b',scalar]]));
$goMergeArray($goParseJSON('[{"a":3}]'),firstArray,(item,prior) => $goMergeObject(item,prior,[['a',scalar],['b',scalar]]));
equal(sourceArray,[{a:1,b:2}],'slice merging mutated input storage');

for(const [name,names,want] of [
 ['K',['K'],'K'],['ſ',['S'],'S'],['ς',['Σ'],'Σ'],['ẞ',['ß'],'ß'],
 ['X',['x','X'],'X'],['x',['X','x'],'x'],['ss',['ß'],undefined],
 ['ı',['I'],undefined],['İ',['i'],undefined],['x\n',['x'],undefined],
 ['a.b',['a.b'],'a.b'],['A.B',['a.b'],'a.b'],['axb',['a.b'],undefined],
] as const) assert($goJSONFieldName(name,names) === want,'incorrect Go JSON field matching: '+name);
const folded = $goMergeObject($goParseJSON('{"K":1,"ſ":2,"ς":3,"ẞ":4,"X":5,"x":6}'),undefined,
 [['K',scalar],['S',scalar],['Σ',scalar],['ß',scalar],['X',scalar],['x',scalar]]);
equal(folded,{K:1,S:2,'Σ':3,'ß':4,X:5,x:6},'case folding/exact priority changed field values');

const ordinary = {'01':1,'1':2};
assert($goMergeMap(ordinary,undefined) === ordinary,'ordinary map gained trusted source metadata');
const stale = $goParseJSON('{"01":1,"1":2}') as {[key:string]:number};
stale['01'] = 3;
assert($goMergeMap(stale,undefined) === stale,'stale map gained trusted source metadata');
const mixed = $goMergeMap($goParseJSON('{"2":2}'),stale) as object;
assert($goJSONEntries(mixed) === undefined,'mixed stale/raw map gained trusted source metadata');

const schema = $goDecodeStruct(z.object({value:z.number()}),(value,path) => {
  const bad = ($goJSONEntries(value as object) ?? []).find(([name,item]) => name === 'value' && typeof item !== 'number');
  return bad === undefined ? undefined : $goWireKind('number',bad[1],[...path,'value']);
},value => $goMergeObject(value,undefined,[['value',scalar]]));
equal(schema.parse($goParseJSON('{"value":1,"value":2}')),{value:2},'wrapper lost final scalar');
const earlier = schema.safeParse($goParseJSON('{"value":"bad","value":2}'));
assert(!earlier.success,'wrapper lost an earlier decoder error');
equal(earlier.error.issues.map(issue => [issue.code,issue.path]),[['invalid_type',['value']]],'wrapper lost the failing value or its path');
const schemaInput: z.core.input<typeof schema> = {value:1};
const schemaOutput: {value:number} = schema.parse(schemaInput);
void schemaOutput;
`
