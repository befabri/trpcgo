package trpcgo_test

import (
	"bytes"
	"context"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/befabri/trpcgo"
	"github.com/befabri/trpcgo/internal/codegen"
	"github.com/befabri/trpcgo/internal/typemap"
	"github.com/befabri/trpcgo/testdata/schemaissues"
)

func TestZodSchemaSpecificRecursiveTypes(t *testing.T) {
	for _, mini := range []bool{false, true} {
		for _, static := range []bool{false, true} {
			path := "reflection"
			if static {
				path = "static"
			}
			t.Run(zodStyleName(mini)+"/"+path, func(t *testing.T) {
				dir := t.TempDir()
				symlinkNodeModules(t, dir)
				if static {
					fset := token.NewFileSet()
					file, err := parser.ParseFile(fset, "testdata/schemaissues/types.go", nil, 0)
					if err != nil {
						t.Fatal(err)
					}
					pkg, err := (&types.Config{Importer: importer.Default()}).Check("schemaissues", fset, []*ast.File{file}, nil)
					if err != nil {
						t.Fatal(err)
					}
					m := typemap.NewMapper(map[string]typemap.TypeMeta{"schemaissues.Tags": {IsAlias: true}, "schemaissues.Links": {IsAlias: true}})
					input := m.Convert(pkg.Scope().Lookup("Node").Type())
					procs := []codegen.ProcEntry{{Path: "node", ProcType: "query", InputTS: m.Resolve(input), OutputTS: "string"}}
					defs := m.Defs()
					var schemas, router bytes.Buffer
					style := typemap.ZodStandard
					if mini {
						style = typemap.ZodMini
					}
					if err := codegen.WriteZodSchemas(&schemas, procs, defs, style); err != nil {
						t.Fatal(err)
					}
					if err := codegen.WriteAppRouter(&router, procs, defs); err != nil {
						t.Fatal(err)
					}
					for name, data := range map[string][]byte{"schemas.ts": schemas.Bytes(), "trpc.ts": router.Bytes()} {
						if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
							t.Fatal(err)
						}
					}
				} else {
					r := trpcgo.NewRouter(trpcgo.WithZodMini(mini))
					trpcgo.MustQuery(r, "node", func(context.Context, schemaissues.Node) (string, error) { return "", nil })
					if err := r.GenerateZod(filepath.Join(dir, "schemas.ts")); err != nil {
						t.Fatal(err)
					}
					if err := r.GenerateTS(filepath.Join(dir, "trpc.ts")); err != nil {
						t.Fatal(err)
					}
				}
				script := `
import type * as z from 'zod';
import { NodeSchema } from './schemas';
import type { Node } from './barrel';
const leaf: z.input<typeof NodeSchema> = { date: 'today', tags: [{ name: 'ok' }], inline: { z: 1 } };
const tree: z.input<typeof NodeSchema> = { ...leaf, child: leaf, links: { next: leaf }, inline: { z: 2, next: leaf } };
const parsed = NodeSchema.parse(tree);
const date: string = parsed.child!.date;
const name: string = parsed.tags[0].name;
declare const full: Node;
type Secret = Node['secret'];
// @ts-expect-error schema output omits the secret
type NoSecret = typeof parsed.secret;
// @ts-expect-error schema types must not be exported as domain types
type Private = import('./schemas').Node;
void [date, name];
for (const input of [{ ...leaf, tags: [{ name: '' }] }, { ...leaf, inline: { z: 'bad' } }, { ...leaf, links: { next: { ...leaf, date: 1 } } }]) {
  if (NodeSchema.safeParse(input).success) throw new Error('invalid nested value accepted');
}
`
				for name, data := range map[string]string{"barrel.ts": "export * from './trpc';\nexport * from './schemas';\n", "validate.ts": script} {
					if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0o644); err != nil {
						t.Fatal(err)
					}
				}
				assertTypeScriptCompiles(t, dir, "barrel.ts", "validate.ts")
				tsx, _ := filepath.Abs("testdata/zodruntime/node_modules/.bin/tsx")
				cmd := exec.CommandContext(t.Context(), tsx, "validate.ts")
				cmd.Dir = dir
				if out, err := cmd.CombinedOutput(); err != nil {
					t.Fatalf("runtime: %v\n%s", err, out)
				}
			})
		}
	}
}

func TestZodNonStructExtendsKeepsOtherSchemas(t *testing.T) {
	for _, mini := range []bool{false, true} {
		t.Run(zodStyleName(mini), func(t *testing.T) {
			r := trpcgo.NewRouter(trpcgo.WithZodMini(mini))
			trpcgo.MustQuery(r, "time", func(context.Context, schemaissues.NonStructBase) (string, error) { return "", nil })
			trpcgo.MustQuery(r, "tag", func(context.Context, schemaissues.Tag) (string, error) { return "", nil })
			checkGeneratedZod(t, r, `import {TagSchema} from './schemas'; TagSchema.parse({name:'ok'}); if(TagSchema.safeParse({name:''}).success) throw new Error('unrelated validation lost');`)
		})
	}
}
