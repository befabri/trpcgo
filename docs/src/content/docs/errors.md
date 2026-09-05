---
title: Errors
description: Return tRPC-compatible errors, customize formatting, and understand sanitization.
---

Use a trpcgo error to choose the code and message a client receives. Each code maps to an HTTP status, and an optional cause preserves the underlying Go error for server-side logging.

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
| `CodeParseError` | `PARSE_ERROR` | `400` |
| `CodeBadRequest` | `BAD_REQUEST` | `400` |
| `CodeUnauthorized` | `UNAUTHORIZED` | `401` |
| `CodeForbidden` | `FORBIDDEN` | `403` |
| `CodeNotFound` | `NOT_FOUND` | `404` |
| `CodeMethodNotSupported` | `METHOD_NOT_SUPPORTED` | `405` |
| `CodePayloadTooLarge` | `PAYLOAD_TOO_LARGE` | `413` |
| `CodeUnsupportedMedia` | `UNSUPPORTED_MEDIA_TYPE` | `415` |
| `CodeTooManyRequests` | `TOO_MANY_REQUESTS` | `429` |
| `CodeInternalServerError` | `INTERNAL_SERVER_ERROR` | `500` |

Other standard tRPC-compatible gateway, timeout, conflict, and precondition codes are also available.

## Sanitization

Plain Go errors are converted to `INTERNAL_SERVER_ERROR` with the message `internal server error` before they reach the client.

`WrapError(CodeInternalServerError, message, cause)` also hides the message and cause from clients. Other typed error messages, including `NewError(CodeInternalServerError, message)` without a cause, are sent as written. Use messages that are safe to show to users.

`WithOnError` receives procedure errors before they are sanitized, so you can log the underlying cause:

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

Use `WithErrorFormatter` to customize error responses. Returning an `ErrorEnvelope` keeps the standard format for both HTTP and SSE:

```go
router := trpcgo.NewRouter(
    trpcgo.WithErrorFormatter(func(input trpcgo.ErrorFormatterInput) any {
        shape := input.Shape
        if input.Error.Code == trpcgo.CodeUnauthorized {
            shape.Error.Message = "please sign in"
        }
        return shape
    }),
)
```

`ErrorFormatterInput` includes the client-safe error, procedure type, path, raw JSON input, request context, and default tRPC error shape.

Use `GetProcedureMeta(input.Ctx)` to read the procedure's metadata.

Requests rejected before context creation, such as malformed batch input or a failed origin check, use the default error shape and bypass both the custom formatter and `WithOnError`. Server-side `RawCall` and `Call` also bypass these hooks; they return sanitized errors directly.

:::caution
The context may contain credentials or other sensitive values. Do not blindly serialize context values into error responses.
:::

## Output Hook Errors

Output validators and parsers follow the same error rules as handlers: plain Go errors become `INTERNAL_SERVER_ERROR`, while typed trpcgo errors keep their code and follow the sanitization rules above. For subscriptions, trpcgo sends an SSE `serialized-error` event and closes the stream.

## Streaming Errors

SSE `serialized-error` events contain the inner error shape:

```json
{
  "message": "access denied",
  "code": -32003,
  "data": {
    "code": "FORBIDDEN",
    "httpStatus": 403,
    "path": "chat.messages"
  }
}
```

HTTP errors keep the outer `{ error: ... }` envelope. For SSE, trpcgo unwraps `ErrorEnvelope` and `*ErrorEnvelope`. If your formatter returns another type, it is sent as-is and must use the shape above.

If an SSE formatter result cannot be encoded as JSON, trpcgo sends a generic `INTERNAL_SERVER_ERROR` shape. [JSONL batch streams](/http-protocol/#jsonl-batch-streaming) also fall back to a built-in error envelope when a custom formatter fails to serialize.

With `httpSubscriptionLink` in `@trpc/client` v11, non-retryable errors reach `onError` with their message and data. Retryable errors (`INTERNAL_SERVER_ERROR`, `BAD_GATEWAY`, `SERVICE_UNAVAILABLE`, and `GATEWAY_TIMEOUT`) appear as `state.error` in `onConnectionStateChange` while the link reconnects.
