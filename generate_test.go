package trpcgo_test

import (
	"bytes"
	"context"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"testing"

	"github.com/befabri/trpcgo"
	"github.com/befabri/trpcgo/internal/analysis"
	"github.com/befabri/trpcgo/internal/codegen"
	"github.com/befabri/trpcgo/internal/typemap"
	"github.com/befabri/trpcgo/testdata/fieldcomposition"
	amodels "github.com/befabri/trpcgo/testdata/namecollision/a/models"
	bmodels "github.com/befabri/trpcgo/testdata/namecollision/b/models"
	"github.com/befabri/trpcgo/testdata/typecontracts"
)

type GenPage[T any] struct {
	Items []T `json:"items"`
	Total int `json:"total"`
	Page  int `json:"page"`
}

type GenPair[A any, B any] struct {
	First  A `json:"first"`
	Second B `json:"second"`
}

type GenAlpha struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type GenBeta struct {
	ID    string `json:"id"`
	Score int    `json:"score"`
}

type GenGamma struct {
	ID   string `json:"id"`
	Flag bool   `json:"flag"`
}

type WithRawJSON struct {
	Data json.RawMessage `json:"data"`
	Name string          `json:"name"`
}

type WithAnyField struct {
	Payload any    `json:"payload"`
	Label   string `json:"label"`
}

type WithFixedArray struct {
	Coords [3]float64 `json:"coords"`
	Tags   [2]string  `json:"tags"`
}

type WithIntKeyMap struct {
	Lookup map[int]string `json:"lookup"`
}

type WithDoublePtr struct {
	Value **string `json:"value"`
}

type WithBytes struct {
	Photo []byte `json:"photo"`
	Name  string `json:"name"`
}

type CgAddress struct {
	Street string `json:"street"`
	City   string `json:"city"`
}

type WithNested struct {
	Name    string    `json:"name"`
	Address CgAddress `json:"address"`
}

type TreeNode struct {
	Label    string     `json:"label"`
	Children []TreeNode `json:"children"`
}

type WithTSSkip struct {
	Visible string `json:"visible"`
	Hidden  string `json:"hidden" tstype:"-"`
}

type WithTSOverride struct {
	Raw  string `json:"raw" tstype:"Record<string, unknown>"`
	Name string `json:"name"`
}

type WithReadonly struct {
	ID   string `json:"id" tstype:",readonly"`
	Name string `json:"name"`
}

type WithRequired struct {
	Avatar *string `json:"avatar" tstype:",required"`
	Bio    *string `json:"bio"`
}

type WithJSONSkip struct {
	Public string `json:"public"`
	Secret string `json:"-"`
}

type PtrBase struct {
	ID        string `json:"id"`
	CreatedAt string `json:"createdAt"`
}

type WithPtrEmbed struct {
	*PtrBase
	Name string `json:"name"`
}

type WithUnexported struct {
	Public  string `json:"public"`
	private string //lint:ignore U1000 intentionally unexported for test
}

type AllNumerics struct {
	I   int     `json:"i"`
	I8  int8    `json:"i8"`
	I16 int16   `json:"i16"`
	I32 int32   `json:"i32"`
	I64 int64   `json:"i64"`
	U   uint    `json:"u"`
	U8  uint8   `json:"u8"`
	U16 uint16  `json:"u16"`
	U32 uint32  `json:"u32"`
	U64 uint64  `json:"u64"`
	F32 float32 `json:"f32"`
	F64 float64 `json:"f64"`
}

type WithBool struct {
	Active  bool  `json:"active"`
	Deleted *bool `json:"deleted"`
}

type CgInner struct {
	Value string `json:"value"`
}

type CgMiddle struct {
	Inner CgInner `json:"inner"`
	Count int     `json:"count"`
}

type CgOuter struct {
	Middle CgMiddle `json:"middle"`
	Name   string   `json:"name"`
}

type NestedMapConfig struct {
	Settings map[string]map[string]int `json:"settings"`
}

type KeyboardKey struct {
	Key string `json:"key"`
}

type Keyboard struct {
	Keys [][]KeyboardKey `json:"keys"`
}

type Endpoint struct {
	URL    string `json:"url"`
	Method string `json:"method"`
}

type APIConfig struct {
	Endpoints map[string]Endpoint `json:"endpoints"`
}

type BatchResult struct {
	Results []map[string]int `json:"results"`
}

type GroupedTags struct {
	Groups map[string][]string `json:"groups"`
}

type WithOmitzero struct {
	Name      string `json:"name"`
	CreatedAt string `json:"created_at,omitzero"`
}

type MultiOptional struct {
	Required    string  `json:"required"`
	OmitEmpty   string  `json:"omitEmpty,omitempty"`
	OmitZero    string  `json:"omitZero,omitzero"`
	Pointer     *string `json:"pointer"`
	PtrOmit     *string `json:"ptrOmit,omitempty"`
	PtrRequired *string `json:"ptrRequired" tstype:",required"`
}

type DeeplyNestedMap struct {
	Data map[string]map[string][]int `json:"data"`
}

type Timestamps struct {
	CreatedAt string `json:"createdAt"`
	UpdatedAt string `json:"updatedAt"`
}

type Metadata struct {
	Version int    `json:"version"`
	Source  string `json:"source"`
}

type FullEntity struct {
	Timestamps
	Metadata
	Name string `json:"name"`
}

type NoJSONTags struct {
	PublicName  string
	PublicCount int
	privateVal  string //lint:ignore U1000 intentionally unexported for test
}

type OptionalVariants struct {
	Always    string  `json:"always"`
	OmitE     string  `json:"omitE,omitempty"`
	OmitZ     string  `json:"omitZ,omitzero"`
	Ptr       *string `json:"ptr"`
	PtrOmitE  *string `json:"ptrOmitE,omitempty"`
	ForcedReq *string `json:"forcedReq" tstype:",required"`
}

type ExBase struct {
	ID string `json:"id"`
}

type ExUser struct {
	ExBase `tstype:",extends"`
	Name   string `json:"name"`
}

type ExWithPtrExtends struct {
	*ExBase `tstype:",extends"`
	Name    string `json:"name"`
}

type ExWithPtrReqExtends struct {
	*ExBase `tstype:",extends,required"`
	Name    string `json:"name"`
}

type ExAuditFields struct {
	CreatedBy string `json:"createdBy"`
	UpdatedBy string `json:"updatedBy"`
}

type ExMultiExtends struct {
	ExBase        `tstype:",extends"`
	ExAuditFields `tstype:",extends"`
	Title         string `json:"title"`
}

type WithTSDoc struct {
	Host string `json:"host" ts_doc:"The hostname to connect to"`
	Port int    `json:"port" ts_doc:"Port number (1-65535)"`
	Name string `json:"name"`
}

type WithJSONNumber struct {
	Value json.Number `json:"value"`
	Name  string      `json:"name"`
}

type ZodLoginInput struct {
	Email    string `json:"email" validate:"required,email"`
	Password string `json:"password" validate:"required,min=8,max=128"`
}

type ZodCreateItemInput struct {
	Name  string   `json:"name" validate:"required,min=1,max=100"`
	Tags  []string `json:"tags" validate:"max=10,dive,min=1,max=50"`
	Count int      `json:"count" validate:"gte=0,lte=1000"`
}

type ZodOptionalInput struct {
	Query  string `json:"query,omitempty"`
	Limit  *int   `json:"limit"`
	Offset int    `json:"offset"`
}

type ZodBase struct {
	ID string `json:"id" validate:"required"`
}

type ZodDerived struct {
	ZodBase `tstype:",extends"`
	Name    string `json:"name" validate:"required,min=1"`
}

type ZodMultiBase struct {
	ZodBase       `tstype:",extends"`
	ExAuditFields `tstype:",extends"`
	Title         string `json:"title" validate:"required"`
}

type ZodPtrExtends struct {
	*ZodBase `tstype:",extends"`
	Label    string `json:"label"`
}

type ZodCyclicNode struct {
	ZodBase  `tstype:",extends"`
	Children []ZodCyclicNode `json:"children"`
}

type ZodUnsupportedInput struct {
	MinVal float64 `json:"minVal" validate:"required,gte=0"`
	MaxVal float64 `json:"maxVal" validate:"required,gte=0,gtefield=MinVal"`
	Label  string  `json:"label" validate:"required,custom_check"`
}

type ZodCrossFieldInput struct {
	MinVal   int32 `json:"min_val" validate:"min=1"`
	MaxVal   int32 `json:"max_val" validate:"min=1,gtefield=MinVal"`
	StartVal int32 `json:"start_val" validate:"min=1"`
	EndVal   int32 `json:"end_val" validate:"min=1,gtefield=StartVal"`
}

type ZodCrossFieldAllOps struct {
	A int32 `json:"a"`
	B int32 `json:"b" validate:"gtefield=A"`
	C int32 `json:"c" validate:"ltefield=A"`
	D int32 `json:"d" validate:"gtfield=A"`
	E int32 `json:"e" validate:"ltfield=A"`
	F int32 `json:"f" validate:"eqfield=A"`
	G int32 `json:"g" validate:"nefield=A"`
}

type ZodOmitInput struct {
	ID     string `json:"id" zod_omit:"true"`
	Name   string `json:"name" validate:"required,min=1"`
	Active bool   `json:"active"`
}

type ZodOmitWithRefine struct {
	ID     int32 `json:"id" zod_omit:"true"`
	MinVal int32 `json:"min_val" validate:"min=1,gtefield=ID"`
	MaxVal int32 `json:"max_val" validate:"min=1,gtefield=MinVal"`
}

type ZodInt64Input struct {
	BigSigned   int64  `json:"bigSigned"`
	BigUnsigned uint64 `json:"bigUnsigned"`
	NormalInt   int32  `json:"normalInt"`
}

type ZodNewTagsInput struct {
	Host    string `json:"host" validate:"hostname"`
	Token   string `json:"token" validate:"base64url"`
	Hash    string `json:"hash" validate:"hexadecimal,min=64,max=64"`
	ID      string `json:"id" validate:"ulid"`
	Mac     string `json:"mac" validate:"mac"`
	Subnet  string `json:"subnet" validate:"cidrv4"`
	Code    string `json:"code" validate:"uppercase"`
	Website string `json:"website" validate:"startswith=https://,min=10"`
	File    string `json:"file" validate:"endswith=.json"`
	Path    string `json:"path" validate:"contains=/api/"`
}

type DriftPrivateUser struct {
	ID          string `json:"id"`
	SecretToken string `json:"secretToken"`
}

type DriftPublicUser struct {
	ID string `json:"id"`
}

// TestGeneratedClientContracts checks actual tRPC calls and Zod inference, with
// both positive and negative assertions. It intentionally does not snapshot the
// generated text: formatting changes are harmless; weakening a type is not.
func TestGeneratedClientContracts(t *testing.T) {
	fixture := filepath.Join("testdata", "typecontracts")
	for _, static := range []bool{false, true} {
		t.Run(mapperName(static), func(t *testing.T) {
			dir := t.TempDir()
			symlinkNodeModules(t, dir)
			for _, name := range []string{"assertions.ts", "client.ts", "schemas.contract.ts"} {
				data, err := os.ReadFile(filepath.Join(fixture, name))
				if err != nil {
					t.Fatal(err)
				}
				writeTestSources(t, dir, map[string]string{name: string(data)})
			}
			if static {
				result, err := analysis.Analyze([]string{"."}, fixture)
				if err != nil {
					t.Fatal(err)
				}
				gen := codegen.Prepare(result, result.TypeMetas, nil)
				var router, standard, mini bytes.Buffer
				if err := codegen.WriteAppRouter(&router, gen.Procs, gen.Defs); err != nil {
					t.Fatal(err)
				}
				if err := codegen.WriteZodSchemas(&standard, gen.Procs, gen.Defs, typemap.ZodStandard, codegen.ZodOptions{}); err != nil {
					t.Fatal(err)
				}
				if err := codegen.WriteZodSchemas(&mini, gen.Procs, gen.Defs, typemap.ZodMini, codegen.ZodOptions{}); err != nil {
					t.Fatal(err)
				}
				writeTestSources(t, dir, map[string]string{"trpc.ts": router.String(), "schemas.ts": standard.String(), "schemas-mini.ts": mini.String()})
			} else {
				for _, mini := range []bool{false, true} {
					r := typecontracts.NewRouter(trpcgo.WithZodMini(mini))
					t.Cleanup(func() { _ = r.Close() })
					name := "schemas.ts"
					if mini {
						name = "schemas-mini.ts"
					} else if err := r.GenerateTS(filepath.Join(dir, "trpc.ts")); err != nil {
						t.Fatal(err)
					}
					if err := r.GenerateZod(filepath.Join(dir, name)); err != nil {
						t.Fatal(err)
					}
				}
			}
			assertTypeScriptCompiles(t, dir, "client.ts", "schemas.contract.ts")
		})
	}
}

// Both packages declare models.User. Their names, procedure inputs and runtime
// schemas must stay distinct through each mapper and each Zod style.
func TestGenerateDisambiguatesSamePackageNames(t *testing.T) {
	forEachGeneration(t, func(t *testing.T, mini, static bool) {
		dir := t.TempDir()
		symlinkNodeModules(t, dir)
		copyTypeScriptAssertions(t, dir)
		if static {
			var procedures []analysis.Procedure
			for _, prefix := range []string{"a", "b"} {
				pkg := parseFixture(t, "github.com/befabri/trpcgo/testdata/namecollision/"+prefix+"/models", "testdata/namecollision/"+prefix+"/models/user.go")
				procedures = append(procedures, analysis.Procedure{Path: prefix + ".get", Type: "query", InputType: pkg.Scope().Lookup("User").Type(), OutputType: types.Typ[types.String]})
			}
			gen := codegen.Prepare(&analysis.Result{Procedures: procedures}, nil, nil)
			writeStaticContract(t, dir, mini, gen.Procs, gen.Defs, codegen.ZodOptions{})
		} else {
			r := trpcgo.NewRouter(trpcgo.WithZodMini(mini))
			t.Cleanup(func() { _ = r.Close() })
			trpcgo.MustQuery(r, "a.get", func(context.Context, amodels.User) (string, error) { return "", nil })
			trpcgo.MustQuery(r, "b.get", func(context.Context, bmodels.User) (string, error) { return "", nil })
			if err := r.GenerateTS(filepath.Join(dir, "trpc.ts")); err != nil {
				t.Fatal(err)
			}
			if err := r.GenerateZod(filepath.Join(dir, "schemas.ts")); err != nil {
				t.Fatal(err)
			}
		}
		writeTestSources(t, dir, map[string]string{"validate.ts": nameCollisionAssertions})
		assertTypeScriptCompiles(t, dir, "trpc.ts", "schemas.ts", "validate.ts")
		runTypeScript(t, dir, "validate.ts")
	})
}

const nameCollisionAssertions = `
import type { core } from 'zod';
import type { Assert, Equal } from './assertions';
import type { RouterInputs, AModelsUser, BModelsUser } from './trpc';
import { AModelsUserSchema as a, BModelsUserSchema as b } from './schemas';
type AFields = Assert<Equal<AModelsUser, {name: string}>>;
type BFields = Assert<Equal<BModelsUser, {email: string}>>;
type AProcedure = Assert<Equal<RouterInputs['a']['get'], {name: string}>>;
type BProcedure = Assert<Equal<RouterInputs['b']['get'], {email: string}>>;
type ASchemaInput = Assert<Equal<core.input<typeof a>, {name: string}>>;
type BSchemaInput = Assert<Equal<core.input<typeof b>, {email: string}>>;
type ASchemaOutput = Assert<Equal<core.output<typeof a>, {name: string}>>;
type BSchemaOutput = Assert<Equal<core.output<typeof b>, {email: string}>>;
const name = {name: 'Ada'};
const email = {email: 'ada@example.com'};
if (a.parse(name).name !== name.name || b.parse(email).email !== email.email) throw new Error('distinct field values lost');
for (const value of [email, {}, {name: ''}, {name: 1}]) {
 if (a.safeParse(value).success) throw new Error('name schema accepted ' + JSON.stringify(value));
}
for (const value of [name, {}, {email: ''}, {email: 'invalid'}, {email: 1}]) {
 if (b.safeParse(value).success) throw new Error('email schema accepted ' + JSON.stringify(value));
}
`

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

type GenRecursive[T any] struct {
	Value    T                 `json:"value"`
	Count    int               `json:"count"`
	Children []GenRecursive[T] `json:"children"`
}

func TestGenerateRecursiveGenericContracts(t *testing.T) {
	for _, mini := range []bool{false, true} {
		t.Run(zodStyleName(mini), func(t *testing.T) {
			r := trpcgo.NewRouter(trpcgo.WithZodMini(mini))
			trpcgo.MustQuery(r, "number", func(_ context.Context, in GenRecursive[int]) (GenRecursive[int], error) { return in, nil })
			trpcgo.MustQuery(r, "text", func(_ context.Context, in GenRecursive[string]) (GenRecursive[string], error) { return in, nil })
			checkGeneratedZod(t, r, `
import * as schemas from './schemas.ts';
// Private $go variants and decodeGoJSON sit beside the public schemas; only
// public schemas count.
const all = Object.entries(schemas).filter(([name]) => !name.startsWith('$') && name.endsWith('Schema')).map(([, schema]) => schema as { safeParse(input: unknown): { success: boolean } });
if (all.length !== 2) throw new Error('instantiations were not specialized');
for (const value of [1, 'text']) {
  const input = { value, count: 1, children: [{ value, count: 2, children: [] }] };
  if (all.filter(schema => schema.safeParse(input).success).length !== 1) throw new Error('recursive instantiation lost its type');
  if (all.some(schema => schema.safeParse({ ...input, count: 'invalid' }).success)) throw new Error('concrete count lost its type');
}
`)
			checkGeneratedRouterContract(t, r, `
import type { RouterOutputs } from './trpc';
declare const text: RouterOutputs['text'];
declare const number: RouterOutputs['number'];
const value: string = text.children[0].value;
const count: number = text.count;
const numeric: number = number.children[0].value;
void [value, count, numeric];
`)
		})
	}
}
