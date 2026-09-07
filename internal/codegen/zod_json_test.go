package codegen

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestGoJSONHelpersRuntime(t *testing.T) {
	var helper bytes.Buffer
	writeZodJSONHelpers(newErrWriter(&helper))
	if strings.Count(helper.String(), "export ") != 1 || !strings.Contains(helper.String(), "export function parseGoJSON(") {
		t.Fatal("parseGoJSON must be the helper's only export")
	}
	runJSONHelperProgram(t, map[string]string{
		"main.ts":  helper.String() + goJSONRuntimeAssertions,
		"other.ts": helper.String(),
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
import {parseGoJSON as parseFromOtherModule} from './other.js';

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

const duplicate = object(parseGoJSON('{"1":"first","\\u0031":{"nested":1,"nested":2},"1":"last"}'));
equal(duplicate, {1:"last"}, "native object value changed");
const entries = $goJSONEntries(duplicate);
assert(entries?.length === 3, "duplicate or escaped property name lost");
equal(entries.map(entry => entry[0]), ["1","1","1"], "escaped names were not decoded");
assert(entries[0]![1] === "first" && entries[2]![1] === "last", "duplicate order changed");
const overwritten = object(entries[1]![1]);
equal(overwritten, {nested:2}, "overwritten object was not retained");
equal($goJSONEntries(overwritten), [["nested",1],["nested",2]], "nested duplicates lost");

const reordered = object(parseGoJSON('{"01":"a","1":"b","+1":"c","2":"d","0":"e"}'));
equal(Object.keys(reordered), ["0","1","2","01","+1"], "native property enumeration changed");
equal($goJSONEntries(reordered)?.map(entry => entry[0]), ["01","1","+1","2","0"], "source order changed");
const foreign = object(parseFromOtherModule('{"x":1,"x":2}'));
equal($goJSONEntries(foreign), [["x",1],["x",2]], "metadata cannot cross generated modules");

const array = parseGoJSON('[1,{"x":false,"x":true},null,"9223372036854775807"]');
assert(Array.isArray(array), "array became an object");
equal(array, [1,{x:true},null,"9223372036854775807"], "array/scalar wire values changed");
equal($goJSONEntries(object(array[1])), [["x",false],["x",true]], "array child duplicates lost");
equal($goJSONEntries(object(parseGoJSON('{}'))), [], "empty object metadata missing");
equal(parseGoJSON('[]'), [], "empty array changed");
for (const raw of ['null','true','false','0','-0','1.25','1e3','1e400','"001"','"9223372036854775807"','"emoji: \\ud83d\\ude00"']) {
  assert(Object.is(parseGoJSON(raw), JSON.parse(raw)), "scalar representation changed: " + raw);
}
const escaped = { 'quote"slash\\': 'brace } ], quote" and slash\\ and newline\n and nul\u0000' };
equal(parseGoJSON(JSON.stringify(escaped)), escaped, "string scanner mishandled escapes");
equal(parseGoJSON(' \n\t { "a" : [ 1 , 2 ] } \r '), {a:[1,2]}, "whitespace changed parsing");

const prototype = object(parseGoJSON('{"__proto__":{"polluted":true},"constructor":"own","__proto__":{"safe":true}}'));
assert(Object.getPrototypeOf(prototype) === Object.prototype, "__proto__ changed object prototype");
assert(!Object.hasOwn(Object.prototype, "polluted"), "prototype was polluted");
equal(Object.getOwnPropertyDescriptor(prototype,"__proto__")?.value, {safe:true}, "__proto__ own value lost");
assert($goJSONEntries(prototype)?.length === 3, "__proto__ duplicate lost");

const modified = object(parseGoJSON('{"x":1,"x":2}'));
const metadataDescriptor = Object.getOwnPropertyDescriptor(modified, $goJSONMetadataKey);
assert(metadataDescriptor && !metadataDescriptor.enumerable && !metadataDescriptor.writable && !metadataDescriptor.configurable, "metadata must be hidden and immutable");
assert(Object.isFrozen(entries) && Object.isFrozen(entries[0]), "retained entries must be immutable");
modified.x = 3;
assert($goJSONEntries(modified) === undefined, "changed value retained stale entries");
const deleted = object(parseGoJSON('{"x":1}'));
delete deleted.x;
assert($goJSONEntries(deleted) === undefined, "deleted key retained stale entries");
const added = object(parseGoJSON('{"x":1}'));
added.y = 2;
assert($goJSONEntries(added) === undefined, "added key retained stale entries");
const accessor = object(parseGoJSON('{"x":1}'));
Object.defineProperty(accessor, "x", {get() { throw new Error("must not invoke object getters"); }, enumerable:true});
assert($goJSONEntries(accessor) === undefined, "accessor retained stale entries");
const nested = object(parseGoJSON('{"nested":{"1":1,"01":2}}'));
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
  try { parseGoJSON(raw); } catch { failed = true; }
  assert(failed, "invalid JSON was accepted: " + raw);
}
const deep = '{"child":'.repeat(256) + '0' + '}'.repeat(256);
equal(parseGoJSON(deep), JSON.parse(deep), "nested scanner lost an object level");
`
