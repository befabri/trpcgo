---
title: Response Metadata
description: Set response headers and cookies from procedures, middleware, and server-side calls.
---

Handlers and middleware can add response headers or cookies through the request context. The HTTP handler collects these values and writes them to the response.

## Set Headers

```go
func handler(ctx context.Context, input Input) (Output, error) {
    trpcgo.SetResponseHeader(ctx, "X-Trace-ID", traceIDFrom(ctx))
    return Output{}, nil
}
```

`SetResponseHeader` appends a header value; repeated calls with the same name add values rather than replace them. Metadata collection is safe for concurrent use, but streaming responses have timing limits described below.

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

## When Metadata Is Sent

Headers and cookies must be collected before the response starts:

| Response type | When to set headers and cookies |
| --- | --- |
| Query or mutation | In middleware or the handler. They are included even when the procedure returns an error. |
| Regular JSON batch | In any procedure in the batch. All calls share one HTTP response, so their metadata is combined. |
| SSE subscription | Before the subscription handler returns its channel. Changes made while emitting events arrive too late. |
| JSONL batch stream | Headers and cookies set by procedures are omitted because the response starts before the procedures run. |

Use a single mutation or a regular JSON batch for operations such as login that need to set a cookie.

## No-Op Outside Metadata Context

If the context does not carry response metadata, `SetResponseHeader` and `SetCookie` are safe no-ops. The HTTP handler creates the metadata context automatically.

## RawCall

To read headers or cookies after a server-side call, create the metadata context yourself and pass it to `RawCall` or `Call`:

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

Without `WithResponseMetadata`, `RawCall` creates its own metadata context internally, so the collected values are not accessible through your original context. Server-side calls only collect metadata; your code decides how to use it.
