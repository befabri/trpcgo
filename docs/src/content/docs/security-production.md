---
title: Security & Production
description: Configure validation, input strictness, request limits, CORS, errors, and subscriptions for production.
---

trpcgo provides protocol-level safety checks, but application security still belongs in your middleware, validators, and deployment configuration.

## Enable Runtime Validation

`validate` tags generate Zod schemas, but they do not run on the server unless you configure a validator.

```go
validate := validator.New()

router := trpcgo.NewRouter(
    trpcgo.WithValidator(validate.Struct),
)
```

Validation runs after JSON decoding and only for struct inputs.

## Reject Unknown Fields

Strict input is enabled by default. It rejects unknown JSON object fields and trailing JSON tokens for typed procedure inputs:

```go
router := trpcgo.NewRouter(
    trpcgo.WithStrictInput(true),
)
```

Strict input also applies to `RawCall`. Set `trpcgo.WithStrictInput(false)` only when you intentionally want Go's normal `json.Unmarshal` behavior, which ignores unknown fields.

## Keep Request Limits

Defaults are conservative:

| Limit | Default |
| --- | --- |
| Max body/query input size | `1 MiB` |
| Max batch size | `10` procedures |
| SSE max duration | `30m` |
| SSE max connections | unlimited |

Tune them explicitly for public APIs:

```go
router := trpcgo.NewRouter(
    trpcgo.WithMaxBodySize(512<<10),
    trpcgo.WithMaxBatchSize(20),
    trpcgo.WithSSEMaxConnections(1000),
    trpcgo.WithSSEMaxDuration(10*time.Minute),
)
```

Use `-1` only when you intentionally want an unlimited setting.

## Keep Dev Mode Off

`WithDev(true)` adds stack traces to error responses and enables dev generation behavior. Use it locally, not in production.

```go
router := trpcgo.NewRouter(
    trpcgo.WithDev(os.Getenv("APP_ENV") != "production"),
)
```

Internal error messages are still masked, but stack traces can reveal implementation details.

## Sanitize Error Formatting

Custom error formatters receive request context and raw JSON input. Do not echo secrets, auth tokens, cookies, or arbitrary context values into client responses.

Good formatter pattern:

```go
trpcgo.WithErrorFormatter(func(input trpcgo.ErrorFormatterInput) any {
    return map[string]any{
        "error": map[string]any{
            "code":    input.Shape.Error.Code,
            "message": input.Shape.Error.Message,
            "data":    input.Shape.Error.Data,
        },
    }
})
```

Use `WithOnError` for detailed server-side logs instead.

## Configure CORS

Use `trpc.WithCORS` when browsers call the API from another origin:

```go
trpcHandler := trpc.NewHandler(router, "/trpc",
    trpc.WithCORS(trpc.CORSConfig{
        AllowedOrigins:   []string{"https://app.example.com"},
        AllowedHeaders:   []string{"Authorization", "Content-Type", "Last-Event-Id", "trpc-accept", "X-Request-ID"},
        AllowCredentials: true,
    }),
    trpc.WithTrustedOrigins("https://app.example.com"),
)
```

Only set `AllowCredentials: true` when you intentionally use cookies or other credentialed browser requests, and do not combine it with wildcard origins. `AllowedHeaders` replaces the default list, so keep `Last-Event-Id` if clients resume subscriptions and keep `trpc-accept` if clients request JSONL batch streaming.

## Protect Cookie-Authenticated Browsers

CORS controls which browsers can read responses. It does not by itself protect cookie-authenticated mutation requests from cross-site form or fetch attempts.

The tRPC handler enables Origin/Referer CSRF protection by default for `POST` requests. Same-origin requests are allowed, and exact origins configured with `WithTrustedOrigins` may send cross-origin POSTs. CORS origins are not trusted for CSRF unless you also pass them to `WithTrustedOrigins`.

```go
trpcHandler := trpc.NewHandler(router, "/trpc",
    trpc.WithTrustedOrigins("https://app.example.com"),
)
```

When both `Origin` and `Referer` are missing, non-cookie API clients are allowed by default. Cookie-bearing POSTs are rejected without one of those headers. Use `trpc.WithCSRFRequireOrigin(true)` when every POST to the handler should carry `Origin` or `Referer`.

If the Go server runs behind TLS termination and receives internal `http` requests, add the public API origin with `WithPublicOrigin("https://api.example.com")`. Disable the built-in check with `trpc.WithCSRFProtection(false)` only when another layer enforces it.

CSRF protection covers `POST` only. Queries and subscriptions use `GET`, which gets no Origin/Referer check — CORS, not CSRF, decides who may read their responses. But the resolver runs before that CORS check does, so keep query and subscription resolvers free of side effects and move any state change into a mutation. Setting `SameSite=Lax` (or `Strict`) on session cookies adds a second layer: browsers then omit them from cross-site subscription requests.

For defense in depth on side-effectful subscriptions, enable `trpc.WithSubscriptionOriginCheck(true)`. It extends the Origin/Referer check to GET/SSE subscriptions, accepting same-origin requests and origins allowed by `WithTrustedOrigins`, `WithPublicOrigin`, or `WithCORS`. A request with neither header is rejected only when it carries a cookie, so non-browser clients are unaffected. POST subscriptions go through the CSRF check first.

## Treat Reconnect IDs As Untrusted

For subscriptions, `Last-Event-Id` is merged into input as `lastEventId`. Validate it like any other client input before using it as a cursor.

```go
type StreamInput struct {
    LastEventID string `json:"lastEventId,omitempty" validate:"omitempty,max=200"`
}
```

## Register Before Serving

Because `trpc.NewHandler` snapshots procedures, construct the handler after all routes and middleware are registered. Creating the handler too early can accidentally leave procedures unserved.

## Generate In CI

For production builds, run static generation before building the frontend:

```bash
go generate ./...
npm run build
```

Do not rely on dev watch as a production build step.
