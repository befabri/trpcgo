---
title: Response Metadata
description: Set response headers and cookies from procedures, middleware, and server-side calls.
---

trpcgo injects response metadata into the context before HTTP procedure execution. Handlers and middleware can add response headers or cookies without depending on `http.ResponseWriter`.

## Set Headers

```go
func handler(ctx context.Context, input Input) (Output, error) {
    trpcgo.SetResponseHeader(ctx, "X-Trace-ID", traceIDFrom(ctx))
    return Output{}, nil
}
```

`SetResponseHeader` adds a header value. It is safe to call from JSONL batch handlers concurrently.

## Set Cookies

```go
func login(ctx context.Context, input LoginInput) (User, error) {
    trpcgo.SetCookie(ctx, &http.Cookie{
        Name:     "session",
        Value:    issueSession(input),
        Path:     "/",
        HttpOnly: true,
        Secure:   true,
        SameSite: http.SameSiteLaxMode,
    })

    return user, nil
}
```

Headers and cookies are applied before the status line is written.

## No-Op Outside Metadata Context

If the context does not carry response metadata, `SetResponseHeader` and `SetCookie` are safe no-ops. The HTTP handler creates the metadata context automatically.

## RawCall

`RawCall` injects response metadata if the context does not already have it, but callers only receive headers and cookies if they keep and inspect the context that carries metadata.

```go
ctx := trpcgo.WithResponseMetadata(context.Background())

result, err := router.RawCall(ctx, "auth.login", rawInput)
if err != nil {
    return err
}

headers := trpcgo.GetResponseHeaders(ctx)
cookies := trpcgo.GetResponseCookies(ctx)
_ = result
_ = headers
_ = cookies
```

Use this when server-side procedure calls need to observe cookies or custom headers set by handlers.
