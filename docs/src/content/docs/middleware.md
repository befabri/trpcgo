---
title: Middleware & Metadata
description: Wrap procedure handlers, attach metadata, and derive request context.
---

Middleware has this shape:

```go
type Middleware func(next trpcgo.HandlerFunc) trpcgo.HandlerFunc
```

`HandlerFunc` receives the decoded Go input, or `nil` for a void procedure. Input decoding and any configured validation happen before middleware, so invalid requests never enter the middleware chain.

## Global Middleware

Global middleware applies to every procedure on a router. Add it before creating the HTTP handler, which captures the current middleware chain.

```go
router.Use(func(next trpcgo.HandlerFunc) trpcgo.HandlerFunc {
    return func(ctx context.Context, input any) (any, error) {
        meta, _ := trpcgo.GetProcedureMeta(ctx)
        start := time.Now()

        result, err := next(ctx, input)

        log.Printf("[%s] %s took %s", meta.Type, meta.Path, time.Since(start))
        return result, err
    }
})
```

## Per-Procedure Middleware

Use `trpcgo.Use(...)` on a single procedure or a base procedure builder.

```go
trpcgo.MustMutation(router, "user.create", createUser,
    trpcgo.Use(requireAuth, rateLimit),
)
```

Global middleware runs first, followed by per-procedure middleware in the order you pass it, then the handler. Code after `next(ctx, input)` runs in reverse order as each call returns. Return an error without calling `next` to stop the call before it reaches the handler.

## Server-Side Calls

`Call` and `RawCall` run the same middleware as HTTP calls. They use the current router configuration; an HTTP handler keeps the configuration from when it was created. See [Handler Snapshot](/http-protocol/#handler-snapshot).

## Procedure Metadata

Attach arbitrary metadata with `WithMeta`:

```go
type RouteMeta struct {
    AuthRequired bool
    AuditAction  string
}

trpcgo.MustMutation(router, "user.create", createUser,
    trpcgo.WithMeta(RouteMeta{AuthRequired: true, AuditAction: "user.create"}),
)
```

Read metadata in middleware:

```go
func requireAuth(next trpcgo.HandlerFunc) trpcgo.HandlerFunc {
    return func(ctx context.Context, input any) (any, error) {
        meta, _ := trpcgo.GetProcedureMeta(ctx)
        routeMeta, _ := trpcgo.GetMeta[RouteMeta](ctx)

        if routeMeta.AuthRequired && ctx.Value(userKey) == nil {
            return nil, trpcgo.NewError(trpcgo.CodeUnauthorized, "login required")
        }

        log.Printf("%s %s", meta.Type, meta.Path)
        return next(ctx, input)
    }
}
```

`ProcedureMeta` contains:

| Field | Meaning |
| --- | --- |
| `Path` | Registered procedure path, such as `user.create`. |
| `Type` | `query`, `mutation`, or `subscription`. |
| `Meta` | The value passed to `WithMeta`. |

`GetMeta[T]` returns the zero value and `false` when metadata is missing or has a different Go type. Check the boolean when your middleware requires metadata to be present.

## Request Context

Use `WithContextCreator` to derive the context passed to procedures from the incoming HTTP request.

```go
router := trpcgo.NewRouter(
    trpcgo.WithContextCreator(func(ctx context.Context, r *http.Request) context.Context {
        token := r.Header.Get("Authorization")
        if token != "" {
            ctx = context.WithValue(ctx, authTokenKey, token)
        }
        return ctx
    }),
)
```

Derive the returned context from `ctx` to keep existing request values. Cancellation follows both contexts: if either the original request context or the returned context is canceled, procedure execution sees cancellation. The context creator runs once per HTTP request, so calls in a batch share its values.

## Response Headers And Cookies

Handlers and middleware can set HTTP response metadata through the context:

```go
trpcgo.SetResponseHeader(ctx, "X-Trace-ID", traceID)
trpcgo.SetCookie(ctx, &http.Cookie{Name: "session", Value: sessionID, Path: "/"})
```

See [Response Metadata](/response-metadata/) for details and `RawCall` behavior.
