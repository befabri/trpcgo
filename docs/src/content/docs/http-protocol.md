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

Other HTTP methods return `METHOD_NOT_SUPPORTED`.

## Inputs

For `GET`, input comes from the `input` query parameter:

```http
GET /trpc/user.get?input={"id":"1"}
```

For `POST`, input comes from the raw request body:

```http
POST /trpc/user.create
Content-Type: application/json

{"name":"Alice","email":"alice@example.com"}
```

Empty input is passed as the zero value for typed procedures or `nil` for void procedures.

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

The response is an array of individual envelopes. If every item has the same HTTP status, the batch response uses that status. Mixed statuses return HTTP `207 Multi-Status`.

Subscriptions cannot be batched.

## JSONL Batch Streaming

Set `trpc-accept: application/jsonl` on a batch request to stream batch results as JSON lines.

```http
GET /trpc/user.get,user.list?batch=1
trpc-accept: application/jsonl
```

JSONL batch calls execute concurrently. Chunks may arrive out of request order, and per-call errors are represented inside their chunks. The HTTP status is `200` after streaming starts.

## Handler Snapshot

`trpc.NewHandler` builds a procedure map when the handler is created. Register or merge procedures before mounting the handler:

```go
trpcgo.MustQuery(router, "user.get", getUser)

handler := trpc.NewHandler(router, "/trpc")

// This registration is not visible to handler.
trpcgo.MustQuery(router, "user.late", lateHandler)
```

If you need dynamic routing, construct a new handler after updating registrations.
