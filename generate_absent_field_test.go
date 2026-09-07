package trpcgo_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/befabri/trpcgo"
)

// AbsentProbe pairs a rule the zero value satisfies with one it fails, so the
// generated type and schema must agree about omitting each property.
type AbsentProbe struct {
	OptMax   string `json:"optMax,omitempty" validate:"max=256"`
	OptMin   string `json:"optMin,omitempty" validate:"min=1"`
	OptReq   string `json:"optReq,omitempty" validate:"required"`
	PlainMax string `json:"plainMax" validate:"max=256"`
}

// TestAbsentFieldSchemaMatchesGeneratedType covers the pair a text assertion
// cannot: the schema's structural optionality has to match the TypeScript type,
// while the values it accepts have to match what Go accepts after decoding. A
// property the type offers to omit must be omittable in the schema, and one
// whose zero value fails its rules must still be refused when absent.
func TestAbsentFieldSchemaMatchesGeneratedType(t *testing.T) {
	const script = `
import type { input, output } from 'zod';
import type { Assert, Equal } from './assertions';
import type { AbsentProbe } from './trpc.ts';
import { AbsentProbeSchema } from './schemas.ts';

type Expected = {
  optMax?: string;
  optMin?: string;
  optReq: string;
  plainMax: string;
};

// The generated type and the schema describe one contract, so neither may
// declare a property optional that the other requires.
type TypeShape = Assert<Equal<AbsentProbe, Expected>>;
type SchemaInput = Assert<Equal<input<typeof AbsentProbeSchema>, Expected>>;
type SchemaOutput = Assert<Equal<output<typeof AbsentProbeSchema>, Expected>>;

function accepts(value: unknown): boolean {
  try {
    AbsentProbeSchema.parse(value);
    return true;
  } catch {
    return false;
  }
}

const complete = { optMax: 'a', optMin: 'a', optReq: 'a', plainMax: 'a' };
if (!accepts(complete)) throw new Error('a fully populated value was rejected');

// max accepts the Go zero value, so omitting the property stays valid.
const { optMax: _optMax, ...withoutMax } = complete;
if (!accepts(withoutMax)) throw new Error('optMax could not be omitted');

// min rejects the Go zero value, so an absent property decodes to something
// the server refuses, and the schema has to refuse it too.
const { optMin: _optMin, ...withoutMin } = complete;
if (accepts(withoutMin)) throw new Error('optMin was omitted despite failing min=1 when absent');

const { optReq: _optReq, ...withoutReq } = complete;
if (accepts(withoutReq)) throw new Error('optReq was omitted despite being required');
`

	forEachStyle(t, func(t *testing.T, mini bool) {
		r := trpcgo.NewRouter(trpcgo.WithZodMini(mini))
		t.Cleanup(func() { _ = r.Close() })
		trpcgo.MustQuery(r, "probe", func(context.Context, AbsentProbe) (string, error) { return "", nil })

		dir := t.TempDir()
		symlinkNodeModules(t, dir)
		copyTypeScriptAssertions(t, dir)
		if err := r.GenerateTS(filepath.Join(dir, "trpc.ts")); err != nil {
			t.Fatal(err)
		}
		if err := r.GenerateZod(filepath.Join(dir, "schemas.ts")); err != nil {
			t.Fatal(err)
		}
		writeTestSources(t, dir, map[string]string{"contract.ts": script})
		assertTypeScriptCompiles(t, dir, "trpc.ts", "schemas.ts", "contract.ts")
		runTypeScript(t, dir, "contract.ts")
	})
}
