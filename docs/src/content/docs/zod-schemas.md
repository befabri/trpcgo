---
title: Zod Schemas
description: Generate Zod input schemas from Go validate tags and use them on the frontend.
---

Generate Zod schemas from your Go procedure inputs to reuse validation rules in frontend forms. The generator translates common `validate` tags; the supported tags are listed below, and [Zod Validation](/reference/zod-validation/) covers edge cases.

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

Zod generation emits schemas for named procedure input types and their dependencies, including recursive, generic, and anonymous nested types. It does not generate schemas for output-only types. Use a named input type when you need an exported schema for a procedure.

## Install Zod

Generated schemas need Zod 4.5.4 or newer. See [Compatibility](/reference/compatibility/#zod) for why.

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

Generated standard Zod:

```ts
import { z } from 'zod';

// Helpers defined earlier in the generated file.
declare function $trpcgoEmail(value: string): boolean;
declare function $trpcgoIssue(valid: (value: any) => boolean, issue: Record<string, unknown>): z.core.$ZodCheck;

export const CreateUserInputSchema = z
  .strictObject({
    name: z.string().min(1).max(100),
    email: z.string().check($trpcgoIssue((value) => $trpcgoEmail(String(value).replace(/\p{Surrogate}/gu, '�')), { code: 'invalid_format', format: 'email' })),
    role: z.enum(['', 'admin', 'editor', 'viewer']).optional(),
    bio: z.string().max(500).optional(),
  })
  .meta({ id: 'CreateUserInput' });
```

`email` uses a generated check that accepts the same addresses as the server's `email` rule. `omitempty` lets `role` be empty, so the enum includes `''`.

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

Convert numeric and boolean form values before parsing; generated schemas do not coerce strings into those types. Fields tagged `json:",string"` are the exception: their schemas expect the string.

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

Use [`.safeExtend()`](https://zod.dev/api#safeextend) to keep refinements from the generated schema. Generated object schemas are plain Zod objects, so `.shape`, `.pick()`, and `z.toJSONSchema()` work too.

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
- `WithValidator` validates decoded inputs at runtime. `StructValidator` applies `validate.Struct` to every struct in the input, including slice and map elements; see [Validation Option](/router-options/#validation-option).

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

trpcgo translates these `go-playground/validator` tags. Which rules apply to a field depends on its Go kind.

| Category | Tags |
| --- | --- |
| Required/optional | `required`, `omitempty`, `omitzero`, `omitnil`, `structonly`, `nostructlevel` |
| Containers | `dive`, `keys`, `endkeys`, `unique` |
| Length/range | `min`, `max`, `len`, `gt`, `gte`, `lt`, `lte` |
| Equality | `eq`, `ne` |
| Formats | `email`, `url`, `uuid`, `e164`, `jwt`, `base64`, `base64url`, `base64rawurl`, `ip`, `ipv4`, `ipv6`, `hostname`, `hostname_rfc1123`, `ulid`, `mac`, `cidrv4`, `cidrv6` |
| Character sets | `alphanum`, `alpha`, `alphanumunicode`, `alphaunicode`, `numeric`, `number`, `ascii`, `printascii`, `hexadecimal` |
| Strings | `lowercase`, `uppercase`, `startswith`, `endswith`, `contains`, `startsnotwith`, `endsnotwith`, `excludes`, `containsany`, `excludesall` |
| Enums | `oneof` |
| Cross-field | `gtefield`, `ltefield`, `gtfield`, `ltfield`, `eqfield`, `nefield` |

Comma-separated rules all apply, and `|` joins alternatives: `startswith=a|startswith=b` accepts either prefix. Alternatives can mix scalar and cross-field checks, such as `email|eqfield=Fallback`.

`required` follows validator. On non-pointer fields it rejects zero values such as `0`, `false`, and `""`. On a pointer it checks presence, so a pointer to zero passes. An empty map or slice passes too; use `min=1` to require an entry.

Unsupported tags appear as comments in the generated schema and are not enforced, so review them when you adopt generated validation. Malformed tags, such as `required,` or `required, email`, fail generation, since the Go validator panics on them.

## `omitempty` Semantics

There are two separate concepts:

- JSON tags and pointers control whether a key may be missing.
- Validator omission tags control whether later rules run for a zero value.

On a `string`, `validate:"omitempty,email"` allows an empty string or a valid email address. On a `*string`, a missing value skips validation, but a supplied value, even `""`, must be a valid email.

The validate tag alone does not make a field optional, and `json:",omitempty"` alone does not skip validation: a missing string decodes to `""` in Go, so an `email` rule still fails. See [Struct Tags](/struct-tags/#json-tags) for optional fields and JSON `null`.

Rule order matters. `omitempty,min=2` permits an empty string, while `min=2,omitempty` rejects it because the minimum is checked first.

`omitnil` skips only nil pointers, maps, and slices. `omitzero` looks through pointers: a pointer to `0` or `""` skips the later rules, and so does an empty map or slice, which `omitempty` still validates. See [Required And Omission](/reference/zod-validation/#required-and-omission) for `[]byte`, `structonly`, and `nostructlevel`.

## Arrays And `dive`

Rules before `dive` apply to the container. Rules after `dive` apply to elements.

```go
type Input struct {
    Tags []string `json:"tags" validate:"min=1,dive,min=1,max=50"`
}
```

Generated Zod checks that the array has at least one item and that each string has 1 to 50 characters.

Each additional `dive` descends another container level. For maps, the rules apply to values:

```go
type GroupInput struct {
    Members map[string][]string `json:"members" validate:"dive,min=1,dive,email"`
}
```

Each map value must be a nonempty array of email addresses. A map's own `min` and `max` count entries.

Use `keys` and `endkeys` immediately after `dive` to validate keys separately from values:

```go
type ContactsInput struct {
    Contacts map[string]string `json:"contacts" validate:"min=1,dive,keys,email,endkeys,required"`
}
```

This requires at least one entry, an email address as each key, and a nonempty string as each value.

As in validator, a slice or map of structs validates its elements only after `dive`; without it, the elements are checked for shape but not for their own rules. See [Containers](/reference/zod-validation/#containers) for integer keys, `unique`, `[]byte`, and fixed arrays.

## Cross-Field Validation

Cross-field tags generate object-level refinements using JSON field names.

```go
type RangeInput struct {
    Start int `json:"start"`
    End   int `json:"end" validate:"gtefield=Start"`
}
```

The generated schema checks the relationship between `end` and `start` after individual fields parse. The tag parameter uses the Go field name (`Start`); the generated comparison and error path use JSON names (`start` and `end`).

Comparisons follow Go kinds rather than TypeScript types: `eqfield` between an `int` and an `int64` fails, and string ordering tags such as `gtfield` compare byte lengths. Nested paths such as `eqfield=Address.Code` are not supported and fail generation. See [Cross-Field Rules](/reference/zod-validation/#cross-field-rules) for how each kind compares.

## Validate Raw JSON

Every generated module exports `decodeGoJSON`. It decodes JSON text as Go's `encoding/json` does, then validates the result with a schema. Use it when you have the raw text, such as a request body:

```ts
import { CreateUserInputSchema, decodeGoJSON } from '../gen/zod.js';

const result = decodeGoJSON(CreateUserInputSchema, body);
if (!result.success) {
  console.error(result.error.issues);
}
```

It returns the same result as `safeParse`. Malformed JSON fails validation instead of throwing. Bundlers drop `decodeGoJSON` from applications that never call it.

`JSON.parse` keeps only the last of a repeated key, and cannot tell that `"01"` and `"1"` name the same `map[int]` entry. When an object contains keys that collide like this, `safeParse` rejects it with an issue that points to `decodeGoJSON`. See [decodeGoJSON](/reference/zod-validation/#decodegojson) for how it matches Go.

## Validation Issues

Failed rules raise the issue Zod's own check would, so Zod writes the message and your locale translates it:

- Length and size rules raise `too_small` or `too_big` with the bound, and `exact` for `len`.
- Formats raise `invalid_format` with Zod's format name, such as `email`, `url`, `uuid`, or `datetime` for `time.Time`. Formats Zod has no name for use the validator tag, such as `hostname`.
- `oneof`, `eq`, and `required` on a `bool` raise `invalid_value` with the accepted values.

Rules without a Zod counterpart, such as `ne`, `excludesall`, `unique`, `required` on a number, and OR groups, raise a `custom` issue. Its message is fixed English text that Zod error maps do not replace. The failed rule is in `params.rule` in validate syntax, such as `"email|url"`; render your own wording from it where you display issues.

`zod/mini` loads no locale, so its messages read `Invalid input` until you configure one, for example with `z.config(z.locales.en())`. Issue codes and fields are the same in both styles.

## Custom Validation Rules

Custom validator tags and aliases need client counterparts. Declare them with `WithZodValidation`:

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

Register `shortname`, `even`, and `available` with your Go validator separately. `WithZodValidation` only affects generated schemas.

| Field | Effect |
| --- | --- |
| `Aliases` | Expands a tag into other rules, like validator's `RegisterAlias`. |
| `Rules` | Maps a custom tag to a TypeScript predicate. `GoKinds` limits the fields it applies to, and `Message` sets the issue message. |
| `Imports` | Imports modules as namespaces for predicates. Paths are relative to the generated file. |
| `StructRules` | Adds object-level checks to a named type's schema. |
| `TagName` | Reads another struct tag instead of `validate`. |
| `Strict` | Fails generation on unsupported tags and invalid parameters instead of leaving comments. |

`ServerOnly: true` marks a rule the client cannot check, such as a database lookup or a permission check. The client skips it, and skips any OR group that contains it, since the server-only branch might pass.

For this example, `validation.ts` next to the generated file can export:

```ts
export function even(value: number, _parameter: string): boolean {
  return value % 2 === 0;
}
```

A predicate receives the value and the rule parameter as a string, and passes only when it returns `true`. Throwing, returning a promise, or returning any other value fails. Predicates can also be inline functions, such as `"(value, parameter) => value.length >= Number(parameter)"`.

Scalar predicates see the value as Go decodes it; integer `json:",string"` fields and integer map keys arrive as `bigint`. A predicate also runs when its field is missing: a missing optional scalar arrives as its Go zero value, and a missing map or slice as `null`. See [Custom Rule Predicates](/reference/zod-validation/#custom-rule-predicates) for the rest.

`StructRules` keys are generated type names or fully qualified Go type names. Predicates and paths use JSON property names:

```go
StructRules: map[string][]zodconfig.StructRule{
    "RangeInput": {{
        Predicate: "data => data.end >= data.start",
        Message: "End must follow start",
        Path: []string{"end"},
    }},
},
```

For the CLI, save the same configuration as JSON and pass `--zod-config`:

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

Watch mode reloads the file when it changes. If the new configuration is invalid, the previously generated files stay.

## Zod-Only Omit

Use `zod_omit:"true"` to keep a field in TypeScript but skip its client validation.

```go
type CreateUserInput struct {
    Name      string `json:"name" validate:"required"`
    CSRFToken string `json:"csrfToken" zod_omit:"true"`
}
```

This is useful when transport or framework code supplies a field. The schema accepts the key with any value and does not require it, so add the value before sending the request if the server needs it. The server still decodes and validates the field. Cross-field rules that reference an omitted field are skipped, and so is any OR group that includes one.

## No Typed Inputs

If every procedure takes no input or an unnamed scalar such as `string`, there are no schemas to export. trpcgo writes no Zod file and removes a stale one at the output path. TypeScript output is unaffected.
