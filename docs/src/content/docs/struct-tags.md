---
title: Struct Tags
description: Control generated TypeScript fields, names, optionality, docs, and embedded struct behavior from Go tags.
---

Use struct tags to set generated field names, mark fields optional, and customize TypeScript types. `json` tags also control Go's JSON encoding; trpcgo-specific tags only affect generated code.

## JSON Tags

`json` tags control field names and optionality.

```go
type User struct {
    ID   string  `json:"id"`
    Name string  `json:"name"`
    Bio  *string `json:"bio,omitempty"`
}
```

Generated TypeScript:

```ts
export interface User {
  id: string;
  name: string;
  bio?: string;
}
```

Rules:

- `json:"name"` sets the TypeScript property name.
- `json:"-"` excludes the field.
- `omitempty` and `omitzero` make the field optional.
- Fields without a JSON name use the Go field name.
- Unexported fields are ignored.

Pointer fields are optional even without `omitempty`. For named structs, `validate:"required"` or `tstype:",required"` overrides that optionality, including optionality from `omitempty` and `omitzero`.

Optional fields allow omission; they do not automatically include `null`. A nil pointer without `omitempty` can still encode as JSON `null`. Use `omitempty` when nil means “leave this field out,” or explicitly model a nullable TypeScript field when `null` is part of your API.

## TypeScript Overrides

Use `tstype` when Go's default type mapping needs help.

```go
type User struct {
    ID          string         `json:"id" tstype:",readonly"`
    Preferences map[string]any `json:"prefs" tstype:"Record<string, unknown>"`
    Internal    string         `json:"internal" tstype:"-"`
    Email       *string        `json:"email,omitempty" tstype:",required"`
}
```

| Tag | Effect |
| --- | --- |
| `tstype:"SomeType"` | Replaces the generated TypeScript type. |
| `tstype:",readonly"` | Emits a readonly property. |
| `tstype:",required"` | Makes a pointer, `omitempty`, or `omitzero` field required. |
| `tstype:"-"` | Excludes the field from generated TypeScript and Zod metadata. |
| `tstype:",extends"` | For embedded structs, emits TypeScript `extends` instead of flattening. |

Type overrides may include commas, such as `Record<string, unknown>`.

Type replacements only change TypeScript; JSON encoding and Zod validation still follow the Go type. The explicit exclusion `tstype:"-"` also removes the field from the generated schema, so strict schemas reject that key if it is supplied. Go still decodes the field. Use `zod_omit:"true"` to retain a known field while skipping its client validation, or `json:"-"` to exclude it from Go JSON decoding and encoding.

## Field Documentation

Static generation converts Go doc comments to JSDoc.

```go
// User represents a registered user in the system.
type User struct {
    // The unique identifier for this user.
    ID string `json:"id" tstype:",readonly"`
}
```

Use `ts_doc` when you need documentation from a tag, including in runtime reflection generation:

```go
type CreateUserInput struct {
    Name string `json:"name" ts_doc:"Human-readable display name."`
}
```

Standard Zod output adds field documentation with `.describe(...)`. In static generation, a Go field doc comment takes precedence over `ts_doc`; reflection generation uses the tag.

## Embedded Structs

Embedded structs without a JSON name are flattened by default. Adding a name, such as ``Base `json:"base"` ``, generates a nested `base` field instead.

```go
type Base struct {
    ID string `json:"id"`
}

type User struct {
    Base
    Name string `json:"name"`
}
```

Embedded fields follow Go's JSON field selection rules. A field declared on the outer struct takes precedence over an embedded field with the same JSON name. Fields from an optional embedded pointer are optional too.

Use `tstype:",extends"` to preserve inheritance in TypeScript:

```go
type User struct {
    Base `tstype:",extends"`
    Name string `json:"name"`
}
```

An embedded `*Base` with `tstype:",extends"` generates `extends Partial<Base>`. Add `required` to the tag to generate `extends Base`.

If embedded fields conflict or inheritance is recursive, the generator flattens the fields. Zod schemas include embedded fields and their validation rules in the object schema.

## Related Tags

`validate` supplies constraints for generated Zod schemas; `required` also makes fields required in TypeScript interfaces. `zod_omit:"true"` excludes a field from Zod while keeping it in TypeScript. See [Zod Schemas](/zod-schemas/) for examples and supported rules.
