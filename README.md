# trpcgo

> **Warning:** This project is under active development. APIs may change and things may break.

trpcgo is a Go implementation of the [tRPC](https://trpc.io) and [oRPC](https://orpc.unnoq.com) protocols. You get the same end-to-end type safety as a TypeScript backend, but your server is written in Go. Your TypeScript client code looks exactly the same as if the server were written in TypeScript. Define your API with Go structs and handlers, and trpcgo generates TypeScript types that plug directly into `@trpc/client` or `@orpc/client`. No manual type syncing, no OpenAPI specs, no protobuf.

One router, two wire formats. Same handlers, same middleware, same types.

## Table of Contents

- [Why](#why)
- [Install](#install)
- [Quick Start](#quick-start)
- [Protocol Handlers](#protocol-handlers)
- [Procedure Types](#procedure-types)
- [Base Procedures](#base-procedures)
- [Router Options](#router-options)
- [Middleware](#middleware)
- [Errors](#errors)
- [Server-Side Caller](#server-side-caller)
- [Struct Tags](#struct-tags)
- [File Uploads](#file-uploads)
- [CLI](#cli)
- [Frontend Setup](#frontend-setup)
- [Router Merging](#router-merging)
- [How It Works](#how-it-works)
- [Examples](#examples)
- [Compatibility](#compatibility)

## Why

[tRPC](https://trpc.io) and [oRPC](https://orpc.unnoq.com) give you end-to-end typesafe APIs: change a type on the server and TypeScript catches every broken call site at compile time. But both require a TypeScript/Node server.

trpcgo removes that constraint. Write your server in Go and still get the full developer experience on the frontend. Your TypeScript client code looks exactly the same as if the server were written in TypeScript.

## Install

```bash
# Add the runtime library to your Go module
go get github.com/befabri/trpcgo@latest

# Install the code generator (Go 1.26+ tool directive)
# In your go.mod:
tool github.com/befabri/trpcgo/cmd/trpcgo
```

## Quick Start

### 1. Define types and handlers in Go

```go
//go:generate go tool trpcgo generate -o ../web/gen/trpc.ts --zod ../web/gen/zod.ts
//go:generate go tool trpcgo generate -format orpc -o ../web/gen/orpc.ts

package main

import (
    "context"
    "net/http"

    "github.com/befabri/trpcgo"
    "github.com/befabri/trpcgo/trpc"
    "github.com/befabri/trpcgo/orpc"
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
    router := trpcgo.NewRouter(
        trpcgo.WithDev(true),
        trpcgo.WithTypeOutput("../web/gen/trpc.ts"),
        trpcgo.WithZodOutput("../web/gen/zod.ts"),
    )
    defer router.Close()

    trpcgo.MustMutation(router, "user.create", func(ctx context.Context, input CreateUserInput) (User, error) {
        return User{ID: "1", Name: input.Name, Email: input.Email}, nil
    })

    // Serve over tRPC, oRPC, or both:
    mux := http.NewServeMux()
    mux.Handle("/trpc/", trpc.NewHandler(router, "/trpc"))
    mux.Handle("/rpc/", orpc.NewHandler(router, "/rpc"))
    http.ListenAndServe(":8080", mux)
}
```

### 2. Generated TypeScript (automatic)

**tRPC** (`trpcgo generate`):

```typescript
export interface CreateUserInput { name: string; email: string; }
export interface User { readonly id: string; name: string; email: string; }
export type AppRouter = { /* structural types for @trpc/client */ };
```

**oRPC** (`trpcgo generate -format orpc`):

```typescript
export interface CreateUserInput { name: string; email: string; }
export interface User { readonly id: string; name: string; email: string; }
export type RouterClient = { /* structural types for @orpc/client */ };
```

**Zod schemas** (from Go `validate` tags):

```typescript
import { z } from "zod";
export const CreateUserInputSchema = z.object({
  name: z.string().min(1).max(100),
  email: z.email(),
});
```

### 3. Use with your client

**tRPC:**

```typescript
import { createTRPCReact } from "@trpc/react-query";
import type { AppRouter } from "../gen/trpc.js";

export const trpc = createTRPCReact<AppRouter>();
const mutation = trpc.user.create.useMutation();
mutation.mutate({ name: "Alice", email: "alice@example.com" });
```

**oRPC:**

```typescript
import { createORPCClient } from "@orpc/client";
import type { RouterClient } from "../gen/orpc.js";

const client = createORPCClient<RouterClient>({ baseURL: "/rpc" });
const user = await client.user.create({ name: "Alice", email: "alice@example.com" });
```

## Protocol Handlers

trpcgo separates routing from wire format. Register procedures once on a `Router`, then serve them over one or both protocols:

```go
import (
    "github.com/befabri/trpcgo/trpc"
    "github.com/befabri/trpcgo/orpc"
)

// tRPC wire format: dot-separated paths, JSON batching, JSONL streaming
trpcHandler := trpc.NewHandler(router, "/trpc")

// oRPC wire format: slash-separated paths, {json,meta} codec, SSE batching
orpcHandler := orpc.NewHandler(router, "/rpc")
```

### REST-style routes (oRPC)

Use `WithRoute` to map procedures to REST endpoints with path parameters:

```go
trpcgo.MustQuery(router, "planet.get", getPlanet,
    trpcgo.WithRoute(http.MethodGet, "/planets/{id}"),
)

trpcgo.MustMutation(router, "planet.create", createPlanet,
    trpcgo.WithRoute(http.MethodPost, "/planets"),
    trpcgo.WithSuccessStatus(http.StatusCreated),
)
```

Path parameters are automatically extracted and merged into the input struct:

```go
type GetPlanetInput struct {
    ID int `json:"id"` // populated from {id} in the URL path
}
```

### Dual-protocol

Mount both handlers on your HTTP router. The same procedures, middleware, and types are served over both wire formats:

```go
r := chi.NewRouter()
r.Handle("/trpc/*", trpc.NewHandler(router, "/trpc"))
r.Handle("/rpc/*", orpc.NewHandler(router, "/rpc"))
```

tRPC clients hit `/trpc/user.create`, oRPC clients hit `/rpc/user/create`. Same handler, same validation, same response.

## Procedure Types

trpcgo supports all procedure types: queries, mutations, and subscriptions.

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
        return map[string]any{
            "error": map[string]any{
                "code":    input.Shape.Error.Code,
                "message": input.Shape.Error.Message,
                "data":    input.Shape.Error.Data,
            },
        }
    }),

    // Context
    trpcgo.WithContextCreator(func(r *http.Request) context.Context {
        return context.WithValue(r.Context(), authKey, r.Header.Get("Authorization"))
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
// Create errors with standard error codes
trpcgo.NewError(trpcgo.CodeNotFound, "user not found")
trpcgo.NewErrorf(trpcgo.CodeBadRequest, "invalid id: %s", id)
trpcgo.WrapError(trpcgo.CodeInternalServerError, "db failed", err)
```

All standard tRPC error codes are available (`CodeNotFound`, `CodeUnauthorized`, `CodeTooManyRequests`, etc.) and map to the correct HTTP status codes for both protocols.

## Server-Side Caller

Call procedures from within your Go code, running the full middleware chain:

```go
// Typed call, input/output marshaled automatically
user, err := trpcgo.Call[CreateUserInput, User](router, ctx, "user.create", input)

// Raw call, JSON in, any out
result, err := router.RawCall(ctx, path, jsonBytes)
```

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

Parser failures return `INTERNAL_SERVER_ERROR`. Clients and `WithErrorFormatter(...)` see a generic `internal server error`, while `WithOnError(...)` still receives the original wrapped cause for logging.

When both are present, the output validator runs before the output parser. For subscriptions, both run on each emitted item before `TrackedEvent` unwrapping. If either fails, the server sends a `serialized-error` SSE event and closes the stream.

### Validation

`validate` tags ([go-playground/validator](https://github.com/go-playground/validator)) generate both server-side validation and Zod schemas:

```go
type Input struct {
    Name  string   `json:"name" validate:"required,min=1,max=100"`    // z.string().min(1).max(100)
    Email string   `json:"email" validate:"required,email"`           // z.email()
    Role  string   `json:"role" validate:"oneof=admin editor viewer"` // z.enum([...])
    Tags  []string `json:"tags" validate:"min=1,dive,min=1,max=50"`   // z.array(z.string().min(1).max(50)).min(1)
    Age   int      `json:"age" validate:"gte=18,lte=150"`             // z.int().gte(18).lte(150)
    URL   string   `json:"url" validate:"url"`                        // z.url()
    UUID  string   `json:"uuid" validate:"uuid"`                      // z.uuidv4()
}
```

## File Uploads

The `Blob` type supports file uploads via oRPC's multipart/form-data wire format. Use it as a field type in your input structs:

```go
type UploadInput struct {
    Name string     `json:"name"`
    File trpcgo.Blob `json:"file"`
}

trpcgo.MustMutation(router, "document.upload", func(ctx context.Context, input UploadInput) (string, error) {
    data := input.File.Bytes()    // raw file content
    name := input.File.Name       // original filename
    mime := input.File.Type       // MIME type
    size := input.File.Len()      // size in bytes
    return fmt.Sprintf("uploaded %s (%d bytes)", name, size), nil
})
```

Use `*Blob` for optional file fields — a `nil` pointer means no file was provided.

## CLI

```bash
trpcgo generate [flags] [packages]
```

| Flag | Description |
|------|-------------|
| `-o, --output` | TypeScript output file (default: stdout) |
| `-dir` | Working directory (default: `.`) |
| `-format` | Output format: `trpc` (default) or `orpc` |
| `-w, --watch` | Watch Go files, regenerate on change |
| `--zod` | Zod schema output file |
| `--zod-mini` | Use `zod/mini` functional syntax |

### With `go:generate`

```go
// tRPC types
//go:generate go tool trpcgo generate -o ../web/gen/trpc.ts --zod ../web/gen/zod.ts

// oRPC types
//go:generate go tool trpcgo generate -format orpc -o ../web/gen/orpc.ts --zod ../web/gen/zod.ts
```

```bash
go generate ./...
```

### Watch mode

```bash
go tool trpcgo generate -o ../web/gen/trpc.ts --zod ../web/gen/zod.ts -w
```

### Runtime watch (zero config)

When you set `WithDev(true)` with `WithTypeOutput` (and optionally `WithZodOutput`) on the router, the handler constructors (`trpc.NewHandler` / `orpc.NewHandler`) start a file watcher automatically. Save a `.go` file anywhere in the project tree and types regenerate instantly, no separate process needed. Call `router.Close()` to stop the watcher on shutdown.

Use `WithWatchPackages` to restrict watching to specific packages (go/packages patterns) — useful in monorepos to avoid watching unrelated directories like frontend build output.

## Frontend Setup

### tRPC — React Query

```typescript
// trpc.ts
import { createTRPCReact } from "@trpc/react-query";
import type { AppRouter } from "../gen/trpc.js";

export const trpc = createTRPCReact<AppRouter>();
```

```typescript
// main.tsx
import { httpBatchLink, splitLink, unstable_httpSubscriptionLink } from "@trpc/client";

const trpcClient = trpc.createClient({
  links: [
    splitLink({
      condition: (op) => op.type === "subscription",
      true: unstable_httpSubscriptionLink({ url: "/trpc" }),
      false: httpBatchLink({ url: "/trpc" }),
    }),
  ],
});
```

### tRPC — Vanilla client

```typescript
import { createTRPCClient, httpBatchLink } from "@trpc/client";
import type { AppRouter } from "../gen/trpc.js";

const client = createTRPCClient<AppRouter>({
  links: [httpBatchLink({ url: "http://localhost:8080/trpc" })],
});

const user = await client.user.getById.query({ id: "1" });
```

### oRPC — Client

```typescript
import { createORPCClient } from "@orpc/client";
import { RPCLink } from "@orpc/client/fetch";
import type { RouterClient } from "../gen/orpc.js";

const link = new RPCLink({ url: "http://localhost:8080/rpc" });
const client = createORPCClient<RouterClient>(link);

const user = await client.user.getById({ id: "1" });
```

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

## How It Works

trpcgo implements both the [tRPC HTTP protocol](https://trpc.io/docs/rpc) and the [oRPC wire format](https://orpc.unnoq.com) in Go, and provides two code generation paths:

1. **Static analysis** (`trpcgo generate`): reads Go source via `go/packages`, extracts types with full fidelity (comments, validate tags, const unions). This is what generates Zod schemas. Use `-format trpc` (default) or `-format orpc` to target your client library.

2. **Runtime reflection** (`Router.GenerateTS`): uses `reflect` to inspect registered procedure types at startup. Faster but less information (no comments, no validate tags).

When you use `WithDev(true)` with `WithTypeOutput`, both paths run: reflection generates types immediately on startup, then a file watcher runs static analysis in the background and overwrites with the richer version. On subsequent file saves, only static analysis runs. In production, use `go generate` pre-build. The watcher only starts in dev mode.

The file watcher is recursive. It watches all subdirectories and handles directory creation/removal automatically. Generated files are only written when content changes, avoiding spurious Vite HMR cycles.

## Examples

- [`examples/start-trpc/`](examples/start-trpc/) — Go server + TanStack Start frontend using `@trpc/client` and `@trpc/tanstack-react-query`.
- [`examples/start-orpc/`](examples/start-orpc/) — Go server + TanStack Start frontend using `@orpc/client`.

## Compatibility

**Go:** Requires Go 1.26+ (uses `tool` directive, `errors.AsType`, generics).

**tRPC client:** Works with `@trpc/client` v11 and `@trpc/react-query` v11. The generated `AppRouter` type imports from `@trpc/server` (which is a dependency of `@trpc/client`).

**oRPC client:** Works with `@orpc/client`. The generated `RouterClient` type imports from `@orpc/client`.

**HTTP:** Pure `net/http`, no framework dependency. Works with any Go router or middleware.

**CORS:** trpcgo does not handle CORS. Use middleware from your HTTP router or a dedicated package (e.g. `rs/cors`).

## License

MIT
