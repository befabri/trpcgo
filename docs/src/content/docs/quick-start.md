---
title: Quick Start
description: Build one Go tRPC endpoint, generate TypeScript, and call it from a frontend.
---

This guide creates a `user.create` mutation and calls it with a typed tRPC client.

You'll need Go 1.26 or newer and a TypeScript frontend. If you're starting a new Go project, create a module first:

```bash
mkdir trpcgo-demo
cd trpcgo-demo
go mod init example.com/trpcgo-demo
```

From the module root, install the runtime, generator, and validator:

```bash
go get github.com/befabri/trpcgo@latest
go get -tool github.com/befabri/trpcgo/cmd/trpcgo@latest
go get github.com/go-playground/validator/v10
```

## 1. Define Types And Handler

Save this as `user.go` in the module root:

```go
package main

import "context"

type CreateUserInput struct {
    Name  string `json:"name" validate:"required,min=1,max=100"`
    Email string `json:"email" validate:"required,email"`
}

type User struct {
    ID    string `json:"id" tstype:",readonly"`
    Name  string `json:"name"`
    Email string `json:"email"`
}

func createUser(ctx context.Context, input CreateUserInput) (User, error) {
    return User{ID: "1", Name: input.Name, Email: input.Email}, nil
}
```

## 2. Register And Serve Procedures

Save this as `main.go` alongside `user.go`:

```go
package main

import (
    "log"
    "net/http"

    "github.com/befabri/trpcgo"
    "github.com/befabri/trpcgo/trpc"
    "github.com/go-playground/validator/v10"
)

//go:generate go tool trpcgo generate -o web/gen/trpc.ts --zod web/gen/zod.ts ./...

func main() {
    validate := validator.New()

    router := trpcgo.NewRouter(
        trpcgo.WithDev(true),
        trpcgo.WithStrictInput(true),
        trpcgo.WithValidator(trpcgo.StructValidator(validate.Struct)),
        trpcgo.WithTypeOutput("web/gen/trpc.ts"),
        trpcgo.WithZodOutput("web/gen/zod.ts"),
    )
    defer router.Close()

    trpcgo.MustMutation(router, "user.create", createUser)

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

`WithValidator(trpcgo.StructValidator(validate.Struct))` is what makes `validate` tags run on the server, for struct inputs and for every struct inside slice and map inputs. Without it, the tags still help Zod generation but runtime input validation is disabled.

The sample handler returns a user with a fixed ID and does not store it. Replace that return value with your persistence code when you add a database.

## 3. Generate Types And Start The Server

Run these commands from the Go module root:

```bash
mkdir -p web/gen
go generate ./...
go run .
```

The CLI does not create missing parent directories, so create `web/gen` before the first generation run.

The server listens on `http://localhost:8080`. Keep it running while you try the frontend call below. In dev mode, saving Go files regenerates the frontend files automatically. Restart the server to apply changes to handler behavior.

The generated `trpc.ts` contains `AppRouter`, `RouterInputs`, `RouterOutputs`, and TypeScript definitions for reachable Go types.

The generated `zod.ts` contains schemas for typed procedure inputs (the Go email helper is abbreviated here):

```ts
import { z } from 'zod';

declare function $trpcgoEmail(value: string): boolean;

export const CreateUserInputSchema = z.strictObject({
  name: z.string().min(1).check(z.refine((value) => Array.from(value).length <= 100)),
  email: z.string().check(z.refine($trpcgoEmail)),
}).meta({ id: 'CreateUserInput' });
```

## 4. Call From TypeScript

In your frontend project, install the client packages and Zod:

```bash
npm install @trpc/client@11 @trpc/server@11 zod@4
```

The following example assumes the calling file is `web/client.ts`, next to the `gen` directory. If your frontend lives elsewhere, adjust the Go output paths and these imports to match.

```ts
import { createTRPCClient, httpBatchLink } from '@trpc/client';
import type { AppRouter } from './gen/trpc.js';
import { CreateUserInputSchema } from './gen/zod.js';

const client = createTRPCClient<AppRouter>({
  links: [httpBatchLink({ url: 'http://localhost:8080/trpc' })],
});

const input = CreateUserInputSchema.parse({
  name: 'Alice',
  email: 'alice@example.com',
});

const user = await client.user.create.mutate(input);
```

The handler configuration above trusts a frontend served from `http://localhost:3000`. If your frontend is served from the same origin as the Go handler, you can omit `WithCORS` and `WithTrustedOrigins` and use a relative client URL such as `/trpc`.

If you change `CreateUserInput` or `User` in Go and regenerate, TypeScript uses the updated definitions and flags incompatible client calls.

## Full Example

The repository includes `examples/start-trpc/`, a Go server plus TanStack Start frontend demonstrating queries, mutations, SSE subscriptions, generated Zod schemas, middleware, error formatting, and server-side calls.
