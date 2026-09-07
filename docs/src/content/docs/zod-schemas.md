---
title: Zod Schemas
description: Generate Zod input schemas from Go validate tags and use them on the frontend.
---

Generate Zod schemas from your Go procedure inputs to reuse validation rules in frontend forms. The generator translates common `validate` tags; the supported tags and mapping limits are listed below.

## Enable Zod Output

With the static CLI:

```bash
go tool trpcgo generate -o web/gen/trpc.ts --zod web/gen/zod.ts ./...
```

With `go:generate`:

```go
//go:generate go tool trpcgo generate -o ../web/gen/trpc.ts --zod ../web/gen/zod.ts ./...
```

With dev watch:

```go
router := trpcgo.NewRouter(
    trpcgo.WithDev(true),
    trpcgo.WithTypeOutput("../web/gen/trpc.ts"),
    trpcgo.WithZodOutput("../web/gen/zod.ts"),
)
defer router.Close()
```

The dev watcher starts when you construct `trpc.NewHandler`. It requires both `WithDev(true)` and `WithTypeOutput`; setting `WithZodOutput` alone does not start generation.

Zod generation emits schemas for named procedure input types and their dependencies. It supports recursive structs, named recursive maps and slices, anonymous nested structs, and concrete generic inputs. Concrete generic schemas retain the original Go kinds, so `Box[int8]` and `Box[int16]` can have different numeric bounds even though both use TypeScript numbers. Output-only types do not receive schemas. Use a named input type when you need an exported schema for a procedure.

## Install Zod

Generated schemas target Zod 4.5.4 or a compatible newer 4.x release. The validation contracts run against 4.5.4. String `min`, `max`, `len`, `gt`, `gte`, `lt`, and `lte` count Unicode code points, as validator does, through `Array.from(value).length` refinements; Zod's native `.min()` and `.max()` count UTF-16 code units and are used only for a lower bound of zero or one, where both agree.

```bash
npm install zod@^4.5.4
```

## Basic Example

```go
type CreateUserInput struct {
    Name  string  `json:"name" validate:"required,min=1,max=100"`
    Email string  `json:"email" validate:"required,email"`
    Role  string  `json:"role,omitempty" validate:"omitempty,oneof=admin editor viewer"`
    Bio   *string `json:"bio,omitempty" validate:"omitempty,max=500"`
}
```

Generated standard Zod has this shape (the generated Go email helper is abbreviated here):

```ts
import { z } from 'zod';

// Generated helper implements the Go validator's email grammar.
declare function $trpcgoEmail(value: string): boolean;

export const CreateUserInputSchema = z
  .strictObject({
    name: z.string().min(1).check(z.refine((value) => Array.from(value).length <= 100)),
    email: z.string().check(z.refine($trpcgoEmail)),
    role: z.enum(['admin', 'editor', 'viewer']).or(z.literal('')).optional(),
    bio: z.string().check(z.refine((value) => Array.from(value).length <= 500)).optional(),
  })
  .meta({ id: 'CreateUserInput' });
```

Pass a plain object to the schema. For this example's string fields, you can convert a browser `FormData` object with `Object.fromEntries`:

```ts
import { CreateUserInputSchema } from '../gen/zod.js';

const values = Object.fromEntries(formData.entries());
const parsed = CreateUserInputSchema.safeParse(values);
if (!parsed.success) {
  setErrors(parsed.error.flatten().fieldErrors);
  return;
}

await client.user.create.mutate(parsed.data);
```

Convert numeric and boolean form values before parsing; generated schemas do not coerce strings into those types. Fields using Go’s `json:",string"` option intentionally retain their encoded string representation, as described below.

## Compose UI Schemas

Keep form rules in a separate schema so regenerating files preserves your changes. For example, add an email confirmation field to the generated user input:

```ts
import { z } from 'zod';
import { CreateUserInputSchema } from '../gen/zod.js';

export const CreateUserFormSchema = CreateUserInputSchema.safeExtend({
  confirmEmail: z.email(),
}).refine((value) => value.email === value.confirmEmail, {
  message: 'Email addresses must match.',
  path: ['confirmEmail'],
});
```

Use [`.safeExtend()`](https://zod.dev/api#safeextend) to keep refinements from generated object schemas. Modules that need Go decoding for integer maps or fixed arrays wrap object schemas in decoding pipelines; those pipelines do not expose object methods such as `.shape` and `.safeExtend()`.

Remove form-only fields before sending the request, since strict input decoding rejects unknown fields:

```ts
const { confirmEmail, ...input } = CreateUserFormSchema.parse(values);
await client.user.create.mutate(input);
```

## Server Validation

`validate` tags do not run on the server unless you configure a validator.

```go
import "github.com/go-playground/validator/v10"

validate := validator.New()

router := trpcgo.NewRouter(
    trpcgo.WithValidator(trpcgo.StructValidator(validate.Struct)),
)
```

Zod generation and server-side validation are related, but separate:

- `--zod` or `WithZodOutput` generates frontend schemas.
- `WithValidator(trpcgo.StructValidator(validate.Struct))` validates decoded inputs at runtime.

The callback runs once for every typed root input, including scalars, slices, maps, pointers, and typed nil values. Procedures with no input skip it. `StructValidator` walks root pointers, interfaces, and collections until it reaches struct values, then passes each to the supplied validator. That validator controls traversal of fields inside each struct. Primitive values, nil pointers, and structs with custom JSON or text unmarshaling are skipped. Pass your own callback to `WithValidator` when scalar or collection roots need rules of their own.

## Custom Validation Configuration

Use `WithZodValidation` to supply explicit client counterparts for your Go validator configuration. It supports alternate tag names, nested aliases, custom scalar or collection predicates, object checks, and namespace imports. The same configuration is available to static generation and the CLI.

```go
import "github.com/befabri/trpcgo/zodconfig"

config := zodconfig.Config{
    Aliases: map[string]string{
        "shortname": "required,min=2,max=40",
    },
    Rules: map[string]zodconfig.Rule{
        "even": {
            Predicate: "checks.even",
            GoKinds: []string{"int"},
            Message: "Must be even",
        },
        "available": {ServerOnly: true},
    },
    Imports: map[string]string{"checks": "./validation"},
    Strict: true,
}

router := trpcgo.NewRouter(
    trpcgo.WithValidator(trpcgo.StructValidator(validate.Struct)),
    trpcgo.WithZodValidation(config),
)
```

Register `shortname`, `even`, and `available` with your Go validator separately. `WithZodValidation` configures generation; it does not register server functions or inspect the callback passed to `WithValidator`.

The import path is relative to the generated TypeScript file. For this example, `validation.ts` can export:

```ts
export function even(value: number, _parameter: string): boolean {
  return value % 2 === 0;
}
```

Predicates may also be inline TypeScript functions, such as `"(value, parameter) => value.length >= Number(parameter)"`. They must return the boolean `true` to pass. Exceptions, promises, and other truthy values fail validation. Predicates run at parse time, including checks on missing fields. Scalar predicates receive the Go-decoded value: float32 rounding and Go string replacement apply, and integer `json:",string"` fields and integer map keys use `bigint`. Missing optional scalars use their Go zero value; missing maps and slices use `null` so a predicate can distinguish nil from an allocated empty collection. Collections and object checks otherwise receive the parsed JSON representation; they are not Go reflection objects. The rule parameter is a decoded string, including validator's escaped commas and pipes.

Set `TagName: "check"` to read `check` instead of `validate`. Aliases expand before resolving Go field names and before compiling `dive` and key scopes. Alias cycles, malformed scopes, aliases used as OR branches or with parameters, and custom rules applied outside their declared `GoKinds` fail generation. Existing built-in mappings cannot be overridden; use a distinct tag name when changing their behavior.

`StructRules` attaches checks to a generated named object schema. Use its generated type name or fully qualified Go type identity:

```go
StructRules: map[string][]zodconfig.StructRule{
    "RangeInput": {{
        Predicate: "data => data.end >= data.start",
        Message: "End must follow start",
        Path: []string{"end"},
    }},
},
```

Predicates and paths use JSON property names. A missing, ambiguous, or non-struct target is an error. These are explicit schema checks, not a translation of Go struct-level callbacks; attach checks for flattened embedded fields to the containing object schema.

`Strict: true` rejects unsupported tags and invalid scalar rules instead of emitting only comments. `ServerOnly: true` explicitly exempts a rule from client enforcement, including strict generation. If such a rule is part of an OR group, the client skips that group because the server-only alternative could satisfy it. Use this for database, authorization, filesystem, or service-dependent checks that belong on the server.

For CLI generation, save the same configuration as JSON and pass `--zod-config`:

```json
{
  "aliases": {"shortname": "required,min=2,max=40"},
  "rules": {"even": {"predicate": "checks.even", "goKinds": ["int"]}},
  "imports": {"checks": "./validation"},
  "strict": true
}
```

```bash
go tool trpcgo generate --zod web/gen/zod.ts --zod-config zod-validation.json ./...
```

Watch mode reloads the configuration when it changes. Invalid configuration leaves previously generated files intact.

## Standard Zod Vs zod/mini

Use `--zod-mini` or `WithZodMini(true)` to emit `zod/mini` functional syntax.

```bash
go tool trpcgo generate -o web/gen/trpc.ts --zod web/gen/zod.ts --zod-mini ./...
```

```go
router := trpcgo.NewRouter(
    trpcgo.WithDev(true),
    trpcgo.WithTypeOutput("../web/gen/trpc.ts"),
    trpcgo.WithZodOutput("../web/gen/zod.ts"),
    trpcgo.WithZodMini(true),
)
defer router.Close()
```

Standard Zod output includes `.meta({ id: "TypeName" })` and `.describe(...)` from field documentation. The generator omits these metadata calls from `zod/mini` output.

## Supported Validate Tags

trpcgo translates the following common `go-playground/validator` tags. Support depends on the Go kind and the scope in which a rule appears.

| Category | Tags |
| --- | --- |
| Required/optional | `required`, `omitempty`, `omitzero`, `omitnil`, `structonly`, `nostructlevel` |
| Containers | `dive`, `keys`, `endkeys`, `unique` for comparable values, `unique=Field` for struct elements |
| Length/range | `min`, `max`, `len`, `gt`, `gte`, `lt`, `lte` |
| Scalar equality | `eq`, `ne` |
| Formats | `email`, `url`, `uuid`, `e164`, `jwt`, `base64`, `base64url`, `base64rawurl`, `ip`, `ipv4`, `ipv6`, `hostname`, `hostname_rfc1123`, `ulid`, `mac`, `cidrv4`, `cidrv6` |
| Character sets | `alphanum`, `alpha`, `alphanumunicode`, `alphaunicode`, `numeric`, `number`, `ascii`, `printascii`, `hexadecimal` |
| Strings | `lowercase`, `uppercase`, `startswith`, `endswith`, `contains`, `startsnotwith`, `endsnotwith`, `excludes`, `containsany`, `excludesall` |
| Enums | `oneof` |
| Cross-field | `gtefield`, `ltefield`, `gtfield`, `ltfield`, `eqfield`, `nefield` |

Comma-separated rules all apply, and `|` joins alternatives. For example, `startswith=a|startswith=b` accepts either prefix. Alternatives can combine scalar and cross-field checks, such as `email|eqfield=Fallback`. Validator’s `0x2C` and `0x7C` escapes represent literal commas and pipes inside parameters.

Constraints are retained when combined with formats: `email,oneof=admin@example.com` must satisfy both rules. `numeric` on a numeric Go field retains its numeric schema; it does not apply a string regular expression.

For non-pointer numeric and boolean fields, `required` rejects `0` and `false`. For pointer fields, `required` checks presence: a non-nil pointer to zero is valid. On maps and slices, an empty but present collection satisfies `required`; use `min=1` to require an entry.

Unsupported tags and invalid scalar parameters appear as comments in generated schemas. Malformed container scopes produce generation errors. Comments do not enforce the omitted rule, so review them when adopting generated validation. See the compatibility limits below.

## `omitempty` Semantics

JSON and TypeScript tags control property optionality. Validator omission rules control whether later validation runs on the decoded Go value.

On a `string`, `validate:"omitempty,email"` permits the empty string and allows omission. On a `*string`, omission skips validation, but a supplied empty string must still pass `email` and therefore fails. `omitnil` skips nil pointers, maps, and slices without treating scalar zero values as empty.

`omitzero` tests the dereferenced value instead: a pointer to `0` or `""` skips later rules, and so does an allocated empty slice or map, which `omitempty` still validates. Rules written before an omission tag always run: `required,omitempty,min=3` rejects the empty string through `required` and only skips `min` for it.

`structonly` and `nostructlevel` end a field's rule list; validator never runs the rules after them, so the generator drops those rules too. On a struct-typed field both directives skip the nested struct's own validation in Go, so the field uses a private variant of that struct's schema that checks the JSON shape and applies none of its rules.

Rule order matters. `omitempty,min=2` permits an empty string, while `min=2,omitempty` rejects it because the minimum is checked first. Similarly, `required,omitempty` still rejects a missing or zero scalar value.

`json:",omitempty"` alone does not skip validation. An absent optional string decodes to `""` in Go, so a remaining `email` rule still fails. For maps and slices, validator `omitempty` skips nil values; it does not skip a present empty collection with a failing size rule.

Optional properties do not automatically accept JSON `null`. See [Struct Tags](/struct-tags/#json-tags) for the TypeScript mapping.

## Arrays And `dive`

Rules before `dive` apply to the container. Rules after `dive` apply to elements. validator enters struct elements only through `dive`: without it, a slice or map of structs uses a private variant of the element schema that checks the JSON shape but applies none of the element type's rules, matching the server. Struct fields and pointers to structs are always entered.

```go
type Input struct {
    Tags []string `json:"tags" validate:"min=1,dive,min=1,max=50"`
}
```

Generated Zod checks that the array contains at least one item and that each string contains between 1 and 50 characters.

Each additional `dive` descends another container level. For maps, the rules apply to values:

```go
type GroupInput struct {
    Members map[string][]string `json:"members" validate:"dive,min=1,dive,email"`
}
```

Each map value must be a nonempty array of email addresses. A map’s own `min` and `max` rules count entries.

Use `keys` and `endkeys` immediately after `dive` to validate keys separately from values:

```go
type ContactsInput struct {
    Contacts map[string]string `json:"contacts" validate:"min=1,dive,keys,email,endkeys,required"`
}
```

This requires at least one entry, an email address as each key, and a nonempty string as each value. Integer map keys are validated as encoded JSON object keys, including their Go integer bounds. Additional `dive` directives can descend through nested maps and arrays.

Go fixed arrays keep their declared length. Generated schemas pad short arrays with Go zero values and skip surplus entries before validating. Padding may produce `null` pointers, maps, or slices, including inside nested structs. Generated public types include those values in fixed-array positions, so parsed data remains assignable to `RouterInputs` and `RouterOutputs`; ordinary named struct declarations and explicit `tstype` overrides retain their existing policy.

For fixed arrays, `required` rejects an all-zero Go array, and `omitempty` skips later rules, including `dive`, for that zero value. A non-nil pointer or allocated empty map/slice inside an array is nonzero. Rules before `omitempty` still run, and omitted optional arrays are checked against the zero array Go creates. Element validators run only when `dive` is reached; wire type and range checks still apply. Zero-value rules that depend on hidden fields, `time.Time` location identity, or the distinct Go zeros of `json.Number` and `json.RawMessage` produce generation errors rather than guessing from JSON.

Integer keys normalize to decimal strings before validation: `"01"`, `"+1"`, and `"1"` identify the same Go map entry. Container length, `unique`, key rules, and `dive` run on the resulting map. Parsed output uses canonical keys without mutating the input.

When inputs include integer maps or fixed arrays, the generated module exports `parseGoJSON`. Use it when validating raw JSON that can contain colliding key spellings:

```ts
import { InputSchema, parseGoJSON } from './schemas';

const result = InputSchema.safeParse(
  parseGoJSON('{"values":{"01":7,"1":2}}'),
);
// For a map[int]int field, the parsed map is { "1": 2 }.
```

This parser retains object entry order, including repeated keys. Ordinary `JSON.parse` loses ordering needed to reproduce Go's overwrite behavior. Schemas reject colliding aliases in ordinary JavaScript objects with an error directing callers to `parseGoJSON`. Objects with distinct decoded keys work with ordinary `safeParse`. Overwritten values still undergo JSON type and range checks; validator constraints apply to the final entries.

For raw parsed inputs, the generated decoding stage also merges repeated struct fields according to their Go types. Repeated map fields combine entries, while each map entry decodes into a fresh value. A later valid field cannot hide an earlier JSON type or range error. This decoding stage preserves the caller's input and runs before schema validation.

For `[]byte`, the JSON value is Base64. Size constraints count decoded bytes, and element rules validate those bytes. Maps whose keys are named Go scalars accept any valid key of that scalar type. Declared constants neither require entries nor restrict which keys are allowed.

## Cross-Field Validation

Cross-field tags generate object-level refinements using JSON field names.

```go
type RangeInput struct {
    Start int `json:"start"`
    End   int `json:"end" validate:"gtefield=Start"`
}
```

The generated schema checks the relationship between `end` and `start` after individual fields parse. The tag parameter uses the Go field name (`Start`); the generated comparison and error path use JSON names (`start` and `end`).

Comparisons follow the original Go kinds:

- Numbers compare decoded numeric values. Integer fields encoded with `json:",string"` compare without losing integer precision.
- String `eqfield` and `nefield` compare contents. String ordering tags compare UTF-8 byte lengths, as Go’s field validators do.
- Array, slice, and map `eqfield` and `nefield` compare lengths, rather than element contents.
- `time.Time` comparisons use instants, normalize timezone offsets, and preserve nanoseconds.

Different Go kinds can compare differently even when their TypeScript types match: `eqfield` between an `int` and an `int64` fails. Ordered cross-field rules on collections follow validator’s reflection behavior; use scalar container bounds such as `min=2` or `gt=1` for size constraints. A target that JSON never sets, such as an unexported field or one tagged `json:"-"`, keeps its Go zero value, and the refinement compares against that zero.

Refinements retain pointer-presence checks, ordered omission, and the conditions under which embedded pointer fields exist. OR alternatives produce one constraint and one error path. A missing direct Go field name causes comparison to fail, except `nefield`, which succeeds when its target is missing. Nested paths such as `eqfield=Address.Code` produce a generation error; nested namespaces and `*csfield` rules are not implemented.

## JSON Encodings And Numeric Bounds

Integer schemas enforce integral values and Go bounds where JavaScript can represent them. Ordinary JavaScript numbers cannot preserve every `int64` or `uint64` value, and Go `int` currently uses Zod’s safe-integer range.

Use the JSON string option when a full-range integer must remain exact:

```go
type LedgerInput struct {
    Amount int64 `json:"amount,string" validate:"gte=0"`
}
```

The generated TypeScript property is a string, and the Zod schema validates the encoded integer with `BigInt`. Parsing preserves the string sent to the server. The option also preserves the wire encoding for supported floating-point, boolean, and string fields.

Quoted scalar validation handles the string `"null"` using Go's zero-value and nil-pointer rules, while preserving its wire spelling.

`time.Time` uses the Go JSON timestamp grammar, including timezone offsets, comma fractions, and fallback spellings accepted by Go. Parsing preserves the original timestamp string; comparisons account for its offset and fractional nanosecond precision.

`float32` fields validate values and numeric rule parameters after rounding to Go's floating point representation. For example, `1.00000001` compares as `1`. Validation preserves the original JSON number or quoted numeric string in the parsed output.

Quoted floats follow Go's JSON string-option grammar, including hexadecimal floats and digit separators. The shared decoder rounds directly to the destination float size, and is also used by cross-field comparisons. String predicates compare the decoded Go text, replacing unpaired surrogate escapes with the replacement character while preserving valid surrogate pairs.

Declared Go constants do not restrict the underlying scalar type. Generated schemas accept unnamed values that fit the Go type, including integer bounds. TypeScript aliases retain known literals for autocomplete and also allow the underlying scalar. Add `validate:"oneof=..."` when the server must reject other values; the generated schema enforces that explicit constraint.

## Compatibility Limits

Generated schemas cover common frontend validation, not every behavior of a configured Go validator.

- Format adapters are tested against Go 1.26 and validator v10.30.4. Email, URL, IP, and timestamps use Go-derived parsers; Unicode predicates use the generator toolchain's Go Unicode tables. Upstream changes to Go or validator still require compatibility tests and updates.
- Arbitrary Go callbacks and validator options are not inferred from `WithValidator`; provide explicit portable counterparts through `WithZodValidation`. Conditional required/excluded tags and nested field namespaces remain unsupported. Noncomparable `unique` values (which panic in the backend), nested pointer identity, and unavailable selected fields produce generation errors.
- JavaScript decoding can lose numeric precision and original JSON spelling. Generated schemas cannot recover that information. For example, Go's `time.Time.UnmarshalJSON` can distinguish escaped timestamp characters that ordinary JSON parsing has already decoded. A `json.Number` field is a `number` in TypeScript and its schema, so a quoted numeric string that Go also accepts is rejected, and its rules compare the decimal text JavaScript prints, as validator compares the string Go stored.
- `parseGoJSON` retains source order for generated decoding; it is not a complete Go JSON decoder. Custom JSON/text unmarshaling is not inferred; unsupported map key kinds produce a generation error.
- Generated objects reject unknown properties by default, matching strict server decoding. `WithStrictInput(false)` generates loose objects that preserve unknown properties. Parsing an ordinary JavaScript object cannot discover unknown properties that were overwritten during earlier JSON parsing; the raw decoding entry point checks each source occurrence where available. Nullability, coercion, defaults, and transformations are not automatically inferred.

Server validation remains necessary. `WithValidator` runs once for every typed procedure input, including scalar, slice, map, pointer, and typed nil values. Procedures without an input skip it. The callback must support the root types your procedures accept: `validator.Struct` is appropriate for struct inputs, while other roots may need `validator.Var` or a custom dispatcher.

## Zod-Only Omit

Use `zod_omit:"true"` to keep a field in TypeScript while skipping its client validation. The generated object shape contains an optional property typed with the field's TypeScript type and no runtime check (`z.custom<string>().optional()`), so strict schemas still accept this known key and parsed values remain assignable to the procedure input. Named type references, which the schema module cannot import, appear as `unknown`.

```go
type CreateUserInput struct {
    Name      string `json:"name" validate:"required"`
    CSRFToken string `json:"csrfToken" zod_omit:"true"`
}
```

This is useful when transport or framework code supplies a field. The property may be absent during parsing; if supplied, its value is preserved without client validation, including in the raw decoding entry point. Populate these fields before using parsed data as the complete procedure input when the Go type requires them. Other unknown properties are still rejected by default. The Go server still decodes and validates the supplied field. Cross-field refinements that reference an omitted field are also skipped. If an OR constraint contains an omitted reference, the entire constraint is skipped because the omitted alternative might satisfy it.

## No Typed Inputs

If no procedures have typed inputs, runtime `GenerateZod` and dev watch remove stale Zod files. The CLI writes an empty file at the requested Zod path.
