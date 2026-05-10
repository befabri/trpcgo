---
title: Zod Schemas
description: Generate Zod input schemas from Go validate tags and use them on the frontend.
---

trpcgo can generate Zod schemas for procedure input types. This lets frontend form validation share the same constraints described by your Go structs.

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
```

Zod generation targets typed procedure input types and their dependencies. It does not generate schemas for output-only types.

## Install Zod

Generated schemas target Zod 4.

```bash
npm install zod
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

Generated standard Zod resembles:

```ts
export const CreateUserInputSchema = z
  .object({
    name: z.string().min(1).max(100),
    email: z.email(),
    role: z.enum(['admin', 'editor', 'viewer']).optional().or(z.literal('')),
    bio: z.string().max(500).optional().or(z.literal('')),
  })
  .meta({ id: 'CreateUserInput' });
```

Use it in the frontend:

```ts
import { CreateUserInputSchema } from '../gen/zod.js';

const parsed = CreateUserInputSchema.safeParse(formData);
if (!parsed.success) {
  setErrors(parsed.error.flatten().fieldErrors);
  return;
}

await client.user.create.mutate(parsed.data);
```

## Compose UI Schemas

Generated schemas are useful as a server-contract base, but forms often need UI-specific rules. Compose them with normal Zod helpers instead of editing generated files:

```ts
import { z } from 'zod';
import { CreateScheduleInputSchema } from '../gen/zod.js';

export const ScheduleFormSchema = CreateScheduleInputSchema.pick({
  broadcaster_id: true,
  quality: true,
  has_min_viewers: true,
  min_viewers: true,
}).extend({
  broadcaster_id: z.string().min(1).regex(/^\d+$/),
}).superRefine((value, ctx) => {
  if (value.has_min_viewers && value.min_viewers == null) {
    ctx.addIssue({
      code: z.ZodIssueCode.custom,
      path: ['min_viewers'],
      message: 'min_viewers is required when enabled',
    });
  }
});
```

Use this pattern when form toggles, hidden fields, or route params make the browser-facing shape narrower than the tRPC input type.

## Runtime Validation Caveat

`validate` tags do not run on the server unless you configure a validator.

```go
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
    trpcgo.WithZodOutput("../web/gen/zod.ts"),
    trpcgo.WithZodMini(true),
)
```

Standard Zod output includes `.meta({ id: "TypeName" })` and `.describe(...)` from `ts_doc`. `zod/mini` skips metadata features that mini does not support.

## Supported Validate Tags

trpcgo supports common `go-playground/validator` tags including:

| Category | Tags |
| --- | --- |
| Required/optional | `required`, `omitempty` |
| Containers | `dive` |
| Length/range | `min`, `max`, `len`, `gt`, `gte`, `lt`, `lte` |
| Formats | `email`, `url`, `uuid`, `e164`, `jwt`, `base64`, `base64url`, `ip`, `ipv4`, `ipv6`, `hostname`, `ulid`, `mac`, `cidrv4`, `cidrv6` |
| Strings | `alphanum`, `alpha`, `numeric`, `lowercase`, `uppercase`, `startswith`, `endswith`, `contains`, `hexadecimal` |
| Enums | `oneof` |
| Cross-field | `gtefield`, `ltefield`, `gtfield`, `ltfield`, `eqfield`, `nefield` |

Unsupported tags are preserved as comments in generated schemas instead of silently disappearing.

## `omitempty` Semantics

There are two separate concepts:

- TypeScript optionality controls whether a field may be `undefined`.
- Validator `omitempty` allows the Go zero value to pass constraints.

For example, `validate:"omitempty,email"` allows an empty string and otherwise requires a valid email. Zod output reflects that with an `or(z.literal(''))` branch.

## Arrays And `dive`

Rules before `dive` apply to the container. Rules after `dive` apply to elements.

```go
type Input struct {
    Tags []string `json:"tags" validate:"min=1,dive,min=1,max=50"`
}
```

Generated Zod applies `.min(1)` to the array and `.min(1).max(50)` to each string.

## Cross-Field Validation

Cross-field tags generate object-level refinements using JSON field names.

```go
type RangeInput struct {
    Start int `json:"start"`
    End   int `json:"end" validate:"gtefield=Start"`
}
```

The generated schema checks the relationship between `end` and `start` after individual fields parse.

## Zod-Only Omit

Use `zod_omit:"true"` to keep a field in TypeScript but leave it out of generated Zod schemas.

```go
type CreateUserInput struct {
    Name      string `json:"name" validate:"required"`
    CSRFToken string `json:"csrfToken" zod_omit:"true"`
}
```

This is useful when a field is supplied by transport or framework code rather than user form data.

## No Typed Inputs

If no procedures have typed inputs, runtime `GenerateZod` and dev watch remove stale Zod files. The CLI can still create an empty file because it opens the requested output path before writing schemas.
