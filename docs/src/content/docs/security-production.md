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

Go's default JSON decoder ignores unknown fields. Enable strict input when you want unknown fields rejected:

```go
router := trpcgo.NewRouter(
    trpcgo.WithStrictInput(true),
)
```

Strict input also applies to `RawCall`.

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

## Add CORS Yourself

trpcgo does not handle CORS. Add it in your HTTP middleware if browsers call the API from another origin.

```go
handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
    w.Header().Set("Access-Control-Allow-Origin", "https://app.example.com")
    w.Header().Set("Access-Control-Allow-Credentials", "true")
    w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
    w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Request-ID, trpc-accept")
    w.Header().Add("Vary", "Origin")
    if r.Method == http.MethodOptions {
        w.WriteHeader(http.StatusNoContent)
        return
    }
    trpcHandler.ServeHTTP(w, r)
})
```

Only set `Access-Control-Allow-Credentials: true` when you intentionally use cookies or other credentialed browser requests, and do not combine it with `Access-Control-Allow-Origin: *`.

## Protect Cookie-Authenticated Browsers

CORS controls which browsers can read responses. It does not by itself protect cookie-authenticated mutation requests from cross-site form or fetch attempts.

For browser dashboards that authenticate with cookies, mount the tRPC handler behind origin/CSRF protection and explicitly trust only your frontend origins:

```go
trpcHandler := trpc.NewHandler(router, "/trpc")

csrf := http.NewCrossOriginProtection()
if err := csrf.AddTrustedOrigin("https://app.example.com"); err != nil {
    log.Fatal(err)
}

mux.Handle("/trpc/", csrf.Handler(trpcHandler))
```

Pair this with credentialed frontend links when the dashboard and API use different origins in development.

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
