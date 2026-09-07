package trpcgo_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/befabri/trpcgo"
	"github.com/befabri/trpcgo/internal/codegen"
	"github.com/befabri/trpcgo/internal/typemap"
	fixture "github.com/befabri/trpcgo/testdata/validationconfig"
	"github.com/befabri/trpcgo/testdata/validationcontract"
	"github.com/befabri/trpcgo/zodconfig"
)

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
			writeStaticContract(t, dir, mini, procs, mapper.Defs(), codegen.ZodOptions{Validation: config})
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
