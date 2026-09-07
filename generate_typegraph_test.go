package trpcgo_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/befabri/trpcgo"
	"github.com/befabri/trpcgo/internal/codegen"
	"github.com/befabri/trpcgo/internal/typemap"
	"github.com/befabri/trpcgo/testdata/typegraph"
)

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
const input = { details:{count:1,name:'ok',low:1,high:2},items:[{name:'ok'}],map:{one:{count:1}} };
const parsed = InlineInputSchema.parse({...input,next:input});
const name: string = parsed.next!.details.name;
const secret: unknown = parsed.next!.details.secret;
// @ts-expect-error recursive unvalidated fields require narrowing before use as strings
const secretString: string = parsed.next!.details.secret;
if ('secret' in parsed.next!.details) throw new Error('absent recursive unvalidated field was materialized');
const opaque = { arbitrary: true };
const withSecret = InlineInputSchema.parse({ ...input, next: { ...input, details: { ...input.details, secret: opaque } } });
if (withSecret.next!.details.secret !== opaque) throw new Error('recursive unvalidated field was not preserved');
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
		writeStaticContract(t, dir, mini, procs, mapper.Defs())
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
		if err := codegen.WriteZodSchemas(&out, procs, mapper.Defs(), zodStyle(mini)); err != nil {
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
		if err := codegen.WriteZodSchemas(&static, []codegen.ProcEntry{{InputTS: input}}, mapper.Defs(), zodStyle(mini)); err != nil {
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
					if strings.Contains(line, "invalid zod params") || !strings.Contains(line, "z.refine($trpcgoEmail)") {
						t.Fatalf("valid typed map scope lost key validation or gained false diagnostic: %s", line)
					}
				}
			}
			if !found {
				t.Fatalf("missing field %s:\n%s", name, output)
			}
		}
	})
}
