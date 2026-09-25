# trpcgo

> **Warning:** This project is under active development. APIs may change and things may break.

trpcgo is a Go implementation of the [tRPC](https://trpc.io) protocol. You get the same end-to-end type safety as a TypeScript backend, but your server is written in Go. Define your API with Go structs and handlers, and trpcgo generates the TypeScript `AppRouter` type that plugs directly into `@trpc/client` and `@trpc/react-query`. No manual type syncing, no OpenAPI specs, no protobuf.

See the [documentation](https://trpcgo.dev/docs/) for guides and the API reference.

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
        trpcgo.WithValidator(trpcgo.StructValidator(validate.Struct)),
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

`WithValidator(trpcgo.StructValidator(validate.Struct))` validates every struct in the input on the server; the tags alone do not validate requests.

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

`web/gen/zod.ts` (abbreviated), with validation schemas from Go `validate` tags:

```typescript
export const CreateUserInputSchema = z.strictObject({
  name: z.string().min(1).max(100),
  email: z.string().check($trpcgoIssue((value) => $trpcgoEmail(String(value).replace(/\p{Surrogate}/gu, "\uFFFD")), { code: "invalid_format", format: "email" })),
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

The Go handler above allows browser requests from `http://localhost:3000`. Use your frontend's origin if it runs elsewhere. Install `zod@^4.5.4` if you also import the generated schemas.

## Documentation

The [documentation](https://trpcgo.dev/docs/) covers everything beyond the quick start:

- [Procedures](https://trpcgo.dev/procedures/): queries, mutations, subscriptions, base procedures, and output hooks.
- [Router & Options](https://trpcgo.dev/router-options/): batching, strict input, body limits, validation, and router merging.
- [Middleware & Metadata](https://trpcgo.dev/middleware/): middleware, typed metadata, request context, and server-side calls.
- [Errors](https://trpcgo.dev/errors/): tRPC error codes, formatting, and logging.
- [Struct Tags](https://trpcgo.dev/struct-tags/) and [Zod Schemas](https://trpcgo.dev/zod-schemas/): shape the generated TypeScript and validation.
- [Code Generation](https://trpcgo.dev/code-generation/) and the [CLI](https://trpcgo.dev/reference/cli/): static generation, `go:generate`, and dev watch.
- [Frontend Setup](https://trpcgo.dev/frontend-setup/): the vanilla client, React Query, and TanStack Query.
- [Security & Production](https://trpcgo.dev/security-production/): CORS, request limits, and production settings.

## Example

See [`examples/start-trpc/`](examples/start-trpc/) for a full working example with a Go server and a TanStack Start frontend using `@trpc/client` and `@trpc/tanstack-react-query`.

## Compatibility

- Go 1.26 or newer.
- tRPC v11 client packages. Install `@trpc/server` alongside `@trpc/client` for the generated type imports, even though your server runs in Go.
- Zod 4.5.4 or newer if you use the generated schemas.

See [Compatibility](https://trpcgo.dev/reference/compatibility/) for subscriptions, CORS, and serialization.

## License

MIT
