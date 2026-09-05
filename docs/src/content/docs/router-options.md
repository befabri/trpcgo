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
| `WithStrictInput(bool)` | `true` | Rejects unknown JSON object fields and trailing JSON tokens. |

Strict input uses Go's `json.Decoder.DisallowUnknownFields` and is enabled by default. Unknown object fields and values that do not match the Go input type return `BAD_REQUEST`; malformed JSON and trailing JSON tokens return `PARSE_ERROR`. Set `WithStrictInput(false)` to ignore unknown object fields using Go's normal `json.Unmarshal` behavior. Malformed JSON and trailing tokens are still rejected.

For `WithMaxBodySize` and `WithMaxBatchSize`, `0` leaves the current setting unchanged. Use `-1` to remove a limit.

## Handler Options

`trpc.NewHandler` accepts options for HTTP-layer behavior:

```go
handler := trpc.NewHandler(router, "/trpc",
    trpc.WithCORS(trpc.CORSConfig{
        AllowedOrigins:   []string{"https://app.example.com"},
        AllowCredentials: true,
    }),
    trpc.WithTrustedOrigins("https://app.example.com"),
)
```

| Option | Default | Behavior |
| --- | --- | --- |
| `trpc.WithContentTypeEnforcement(bool)` | `true` | Requires `Content-Type: application/json` for `POST` requests with bodies. |
| `trpc.WithCSRFProtection(bool)` | `true` | Rejects cross-origin `POST` requests unless the `Origin` or `Referer` is same-origin or trusted. |
| `trpc.WithCSRFRequireOrigin(bool)` | `false` | Rejects all `POST` requests that lack both `Origin` and `Referer`. Cookie-bearing POSTs are rejected without those headers even when this is false. |
| `trpc.WithSubscriptionOriginCheck(bool)` | `false` | Rejects browser subscription requests whose `Origin` or `Referer` is not same-origin, trusted, public, or CORS-allowed. Cookie-bearing subscriptions without either header are rejected. |
| `trpc.WithPublicOrigin(origin)` / `trpc.WithPublicOrigins(origins...)` | none | Treats exact public API origins as same-origin for deployments behind TLS-terminating proxies. |
| `trpc.WithTrustedOrigins(origins...)` | none | Adds exact scheme+host origins that may send cross-origin `POST` requests. |
| `trpc.WithCORS(config)` | disabled | Handles CORS preflights and response headers for configured origins. CORS origins do not grant CSRF trust. |

Handler options are separate from router options because they depend on HTTP deployment details. `WithCORS` accepts exact origins such as `https://app.example.com`; wildcard CORS (`*`) can emit `Access-Control-Allow-Origin: *` when credentials are disabled, but it is not trusted by the CSRF check. For cross-origin browser mutations, configure both CORS read access with `WithCORS` and POST trust with `WithTrustedOrigins`.

`WithSubscriptionOriginCheck(true)` checks subscription origins before their handlers run, accepting same-origin requests and origins from `WithTrustedOrigins`, `WithPublicOrigin`, or `WithCORS`. POST subscriptions go through the normal CSRF check first.

`CORSConfig.AllowedHeaders` replaces the default allow-list. The default is `Authorization`, `Content-Type`, `Last-Event-Id`, and `trpc-accept`; include those headers when you add custom headers and still need auth, tRPC JSONL, or subscription resume support.

Configured origins must be exact `http` or `https` scheme+host values with no path, query, fragment, or user info. Header values such as `Referer` may include a path; trpcgo extracts their origin before comparison.

## Validation Option

`WithValidator` runs after JSON decoding and before middleware. It validates struct inputs, including pointers to structs; primitive, slice, map, and void inputs are skipped.

```go
validate := validator.New()

router := trpcgo.NewRouter(
    trpcgo.WithValidator(validate.Struct),
)
```

`validate` tags do not trigger server-side validation unless you configure this option. A validation failure returns `BAD_REQUEST` with the message `input validation failed`.

## Context And Error Options

| Option | Behavior |
| --- | --- |
| `WithContextCreator(fn)` | Derives the request context from `r.Context()` and `*http.Request`. |
| `WithOnError(fn)` | Receives procedure errors for server-side logging before response formatting. |
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

Use a positive ping interval; `0` uses the default of 10 seconds. For `WithSSEMaxDuration` and `WithSSEMaxConnections`, `0` leaves the current setting unchanged and `-1` removes the limit.

## Generation Options

| Option | Behavior |
| --- | --- |
| `WithTypeOutput(path)` | Writes generated TypeScript in dev mode. |
| `WithZodOutput(path)` | Writes generated Zod schemas in dev mode. |
| `WithZodMini(bool)` | Uses `zod/mini` functional syntax. |
| `WithEnumsOutput(path)` | Writes runtime enum value objects in dev mode. |
| `WithWatchPackages(patterns...)` | Restricts dev watcher analysis to package patterns like `./cmd/api` or `./internal/...`. |

Dev generation starts when `trpc.NewHandler` is constructed and `WithDev(true)` plus `WithTypeOutput(...)` are set. Zod and enum output also require `WithTypeOutput(...)`. Call `router.Close()` during shutdown to stop the watcher.

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
