package codegen

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/befabri/trpcgo/internal/typemap"
)

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
const raw = parseGoJSON('{"details":{"values":{"01":1},"label":"kept"},"DETAILS":{"values":{"2":2},"label":null},"quotedPointer":"1","quotedPointer":"null"}');
const merged = $goMergeObject(raw,undefined,fields) as {details:{values:object,label:string},quotedPointer:string};
equal(merged.details,{values:{'2':2,'01':1},label:'kept'},'repeated struct/map fields were not merged');
equal($goJSONEntries(merged.details.values),[['01',1],['2',2]],'map entry history lost during field merge');
assert(merged.quotedPointer === 'null','quoted nil changed its wire representation');
equal(raw,{details:{values:{'01':1},label:'kept'},DETAILS:{values:{'2':2},label:null},quotedPointer:'null'},'merging mutated the raw input');

const cleared = $goMergeObject(parseGoJSON('{"values":{"1":1},"values":null,"values":{"2":2}}'),undefined,[['values',$goMergeMap]]) as {values:object};
equal(cleared.values,{2:2},'null did not clear the previous map');
equal($goJSONEntries(cleared.values),[['2',2]],'cleared map retained previous entries');
const mapValues = $goMergeMap(parseGoJSON('{"1":{"a":1},"1":{"b":2}}'),undefined) as object;
equal(mapValues,{1:{b:2}},'map values were merged instead of replaced');
assert($goJSONEntries(mapValues)?.length === 2,'map replacement dropped source entries');

const structItems = $goMergeObject(parseGoJSON('{"items":[{"a":1,"b":2}],"items":[{"a":3}]}'),undefined,
 [['items',(value,previous) => $goMergeArray(value,previous,(item,prior) => $goMergeObject(item,prior,[['a',scalar],['b',scalar]]))]]);
equal(structItems,{items:[{a:3,b:2}]},'repeated slice lost existing struct element fields');
const integers: $GoJSONDecoder = (value,previous) => $goMergeArray(value,previous,scalar);
const backing = $goMergeObject(parseGoJSON('{"items":[1,2],"items":[null],"items":[null,null]}'),undefined,[['items',integers]]);
equal(backing,{items:[1,2]},'truncation lost existing slice storage');
const clearedBacking = $goMergeObject(parseGoJSON('{"items":[1,2],"items":[],"items":[null,null]}'),undefined,[['items',integers]]);
equal(clearedBacking,{items:[null,null]},'empty slice incorrectly retained previous storage');
const nullBacking = $goMergeObject(parseGoJSON('{"items":[1,2],"items":null,"items":[null,null]}'),undefined,[['items',integers]]);
equal(nullBacking,{items:[null,null]},'null slice incorrectly retained previous storage');
const sourceArray = parseGoJSON('[{"a":1,"b":2}]') as unknown[];
const firstArray = $goMergeArray(sourceArray,undefined,(item,prior) => $goMergeObject(item,prior,[['a',scalar],['b',scalar]]));
$goMergeArray(parseGoJSON('[{"a":3}]'),firstArray,(item,prior) => $goMergeObject(item,prior,[['a',scalar],['b',scalar]]));
equal(sourceArray,[{a:1,b:2}],'slice merging mutated input storage');

for(const [name,names,want] of [
 ['K',['K'],'K'],['ſ',['S'],'S'],['ς',['Σ'],'Σ'],['ẞ',['ß'],'ß'],
 ['X',['x','X'],'X'],['x',['X','x'],'x'],['ss',['ß'],undefined],
 ['ı',['I'],undefined],['İ',['i'],undefined],['x\n',['x'],undefined],
 ['a.b',['a.b'],'a.b'],['A.B',['a.b'],'a.b'],['axb',['a.b'],undefined],
] as const) assert($goJSONFieldName(name,names) === want,'incorrect Go JSON field matching: '+name);
const folded = $goMergeObject(parseGoJSON('{"K":1,"ſ":2,"ς":3,"ẞ":4,"X":5,"x":6}'),undefined,
 [['K',scalar],['S',scalar],['Σ',scalar],['ß',scalar],['X',scalar],['x',scalar]]);
equal(folded,{K:1,S:2,'Σ':3,'ß':4,X:5,x:6},'case folding/exact priority changed field values');

const ordinary = {'01':1,'1':2};
assert($goMergeMap(ordinary,undefined) === ordinary,'ordinary map gained trusted source metadata');
const stale = parseGoJSON('{"01":1,"1":2}') as {[key:string]:number};
stale['01'] = 3;
assert($goMergeMap(stale,undefined) === stale,'stale map gained trusted source metadata');
const mixed = $goMergeMap(parseGoJSON('{"2":2}'),stale) as object;
assert($goJSONEntries(mixed) === undefined,'mixed stale/raw map gained trusted source metadata');

const schema = $goDecodeStruct(z.object({value:z.number()}),value => {
  return ($goJSONEntries(value as object) ?? []).every(([name,item]) => name !== 'value' || typeof item === 'number');
},value => $goMergeObject(value,undefined,[['value',scalar]]));
equal(schema.parse(parseGoJSON('{"value":1,"value":2}')),{value:2},'wrapper lost final scalar');
assert(!schema.safeParse(parseGoJSON('{"value":"bad","value":2}')).success,'wrapper lost an earlier decoder error');
const schemaInput: z.core.input<typeof schema> = {value:1};
const schemaOutput: {value:number} = schema.parse(schemaInput);
void schemaOutput;
`
