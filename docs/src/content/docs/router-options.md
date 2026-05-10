---
title: Router & Options
description: Configure the trpcgo runtime, serve procedures over HTTP, and merge routers.
---

`Router` owns procedure registrations and runtime configuration.

```go
router := trpcgo.NewRouter(
    trpcgo.WithBatching(true),
    trpcgo.WithStrictInput(true),
    trpcgo.WithValidator(validate.Struct),
)
defer router.Close()
```

## Serve A Router

Use the `trpc` package to serve a router over the tRPC HTTP protocol:

```go
mux := http.NewServeMux()
mux.Handle("/trpc/", trpc.NewHandler(router, "/trpc"))
```

The base path is stripped before procedure lookup. With base path `/trpc`, `/trpc/user.get` maps to procedure `user.get`.

:::caution
`trpc.NewHandler` snapshots the router procedure map. Register procedures, merge routers, and add global middleware before constructing the handler.
:::

## Request Options

| Option | Default | Behavior |
| --- | --- | --- |
| `WithBatching(bool)` | `true` | Enables tRPC batch requests with `?batch=1`. |
| `WithMethodOverride(bool)` | `false` | Allows queries to be called with `POST`. |
| `WithMaxBodySize(n)` | `1 MiB` | Limits POST bodies and GET `input` query values. `-1` disables the limit. |
| `WithMaxBatchSize(n)` | `10` | Limits procedures in one batch. `-1` disables the limit. |
| `WithStrictInput(bool)` | `false` | Rejects unknown JSON object fields with `BAD_REQUEST`. |

Strict input uses Go's `json.Decoder.DisallowUnknownFields`. By default, unknown JSON fields are silently ignored like normal `json.Unmarshal`.

## Validation Option

`WithValidator` runs after JSON decoding and only for struct-typed inputs.

```go
validate := validator.New()

router := trpcgo.NewRouter(
    trpcgo.WithValidator(validate.Struct),
)
```

`validate` tags do not run at runtime unless you configure this option.

## Context And Error Options

| Option | Behavior |
| --- | --- |
| `WithContextCreator(fn)` | Derives the request context from `r.Context()` and `*http.Request`. |
| `WithOnError(fn)` | Receives tRPC errors for server-side logging/observability before response formatting. |
| `WithErrorFormatter(fn)` | Changes the serialized error shape sent to clients. |
| `WithDev(bool)` | Adds stack traces to error responses and enables dev generation behavior. |

Example context creator:

```go
router := trpcgo.NewRouter(
    trpcgo.WithContextCreator(func(ctx context.Context, r *http.Request) context.Context {
        if reqID := r.Header.Get("X-Request-ID"); reqID != "" {
            ctx = context.WithValue(ctx, requestIDKey, reqID)
        }
        return ctx
    }),
)
```

The returned context still cancels when the original request context cancels.

## SSE Options

| Option | Default | Behavior |
| --- | --- | --- |
| `WithSSEPingInterval(d)` | `10s` | Sends keep-alive `ping` events. |
| `WithSSEMaxDuration(d)` | `30m` | Closes streams with a `return` event after the duration. `-1` means unlimited. |
| `WithSSEReconnectAfterInactivity(d)` | disabled | Sends `reconnectAfterInactivityMs` in the `connected` event. |
| `WithSSEMaxConnections(n)` | unlimited | Rejects extra streams with `TOO_MANY_REQUESTS`. |

## Generation Options

| Option | Behavior |
| --- | --- |
| `WithTypeOutput(path)` | Writes generated TypeScript in dev mode. |
| `WithZodOutput(path)` | Writes generated Zod schemas in dev mode. |
| `WithZodMini(bool)` | Uses `zod/mini` functional syntax. |
| `WithWatchPackages(patterns...)` | Restricts dev watcher analysis to package patterns like `./cmd/api` or `./internal/...`. |

Dev generation starts when `trpc.NewHandler` is constructed and `WithDev(true)` plus `WithTypeOutput(...)` are set.

## Router Merging

Use router merging to split registrations across packages.

```go
userRouter := trpcgo.NewRouter()
trpcgo.MustQuery(userRouter, "user.list", listUsers)

adminRouter := trpcgo.NewRouter()
trpcgo.MustMutation(adminRouter, "admin.ban", banUser)

apiRouter := trpcgo.NewRouter()
if err := apiRouter.Merge(userRouter, adminRouter); err != nil {
    log.Fatal(err)
}
```

`Merge` is atomic: if any duplicate path is found, no procedures are copied. Source router options and global middleware are not copied, only procedures with their per-procedure middleware, metadata, and output hooks.

`MergeRouters(...)` creates a new router with default options and no global middleware.
