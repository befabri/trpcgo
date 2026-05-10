---
title: Core Concepts
description: Understand how trpcgo connects Go handlers, the tRPC HTTP protocol, and generated TypeScript contracts.
---

trpcgo has three main pieces: a Go procedure registry, an HTTP protocol handler, and TypeScript/Zod generation.

## Router

`Router` owns registered procedures, global middleware, router options, output hooks, and the optional development file watcher.

Create one router for the API surface you want to serve:

```go
router := trpcgo.NewRouter(
    trpcgo.WithBatching(true),
    trpcgo.WithStrictInput(true),
    trpcgo.WithValidator(validate.Struct),
)
defer router.Close()
```

Register all procedures and middleware before constructing the HTTP handler. `trpc.NewHandler(router, "/trpc")` snapshots the current procedure map, so later registrations do not affect that handler.

## Procedures

A procedure is a typed Go function registered at a string path:

```go
trpcgo.MustQuery(router, "user.get", getUser)
trpcgo.MustMutation(router, "user.create", createUser)
trpcgo.MustVoidSubscribe(router, "user.onCreated", onUserCreated)
```

The path becomes the tRPC procedure name. Dot-separated paths become nested TypeScript client properties, so `"user.get"` becomes `client.user.get.query(...)`.

trpcgo supports:

- Queries for reads.
- Mutations for writes.
- Subscriptions for server-sent event streams.
- Void variants for procedures with no input.
- `Must*` variants for startup code where duplicate paths are programmer errors.

## HTTP Handler

Package `github.com/befabri/trpcgo/trpc` exposes the tRPC-compatible HTTP handler:

```go
mux.Handle("/trpc/", trpc.NewHandler(router, "/trpc"))
```

If the base path is `/trpc`, a request to `/trpc/user.get` resolves to procedure `user.get`.

The handler implements the important tRPC wire behavior:

- `GET` queries with `?input=<json>`.
- `POST` mutations with a JSON body.
- Optional query-over-POST when `WithMethodOverride(true)` is set.
- JSON batch requests with `?batch=1`.
- JSONL batch streaming with `trpc-accept: application/jsonl`.
- SSE subscriptions using `text/event-stream`.

## Middleware And Context

Middleware wraps decoded procedure input, not raw JSON. Global middleware runs before per-procedure middleware.

```go
router.Use(requestTimer)

trpcgo.MustMutation(router, "user.create", createUser,
    trpcgo.Use(requireAuth),
    trpcgo.WithMeta(map[string]string{"action": "write"}),
)
```

Inside middleware or handlers, use `GetProcedureMeta(ctx)` to read the active path, procedure type, and custom metadata.

Use `WithContextCreator` to derive a request context from `*http.Request`, for example to attach auth claims or request IDs.

## Generation Paths

There are two ways to generate TypeScript:

- Static analysis: `go tool trpcgo generate` reads Go source with `go/packages`. This is the recommended production path because it sees comments, aliases, const unions, validate tags, and typed output parsers.
- Runtime reflection: `router.GenerateTS(...)` and `router.GenerateZod(...)` read registered procedure reflection types. They are useful at startup but cannot see source-only metadata like Go doc comments.

In development, `WithDev(true)` plus `WithTypeOutput(...)` starts a source-analysis watcher when the HTTP handler is constructed. It generates once from source, then updates generated files on Go file changes if the source still type-checks.

## Source Of Truth

Go types define the API contract:

```go
type CreateUserInput struct {
    Name  string `json:"name" validate:"required,min=1,max=100"`
    Email string `json:"email" validate:"required,email"`
}
```

trpcgo uses those types for request decoding, optional server-side validation, TypeScript generation, and optional Zod generation. The generated frontend types should be treated as build artifacts, not hand-edited source.
