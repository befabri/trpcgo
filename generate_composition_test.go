package trpcgo_test

import (
	"bytes"
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"testing"

	"github.com/befabri/trpcgo"
	"github.com/befabri/trpcgo/internal/codegen"
	"github.com/befabri/trpcgo/internal/typemap"
	"github.com/befabri/trpcgo/testdata/fieldcomposition"
)

type (
	CompositionRecursiveBase       = fieldcomposition.CompositionRecursiveBase
	CompositionRecursiveDerived    = fieldcomposition.CompositionRecursiveDerived
	CompositionRecursivePointer    = fieldcomposition.CompositionRecursivePointer
	CompositionRecursiveAudit      = fieldcomposition.CompositionRecursiveAudit
	CompositionRecursiveMulti      = fieldcomposition.CompositionRecursiveMulti
	CompositionForwardBase         = fieldcomposition.CompositionForwardBase
	CompositionForwardDerived      = fieldcomposition.CompositionForwardDerived
	CompositionExtendsA            = fieldcomposition.CompositionExtendsA
	CompositionExtendsB            = fieldcomposition.CompositionExtendsB
	CompositionExtendsWrapper      = fieldcomposition.CompositionExtendsWrapper
	CompositionHiddenBase          = fieldcomposition.CompositionHiddenBase
	CompositionHiddenDerived       = fieldcomposition.CompositionHiddenDerived
	CompositionHiddenExtended      = fieldcomposition.CompositionHiddenExtended
	CompositionHiddenDeep          = fieldcomposition.CompositionHiddenDeep
	CompositionHiddenEmbedding     = fieldcomposition.CompositionHiddenEmbedding
	CompositionNumericBase         = fieldcomposition.CompositionNumericBase
	CompositionHiddenAmbiguous     = fieldcomposition.CompositionHiddenAmbiguous
	CompositionJSONExcluded        = fieldcomposition.CompositionJSONExcluded
	CompositionRangeBase           = fieldcomposition.CompositionRangeBase
	CompositionRangeDerived        = fieldcomposition.CompositionRangeDerived
	CompositionRangeShadowedTarget = fieldcomposition.CompositionRangeShadowedTarget
	CompositionLowerBound          = fieldcomposition.CompositionLowerBound
	CompositionScopedRange         = fieldcomposition.CompositionScopedRange
	CompositionNestedRange         = fieldcomposition.CompositionNestedRange
	CompositionCaseEnum            = fieldcomposition.CompositionCaseEnum
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

func TestGenerateMutualRecursiveEmbedding(t *testing.T) {
	r := trpcgo.NewRouter()
	trpcgo.MustQuery(r, "check", func(_ context.Context, in CompositionExtendsA) (CompositionExtendsA, error) { return in, nil })
	trpcgo.MustQuery(r, "b", func(_ context.Context, in CompositionExtendsB) (CompositionExtendsB, error) { return in, nil })
	trpcgo.MustQuery(r, "wrapper", func(_ context.Context, in CompositionExtendsWrapper) (CompositionExtendsWrapper, error) {
		return in, nil
	})
	checkGeneratedRouterContract(t, r, `
import type { RouterOutputs } from './trpc';
const value: RouterOutputs['check'] = { a: 'x', b: 1 };
const withoutPointer: RouterOutputs['check'] = { a: 'x' };
const b: RouterOutputs['b'] = { b: 1 };
const wrapper: RouterOutputs['wrapper'] = { a: 'x', b: 1, c: true };
// @ts-expect-error the direct a field remains required
const missing: RouterOutputs['check'] = { b: 1 };
// @ts-expect-error the promoted b field remains numeric
const wrong: RouterOutputs['check'] = { a: 'x', b: 'bad' };
void [value, withoutPointer, b, wrapper, missing, wrong];
`)
	for _, mini := range []bool{false, true} {
		t.Run(zodStyleName(mini), func(t *testing.T) {
			r := trpcgo.NewRouter(trpcgo.WithZodMini(mini))
			trpcgo.MustQuery(r, "a", func(context.Context, CompositionExtendsA) (string, error) { return "ok", nil })
			trpcgo.MustQuery(r, "b", func(context.Context, CompositionExtendsB) (string, error) { return "ok", nil })
			trpcgo.MustQuery(r, "wrapper", func(context.Context, CompositionExtendsWrapper) (string, error) { return "ok", nil })
			checkGeneratedZod(t, r, `
import { CompositionExtendsASchema as a, CompositionExtendsBSchema as b, CompositionExtendsWrapperSchema as wrapper } from './schemas.ts';
a.parse({ a: 'x' });
a.parse({ a: 'x', b: 1 });
b.parse({ b: 1 });
wrapper.parse({ a: 'x', b: 1, c: true });
if (a.safeParse({ b: 1 }).success) throw new Error('direct required field became optional');
if (a.safeParse({ a: 'x', b: 'bad' }).success) throw new Error('promoted field lost its type');
`)
		})
	}
}

func TestGenerateExcludedFieldDominance(t *testing.T) {
	r := trpcgo.NewRouter()
	trpcgo.MustVoidQuery(r, "check", func(context.Context) (CompositionHiddenDerived, error) { return CompositionHiddenDerived{}, nil })
	trpcgo.MustVoidQuery(r, "extended", func(context.Context) (CompositionHiddenExtended, error) { return CompositionHiddenExtended{}, nil })
	trpcgo.MustVoidQuery(r, "embedding", func(context.Context) (CompositionHiddenEmbedding, error) { return CompositionHiddenEmbedding{}, nil })
	trpcgo.MustVoidQuery(r, "ambiguous", func(context.Context) (CompositionHiddenAmbiguous, error) { return CompositionHiddenAmbiguous{}, nil })
	trpcgo.MustVoidQuery(r, "jsonExcluded", func(context.Context) (CompositionJSONExcluded, error) { return CompositionJSONExcluded{}, nil })
	checkGeneratedRouterContract(t, r, `
import type { RouterOutputs } from './trpc';
declare const value: RouterOutputs['check'];
// @ts-expect-error the dominant field is explicitly excluded from TypeScript
value.value;
declare const extended: RouterOutputs['extended'];
// @ts-expect-error hidden fields do not reappear through an extends clause
extended.value;
declare const embedding: RouterOutputs['embedding'];
// @ts-expect-error an excluded embedded field still shadows a deeper field
embedding.value;
declare const ambiguous: RouterOutputs['ambiguous'];
// @ts-expect-error exclusions do not remove encoding/json's ambiguity
ambiguous.value;
const jsonExcluded: RouterOutputs['jsonExcluded'] = { value: 'visible embedded value' };
void jsonExcluded;
`)
	for _, mini := range []bool{false, true} {
		t.Run(zodStyleName(mini), func(t *testing.T) {
			r := trpcgo.NewRouter(trpcgo.WithZodMini(mini))
			trpcgo.MustQuery(r, "check", func(context.Context, CompositionHiddenDerived) (string, error) { return "ok", nil })
			trpcgo.MustQuery(r, "extended", func(context.Context, CompositionHiddenExtended) (string, error) { return "ok", nil })
			trpcgo.MustQuery(r, "embedding", func(context.Context, CompositionHiddenEmbedding) (string, error) { return "ok", nil })
			trpcgo.MustQuery(r, "ambiguous", func(context.Context, CompositionHiddenAmbiguous) (string, error) { return "ok", nil })
			checkGeneratedZod(t, r, `
import { CompositionHiddenDerivedSchema, CompositionHiddenExtendedSchema, CompositionHiddenEmbeddingSchema, CompositionHiddenAmbiguousSchema } from './schemas.ts';
for (const schema of [CompositionHiddenDerivedSchema, CompositionHiddenExtendedSchema, CompositionHiddenEmbeddingSchema, CompositionHiddenAmbiguousSchema]) {
  schema.parse({});
  if ('value' in schema.shape) throw new Error('excluded or ambiguous property was emitted');
  if (schema.safeParse({ value: 123 }).success) throw new Error('strict schema accepted an excluded or ambiguous property');
}
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

func TestStaticFieldComposition(t *testing.T) {
	// Type-check the same fixture file so reflection and go/types see identical types.
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "testdata/fieldcomposition/types.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	typeFile := &ast.File{Name: file.Name}
	for _, decl := range file.Decls {
		if gen, ok := decl.(*ast.GenDecl); ok && gen.Tok == token.TYPE {
			typeFile.Decls = append(typeFile.Decls, gen)
		}
	}
	pkg := typeCheck(t, "example.com/fieldcomposition", fset, []*ast.File{typeFile})

	t.Run("hidden_shadow", func(t *testing.T) {
		for _, name := range []string{"CompositionHiddenDerived", "CompositionHiddenExtended", "CompositionHiddenEmbedding", "CompositionHiddenAmbiguous"} {
			m := typemap.NewMapper(nil)
			typ := pkg.Scope().Lookup(name).Type()
			m.Convert(typ)
			for _, def := range m.Defs() {
				if def.Name == name && (len(def.Fields) != 0 || len(def.Extends) != 0) {
					t.Fatalf("%s: excluded dominant field reappeared: %+v", name, def)
				}
			}
			if got := m.Convert(typ.Underlying()); got != "Record<string, never>" {
				t.Fatalf("%s: anonymous object did not apply the same exclusion rules: %s", name, got)
			}
		}
	})

	t.Run("inherited_refinement", func(t *testing.T) {
		m := typemap.NewMapper(nil)
		m.Convert(pkg.Scope().Lookup("CompositionRangeDerived").Type())
		m.Convert(pkg.Scope().Lookup("CompositionNestedRange").Type())
		m.Convert(pkg.Scope().Lookup("CompositionRangeShadowedTarget").Type())
		for _, def := range m.Defs() {
			if def.Name == "CompositionRangeDerived" {
				if len(def.Refinements) != 1 || def.Refinements[0].Field != "max" || def.Refinements[0].OtherField != "min" {
					t.Fatalf("inherited max >= min constraint was lost: %+v", def.Refinements)
				}
			}
			if def.Name == "CompositionNestedRange" && (len(def.Refinements) != 1 || def.Refinements[0].Field != "upper-bound" || def.Refinements[0].OtherField != "lower-bound") {
				t.Fatalf("promoted reference lost its scope: %+v", def.Refinements)
			}
			if def.Name == "CompositionRangeShadowedTarget" && len(def.Refinements) != 0 {
				t.Fatalf("refinement was rebound to a different Go field: %+v", def.Refinements)
			}
		}
	})

	t.Run("mutual_extends", func(t *testing.T) {
		m := typemap.NewMapper(nil)
		output := m.Convert(pkg.Scope().Lookup("CompositionExtendsA").Type())
		procs := []codegen.ProcEntry{{Path: "check", ProcType: "query", InputTS: "void", OutputTS: m.Resolve(output)}}
		var generated bytes.Buffer
		if err := codegen.WriteAppRouter(&generated, procs, m.Defs()); err != nil {
			t.Fatal(err)
		}
		dir := t.TempDir()
		symlinkNodeModules(t, dir)
		if err := os.WriteFile(filepath.Join(dir, "trpc.ts"), generated.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
		assertTypeScriptCompiles(t, dir, "trpc.ts")
	})
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
import { InheritanceOrderOuterSchema as schema, parseGoJSON } from './schemas.ts';
const result = schema.safeParse(parseGoJSON('{"Ab":7,"ab":1,"arr":[1,2]}'));
if (!result.success) throw new Error('folded key must select the first Go field: ' + JSON.stringify(result.error.issues));
const data = result.data as { aB: number; ab: number };
if (data.aB !== 7 || data.ab !== 1) throw new Error('folded key landed on the wrong field: ' + JSON.stringify(data));
`)
}
