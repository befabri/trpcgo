---
title: Subscriptions
description: Stream procedure results over Server-Sent Events with tracked IDs and reconnect support.
---

Subscriptions expose a Go channel as a tRPC-compatible SSE stream.

## Register A Subscription

```go
type RoomInput struct {
    RoomID string `json:"roomId" validate:"required"`
}

type Message struct {
    ID   string `json:"id"`
    Text string `json:"text"`
}

trpcgo.MustSubscribe(router, "chat.messages", func(ctx context.Context, input RoomInput) (<-chan Message, error) {
    ch := make(chan Message)
    messages := broker.Subscribe(input.RoomID)

    go func() {
        defer close(ch)
        for {
            select {
            case <-ctx.Done():
                return
            case msg, ok := <-messages:
                if !ok {
                    return
                }
                select {
                case ch <- msg:
                case <-ctx.Done():
                    return
                }
            }
        }
    }()

    return ch, nil
})
```

The request setup phase can return a normal tRPC error. Once streaming starts, later errors are sent as SSE `serialized-error` events.

## Wire Events

The server sends:

| Event | Meaning |
| --- | --- |
| `connected` | First event. Includes `reconnectAfterInactivityMs` when configured. |
| default `message` | Data emitted from the Go channel. |
| `ping` | Keep-alive event. |
| `return` | Stream completed or max duration reached. |
| `serialized-error` | Stream failed after SSE started. |

Data messages use the SSE default event type. They do not include an explicit `event: message` line.

## Client With EventSource

```ts
const source = new EventSource('/trpc/user.onCreated');

source.onmessage = (event) => {
  const user = JSON.parse(event.data);
  console.log(user);
};

source.addEventListener('serialized-error', (event) => {
  console.error(JSON.parse(event.data));
});
```

## Tracked Events

Wrap values with `Tracked` to send an SSE `id` field:

```go
ch <- trpcgo.Tracked("message-42", Message{ID: "42", Text: "hello"})
```

The client receives only the wrapped data as JSON. The tracking ID is transport metadata, not part of the payload.

Use `TrackedEvent` directly when you also need to set SSE retry:

```go
ch <- trpcgo.TrackedEvent[Message]{
    ID:    "message-42",
    Retry: 5000,
    Data:  Message{ID: "42", Text: "hello"},
}
```

trpcgo strips newlines from SSE IDs before writing them.

## Reconnect Input

When the client reconnects with `Last-Event-Id`, trpcgo merges it into subscription input as `lastEventId`.

```go
type StreamInput struct {
    LastEventID string `json:"lastEventId,omitempty"`
}
```

Sources are checked in this order:

- `Last-Event-Id` header.
- `lastEventId` query parameter.
- `Last-Event-Id` query parameter.

Treat `lastEventId` as untrusted client input.

## Final Values

Use `SubscribeWithFinal` when the stream should end with a final value:

```go
trpcgo.MustSubscribeWithFinal(router, "job.progress", func(ctx context.Context, input JobInput) (<-chan Progress, func() any, error) {
    ch := make(chan Progress)
    final := func() any { return JobResult{Done: true} }
    return ch, final, nil
})
```

The final value is sent in the SSE `return` event.

## Limits

Use router options to control stream behavior:

```go
router := trpcgo.NewRouter(
    trpcgo.WithSSEPingInterval(5*time.Second),
    trpcgo.WithSSEMaxDuration(10*time.Minute),
    trpcgo.WithSSEMaxConnections(1000),
    trpcgo.WithSSEReconnectAfterInactivity(30*time.Second),
)
```

`WithSSEMaxConnections` rejects extra streams with `TOO_MANY_REQUESTS`.
