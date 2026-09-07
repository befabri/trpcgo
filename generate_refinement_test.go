package trpcgo_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/befabri/trpcgo"
	"github.com/befabri/trpcgo/internal/codegen"
	"github.com/befabri/trpcgo/internal/typemap"
	"github.com/befabri/trpcgo/testdata/fieldcomposition"
)

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
const byName: Record<string, { safeParse(input: unknown): { success: boolean } }> = schemas;
const cases = ` + string(cases) + `;
for (const test of cases) {
  const result = byName[test.type + 'Schema'].safeParse(JSON.parse(test.json));
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
			if err := codegen.WriteZodSchemas(&out, procs, m.Defs(), zodStyle(mini)); err != nil {
				return err
			}
			return os.WriteFile(path, out.Bytes(), 0o644)
		}, script)
	})
}
