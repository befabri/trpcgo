# trpcgo

> **Warning:** This project is under active development. APIs may change and things may break.

trpcgo is a Go implementation of the [tRPC](https://trpc.io) protocol. You get the same end-to-end type safety as a TypeScript backend, but your server is written in Go. Define your API with Go structs and handlers, and trpcgo generates the TypeScript `AppRouter` type that plugs directly into `@trpc/client` and `@trpc/react-query`. No manual type syncing, no OpenAPI specs, no protobuf.

See the [documentation](https://trpcgo.dev/docs/) for guides and the API reference.

## Table of Contents

- [Why](#why)
- [Install](#install)
- [Quick Start](#quick-start)
- [Procedure Types](#procedure-types)
- [Base Procedures](#base-procedures)
- [Router Options](#router-options)
- [Middleware](#middleware)
- [Errors](#errors)
- [Server-Side Caller](#server-side-caller)
- [Struct Tags](#struct-tags)
- [CLI](#cli)
- [Frontend Setup](#frontend-setup)
- [Router Merging](#router-merging)
- [How It Works](#how-it-works)
- [Example](#example)
- [Compatibility](#compatibility)

## Why

[tRPC](https://trpc.io) gives you end-to-end typesafe APIs: change a type on the server and TypeScript catches broken call sites at compile time. But tRPC requires a TypeScript server.

trpcgo removes that constraint. Write your server in Go and still get the full tRPC developer experience on the frontend. Your TypeScript client code looks exactly the same as if the server were written in TypeScript.

## Install

Requires Go 1.26+. Run these commands in your Go module:

```bash
# Add the runtime library to your Go module
go get github.com/befabri/trpcgo@latest

# Add the code generator to go.mod's tool directives
go get -tool github.com/befabri/trpcgo/cmd/trpcgo@latest
```

## Quick Start

### 1. Define types and handlers in Go

This example uses `go-playground/validator` to validate inputs on the server:

```bash
go get github.com/go-playground/validator/v10
```

Save the following as `main.go` in your module root. Output paths are relative to that directory.

```go
//go:generate go tool trpcgo generate -o web/gen/trpc.ts --zod web/gen/zod.ts

package main

import (
    "context"
    "log"
    "net/http"

    "github.com/befabri/trpcgo"
    "github.com/befabri/trpcgo/trpc"
    "github.com/go-playground/validator/v10"
)

type CreateUserInput struct {
    Name  string `json:"name" validate:"required,min=1,max=100"`
    Email string `json:"email" validate:"required,email"`
}

type User struct {
    ID    string `json:"id" tstype:",readonly"`
    Name  string `json:"name"`
    Email string `json:"email"`
}

func main() {
    validate := validator.New()
    router := trpcgo.NewRouter(
        trpcgo.WithDev(true),
        trpcgo.WithValidator(validate.Struct),
        trpcgo.WithTypeOutput("web/gen/trpc.ts"),
        trpcgo.WithZodOutput("web/gen/zod.ts"),
    )
    defer router.Close()

    trpcgo.MustMutation(router, "user.create", func(ctx context.Context, input CreateUserInput) (User, error) {
        return User{ID: "1", Name: input.Name, Email: input.Email}, nil
    })

    handler := trpc.NewHandler(router, "/trpc",
        trpc.WithCORS(trpc.CORSConfig{
            AllowedOrigins: []string{"http://localhost:3000"},
        }),
        trpc.WithTrustedOrigins("http://localhost:3000"),
    )

    mux := http.NewServeMux()
    mux.Handle("/trpc/", handler)
    if err := http.ListenAndServe(":8080", mux); err != nil {
        log.Print(err)
    }
}
```

`WithValidator(validate.Struct)` enables server-side validation; the tags alone do not validate requests.

### 2. Generate types and start the server

```bash
mkdir -p web/gen
go generate ./...
go run .
```

The CLI needs the output directory to exist. With the server running in dev mode, saving Go files also regenerates the frontend files. Restart the server to apply changes to handler behavior.

`web/gen/trpc.ts` (abbreviated):

```typescript
export interface CreateUserInput {
  name: string;
  email: string;
}

export interface User {
  readonly id: string;
  name: string;
  email: string;
}

export type AppRouter = { /* ... structural types matching @trpc/client */ };
```

`web/gen/zod.ts` (validation schemas from Go `validate` tags):

```typescript
import { z } from "zod";

export const CreateUserInputSchema = z.object({
  name: z.string().min(1).max(100),
  email: z.email().min(1),
}).meta({ id: "CreateUserInput" });
```

### 3. Use with @trpc/client

Install the frontend dependencies in your frontend project:

```bash
npm install @trpc/client@11 @trpc/server@11
```

For example, in `web/client.ts`:

```typescript
import { createTRPCClient, httpBatchLink } from "@trpc/client";
import type { AppRouter } from "./gen/trpc.js";

const client = createTRPCClient<AppRouter>({
  links: [httpBatchLink({ url: "http://localhost:8080/trpc" })],
});

// Fully typed: input and output inferred from Go types
const user = await client.user.create.mutate({
  name: "Alice",
  email: "alice@example.com",
});
```

The Go handler above allows browser requests from `http://localhost:3000`. Use your frontend's origin if it runs elsewhere. Install `zod@4` if you also import the generated schemas.

## Procedure Types

trpcgo supports all tRPC procedure types: queries, mutations, and subscriptions.

Each registration function returns an `error` (duplicate path). The `Must*` variants panic instead and are the idiomatic choice for application bootstrap code:

```go
// Query (read, with input)
trpcgo.MustQuery(router, "user.getById", func(ctx context.Context, input GetUserInput) (User, error) {
    return db.FindUser(input.ID)
})

// VoidQuery (read, no input)
trpcgo.MustVoidQuery(router, "system.health", func(ctx context.Context) (HealthInfo, error) {
    return HealthInfo{OK: true}, nil
})

// Mutation (write, with input)
trpcgo.MustMutation(router, "user.create", func(ctx context.Context, input CreateUserInput) (User, error) {
    return db.CreateUser(input)
})

// VoidMutation (write, no input)
trpcgo.MustVoidMutation(router, "system.reset", func(ctx context.Context) (string, error) {
    return "done", nil
})

// Subscribe (SSE, with input)
trpcgo.MustSubscribe(router, "chat.messages", func(ctx context.Context, input RoomInput) (<-chan Message, error) {
    ch := make(chan Message)
    // push messages to ch, close when ctx.Done()
    return ch, nil
})

// VoidSubscribe (SSE, no input)
trpcgo.MustVoidSubscribe(router, "user.onCreated", func(ctx context.Context) (<-chan User, error) {
    ch := make(chan User)
    // push to ch when users are created
    return ch, nil
})

// Non-Must variants return error — use when you need to handle the failure:
if err := trpcgo.Query(router, "user.getById", handler); err != nil {
    log.Fatal(err)
}
```

For resumable subscriptions, send values with `trpcgo.Tracked("message-42", message)`. The tRPC subscription link delivers each tracked item as `{ id, data }`; read the payload from `event.data`. See [Subscriptions](https://trpcgo.dev/subscriptions/) for registration and reconnect examples.

## Base Procedures

`trpcgo.Procedure()` creates a reusable builder that bundles middleware and metadata — the Go equivalent of tRPC's composable procedure pattern. Builders are immutable: every chain call returns a new instance, so sharing a base never causes accidental mutation.

```go
// Define reusable base procedures once
publicProcedure := trpcgo.Procedure()
authedProcedure := publicProcedure.Use(authMiddleware)
adminProcedure  := authedProcedure.Use(adminCheckMiddleware).WithMeta(roleMeta{Admin: true})

// Use them at every registration site
trpcgo.MustQuery(router,    "user.list",    listUsers,  authedProcedure)
trpcgo.MustMutation(router, "user.create",  createUser, authedProcedure)
trpcgo.MustMutation(router, "admin.ban",    banUser,    adminProcedure)

// Combine with per-procedure options — all options merge
trpcgo.MustQuery(router, "report.get", getReport, authedProcedure, trpcgo.WithMeta(auditLog{}))
```

Builders can also be seeded from an existing builder:

```go
// Inherits all of authedProcedure's middleware, then adds more
orgProcedure := trpcgo.Procedure(authedProcedure).Use(orgScopeMiddleware)
```

## Router Options

```go
router := trpcgo.NewRouter(
    // Request handling
    trpcgo.WithBatching(true),               // enable batch requests
    trpcgo.WithMethodOverride(true),          // allow POST for queries
    trpcgo.WithMaxBodySize(2 << 20),          // 2MB request limit (default 1MB)

    // Validation
    trpcgo.WithValidator(validate.Struct),     // go-playground/validator compatible

    // SSE subscriptions
    trpcgo.WithSSEPingInterval(5 * time.Second),
    trpcgo.WithSSEMaxDuration(10 * time.Minute),     // default 30m, -1 for unlimited
    trpcgo.WithSSEMaxConnections(1000),               // concurrent SSE limit
    trpcgo.WithSSEReconnectAfterInactivity(30 * time.Second),

    // Errors
    trpcgo.WithDev(true),                     // stack traces in error responses
    trpcgo.WithOnError(func(ctx context.Context, err *trpcgo.Error, path string) {
        log.Printf("error on %s: %v", path, err)
    }),
    trpcgo.WithErrorFormatter(func(input trpcgo.ErrorFormatterInput) any {
        shape := input.Shape
        if input.Error.Code == trpcgo.CodeUnauthorized {
            shape.Error.Message = "please sign in"
        }
        return shape
    }),

    // Context
    trpcgo.WithContextCreator(func(ctx context.Context, r *http.Request) context.Context {
        return context.WithValue(ctx, authKey, r.Header.Get("Authorization"))
    }),

    // Code generation (auto-regenerates on file save in dev mode)
    trpcgo.WithTypeOutput("../web/gen/trpc.ts"),
    trpcgo.WithZodOutput("../web/gen/zod.ts"),
    trpcgo.WithZodMini(false),                // true for zod/mini syntax
    trpcgo.WithWatchPackages("./internal/...", "./cmd/api"), // scope watcher to specific packages
)
```

## Middleware

### Global middleware

```go
router.Use(func(next trpcgo.HandlerFunc) trpcgo.HandlerFunc {
    return func(ctx context.Context, input any) (any, error) {
        meta, _ := trpcgo.GetProcedureMeta(ctx)
        start := time.Now()
        result, err := next(ctx, input)
        log.Printf("[%s] %s took %s", meta.Type, meta.Path, time.Since(start))
        return result, err
    }
})
```

### Per-procedure middleware

```go
trpcgo.MustMutation(router, "user.create", handler,
    trpcgo.Use(authRequired, rateLimiter),
    trpcgo.WithMeta(map[string]string{"action": "write"}),
)
```

### Accessing metadata in middleware

```go
func authRequired(next trpcgo.HandlerFunc) trpcgo.HandlerFunc {
    return func(ctx context.Context, input any) (any, error) {
        meta, _ := trpcgo.GetProcedureMeta(ctx)
        // meta.Path = "user.create"
        // meta.Type = "mutation"
        // meta.Meta = map[string]string{"action": "write"}
        return next(ctx, input)
    }
}
```

## Errors

```go
// Create errors with tRPC error codes
trpcgo.NewError(trpcgo.CodeNotFound, "user not found")
trpcgo.NewErrorf(trpcgo.CodeBadRequest, "invalid id: %s", id)
trpcgo.WrapError(trpcgo.CodeInternalServerError, "db failed", err)
```

All standard tRPC error codes are available (`CodeNotFound`, `CodeUnauthorized`, `CodeTooManyRequests`, etc.) and map to the correct HTTP status codes.

## Server-Side Caller

Call procedures from within your Go code, running the full middleware chain:

```go
// Typed call, input/output marshaled automatically
user, err := trpcgo.Call[CreateUserInput, User](router, ctx, "user.create", input)

// Raw call, JSON in, any out
result, err := router.RawCall(ctx, path, jsonBytes)
```

`Call` and `RawCall` support queries and mutations. To use a subscription within Go, call its handler directly.

## Struct Tags

### JSON mapping

Standard `json` tags control field names and optionality:

```go
type User struct {
    ID   string `json:"id"`
    Name string `json:"name"`
    Bio  string `json:"bio,omitempty"` // optional in TypeScript
}
```

### TypeScript overrides

The `tstype` tag controls TypeScript generation:

```go
type User struct {
    ID          string         `json:"id" tstype:",readonly"`          // readonly id: string
    Preferences map[string]any `json:"prefs" tstype:"Record<string, unknown>"`
    Internal    string         `json:"internal" tstype:"-"`            // excluded from TS
    Email       string         `json:"email" tstype:",required"`       // never optional
}
```

### Output Validation And Parsing

Use output hooks when a procedure should validate or transform its handler result before it is sent.

- `OutputValidator[O]` validates the handler output without changing its type.
- `WithOutputValidator(func(any) error)` is the builder-friendly untyped validator form.
- `OutputParser[O, P]` is the typed form and updates generated output types to `P`.
- `WithOutputParser(func(any) (any, error))` is the builder-friendly untyped form; codegen falls back to `unknown` unless a typed `OutputParser` override is present.

Each registration below is a separate example:

```go
// Typed: validate only
trpcgo.MustQuery(router, "user.get", getUser,
    trpcgo.OutputValidator(func(u User) error {
        if u.ID == "" { return errors.New("id required") }
        return nil
    }),
)

// Typed: validate or transform the output
trpcgo.MustQuery(router, "user.get", getUser,
    trpcgo.OutputParser(func(u User) (User, error) {
        if u.ID == "" { return User{}, errors.New("id required") }
        return u, nil
    }),
)

// Typed — transform (strip sensitive fields before sending to client)
type PublicUser struct { ID string `json:"id"` }
trpcgo.MustQuery(router, "user.get", getUser,
    trpcgo.OutputParser(func(u User) (PublicUser, error) {
        return PublicUser{ID: u.ID}, nil
    }),
)

// Untyped: useful on reusable builders
authedProcedure := trpcgo.Procedure().Use(authMW).
    WithOutputValidator(func(v any) error {
        return nil
    }).
    WithOutputParser(func(v any) (any, error) {
        // validate or transform v
        return v, nil
    })
```

Plain Go errors from output validators or parsers become `INTERNAL_SERVER_ERROR`. Clients and `WithErrorFormatter(...)` see a generic `internal server error`, while `WithOnError(...)` receives the wrapped cause for logging. Typed trpcgo errors keep their code and follow the same sanitization rules as handler errors.

When both are present, the output validator runs before the output parser. For subscriptions, both run on each emitted item before `TrackedEvent` unwrapping. If either fails, the server sends a `serialized-error` SSE event and closes the stream. See [Output Hooks](https://trpcgo.dev/procedures/#output-hooks) for final values.

### Validation

The generator reads supported `validate` tags ([go-playground/validator](https://github.com/go-playground/validator)) to produce Zod schemas. Pass `WithValidator(validate.Struct)` to the router to apply validation on the server as well:

```go
type Input struct {
    Name  string   `json:"name" validate:"required,min=1,max=100"`    // z.string().min(1).max(100)
    Email string   `json:"email" validate:"required,email"`           // z.email().min(1)
    Role  string   `json:"role" validate:"oneof=admin editor viewer"` // z.enum([...])
    Tags  []string `json:"tags" validate:"min=1,dive,min=1,max=50"`   // z.array(z.string().min(1).max(50)).min(1)
    Age   int      `json:"age" validate:"gte=18,lte=150"`             // z.int().gte(18).lte(150)
    URL   string   `json:"url" validate:"url"`                        // z.url()
    UUID  string   `json:"uuid" validate:"uuid"`                      // z.uuidv4()
}
```

## CLI

```bash
go tool trpcgo generate [flags] [packages]
```

| Flag | Description |
|------|-------------|
| `-o, --output` | TypeScript output file (default: stdout) |
| `-dir` | Working directory (default: `.`) |
| `-w, --watch` | Watch Go files, regenerate on change |
| `--zod` | Zod schema output file |
| `--zod-mini` | Use `zod/mini` functional syntax |
| `--enums` | Runtime enum value object output file |

### With `go:generate`

```go
//go:generate go tool trpcgo generate -o ../web/gen/trpc.ts --zod ../web/gen/zod.ts
```

```bash
mkdir -p ../web/gen
go generate ./...
```

### Watch mode

```bash
go tool trpcgo generate -o ../web/gen/trpc.ts --zod ../web/gen/zod.ts -w
```

### Runtime watch

When you set `WithDev(true)` with `WithTypeOutput` (and optionally `WithZodOutput` or `WithEnumsOutput`) on the router, `trpc.NewHandler` starts a file watcher automatically. Saving a `.go` file in a watched directory regenerates the frontend files. Call `router.Close()` to stop the watcher on shutdown. The watcher updates generated files; restart your Go server to run changed handlers.

Use `WithWatchPackages` to restrict watching to specific packages (go/packages patterns) — useful in monorepos to avoid watching unrelated directories like frontend build output.

## Frontend Setup

### React Query

```typescript
// trpc.ts
import { createTRPCReact } from "@trpc/react-query";
import type { AppRouter } from "../gen/trpc.js";

export const trpc = createTRPCReact<AppRouter>();
```

```typescript
// main.tsx
import { httpBatchLink, httpSubscriptionLink, splitLink } from "@trpc/client";

const trpcClient = trpc.createClient({
  links: [
    splitLink({
      condition: (op) => op.type === "subscription",
      true: httpSubscriptionLink({ url: "/trpc" }),
      false: httpBatchLink({ url: "/trpc" }),
    }),
  ],
});
```

### Vanilla client

```typescript
import { createTRPCClient, httpBatchLink } from "@trpc/client";
import type { AppRouter } from "../gen/trpc.js";

const client = createTRPCClient<AppRouter>({
  links: [httpBatchLink({ url: "http://localhost:8080/trpc" })],
});

const user = await client.user.getById.query({ id: "1" });
```

When a browser app is served from a different origin, such as `http://localhost:3000`, configure the Go handler with both `trpc.WithCORS` and `trpc.WithTrustedOrigins` for that exact frontend origin.

## Router Merging

Split procedures across files and merge:

```go
userRouter := trpcgo.NewRouter()
trpcgo.MustQuery(userRouter, "user.list", listUsers)

adminRouter := trpcgo.NewRouter()
trpcgo.MustMutation(adminRouter, "admin.ban", banUser)

router := trpcgo.NewRouter()
if err := router.Merge(userRouter, adminRouter); err != nil {
    log.Fatal(err) // duplicate procedure path
}
// or: router, err := trpcgo.MergeRouters(userRouter, adminRouter)
```

Merging copies procedures and their per-procedure middleware. It does not copy global middleware or router options, so configure those on the destination router. Register and merge procedures before creating the HTTP handler.

## How It Works

trpcgo implements the [tRPC HTTP protocol](https://trpc.io/docs/rpc) in Go and provides two code generation paths:

1. **Static analysis** (`go tool trpcgo generate`): reads Go source via `go/packages`, including comments, validation tags, and const unions. It can generate TypeScript types, Zod schemas, and runtime enum values.

2. **Runtime reflection** (`Router.GenerateTS` and `Router.GenerateZod`): inspects registered procedure types and struct tags, including `validate` tags. It does not have access to source comments or const declarations.

See [Code Generation](https://trpcgo.dev/code-generation/) for the differences between the two, and [Compatibility](https://trpcgo.dev/reference/compatibility/) when upgrading generated files.

When you use `WithDev(true)` with `WithTypeOutput`, `trpc.NewHandler` starts a watcher that runs static analysis in the background, first at startup and again when Go files change. Reflection generation is available through explicit calls to `GenerateTS` or `GenerateZod`. In production, generate frontend files before building your app. The watcher only starts in dev mode.

The file watcher discovers Go directories recursively and handles new and removed directories. During source regeneration, it writes generated files only when their content changes, avoiding unnecessary frontend reloads.

## Example

See [`examples/start-trpc/`](examples/start-trpc/) for a full working example with a Go server and a TanStack Start frontend using `@trpc/client` and `@trpc/tanstack-react-query`.

## Compatibility

**Go:** Requires Go 1.26+ (uses `tool` directive, `errors.AsType`, generics).

**tRPC client:** Works with `@trpc/client`, `@trpc/react-query`, and `@trpc/tanstack-react-query` v11. Keep your `@trpc/*` packages on matching versions. Install `@trpc/server` for the generated type imports, even though your API server runs in Go.

**Subscriptions:** Use `httpSubscriptionLink` for SSE. The link does not expose the payload of final `return` events; see [Subscription Limitations](https://trpcgo.dev/reference/compatibility/#subscription-limitations).

**HTTP:** Pure `net/http`, no framework dependency. Works with any Go router or middleware.

**CORS:** trpcgo includes optional CORS handling via `trpc.WithCORS`. You can also use middleware from your HTTP router or a dedicated package (e.g. `rs/cors`).

## License

MIT
