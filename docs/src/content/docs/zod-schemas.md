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

Zod generation emits schemas for named procedure input types and their dependencies. It does not generate schemas for output-only types. Use a named input struct when you need an exported schema for a procedure.

## Install Zod

Generated schemas target Zod 4.

```bash
npm install zod@4
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

export const CreateUserInputSchema = z
  .object({
    name: z.string().min(1).max(100),
    email: z.email().min(1),
    role: z.enum(['admin', 'editor', 'viewer']).or(z.literal('')).optional(),
    bio: z.string().max(500).optional(),
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

Convert numeric and boolean form values before parsing; generated schemas do not coerce strings into those types.

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

Use [`.safeExtend()`](https://zod.dev/api#safeextend) to keep refinements from the generated schema. This also works with schemas for embedded and recursive Go types.

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
    trpcgo.WithValidator(validate.Struct),
)
```

Zod generation and server-side validation are related, but separate:

- `--zod` or `WithZodOutput` generates frontend schemas.
- `WithValidator(validate.Struct)` validates decoded inputs at runtime.

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

trpcgo supports common `go-playground/validator` tags including:

| Category | Tags |
| --- | --- |
| Required/optional | `required`, `omitempty` |
| Containers | `dive` |
| Length/range | `min`, `max`, `len`, `gt`, `gte`, `lt`, `lte` |
| Formats | `email`, `url`, `uuid`, `e164`, `jwt`, `base64`, `base64url`, `ip`, `ipv4`, `ipv6`, `hostname`, `hostname_rfc1123`, `ulid`, `mac`, `cidrv4`, `cidrv6` |
| Strings | `alphanum`, `alpha`, `numeric`, `lowercase`, `uppercase`, `startswith`, `endswith`, `contains`, `hexadecimal` |
| Enums | `oneof` |
| Cross-field | `gtefield`, `ltefield`, `gtfield`, `ltfield`, `eqfield`, `nefield` |

Unsupported tags appear as comments in the generated schemas.

Some mappings are narrower than their Go validator counterparts: `uuid` generates `z.uuidv4()`, and `ip` generates `z.ipv4()`. The generated schemas do not reproduce every server-side validation rule. For example, `required` makes numeric and boolean fields mandatory but does not reject `0` or `false`; add a form refinement if those values must be rejected in the browser.

## `omitempty` Semantics

There are two separate concepts:

- TypeScript optionality controls whether a field may be `undefined`.
- Validator `omitempty` allows the Go zero value to pass constraints.

On a `string`, `validate:"omitempty,email"` allows an empty string or a valid email address. On a `*string`, a missing value skips validation, but a supplied value must be a valid email.

The validate tag alone does not make a field optional. See [Struct Tags](/struct-tags/#json-tags) for optional fields and JSON `null`.

## Arrays And `dive`

Rules before `dive` apply to the container. Rules after `dive` apply to elements.

```go
type Input struct {
    Tags []string `json:"tags" validate:"min=1,dive,min=1,max=50"`
}
```

Generated Zod applies `.min(1)` to the array and `.min(1).max(50)` to each string.

Each additional `dive` descends another container level. For maps, the rules apply to values:

```go
type GroupInput struct {
    Members map[string][]string `json:"members" validate:"dive,min=1,dive,email"`
}
```

Each map value must be a nonempty array of email addresses. The generated field is:

```ts
members: z.record(z.string(), z.array(z.email()).min(1))
```

## Cross-Field Validation

Cross-field tags generate object-level refinements using JSON field names.

```go
type RangeInput struct {
    Start int `json:"start"`
    End   int `json:"end" validate:"gtefield=Start"`
}
```

The generated schema checks the relationship between `end` and `start` after individual fields parse. The tag parameter uses the Go field name (`Start`); the generated comparison and error path use JSON names (`start` and `end`).

## Zod-Only Omit

Use `zod_omit:"true"` to keep a field in TypeScript but leave it out of generated Zod schemas.

```go
type CreateUserInput struct {
    Name      string `json:"name" validate:"required"`
    CSRFToken string `json:"csrfToken" zod_omit:"true"`
}
```

This is useful when a field is supplied by transport or framework code rather than user form data. Add the omitted field before sending the request; it will not be present in the schema's parsed result. Cross-field refinements that reference an omitted field are also skipped.

## No Typed Inputs

If no procedures have typed inputs, runtime `GenerateZod` and dev watch remove stale Zod files. The CLI writes an empty file at the requested Zod path.
