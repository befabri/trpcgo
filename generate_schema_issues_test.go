package trpcgo_test

import (
	"bytes"
	"context"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"

	"github.com/befabri/trpcgo"
	"github.com/befabri/trpcgo/internal/codegen"
	"github.com/befabri/trpcgo/internal/typemap"
	"github.com/befabri/trpcgo/testdata/schemaissues"
)

func TestZodSchemaSpecificRecursiveTypes(t *testing.T) {
	forEachGeneration(t, func(t *testing.T, mini, static bool) {
		dir := t.TempDir()
		symlinkNodeModules(t, dir)
		if static {
			pkg := parseFixture(t, "schemaissues", "testdata/schemaissues/types.go")
			m := typemap.NewMapper(map[string]typemap.TypeMeta{"schemaissues.Tags": {IsAlias: true}, "schemaissues.Links": {IsAlias: true}})
			input := m.Convert(pkg.Scope().Lookup("Node").Type())
			procs := []codegen.ProcEntry{{Path: "node", ProcType: "query", InputTS: m.Resolve(input), OutputTS: "string"}}
			writeStaticContract(t, dir, mini, procs, m.Defs())
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
const secret: unknown = parsed.secret;
// @ts-expect-error unvalidated fields require narrowing before use as strings
const secretString: string = parsed.secret;
if ('secret' in parsed) throw new Error('absent unvalidated field was materialized');
const opaque = { arbitrary: true };
if ((NodeSchema.parse({ ...tree, secret: opaque } as unknown).secret as unknown) !== opaque) throw new Error('unvalidated field was not preserved');
// @ts-expect-error schema types must not be exported as domain types
type Private = import('./schemas').Node;
void [date, name];
for (const input of [{ ...leaf, tags: [{ name: '' }] }, { ...leaf, inline: { z: 'bad' } }, { ...leaf, links: { next: { ...leaf, date: 1 } } }]) {
  if (NodeSchema.safeParse(input).success) throw new Error('invalid nested value accepted');
}
`
		writeTestSources(t, dir, map[string]string{"barrel.ts": "export * from './trpc';\nexport * from './schemas';\n", "validate.ts": script})
		assertTypeScriptCompiles(t, dir, "barrel.ts", "validate.ts")
		runTypeScript(t, dir, "validate.ts")
	})
}

func TestZodNonStructExtendsKeepsOtherSchemas(t *testing.T) {
	forEachStyle(t, func(t *testing.T, mini bool) {
		r := trpcgo.NewRouter(trpcgo.WithZodMini(mini))
		trpcgo.MustQuery(r, "time", func(context.Context, schemaissues.NonStructBase) (string, error) { return "", nil })
		trpcgo.MustQuery(r, "tag", func(context.Context, schemaissues.Tag) (string, error) { return "", nil })
		checkGeneratedZod(t, r, `import {TagSchema} from './schemas'; TagSchema.parse({name:'ok'}); if(TagSchema.safeParse({name:''}).success) throw new Error('unrelated validation lost');`)
	})
}

type malformedDiveParam struct {
	Value []string `json:"value" validate:"dive=1,required"`
}

type malformedDiveEmptyParam struct {
	Value []string `json:"value" validate:"dive=,required"`
}

type malformedDiveAlternative struct {
	Value []string `json:"value" validate:"required|dive"`
}

type malformedOmitAlternative struct {
	Value []string `json:"value" validate:"omitempty|required"`
}

type malformedOmitEmptyParam struct {
	Value []string `json:"value" validate:"omitempty=,required"`
}

func registerMalformedScope[T any](r *trpcgo.Router) {
	trpcgo.MustQuery(r, "scope", func(context.Context, T) (bool, error) { return true, nil })
}

// The invalid directive must survive both mappers until grammar validation. In
// particular, a malformed dive must not disappear at the first split.
func TestZodMalformedDirectiveContract(t *testing.T) {
	for _, tc := range []struct {
		tag, message string
		register     func(*trpcgo.Router)
	}{
		{"dive=1,required", "dive does not accept a parameter", registerMalformedScope[malformedDiveParam]},
		{"dive=,required", "dive does not accept a parameter", registerMalformedScope[malformedDiveEmptyParam]},
		{"required|dive", "dive cannot be used in an OR group", registerMalformedScope[malformedDiveAlternative]},
		{"omitempty|required", "omitempty cannot be used in an OR group", registerMalformedScope[malformedOmitAlternative]},
		{"omitempty=,required", "omitempty does not accept a parameter", registerMalformedScope[malformedOmitEmptyParam]},
	} {
		t.Run(tc.tag, func(t *testing.T) {
			fset := token.NewFileSet()
			source := fmt.Sprintf("package contract; type Input struct { Value []string `json:\"value\" validate:%q` }", tc.tag)
			syntax, err := parser.ParseFile(fset, "input.go", source, 0)
			if err != nil {
				t.Fatal(err)
			}
			pkg := typeCheck(t, "contract", fset, []*ast.File{syntax})
			forEachStyle(t, func(t *testing.T, mini bool) {
				router := trpcgo.NewRouter(trpcgo.WithZodMini(mini))
				t.Cleanup(func() { _ = router.Close() })
				tc.register(router)
				err := router.GenerateZod(filepath.Join(t.TempDir(), "schemas.ts"))
				if err == nil || !strings.Contains(err.Error(), ".value") || !strings.Contains(err.Error(), tc.message) {
					t.Fatalf("reflection diagnostic=%v; want field path and %q", err, tc.message)
				}
				mapper := typemap.NewMapper(nil)
				input := mapper.Resolve(mapper.Convert(pkg.Scope().Lookup("Input").Type()))
				var out bytes.Buffer
				err = codegen.WriteZodSchemas(&out, []codegen.ProcEntry{{InputTS: input}}, mapper.Defs(), zodStyle(mini))
				if err == nil || !strings.Contains(err.Error(), "Input.value") || !strings.Contains(err.Error(), tc.message) {
					t.Fatalf("static diagnostic=%v; want field path and %q", err, tc.message)
				}
				if out.Len() != 0 {
					t.Fatalf("invalid scope wrote a partial schema: %s", &out)
				}
			})
		})
	}
}
