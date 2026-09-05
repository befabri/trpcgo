---
title: Compatibility
description: Supported Go, tRPC client, HTTP, Zod, and CORS expectations.
---

## Go

trpcgo requires Go 1.26 or newer.

The module's `go.mod` sets this minimum, and the implementation uses APIs such as `errors.AsType`.

## tRPC Client

Generated router types target tRPC v11 client packages.

The generated `AppRouter` type imports from `@trpc/server`, which should be installed in the frontend package alongside `@trpc/client`. Keep both packages on the same v11 version. The server package supplies TypeScript types; your server still runs in Go.

### Subscription Limitations

Use `httpSubscriptionLink` for typed SSE subscriptions. Its callbacks handle events as follows:

| Feature | Behavior |
| --- | --- |
| Tracked events | `onData` receives `{ id: string; data: T }` for `TrackedEvent[T]` items. IDs must be nonempty and contain no CR, LF, or NUL characters. |
| Streamed errors | Non-retryable errors reach `onError`. Retryable errors appear in `state.error` in `onConnectionStateChange` while the link reconnects. |
| Final values | `return` calls `onComplete`. Its payload is not passed to `onData`; use a raw `EventSource` or send the result as a normal stream item. |
| Duration limit | The server sends `return` and closes the connection. Start a new subscription to continue listening. |

See [Subscriptions](/subscriptions/) for tracked events, reconnect input, and final values, and [Errors](/errors/#streaming-errors) for retryable error codes.

If your generated subscription types describe `TrackedEvent[T]` as `T`, regenerate them and update callbacks to read the payload from `event.data`. Raw `EventSource` handlers for `serialized-error` should read `JSON.parse(event.data)` directly; the default SSE error has no outer `error` property. Custom formatter handling is described in [Streaming Errors](/errors/#streaming-errors).

## Generated Types

The CLI preserves generic declarations such as `Page<T>`. Reflection generates a separate concrete type for each instantiation.

If you use `GenerateTS` or `GenerateZod`, regenerate both files after upgrading and update imports of older generic type names. Use `RouterInputs` and `RouterOutputs` to refer to procedure types without importing the generated interfaces directly.

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

- `time.Time` is represented as an RFC 3339 string in TypeScript.
- `[]byte` is represented as a base64 string.
- `json.RawMessage`, `any`, and `interface{}` become `unknown`.
- `int64` and `uint64` generate number-based Zod schemas because JSON sends numbers, not JavaScript `bigint` values.

JavaScript numbers cannot represent every 64-bit integer exactly. Send large identifiers as strings when they can exceed `Number.MAX_SAFE_INTEGER` (`9,007,199,254,740,991`). A TypeScript type override alone does not change Go's JSON encoding.

Generated optional fields do not automatically accept JSON `null`. See [Struct Tags](/struct-tags/) for pointer fields, `omitempty`, and required fields.

## Example App

`examples/start-trpc/` contains a complete Go server and TanStack Start frontend showing:

- Queries, mutations, void procedures, and SSE subscriptions.
- Generated `AppRouter`, `RouterInputs`, and `RouterOutputs`.
- Generated Zod schemas used for form validation.
- Middleware, metadata, custom error formatting, and server-side `Call`.
