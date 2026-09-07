package trpcgo_test

import (
	"bytes"
	"context"
	"go/types"
	"os"
	"path/filepath"
	"testing"

	"github.com/befabri/trpcgo"
	"github.com/befabri/trpcgo/internal/analysis"
	"github.com/befabri/trpcgo/internal/codegen"
	"github.com/befabri/trpcgo/internal/typemap"
	amodels "github.com/befabri/trpcgo/testdata/namecollision/a/models"
	bmodels "github.com/befabri/trpcgo/testdata/namecollision/b/models"
	"github.com/befabri/trpcgo/testdata/typecontracts"
)

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
				gen := codegen.Prepare(result, result.TypeMetas)
				var router, standard, mini bytes.Buffer
				if err := codegen.WriteAppRouter(&router, gen.Procs, gen.Defs); err != nil {
					t.Fatal(err)
				}
				if err := codegen.WriteZodSchemas(&standard, gen.Procs, gen.Defs, typemap.ZodStandard); err != nil {
					t.Fatal(err)
				}
				if err := codegen.WriteZodSchemas(&mini, gen.Procs, gen.Defs, typemap.ZodMini); err != nil {
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
			gen := codegen.Prepare(&analysis.Result{Procedures: procedures}, nil)
			writeStaticContract(t, dir, mini, gen.Procs, gen.Defs)
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
