---
title: Errors
description: Return tRPC-compatible errors, customize formatting, and understand sanitization.
---

trpcgo errors carry a tRPC code, a message, an optional cause, and an HTTP status mapping.

## Return Typed Errors

```go
return User{}, trpcgo.NewError(trpcgo.CodeNotFound, "user not found")
```

```go
return nil, trpcgo.NewErrorf(trpcgo.CodeBadRequest, "invalid id: %s", id)
```

```go
return nil, trpcgo.WrapError(trpcgo.CodeInternalServerError, "database failed", err)
```

Common codes:

| Go constant | tRPC name | HTTP status |
| --- | --- | --- |
| `CodeBadRequest` | `BAD_REQUEST` | `400` |
| `CodeUnauthorized` | `UNAUTHORIZED` | `401` |
| `CodeForbidden` | `FORBIDDEN` | `403` |
| `CodeNotFound` | `NOT_FOUND` | `404` |
| `CodeMethodNotSupported` | `METHOD_NOT_SUPPORTED` | `405` |
| `CodePayloadTooLarge` | `PAYLOAD_TOO_LARGE` | `413` |
| `CodeTooManyRequests` | `TOO_MANY_REQUESTS` | `429` |
| `CodeInternalServerError` | `INTERNAL_SERVER_ERROR` | `500` |

Other standard tRPC-compatible gateway, timeout, conflict, and precondition codes are also available.

## Sanitization

Plain Go errors are converted to `INTERNAL_SERVER_ERROR` before they reach the client.

Internal errors with causes are masked as `internal server error` in client responses. `WithOnError` runs before response formatting and can receive the wrapped cause for server-side logging.

```go
router := trpcgo.NewRouter(
    trpcgo.WithOnError(func(ctx context.Context, err *trpcgo.Error, path string) {
        log.Printf("trpc error on %s: %v", path, err)
    }),
)
```

## Dev Mode

`WithDev(true)` adds Go stack traces to `error.data.stack`. It does not expose wrapped internal cause messages to clients.

Keep dev mode off in production.

## Custom Error Formatter

Use `WithErrorFormatter` to extend or replace the serialized error shape.

```go
router := trpcgo.NewRouter(
    trpcgo.WithErrorFormatter(func(input trpcgo.ErrorFormatterInput) any {
        return map[string]any{
            "error": map[string]any{
                "code":      input.Shape.Error.Code,
                "message":   input.Shape.Error.Message,
                "data":      input.Shape.Error.Data,
                "timestamp": time.Now().UTC().Format(time.RFC3339),
            },
        }
    }),
)
```

`ErrorFormatterInput` includes the client-safe error, procedure type, path, raw JSON input, request context, and default tRPC error shape.

:::caution
The context may contain credentials or other sensitive values. Do not blindly serialize context values into error responses.
:::

## Output Hook Errors

Errors from output validators or output parsers become `INTERNAL_SERVER_ERROR`. For subscriptions, trpcgo sends an SSE `serialized-error` event and closes the stream.
