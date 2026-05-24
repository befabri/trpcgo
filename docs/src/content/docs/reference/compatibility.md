---
title: Compatibility
description: Supported Go, tRPC client, HTTP, Zod, and CORS expectations.
---

## Go

trpcgo requires Go 1.26 or newer.

The project uses modern Go features including generics, tool directives, and `errors.AsType`.

## tRPC Client

Generated router types target tRPC v11 client packages.

The generated `AppRouter` type imports from `@trpc/server`, which should be installed in the frontend package alongside `@trpc/client`.

## Zod

Generated schemas target Zod 4.

Use `--zod-mini` or `WithZodMini(true)` to generate `zod/mini` functional syntax instead of standard chained syntax.

## HTTP

The runtime is plain `net/http`. No web framework is required.

You can mount `trpc.NewHandler(router, basePath)` behind any router or middleware stack that can serve an `http.Handler`.

## CORS

trpcgo includes optional CORS handling through `trpc.WithCORS`. You can also handle CORS in your web framework, HTTP middleware, reverse proxy, or edge layer.

## Serialization

trpcgo uses JSON over the tRPC HTTP protocol.

Notable mappings:

- `time.Time` is represented as a string in TypeScript.
- `[]byte` is represented as a string.
- `json.RawMessage`, `any`, and `interface{}` become `unknown`.
- `int64` and `uint64` generate number-based Zod schemas because JSON sends numbers, not JavaScript `bigint` values.

## Example App

`examples/start-trpc/` contains a complete Go server and TanStack Start frontend showing:

- Queries, mutations, void procedures, and SSE subscriptions.
- Generated `AppRouter`, `RouterInputs`, and `RouterOutputs`.
- Generated Zod schemas used for form validation.
- Middleware, metadata, custom error formatting, and server-side `Call`.
