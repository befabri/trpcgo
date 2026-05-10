---
title: Struct Tags
description: Control generated TypeScript fields, names, optionality, docs, and embedded struct behavior from Go tags.
---

Go struct tags are the contract between your Go runtime and generated frontend code. They control how fields are named, whether they are optional, and how TypeScript output is customized.

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
- No tag uses the Go field name.
- Unexported fields are ignored.

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
| `tstype:",required"` | Forces a pointer or `omitempty` field to be required. |
| `tstype:"-"` | Excludes the field from generated TypeScript and Zod metadata. |
| `tstype:",extends"` | For embedded structs, emits TypeScript `extends` instead of flattening. |

Type overrides may include commas, such as `Record<string, unknown>`.

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

Standard Zod output also uses `ts_doc` in `.describe(...)`.

## Embedded Structs

Embedded structs are flattened by default.

```go
type Base struct {
    ID string `json:"id"`
}

type User struct {
    Base
    Name string `json:"name"`
}
```

Use `tstype:",extends"` to preserve inheritance in TypeScript:

```go
type User struct {
    Base `tstype:",extends"`
    Name string `json:"name"`
}
```

Pointer embedded extends become `Partial<Base>` unless marked `required`.

## Related Tags

`validate` and `zod_omit` affect generated Zod schemas rather than the main TypeScript field contract. See [Zod Schemas](/zod-schemas/) for those rules.
