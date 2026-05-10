---
title: Quick Start
description: Build one Go tRPC endpoint, generate TypeScript, and call it from a frontend.
---

This guide creates a `user.create` mutation and calls it with a typed tRPC client.

## 1. Define Types And Handler

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
        trpcgo.WithValidator(validate.Struct),
        trpcgo.WithTypeOutput("web/gen/trpc.ts"),
        trpcgo.WithZodOutput("web/gen/zod.ts"),
    )
    defer router.Close()

    trpcgo.MustMutation(router, "user.create", createUser)

    mux := http.NewServeMux()
    mux.Handle("/trpc/", trpc.NewHandler(router, "/trpc"))

    log.Fatal(http.ListenAndServe(":8080", mux))
}
```

`WithValidator(validate.Struct)` is what makes `validate` tags run on the server. Without it, the tags still help Zod generation but runtime input validation is disabled.

## 3. Generate Types

```bash
mkdir -p web/gen
go generate ./...
```

The CLI writes output files directly and does not create missing parent directories, so create `web/gen` before the first generation run.

The generated `trpc.ts` contains `AppRouter`, `RouterInputs`, `RouterOutputs`, and TypeScript definitions for reachable Go types.

The generated `zod.ts` contains schemas for typed procedure inputs:

```ts
import { z } from 'zod';

export const CreateUserInputSchema = z.object({
  name: z.string().min(1).max(100),
  email: z.email(),
});
```

## 4. Call From TypeScript

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

If you change `CreateUserInput` or `User` in Go and regenerate, TypeScript call sites update immediately.

## Full Example

The repository includes `examples/start-trpc/`, a Go server plus TanStack Start frontend demonstrating queries, mutations, SSE subscriptions, generated Zod schemas, middleware, error formatting, and server-side calls.
