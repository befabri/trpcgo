---
title: Security & Production
description: Configure validation, input strictness, request limits, CORS, errors, and subscriptions for production.
---

trpcgo checks request formats and enforces configurable limits. Use your own middleware and validators for authentication, authorization, and application rules.

## Enable Runtime Validation

The generator reads `validate` tags to produce Zod schemas. To enforce those rules on the server, install a validator and pass it to the router:

```bash
go get github.com/go-playground/validator/v10
```

```go
import "github.com/go-playground/validator/v10"

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

The default limits are:

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
    trpcgo.WithDev(os.Getenv("APP_ENV") == "development"),
)
```

Plain Go errors are still masked, but stack traces can reveal implementation details. See [Errors](/errors/#sanitization) for which typed error messages are sent to clients.

Development mode is off by default. Checking explicitly for `development` keeps it off when `APP_ENV` is unset.

## Sanitize Error Formatting

Custom error formatters receive request context and raw JSON input. Do not echo secrets, auth tokens, cookies, or arbitrary context values into client responses.

Start with the sanitized fields in `input.Shape` and return an `ErrorEnvelope` to keep HTTP and SSE formatting intact. [Custom Error Formatter](/errors/#custom-error-formatter) shows an example. Use `WithOnError` for detailed server-side logs.

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

By default, CSRF protection covers `POST` only. Queries and subscriptions normally use `GET`, so they can reach your handler even when the browser will block JavaScript from reading the response under CORS. Keep state changes in mutations, and check authentication and authorization in every protected procedure. `SameSite=Lax` or `Strict` session cookies also help by keeping cookies out of cross-site subscription requests.

To check browser origins before a subscription handler runs, enable `trpc.WithSubscriptionOriginCheck(true)`. It extends the Origin/Referer check to GET/SSE subscriptions, accepting same-origin requests and origins allowed by `WithTrustedOrigins`, `WithPublicOrigin`, or `WithCORS`. Wildcard CORS does not allow cookie-bearing cross-origin subscriptions through this check. A request with neither header is rejected only when it carries a cookie, so non-cookie API clients can still connect. POST subscriptions go through the CSRF check first.

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
# From your Go module directory:
go generate ./...

# From your frontend directory:
npm run build
```

Do not rely on dev watch as a production build step.
