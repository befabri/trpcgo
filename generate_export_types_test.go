package trpcgo_test

import (
	"bytes"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/befabri/trpcgo/internal/analysis"
	"github.com/befabri/trpcgo/internal/codegen"
	"github.com/befabri/trpcgo/internal/typemap"
)

// protocolFixture is a package holding a wire contract and nothing else: no
// procedure is registered in it, and it does not import trpcgo. It stands for a
// project whose TypeScript peer is not a tRPC client.
func protocolFixture() string {
	return filepath.Join("internal", "analysis", "testdata", "protocol")
}

func prepareFixture(t *testing.T, dir string, opts ...analysis.Option) *codegen.GenerateResult {
	t.Helper()
	result, err := analysis.Analyze([]string{"."}, dir, opts...)
	if err != nil {
		t.Fatal(err)
	}
	return codegen.Prepare(result, result.TypeMetas)
}

func TestExportedTypesSeedGenerationWithoutProcedures(t *testing.T) {
	gen := prepareFixture(t, protocolFixture(), analysis.WithExportedTypes())

	if len(gen.Procs) != 0 {
		t.Fatalf("fixture must hold no procedures, got %d", len(gen.Procs))
	}
	if want := []string{"Envelope", "Heartbeat", "Heartbeats", "JobID", "JobState", "Runner"}; !slices.Equal(gen.Roots, want) {
		t.Errorf("roots = %v, want %v", gen.Roots, want)
	}
	// A func type reaches TypeScript as nothing, and a generic declaration
	// reaches Zod as nothing. Silence would hide both.
	want := []codegen.SkippedType{
		{Name: "Batch", Reason: "generic declaration: a type, but only its instantiations get schemas"},
		{Name: "Dispatch", Reason: "no TypeScript representation"},
	}
	if !slices.Equal(gen.Skipped, want) {
		t.Errorf("skipped = %v, want %v", gen.Skipped, want)
	}

	var router bytes.Buffer
	if err := codegen.WriteAppRouter(&router, gen.Procs, gen.Defs); err != nil {
		t.Fatal(err)
	}
	ts := router.String()
	for _, want := range []string{
		"export interface Envelope",
		"export interface Heartbeat",
		`export type JobState = "pending" | "running" | "exited"`,
		"/** Heartbeat is pushed by a runner; nothing requests it. */",
		// Unexported, and reachable only as a field of Envelope.
		"export interface routing",
		// A generic declaration has no schema, but it is still a type.
		"export interface Batch<T>",
		"export type Heartbeats = Batch<Heartbeat>",
	} {
		if !strings.Contains(ts, want) {
			t.Errorf("generated types missing %q:\n%s", want, ts)
		}
	}
	// Nothing describes a router, so nothing may depend on tRPC.
	for _, unwanted := range []string{"@trpc/server", "AppRouter", "RouterInputs", "RouterOutputs", "$Query"} {
		if strings.Contains(ts, unwanted) {
			t.Errorf("generated types should not mention %q:\n%s", unwanted, ts)
		}
	}
}

func TestExportedTypesGetZodSchemas(t *testing.T) {
	gen := prepareFixture(t, protocolFixture(), analysis.WithExportedTypes())

	var schemas bytes.Buffer
	if err := codegen.WriteZodSchemas(&schemas, gen.Procs, gen.Defs, typemap.ZodStandard, codegen.ZodOptions{Roots: gen.Roots}); err != nil {
		t.Fatal(err)
	}
	zod := schemas.String()
	for _, want := range []string{
		// Reachable from no input anywhere: the point of the flag.
		"export const HeartbeatSchema",
		"export const EnvelopeSchema",
		// Transitively reached from a root.
		"export const routingSchema",
		// Validation rules describe the type, so they hold for a root too.
		"z.int().gte(0).lte(5)",
		"z.string().min(1)",
		// The concrete instantiation carries what the declaration cannot.
		"export const HeartbeatsSchema",
	} {
		if !strings.Contains(zod, want) {
			t.Errorf("generated schemas missing %q:\n%s", want, zod)
		}
	}
	// A type parameter has no schema to reference, so emitting one for the
	// generic declaration would name an identifier that does not exist.
	if strings.Contains(zod, "TSchema") {
		t.Errorf("schema references a type parameter:\n%s", zod)
	}

	// Without roots there is nothing to validate, and no file to write.
	var none bytes.Buffer
	if err := codegen.WriteZodSchemas(&none, gen.Procs, gen.Defs, typemap.ZodStandard); err != nil {
		t.Fatal(err)
	}
	if none.Len() != 0 {
		t.Errorf("schemas written with neither inputs nor roots:\n%s", none.String())
	}
}

// TestExportedTypesComposeWithProcedures pins the property that made an
// additive flag the right shape: one invocation, one merged definition set, so
// a type reached from both a procedure and an exported root is declared once.
func TestExportedTypesComposeWithProcedures(t *testing.T) {
	fixture := filepath.Join("internal", "analysis", "testdata", "basic")
	plain := prepareFixture(t, fixture)
	rooted := prepareFixture(t, fixture, analysis.WithExportedTypes())

	if len(plain.Procs) == 0 {
		t.Fatal("fixture must hold procedures for this to prove anything")
	}
	if len(rooted.Roots) == 0 {
		t.Fatal("fixture must hold exported types for this to prove anything")
	}

	var withoutFlag, withFlag bytes.Buffer
	if err := codegen.WriteAppRouter(&withoutFlag, plain.Procs, plain.Defs); err != nil {
		t.Fatal(err)
	}
	if err := codegen.WriteAppRouter(&withFlag, rooted.Procs, rooted.Defs); err != nil {
		t.Fatal(err)
	}
	// Adding roots a procedure already reached must not move the router half.
	if withoutFlag.String() != withFlag.String() {
		t.Errorf("router output changed when exported types were added:\n--- without ---\n%s\n--- with ---\n%s", withoutFlag.String(), withFlag.String())
	}
	if got := countPattern(withFlag.String(), `export interface User \{`); got != 1 {
		t.Errorf("User declared %d times, want 1", got)
	}

	var zodPlain, zodRooted bytes.Buffer
	if err := codegen.WriteZodSchemas(&zodPlain, plain.Procs, plain.Defs, typemap.ZodStandard); err != nil {
		t.Fatal(err)
	}
	if err := codegen.WriteZodSchemas(&zodRooted, rooted.Procs, rooted.Defs, typemap.ZodStandard, codegen.ZodOptions{Roots: rooted.Roots}); err != nil {
		t.Fatal(err)
	}
	// User is an output type: it has a schema only once it is a root itself.
	if strings.Contains(zodPlain.String(), "export const UserSchema") {
		t.Fatal("fixture no longer proves the point: User already had a schema")
	}
	if !strings.Contains(zodRooted.String(), "export const UserSchema") {
		t.Errorf("exported output type gained no schema:\n%s", zodRooted.String())
	}
	// Every schema the procedures produced survives the merge.
	for _, line := range strings.Split(zodPlain.String(), "\n") {
		name, ok := strings.CutPrefix(line, "export const ")
		if !ok {
			continue
		}
		if decl := "export const " + name; !strings.Contains(zodRooted.String(), decl) {
			t.Errorf("procedure schema %q lost when roots were added", strings.TrimSuffix(name, " = z.strictObject({"))
		}
	}
}

// TestExportedTypeSchemasParseAtRuntime runs the generated schemas against
// values, in both Zod styles. Text assertions cannot show that a schema for a
// type no procedure mentions actually parses.
func TestExportedTypeSchemasParseAtRuntime(t *testing.T) {
	const script = `
import type { Envelope, Heartbeat, Heartbeats } from './protocol.ts';
import { EnvelopeSchema, HeartbeatSchema, HeartbeatsSchema, RunnerSchema } from './schemas.ts';

// zod/mini keeps parse as a method but drops much of the chained surface, so
// rejection is asserted through parse alone.
function rejects(schema: { parse(value: unknown): unknown }, value: unknown, message: string) {
  try {
    schema.parse(value);
  } catch {
    return;
  }
  throw new Error(message);
}

const envelope = {
  id: 'job-1',
  state: 'running',
  payload: '',
  attempts: 2,
  route: { hop: 1 },
};

const parsed: Envelope = EnvelopeSchema.parse(envelope);
if (parsed.route.hop !== 1) throw new Error('unexported nested type lost its value');
if (parsed.runner !== undefined) throw new Error('omitempty pointer materialized');

// A type reachable from no procedure at all still validates.
const beat: Heartbeat = HeartbeatSchema.parse({ runner: 'r1', pending: 0 });
if (beat.runner !== 'r1') throw new Error('heartbeat schema dropped a field');

// A generic instantiation validates through the schema of its concrete form.
const batch: Heartbeats = HeartbeatsSchema.parse({ items: [{ runner: 'r1', pending: 0 }], count: 1 });
if (batch.items[0].runner !== 'r1') throw new Error('instantiated generic lost its element');

// Rules from validate tags hold for roots that are not procedure inputs.
rejects(EnvelopeSchema, { ...envelope, attempts: 9 }, 'max=5 not enforced on a root');
rejects(RunnerSchema, { name: '', tags: [] }, 'required not enforced on a root');
rejects(EnvelopeSchema, { ...envelope, extra: true }, 'unknown field accepted');
`

	forEachStyle(t, func(t *testing.T, mini bool) {
		gen := prepareFixture(t, protocolFixture(), analysis.WithExportedTypes())
		dir := t.TempDir()
		symlinkNodeModules(t, dir)

		var schemas, protocol bytes.Buffer
		if err := codegen.WriteZodSchemas(&schemas, gen.Procs, gen.Defs, zodStyle(mini), codegen.ZodOptions{Roots: gen.Roots}); err != nil {
			t.Fatal(err)
		}
		if err := codegen.WriteAppRouter(&protocol, gen.Procs, gen.Defs); err != nil {
			t.Fatal(err)
		}
		writeTestSources(t, dir, map[string]string{
			"schemas.ts":  schemas.String(),
			"protocol.ts": protocol.String(),
			"validate.ts": script,
		})
		assertTypeScriptCompiles(t, dir, "schemas.ts", "protocol.ts", "validate.ts")
		runTypeScript(t, dir, "validate.ts")
	})
}

// TestExportedTypesAcrossCollidingPackages pins that roots are tracked per
// declaration rather than per name. Two packages exporting User both earn a
// schema, under the disambiguated names the type output already uses.
func TestExportedTypesAcrossCollidingPackages(t *testing.T) {
	result, err := analysis.Analyze([]string{"./testdata/namecollision/..."}, ".", analysis.WithExportedTypes())
	if err != nil {
		t.Fatal(err)
	}
	gen := codegen.Prepare(result, result.TypeMetas)

	if want := []string{"AModelsUser", "BModelsUser"}; !slices.Equal(gen.Roots, want) {
		t.Errorf("roots = %v, want %v", gen.Roots, want)
	}

	var schemas bytes.Buffer
	if err := codegen.WriteZodSchemas(&schemas, gen.Procs, gen.Defs, typemap.ZodStandard, codegen.ZodOptions{Roots: gen.Roots}); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"export const AModelsUserSchema", "export const BModelsUserSchema"} {
		if !strings.Contains(schemas.String(), want) {
			t.Errorf("generated schemas missing %q:\n%s", want, schemas.String())
		}
	}
}
