---
title: HTTP Protocol
description: How the trpcgo HTTP handler maps tRPC requests to Go procedures.
---

The `trpc` package implements the tRPC HTTP wire format on top of `net/http`.

## Mounting

```go
mux.Handle("/trpc/", trpc.NewHandler(router, "/trpc"))
```

The base path is stripped before procedure lookup:

| URL | Procedure |
| --- | --- |
| `/trpc/user.get` | `user.get` |
| `/trpc/admin.audit.list` | `admin.audit.list` |

Path traversal segments `.` and `..` are rejected.

## Methods

| Procedure type | Method |
| --- | --- |
| Query | `GET` by default. `POST` only with `WithMethodOverride(true)`. |
| Mutation | `POST`. |
| Subscription | `GET` or `POST`, served as SSE after setup succeeds. |

Other HTTP methods return `METHOD_NOT_SUPPORTED`, except CORS preflight `OPTIONS` requests handled by `trpc.WithCORS`.

## Inputs

For `GET`, input comes from the `input` query parameter:

```http
GET /trpc/user.get?input={"id":"1"}
```

The examples show readable JSON. URL-encode the `input` parameter in actual requests; the tRPC client handles this for you. With `curl`:

```sh
curl --get 'http://localhost:8080/trpc/user.get' \
  --data-urlencode 'input={"id":"1"}'
```

For `POST`, input comes from the raw request body:

```http
POST /trpc/user.create
Content-Type: application/json

{"name":"Alice","email":"alice@example.com"}
```

Missing input is passed as the Go zero value for typed procedures or `nil` for void procedures. A configured input validator still checks struct inputs, so omitted required fields can fail validation.

JSON `null` follows Go's decoding rules. For an `any` or interface input, the handler receives `nil`. This also applies to subscriptions and server-side calls.

`POST` requests with bodies must use `Content-Type: application/json`; charset parameters are allowed, and empty-body `POST` requests do not need a content type.

The handler checks `Origin` or `Referer` on `POST` requests by default. Same-origin requests and origins configured with `trpc.WithTrustedOrigins` are accepted. Requests without either header are allowed only when they do not carry cookies, unless you also enable `trpc.WithCSRFRequireOrigin(true)` to require an origin for all POSTs.

`GET` subscriptions use a separate, optional origin check. Enable `trpc.WithSubscriptionOriginCheck(true)` to check their origins before handlers run. For reverse proxies that terminate TLS, configure the public API origin with `trpc.WithPublicOrigin`. See [Router & Options](/router-options/#handler-options) for the full CORS and origin configuration.

## Success Envelope

Normal query and mutation responses are wrapped in the tRPC result envelope:

```json
{
  "result": {
    "data": {
      "id": "1",
      "name": "Alice"
    }
  }
}
```

## Error Envelope

Errors use the tRPC error shape:

```json
{
  "error": {
    "code": -32004,
    "message": "procedure not found",
    "data": {
      "code": "NOT_FOUND",
      "httpStatus": 404,
      "path": "user.missing"
    }
  }
}
```

`WithDev(true)` adds `data.stack` for debugging.

SSE `serialized-error` events carry the inner `{ code, message, data }` object. See [Streaming Errors](/errors/#streaming-errors) for custom formatter behavior.

## JSON Batching

Batch requests use `?batch=1` and comma-separated procedure paths.

For `GET`, the `input` query parameter is an object keyed by batch index:

```http
GET /trpc/user.get,system.health?batch=1&input={"0":{"id":"1"}}
```

For `POST`, the body has the same indexed shape:

```json
{
  "0": { "id": "1" },
  "1": { "page": 1, "perPage": 20 }
}
```

Regular JSON batch calls run sequentially in request order. The response is an array of envelopes in the same order. Each call follows the method rules above: a POST batch containing queries needs `WithMethodOverride(true)`.

If every item has the same HTTP status, the batch response uses that status. Mixed statuses return HTTP `207 Multi-Status`.

Subscriptions cannot be batched.

## JSONL Batch Streaming

Set `trpc-accept: application/jsonl` on a batch request to stream batch results as JSON lines.

```http
GET /trpc/user.get,user.list?batch=1
trpc-accept: application/jsonl
```

JSONL batch calls execute concurrently. Chunks may arrive out of request order, and per-call errors are represented inside their chunks. The HTTP status is `200` after streaming starts.

If a result cannot be encoded as JSON, that call receives a complete `INTERNAL_SERVER_ERROR` envelope and the other results continue streaming. If the custom error formatter's result also cannot be encoded, trpcgo uses its built-in error envelope for that call.

Headers are sent before the procedures run, so cookies and response headers set by those procedures are not included. Use regular JSON requests for calls that need to set cookies. See [Response Metadata](/response-metadata/#when-metadata-is-sent).

## Handler Snapshot

`trpc.NewHandler` builds a procedure map when the handler is created. Register or merge procedures before mounting the handler:

```go
trpcgo.MustQuery(router, "user.get", getUser)

handler := trpc.NewHandler(router, "/trpc")

// This registration is not visible to handler.
trpcgo.MustQuery(router, "user.late", lateHandler)
```

If you need dynamic routing, construct a new handler after updating registrations.
