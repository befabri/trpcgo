# trpcgo

Write Go structs and handlers, get TypeScript types automatically.

> **Warning:** This project is under active development. APIs may change and things may break.

```
Go structs + handlers  →  trpcgo generate  →  TypeScript AppRouter + Zod schemas
```

trpcgo is a Go runtime library and code generator that gives you the tRPC developer experience: change a Go struct, save the file, and your TypeScript types update instantly. No manual type syncing, no OpenAPI specs, no protobuf.

## Install

```bash
# Add to your Go module
go get github.com/befabri/trpcgo@latest

# Install the CLI (Go 1.26+ tool directive)
# In your go.mod:
tool github.com/befabri/trpcgo/cmd/trpcgo
```

## Quick Start

### 1. Define types and handlers in Go

```go
//go:generate go tool trpcgo generate -o ../web/gen/trpc.ts --zod ../web/gen/zod.ts

package main

import (
    "context"
    "github.com/befabri/trpcgo"
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

func CreateUser(ctx context.Context, input CreateUserInput) (User, error) {
    // your logic here
    return User{ID: "1", Name: input.Name, Email: input.Email}, nil
}

func main() {
    router := trpcgo.NewRouter(
        trpcgo.WithTypeOutput("../web/gen/trpc.ts"),
        trpcgo.WithZodOutput("../web/gen/zod.ts"),
    )
    trpcgo.Mutation(router, "user.create", CreateUser)

    http.ListenAndServe(":8080", router.Handler("/trpc"))
}
```

### 2. Generated TypeScript (automatic)

**`trpc.ts`** — full AppRouter type:

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

**`zod.ts`** — validation schemas from Go `validate` tags:

```typescript
import { z } from "zod";

export const CreateUserInputSchema = z.object({
  name: z.string().min(1).max(100),
  email: z.email(),
});
```

### 3. Use in your frontend

```typescript
import { createTRPCReact } from "@trpc/react-query";
import type { AppRouter } from "../gen/trpc.js";

export const trpc = createTRPCReact<AppRouter>();

// Fully typed — input and output inferred from Go types
const mutation = trpc.user.create.useMutation();
mutation.mutate({ name: "Alice", email: "alice@example.com" });
```

## Procedure Types

```go
// Query — read operation with typed input
trpcgo.Query(router, "user.getById", func(ctx context.Context, input GetUserInput) (User, error) {
    return db.FindUser(input.ID)
})

// VoidQuery — read operation with no input
trpcgo.VoidQuery(router, "system.health", func(ctx context.Context) (HealthInfo, error) {
    return HealthInfo{OK: true}, nil
})

// Mutation — write operation with typed input
trpcgo.Mutation(router, "user.create", func(ctx context.Context, input CreateUserInput) (User, error) {
    return db.CreateUser(input)
})

// VoidMutation — write operation with no input
trpcgo.VoidMutation(router, "system.reset", func(ctx context.Context) (string, error) {
    return "done", nil
})

// Subscribe — SSE subscription with typed input
trpcgo.Subscribe(router, "chat.messages", func(ctx context.Context, input RoomInput) (<-chan Message, error) {
    ch := make(chan Message)
    // push messages to ch, close when ctx.Done()
    return ch, nil
})

// VoidSubscribe — SSE subscription with no input
trpcgo.VoidSubscribe(router, "user.onCreated", func(ctx context.Context) (<-chan User, error) {
    ch := make(chan User)
    // push to ch when users are created
    return ch, nil
})
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
    trpcgo.WithSSEMaxDuration(10 * time.Minute),
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

    // Code generation (auto-regenerates on file save)
    trpcgo.WithTypeOutput("../web/gen/trpc.ts"),
    trpcgo.WithZodOutput("../web/gen/zod.ts"),
    trpcgo.WithZodMini(false),                // true for zod/mini syntax
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
trpcgo.Mutation(router, "user.create", handler,
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

Error codes follow the tRPC/JSON-RPC convention:

| Code | Name | HTTP Status |
|------|------|-------------|
| `-32700` | `CodeParseError` | 400 |
| `-32600` | `CodeBadRequest` | 400 |
| `-32001` | `CodeUnauthorized` | 401 |
| `-32003` | `CodeForbidden` | 403 |
| `-32004` | `CodeNotFound` | 404 |
| `-32603` | `CodeInternalServerError` | 500 |
| `-32029` | `CodeTooManyRequests` | 429 |
| `-32008` | `CodeTimeout` | 408 |

## Server-Side Caller

Call procedures from within your Go code, running the full middleware chain:

```go
// Typed call — input/output marshaled automatically
user, err := trpcgo.Call[CreateUserInput, User](router, ctx, "user.create", input)

// Raw call — JSON in, any out
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

### Validation

`validate` tags (go-playground/validator) generate both server-side validation and Zod schemas:

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

## CLI

```bash
trpcgo generate [flags] [packages]
```

| Flag | Description |
|------|-------------|
| `-o, --output` | TypeScript output file (default: stdout) |
| `-dir` | Working directory (default: `.`) |
| `-w, --watch` | Watch Go files, regenerate on change |
| `--zod` | Zod schema output file |
| `--zod-mini` | Use `zod/mini` functional syntax |

### With `go:generate`

```go
//go:generate go tool trpcgo generate -o ../web/gen/trpc.ts --zod ../web/gen/zod.ts
```

```bash
go generate ./...
```

### Watch mode

```bash
go tool trpcgo generate -o ../web/gen/trpc.ts --zod ../web/gen/zod.ts -w
```

### Runtime watch (zero config)

When you set `WithTypeOutput` (and optionally `WithZodOutput`) on the router, `Handler()` starts a file watcher automatically. Save a `.go` file anywhere in the project tree and types regenerate instantly — no separate process needed.

## Frontend Setup

### React Query (recommended)

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

### Vanilla client

```typescript
import { createTRPCClient, httpBatchLink } from "@trpc/client";
import type { AppRouter } from "../gen/trpc.js";

const client = createTRPCClient<AppRouter>({
  links: [httpBatchLink({ url: "http://localhost:8080/trpc" })],
});

const user = await client.user.getById.query({ id: "1" });
```

## Router Merging

Split procedures across files and merge:

```go
userRouter := trpcgo.NewRouter()
trpcgo.Query(userRouter, "user.list", listUsers)

adminRouter := trpcgo.NewRouter()
trpcgo.Mutation(adminRouter, "admin.ban", banUser)

router := trpcgo.NewRouter()
router.Merge(userRouter, adminRouter)
// or: router := trpcgo.MergeRouters(userRouter, adminRouter)
```

## Example

See [`examples/tanstack-query/`](examples/tanstack-query/) for a full working example with:

- Go server with all procedure types (Query, VoidQuery, Mutation, VoidMutation, VoidSubscribe)
- Global and per-procedure middleware
- Input validation with go-playground/validator
- Custom error formatter
- Server-side caller (`trpcgo.Call`)
- SSE subscriptions with live broadcasting
- React frontend with TanStack Router + React Query
- Pagination, Zod validation, real-time feed

```bash
# Start the Go server
cd examples/tanstack-query/go-server
go run .

# In another terminal, start the frontend
cd examples/tanstack-query/web
npm install
npm run dev
```

## How It Works

trpcgo has two code generation paths:

1. **Static analysis** (`trpcgo generate`) — reads Go source via `go/packages`, extracts types with full fidelity (comments, validate tags, const unions). This is what generates Zod schemas.

2. **Runtime reflection** (`Router.GenerateTS`) — uses `reflect` to inspect registered procedure types at startup. Faster but less information (no comments, no validate tags).

When you use `WithTypeOutput`, both paths run: reflection generates types immediately on startup, then a file watcher runs static analysis in the background and overwrites with the richer version. On subsequent file saves, only static analysis runs.

The file watcher is recursive — it watches all subdirectories and handles directory creation/removal automatically. Generated files are only written when content changes, avoiding spurious Vite HMR cycles.

## License

MIT
