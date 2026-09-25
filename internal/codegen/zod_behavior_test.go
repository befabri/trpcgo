package codegen_test

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/befabri/trpcgo/internal/analysis"
	"github.com/befabri/trpcgo/internal/codegen"
	"github.com/befabri/trpcgo/internal/tstest"
	"github.com/befabri/trpcgo/internal/typemap"
)

// zodModuleBehavior specifies an exported schema by the documents it accepts
// and the issues it reports, rather than by its source text.
type zodModuleBehavior struct {
	schema string
	accept []string
	// reject maps a JSON document to a JSON object whose fields at least one
	// reported issue must have, usually including its path.
	reject map[string]string
}

// checkZodModule generates the schema module with both Zod styles, then
// type-checks it and runs the cases against it.
func checkZodModule(t *testing.T, procs []codegen.ProcEntry, defs []typemap.TypeDef, cases []zodModuleBehavior) {
	t.Helper()
	type data struct {
		Schema string            `json:"schema"`
		Accept []string          `json:"accept"`
		Reject map[string]string `json:"reject"`
	}
	encoded := make([]data, len(cases))
	for i, tc := range cases {
		encoded[i] = data{tc.schema, tc.accept, tc.reject}
	}
	payload, err := json.Marshal(encoded)
	if err != nil {
		t.Fatal(err)
	}
	script := `import * as schemas from "./schemas";
type Parser = { safeParse(input: unknown): { success: true } | { success: false; error: { issues: Record<string, unknown>[] } } };
const exported = schemas as unknown as Record<string, Parser>;
const cases: { schema: string; accept: string[] | null; reject: Record<string, string> | null }[] = ` + string(payload) + `;
const matches = (actual: Record<string, unknown>, want: Record<string, unknown>) => Object.entries(want).every(([key, value]) => JSON.stringify(actual[key]) === JSON.stringify(value));
const failures: string[] = [];
for (const test of cases) {
  const schema = exported[test.schema];
  if (!schema) { failures.push("missing " + test.schema); continue; }
  for (const input of test.accept ?? []) {
    const result = schema.safeParse(JSON.parse(input));
    if (!result.success) failures.push(test.schema + ": rejected " + input + " " + JSON.stringify(result.error.issues));
  }
  for (const [input, issue] of Object.entries(test.reject ?? {})) {
    const result = schema.safeParse(JSON.parse(input));
    const want = JSON.parse(issue);
    if (result.success) failures.push(test.schema + ": accepted " + input);
    else if (!result.error.issues.some((actual) => matches(actual, want))) failures.push(test.schema + ": " + input + " raised " + JSON.stringify(result.error.issues) + ", want " + issue);
  }
}
if (failures.length > 0) throw new Error("\n" + failures.join("\n"));
`
	for _, style := range []typemap.ZodStyle{typemap.ZodStandard, typemap.ZodMini} {
		name := map[typemap.ZodStyle]string{typemap.ZodStandard: "standard", typemap.ZodMini: "mini"}[style]
		t.Run(name, func(t *testing.T) {
			var module bytes.Buffer
			if err := codegen.WriteZodSchemas(&module, procs, defs, style, codegen.ZodOptions{}); err != nil {
				t.Fatal(err)
			}
			tstest.Run(t, map[string]string{"schemas.ts": module.String(), "behavior.ts": script}, "behavior.ts")
		})
	}
}

// fixtureZodInputs maps an analysis fixture to the procedures and definitions
// the schema writer receives.
func fixtureZodInputs(t *testing.T, name string) ([]codegen.ProcEntry, []typemap.TypeDef) {
	t.Helper()
	result, err := analysis.Analyze([]string{"."}, testdataDir(name))
	if err != nil {
		t.Fatal(err)
	}
	gen := codegen.Prepare(result, result.TypeMetas, nil)
	return gen.Procs, gen.Defs
}

// Tags validate their container and, after dive, each element, and report the
// issue at the element's path.
func TestZodSchemaFromEnhancedBehavior(t *testing.T) {
	procs, defs := fixtureZodInputs(t, "enhanced")
	checkZodModule(t, procs, defs, []zodModuleBehavior{{
		schema: "CreateUserInputSchema",
		accept: []string{`{"name":"Ada","email":"ada@example.com","tags":["x"]}`},
		reject: map[string]string{
			`{"name":"","email":"ada@example.com","tags":["x"]}`:                   `{"code":"too_small","path":["name"],"minimum":1}`,
			`{"name":"Ada","email":"ada","tags":["x"]}`:                            `{"code":"invalid_format","path":["email"],"format":"email"}`,
			`{"name":"Ada","email":"ada@example.com","tags":[]}`:                   `{"code":"too_small","path":["tags"],"minimum":1}`,
			`{"name":"Ada","email":"ada@example.com","tags":[""]}`:                 `{"code":"too_small","path":["tags",0],"minimum":1}`,
			`{"name":"Ada","email":"ada@example.com","tags":["` + long(51) + `"]}`: `{"code":"too_big","path":["tags",0],"maximum":50}`,
			`{"name":"Ada","email":"ada@example.com","tags":["x"],"extra":1}`:      `{"code":"unrecognized_keys"}`,
		},
	}})
}

// Element rules apply inside the array and container rules to the array.
func TestZodDiveArrayBehavior(t *testing.T) {
	procs := []codegen.ProcEntry{{Path: "items.create", ProcType: "mutation", InputTS: "CreateItemInput", OutputTS: "Item"}}
	defs := []typemap.TypeDef{{
		Name: "CreateItemInput",
		Kind: typemap.TypeDefInterface,
		Fields: []typemap.Field{
			{Name: "name", Type: "string", GoKind: "string", Validate: []typemap.ValidateRule{{Tag: "required"}, {Tag: "min", Param: "1"}, {Tag: "max", Param: "100"}}},
			{Name: "tags", Type: "string[]", GoKind: "slice",
				Validate:        []typemap.ValidateRule{{Tag: "required"}, {Tag: "min", Param: "1"}, {Tag: "max", Param: "10"}},
				ElementValidate: []typemap.ValidateRule{{Tag: "min", Param: "2"}, {Tag: "max", Param: "64"}},
				Element:         &typemap.ElementType{GoKind: "string"}},
			{Name: "labels", Type: "string[]", GoKind: "slice", Element: &typemap.ElementType{GoKind: "string"}},
		},
	}}
	checkZodModule(t, procs, defs, []zodModuleBehavior{{
		schema: "CreateItemInputSchema",
		accept: []string{`{"name":"a","tags":["ab","😀😀"],"labels":[""]}`},
		reject: map[string]string{
			`{"name":"` + long(101) + `","tags":["ab"],"labels":[]}`:                        `{"code":"too_big","path":["name"],"maximum":100}`,
			`{"name":"a","tags":[],"labels":[]}`:                                            `{"code":"too_small","path":["tags"],"minimum":1}`,
			`{"name":"a","tags":["a","b","c","d","e","f","g","h","i","j","k"],"labels":[]}`: `{"code":"too_big","path":["tags"],"maximum":10}`,
			`{"name":"a","tags":["a"],"labels":[]}`:                                         `{"code":"too_small","path":["tags",0],"minimum":2}`,
			`{"name":"a","tags":["` + long(65) + `"],"labels":[]}`:                          `{"code":"too_big","path":["tags",0],"maximum":64}`,
		},
	}})
}

// omitempty admits the zero value and leaves every other value to the rules.
// Only a pointer, or a JSON-omitted field, may be absent.
func TestZodOmitemptyBehavior(t *testing.T) {
	procs, defs := fixtureZodInputs(t, "omitempty")
	checkZodModule(t, procs, defs, []zodModuleBehavior{
		{
			schema: "ConfirmTOTPInputSchema",
			accept: []string{`{"code":"","current_code":"123456","name":"a"}`, `{"code":"123456","current_code":"","name":"a"}`},
			reject: map[string]string{
				`{"code":"123","current_code":"","name":"a"}`: `{"code":"too_small","path":["code"],"minimum":6,"exact":true}`,
				`{"code":"","current_code":"","name":""}`:     `{"code":"too_small","path":["name"],"minimum":1}`,
				`{"current_code":"","name":"a"}`:              `{"code":"invalid_type","path":["code"]}`,
			},
		},
		{
			schema: "OptionalEmailInputSchema",
			accept: []string{`{"backup_email":"","primary_email":"a@b.co"}`, `{"backup_email":"c@d.co","primary_email":"a@b.co","nickname":"abc"}`},
			reject: map[string]string{
				`{"backup_email":"x","primary_email":"a@b.co"}`:              `{"code":"invalid_format","path":["backup_email"],"format":"email"}`,
				`{"backup_email":"","primary_email":""}`:                     `{"code":"invalid_format","path":["primary_email"]}`,
				`{"backup_email":"","primary_email":"a@b.co","nickname":""}`: `{"code":"too_small","path":["nickname"],"minimum":3}`,
			},
		},
	})
}

func long(n int) string {
	return string(bytes.Repeat([]byte("a"), n))
}
