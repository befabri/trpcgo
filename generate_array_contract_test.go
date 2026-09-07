package trpcgo_test

import (
	"context"
	"go/types"
	"path/filepath"
	"slices"
	"testing"

	"github.com/befabri/trpcgo"
	"github.com/befabri/trpcgo/internal/analysis"
	"github.com/befabri/trpcgo/internal/codegen"
	"github.com/befabri/trpcgo/testdata/validationcontract"
)

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
			gen := codegen.Prepare(&analysis.Result{Procedures: sourceProcs}, nil)
			writeStaticContract(t, dir, mini, gen.Procs, gen.Defs)
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
