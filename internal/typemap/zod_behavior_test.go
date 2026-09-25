package typemap

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/befabri/trpcgo/internal/tstest"
)

// zodMissing stands for an absent property in accept and reject lists.
const zodMissing = "<missing>"

// zodBehavior specifies a field schema by what it does rather than by its
// source text, which is free to change. Each input is the JSON value of the
// field inside an object; zodMissing omits it.
type zodBehavior struct {
	name   string
	field  Field
	accept []string
	// reject maps an input to a JSON object whose fields at least one
	// reported issue must have, such as {"code":"too_small","minimum":3}.
	reject map[string]string
	// schema, when set, is a JSON object whose fields the property's exported
	// JSON Schema must have. Only Zod's own checks appear there.
	schema string
}

// checkZodBehavior type-checks and runs every case with both Zod styles, one
// compiler and one runtime process per style.
func checkZodBehavior(t *testing.T, cases []zodBehavior) {
	t.Helper()
	for _, style := range []ZodStyle{ZodStandard, ZodMini} {
		name := map[ZodStyle]string{ZodStandard: "standard", ZodMini: "mini"}[style]
		t.Run(name, func(t *testing.T) {
			var body strings.Builder
			for _, tc := range cases {
				data, err := json.Marshal(map[string]any{"name": tc.name, "accept": tc.accept, "reject": tc.reject, "schema": tc.schema})
				if err != nil {
					t.Fatal(err)
				}
				body.WriteString("  { ...(" + string(data) + "), zod: z.strictObject({ value: " + ZodType(tc.field, style) + " }) },\n")
			}
			helpers, cases := HoistZodRuntimeHelpers(body.String())
			imports := `import { z } from "zod";`
			if style == ZodMini {
				imports = `import * as z from "zod/mini";`
			}
			script := imports + "\n" + helpers + `
type Parser = { safeParse(input: unknown): { success: true } | { success: false; error: { issues: Record<string, unknown>[] } } };
type Case = { name: string; accept: string[] | null; reject: Record<string, string> | null; schema: string; zod: z.core.$ZodType };
const cases: Case[] = [
` + cases + `];
const missing = ` + ZodStringLiteral(zodMissing) + `;
const matches = (actual: unknown, want: Record<string, unknown>) => Object.entries(want).every(([key, value]) => JSON.stringify((actual as Record<string, unknown>)[key]) === JSON.stringify(value));
const failures: string[] = [];
for (const test of cases) {
  const parse = (input: string) => (test.zod as unknown as Parser).safeParse(input === missing ? {} : { value: JSON.parse(input) });
  for (const input of test.accept ?? []) {
    const result = parse(input);
    if (!result.success) failures.push(test.name + ": rejected " + input + " " + JSON.stringify(result.error.issues));
  }
  for (const [input, issue] of Object.entries(test.reject ?? {})) {
    const result = parse(input);
    const want = JSON.parse(issue);
    if (result.success) failures.push(test.name + ": accepted " + input);
    else if (!result.error.issues.some((actual) => matches(actual, want))) failures.push(test.name + ": " + input + " raised " + JSON.stringify(result.error.issues) + ", want " + issue);
  }
  if (test.schema) {
    const property = (z.toJSONSchema(test.zod) as { properties: Record<string, unknown> }).properties.value;
    if (!matches(property, JSON.parse(test.schema))) failures.push(test.name + ": JSON Schema " + JSON.stringify(property) + ", want " + test.schema);
  }
}
if (failures.length > 0) throw new Error("\n" + failures.join("\n"));
`
			tstest.Run(t, map[string]string{"behavior.ts": script}, "behavior.ts")
		})
	}
}

func stringField(rules ...ValidateRule) Field {
	return Field{Name: "value", Type: "string", GoKind: "string", Validate: rules}
}

func kindField(tsType, goKind string, rules ...ValidateRule) Field {
	return Field{Name: "value", Type: tsType, GoKind: goKind, Validate: rules}
}

func issue(code string, fields ...string) string {
	return `{"code":"` + code + `"` + strings.Join(append([]string{""}, fields...), ",") + "}"
}

// Formats follow validator's grammar and raise Zod's invalid_format issue with
// Zod's own format name where one exists.
func TestZodFormatBehavior(t *testing.T) {
	format := func(name, tag, valid, invalid string) zodBehavior {
		return zodBehavior{name: name, field: stringField(ValidateRule{Tag: tag}), accept: []string{valid}, reject: map[string]string{invalid: issue("invalid_format", `"format":"`+name+`"`)}}
	}
	checkZodBehavior(t, []zodBehavior{
		format("email", "email", `"alice@example.com"`, `"alice"`),
		format("url", "url", `"https://example.com/path"`, `"not a url"`),
		format("uuid", "uuid", `"123e4567-e89b-12d3-a456-426614174000"`, `"123e4567"`),
		format("e164", "e164", `"+14155552671"`, `"415"`),
		format("jwt", "jwt", `"a.b.c"`, `"abc"`),
		format("base64", "base64", `"YWJj"`, `"abc"`),
		format("base64url", "base64url", `"YWJj"`, `"a+b/"`),
		format("ipv4", "ipv4", `"192.168.0.1"`, `"::1"`),
		format("ipv6", "ipv6", `"::1"`, `"192.168.0.1"`),
		format("hostname", "hostname", `"example.com"`, `"-example.com"`),
		format("hostname_rfc1123", "hostname_rfc1123", `"1example.com"`, `"bad_host"`),
		format("hexadecimal", "hexadecimal", `"0xff"`, `"xyz"`),
		format("ulid", "ulid", `"01ARZ3NDEKTSV4RRFFQ69G5FAV"`, `"01ARZ3"`),
		format("mac", "mac", `"00:1a:2b:3c:4d:5e"`, `"00:1a"`),
		format("cidrv4", "cidrv4", `"10.0.0.0/8"`, `"10.0.0.1/8"`),
		format("cidrv6", "cidrv6", `"2001:db8::/32"`, `"10.0.0.0/8"`),
		format("alpha", "alpha", `"abc"`, `"ab1"`),
		format("printascii", "printascii", `"a b"`, `"\t"`),
		format("lowercase", "lowercase", `"abc"`, `"Abc"`),
		{name: "uppercase requires a letter", field: stringField(ValidateRule{Tag: "uppercase"}), accept: []string{`"ABC"`}, reject: map[string]string{`""`: issue("invalid_format", `"format":"uppercase"`), `"Abc"`: issue("invalid_format", `"format":"uppercase"`)}},
		// Go decodes an unpaired surrogate escape to U+FFFD before validating.
		{name: "rules see Go-decoded strings", field: stringField(ValidateRule{Tag: "contains", Param: "\uFFFD"}), accept: []string{`"a\ud800"`}, reject: map[string]string{`"a"`: issue("invalid_format", `"format":"includes"`)}},
	})
}

// Go kinds keep their decoding bounds, and violations raise the issue Zod's
// own number and format checks raise.
func TestZodKindBehavior(t *testing.T) {
	checkZodBehavior(t, []zodBehavior{
		{name: "time", field: kindField("string", "time.Time"), accept: []string{`"2024-01-02T03:04:05Z"`, `"2024-01-02T03:04:05.123+02:00"`}, reject: map[string]string{`"yesterday"`: issue("invalid_format", `"format":"datetime"`)}},
		{name: "bytes", field: kindField("string", "[]byte"), accept: []string{`"YWJj"`, `""`}, reject: map[string]string{`"%%%"`: issue("invalid_format", `"format":"base64"`)}},
		{name: "int", field: kindField("number", "int"), accept: []string{`1`, `-9007199254740991`}, reject: map[string]string{`1.5`: issue("invalid_type")}},
		{name: "int8", field: kindField("number", "int8"), accept: []string{`-128`, `127`}, reject: map[string]string{`128`: issue("too_big", `"maximum":127`), `-129`: issue("too_small", `"minimum":-128`), `1.5`: issue("invalid_type", `"expected":"int"`)}},
		{name: "uint16", field: kindField("number", "uint16"), accept: []string{`0`, `65535`}, reject: map[string]string{`-1`: issue("too_small", `"minimum":0`), `65536`: issue("too_big")}},
		{name: "int32", field: kindField("number", "int32"), accept: []string{`2147483647`}, reject: map[string]string{`2147483648`: issue("too_big")}},
		{name: "uint32", field: kindField("number", "uint32"), accept: []string{`4294967295`}, reject: map[string]string{`-1`: issue("too_small")}},
		// Integers beyond 2^53 are inexact in JavaScript but decode in Go.
		{name: "int64", field: kindField("number", "int64"), accept: []string{`9007199254740993`, `-9223372036854775808`}, reject: map[string]string{`1.5`: issue("invalid_type", `"expected":"int"`), `9223372036854775808`: issue("too_big")}},
		{name: "uint64", field: kindField("number", "uint64"), accept: []string{`0`, `18446744073709550000`}, reject: map[string]string{`-1`: issue("too_small", `"minimum":0`), `1.5`: issue("invalid_type")}},
		{name: "float32 overflow", field: kindField("number", "float32"), accept: []string{`3.4e38`, `-3.4e38`}, reject: map[string]string{`1e39`: issue("too_big", `"origin":"number"`), `-1e39`: issue("too_small", `"origin":"number"`)}},
		{name: "float64", field: kindField("number", "float64"), accept: []string{`1.5`}, reject: map[string]string{`"1.5"`: issue("invalid_type")}},
		{name: "bool", field: kindField("boolean", "bool"), accept: []string{`false`}, reject: map[string]string{`0`: issue("invalid_type")}},
		{name: "typescript number", field: kindField("number", ""), accept: []string{`1.5`}},
		{name: "unknown", field: kindField("unknown", ""), accept: []string{`null`, `{"any":[1]}`}},
	})
}

// String lengths count runes, as validator does, and use Zod's own length
// checks, which count code points since Zod 4.5 and appear in JSON Schema.
func TestZodStringRuleBehavior(t *testing.T) {
	long := `"` + strings.Repeat("a", 51) + `"`
	checkZodBehavior(t, []zodBehavior{
		{name: "min and max", field: stringField(ValidateRule{Tag: "min", Param: "3"}, ValidateRule{Tag: "max", Param: "50"}),
			accept: []string{`"abc"`, `"😀😀😀"`},
			reject: map[string]string{`"ab"`: issue("too_small", `"minimum":3`), `"😀😀"`: issue("too_small"), long: issue("too_big", `"maximum":50`)},
			schema: `{"minLength":3,"maxLength":50}`},
		{name: "len counts runes", field: stringField(ValidateRule{Tag: "len", Param: "2"}), accept: []string{`"😀é"`}, reject: map[string]string{`"😀😀😀"`: issue("too_big", `"exact":true`), `"a"`: issue("too_small", `"exact":true`)}, schema: `{"minLength":2,"maxLength":2}`},
		{name: "gte is inclusive", field: stringField(ValidateRule{Tag: "gte", Param: "3"}), accept: []string{`"abc"`}, reject: map[string]string{`"ab"`: issue("too_small", `"minimum":3`)}},
		{name: "gt is exclusive", field: stringField(ValidateRule{Tag: "gt", Param: "3"}), accept: []string{`"abcd"`}, reject: map[string]string{`"abc"`: issue("too_small")}},
		{name: "lte is inclusive", field: stringField(ValidateRule{Tag: "lte", Param: "3"}), accept: []string{`"abc"`}, reject: map[string]string{`"abcd"`: issue("too_big", `"maximum":3`)}},
		{name: "lt is exclusive", field: stringField(ValidateRule{Tag: "lt", Param: "3"}), accept: []string{`"ab"`}, reject: map[string]string{`"abc"`: issue("too_big")}},
		{name: "lt zero rejects everything", field: stringField(ValidateRule{Tag: "lt", Param: "0"}), reject: map[string]string{`""`: issue("too_big", `"maximum":0`, `"inclusive":false`)}},
		{name: "base zero parameter", field: stringField(ValidateRule{Tag: "min", Param: "0x10"}), accept: []string{`"` + strings.Repeat("a", 16) + `"`}, reject: map[string]string{`"` + strings.Repeat("a", 15) + `"`: issue("too_small", `"minimum":16`)}},
		{name: "required rejects empty", field: stringField(ValidateRule{Tag: "required"}, ValidateRule{Tag: "max", Param: "100"}), accept: []string{`"a"`}, reject: map[string]string{`""`: issue("too_small", `"minimum":1`), zodMissing: issue("invalid_type")}},
		{name: "required keeps a larger minimum", field: stringField(ValidateRule{Tag: "required"}, ValidateRule{Tag: "min", Param: "8"}, ValidateRule{Tag: "max", Param: "128"}), accept: []string{`"12345678"`}, reject: map[string]string{`"1234567"`: issue("too_small", `"minimum":8`)}},
		{name: "required pointer accepts empty", field: Field{Name: "value", Type: "string", GoKind: "string", IsPointer: true, Validate: []ValidateRule{{Tag: "required"}, {Tag: "max", Param: "100"}}}, accept: []string{`""`}, reject: map[string]string{zodMissing: issue("invalid_type")}},
		{name: "hostname with a length", field: stringField(ValidateRule{Tag: "hostname"}, ValidateRule{Tag: "min", Param: "5"}), accept: []string{`"a.com"`}, reject: map[string]string{`"a.co"`: issue("too_small", `"minimum":5`), `"-bad.com"`: issue("invalid_format", `"format":"hostname"`)}},
		{name: "ulid with a maximum", field: stringField(ValidateRule{Tag: "ulid"}, ValidateRule{Tag: "max", Param: "26"}), accept: []string{`"01ARZ3NDEKTSV4RRFFQ69G5FAV"`}, reject: map[string]string{`"01ARZ3NDEKTSV4RRFFQ69G5FAVX"`: issue("too_big", `"maximum":26`)}},
		{name: "prefix, suffix and substring", field: stringField(ValidateRule{Tag: "startswith", Param: "https://"}, ValidateRule{Tag: "endswith", Param: ".json"}, ValidateRule{Tag: "contains", Param: "/api/"}),
			accept: []string{`"https://x/api/a.json"`},
			reject: map[string]string{`"http://x/api/a.json"`: issue("invalid_format", `"format":"starts_with"`), `"https://x/api/a.xml"`: issue("invalid_format", `"format":"ends_with"`), `"https://x/a.json"`: issue("invalid_format", `"format":"includes"`)}},
		{name: "literal parameters stay literal", field: stringField(ValidateRule{Tag: "contains", Param: `a)b"`}), accept: []string{`"xa)b\"x"`}, reject: map[string]string{`"ab"`: issue("invalid_format", `"includes":"a)b\""`)}},
		{name: "eq compares the string", field: stringField(ValidateRule{Tag: "eq", Param: "abc"}), accept: []string{`"abc"`}, reject: map[string]string{`"xyz"`: issue("invalid_value", `"values":["abc"]`)}},
		{name: "ne names the rule", field: stringField(ValidateRule{Tag: "ne", Param: "abc"}), accept: []string{`"xyz"`}, reject: map[string]string{`"abc"`: issue("custom", `"params":{"rule":"ne=abc"}`)}},
		{name: "excludesall names the rule", field: stringField(ValidateRule{Tag: "excludesall", Param: "!@"}), accept: []string{`"abc"`}, reject: map[string]string{`"a@b"`: issue("custom", `"params":{"rule":"excludesall=!@"}`)}},
		{name: "OR names both branches", field: stringField(ValidateRule{Alternatives: []ValidateRule{{Tag: "email"}, {Tag: "url"}}}), accept: []string{`"a@b.co"`, `"https://b.co"`}, reject: map[string]string{`"neither"`: issue("custom", `"message":"Invalid input: must satisfy email or url"`, `"params":{"rule":"email|url"}`)}},
		{name: "optional format checks the zero value when absent", field: Field{Name: "value", Type: "string", GoKind: "string", Optional: true, Validate: []ValidateRule{{Tag: "cidrv4"}}}, accept: []string{`"10.0.0.0/8"`}, reject: map[string]string{zodMissing: issue("invalid_type"), `"10.0.0.1/8"`: issue("invalid_format")}},
		{name: "optional without rules", field: Field{Name: "value", Type: "string", GoKind: "string", Optional: true}, accept: []string{zodMissing, `""`}},
	})
}

// Numeric parameters follow Go's parsing: base prefixes are integers, and a
// parameter the kind cannot hold is dropped rather than emitted.
func TestZodNumericRuleBehavior(t *testing.T) {
	checkZodBehavior(t, []zodBehavior{
		{name: "float minimum", field: kindField("number", "float64", ValidateRule{Tag: "min", Param: "0"}), accept: []string{`0`}, reject: map[string]string{`-1`: issue("too_small", `"minimum":0`)}, schema: `{"minimum":0}`},
		{name: "int range", field: kindField("number", "int", ValidateRule{Tag: "gte", Param: "1"}, ValidateRule{Tag: "lte", Param: "100"}), accept: []string{`1`, `100`}, reject: map[string]string{`0`: issue("too_small", `"minimum":1`), `101`: issue("too_big", `"maximum":100`)}, schema: `{"minimum":1,"maximum":100}`},
		{name: "int base zero parameter", field: kindField("number", "int", ValidateRule{Tag: "min", Param: "0x10"}), accept: []string{`16`}, reject: map[string]string{`15`: issue("too_small", `"minimum":16`)}},
		{name: "int drops a float parameter", field: kindField("number", "int", ValidateRule{Tag: "min", Param: "1e3"}), accept: []string{`5`}},
		{name: "uint drops a negative parameter", field: kindField("number", "uint", ValidateRule{Tag: "min", Param: "-1"}), accept: []string{`0`}, reject: map[string]string{`-1`: issue("too_small", `"minimum":0`)}},
		{name: "float exponent parameter", field: kindField("number", "float64", ValidateRule{Tag: "min", Param: "1e3"}), accept: []string{`1000`}, reject: map[string]string{`999`: issue("too_small", `"minimum":1000`)}},
		{name: "int len is one value", field: kindField("number", "int", ValidateRule{Tag: "len", Param: "0x10"}), accept: []string{`16`}, reject: map[string]string{`15`: issue("too_small"), `17`: issue("too_big")}},
		{name: "float len is one value", field: kindField("number", "float64", ValidateRule{Tag: "len", Param: "1.5"}), accept: []string{`1.5`}, reject: map[string]string{`1.4`: issue("too_small")}},
		{name: "bool drops a numeric rule", field: kindField("boolean", "bool", ValidateRule{Tag: "min", Param: "1"}), accept: []string{`false`}},
		{name: "float32 compares the rounded value", field: kindField("number", "float32", ValidateRule{Tag: "gt", Param: "0.1"}), accept: []string{`0.2`}, reject: map[string]string{`0.1`: issue("too_small", `"origin":"number"`, `"inclusive":false`)}},
		{name: "int required rejects zero", field: kindField("number", "int", ValidateRule{Tag: "required"}), accept: []string{`-1`}, reject: map[string]string{`0`: issue("custom", `"message":"Required"`, `"params":{"rule":"required"}`)}},
		// A bound that already rejects zero reports the violation once.
		{name: "int required with a positive minimum", field: kindField("number", "int", ValidateRule{Tag: "required"}, ValidateRule{Tag: "min", Param: "1"}), accept: []string{`1`}, reject: map[string]string{`0`: issue("too_small", `"minimum":1`)}, schema: `{"minimum":1}`},
		{name: "int required keeps its check when zero passes the bound", field: kindField("number", "int", ValidateRule{Tag: "required"}, ValidateRule{Tag: "min", Param: "0"}), accept: []string{`1`}, reject: map[string]string{`0`: issue("custom", `"message":"Required"`)}},
		{name: "bool required needs true", field: kindField("boolean", "bool", ValidateRule{Tag: "required"}), accept: []string{`true`}, reject: map[string]string{`false`: issue("invalid_value", `"values":[true]`)}},
	})
}

// oneof enumerates its values, so Zod reports invalid_value with them and
// JSON Schema lists them. Values Go would never match are not enumerated.
func TestZodOneofBehavior(t *testing.T) {
	checkZodBehavior(t, []zodBehavior{
		{name: "strings", field: stringField(ValidateRule{Tag: "oneof", Param: "admin editor viewer"}), accept: []string{`"admin"`}, reject: map[string]string{`"root"`: issue("invalid_value", `"values":["admin","editor","viewer"]`)}, schema: `{"enum":["admin","editor","viewer"]}`},
		{name: "quoted spaces", field: stringField(ValidateRule{Tag: "oneof", Param: "'red green' blue"}), accept: []string{`"red green"`}, reject: map[string]string{`"red"`: issue("invalid_value")}},
		{name: "ints", field: kindField("number", "int", ValidateRule{Tag: "oneof", Param: "1 2 3"}), accept: []string{`1`}, reject: map[string]string{`4`: issue("invalid_value", `"values":[1,2,3]`)}, schema: `{"enum":[1,2,3]}`},
		{name: "one int", field: kindField("number", "int32", ValidateRule{Tag: "oneof", Param: "404"}), accept: []string{`404`}, reject: map[string]string{`200`: issue("invalid_value")}},
		{name: "uints", field: kindField("number", "uint", ValidateRule{Tag: "oneof", Param: "0 1 2"}), accept: []string{`0`}, reject: map[string]string{`3`: issue("invalid_value")}},
		{name: "floats are not enumerated", field: kindField("number", "float64", ValidateRule{Tag: "oneof", Param: "0.5 1.0"}), accept: []string{`0.7`}},
		{name: "non-canonical ints are not enumerated", field: kindField("number", "int", ValidateRule{Tag: "oneof", Param: "01 2"}), accept: []string{`5`}},
		// An absent key decodes to the zero value, which oneof rejects.
		{name: "optional strings", field: Field{Name: "value", Type: "string", GoKind: "string", Optional: true, Validate: []ValidateRule{{Tag: "oneof", Param: "low high"}}}, accept: []string{`"low"`}, reject: map[string]string{zodMissing: issue("invalid_value"), `"mid"`: issue("invalid_value")}},
		{name: "optional ints", field: Field{Name: "value", Type: "number", GoKind: "int", Optional: true, Validate: []ValidateRule{{Tag: "oneof", Param: "1 2 3"}}}, accept: []string{`2`}, reject: map[string]string{zodMissing: issue("invalid_value", `"values":[1,2,3]`)}},
	})
}

// omitempty skips the later rules only for the zero value; the rules still
// raise their own issues for every other value.
func TestZodOmitemptyBehavior(t *testing.T) {
	omit := ValidateRule{Tag: "omitempty"}
	long := `"` + strings.Repeat("a", 51) + `"`
	checkZodBehavior(t, []zodBehavior{
		{name: "exact length", field: stringField(omit, ValidateRule{Tag: "len", Param: "6"}), accept: []string{`""`, `"abcdef"`}, reject: map[string]string{`"abc"`: issue("too_small", `"minimum":6`, `"exact":true`)}},
		{name: "format", field: stringField(omit, ValidateRule{Tag: "email"}), accept: []string{`""`, `"a@b.co"`}, reject: map[string]string{`"x"`: issue("invalid_format", `"format":"email"`)}},
		{name: "uuid", field: stringField(omit, ValidateRule{Tag: "uuid"}), accept: []string{`""`, `"123e4567-e89b-12d3-a456-426614174000"`}, reject: map[string]string{`"x"`: issue("invalid_format", `"format":"uuid"`)}},
		{name: "hostname", field: stringField(omit, ValidateRule{Tag: "hostname"}), accept: []string{`""`, `"example.com"`}, reject: map[string]string{`"-x"`: issue("invalid_format", `"format":"hostname"`)}},
		{name: "mac", field: stringField(omit, ValidateRule{Tag: "mac"}), accept: []string{`""`}, reject: map[string]string{`"zz"`: issue("invalid_format", `"format":"mac"`)}},
		{name: "range", field: stringField(omit, ValidateRule{Tag: "min", Param: "3"}, ValidateRule{Tag: "max", Param: "50"}), accept: []string{`""`, `"abc"`}, reject: map[string]string{`"ab"`: issue("too_small", `"minimum":3`), long: issue("too_big", `"maximum":50`)}},
		{name: "alone", field: stringField(omit), accept: []string{`""`, `"x"`}},
		{name: "int exclusive minimum", field: kindField("number", "int", omit, ValidateRule{Tag: "gt", Param: "0"}), accept: []string{`0`, `5`}, reject: map[string]string{`-1`: issue("too_small", `"minimum":0`, `"inclusive":false`)}},
		{name: "int inclusive minimum", field: kindField("number", "int", omit, ValidateRule{Tag: "gte", Param: "1"}), accept: []string{`0`, `1`}, reject: map[string]string{`-1`: issue("too_small", `"minimum":1`)}},
		{name: "optional key", field: Field{Name: "value", Type: "string", GoKind: "string", Optional: true, ValidateOmitempty: true, Validate: []ValidateRule{omit, {Tag: "len", Param: "6"}}}, accept: []string{zodMissing, `""`, `"abcdef"`}, reject: map[string]string{`"abc"`: issue("too_small")}},
		{name: "oneof admits zero", field: kindField("number", "int", omit, ValidateRule{Tag: "oneof", Param: "1 2"}), accept: []string{`0`, `2`}, reject: map[string]string{`3`: issue("invalid_value", `"values":[0,1,2]`)}},
		// Rules before an omission run on every value.
		{name: "earlier rules still run", field: stringField(ValidateRule{Tag: "min", Param: "2"}, omit, ValidateRule{Tag: "max", Param: "3"}), accept: []string{`"abc"`}, reject: map[string]string{`""`: issue("too_small", `"minimum":2`), `"abcd"`: issue("too_big")}},
		{name: "without omitempty", field: stringField(ValidateRule{Tag: "len", Param: "6"}), accept: []string{`"abcdef"`}, reject: map[string]string{`""`: issue("too_small", `"exact":true`)}},
	})
}
