---
title: Subscriptions
description: Stream procedure results over Server-Sent Events with tracked IDs and reconnect support.
---

Subscriptions stream values from a Go channel to the client over Server-Sent Events (SSE).

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

Close the channel when the stream finishes and stop sending when `ctx` is canceled. If your broker needs an unsubscribe call, defer it in the producer goroutine.

The handler can return a normal tRPC error before streaming starts. Once streaming starts, failures such as output validation errors are sent as SSE `serialized-error` events.

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

For the `chat.messages` procedure above, JSON-encode the input in the query string:

```ts
const params = new URLSearchParams({ input: JSON.stringify({ roomId: 'general' }) });
const source = new EventSource(`/trpc/chat.messages?${params}`);

source.onmessage = (event) => {
  const message = JSON.parse(event.data);
  console.log(message);
};

source.addEventListener('serialized-error', (event) => {
  console.error(JSON.parse(event.data));
  source.close();
});

source.addEventListener('return', () => source.close());
```

Call `source.close()` when the component unmounts to stop listening.

For typed subscriptions, use [`httpSubscriptionLink`](https://trpc.io/docs/client/links/httpSubscriptionLink). [Frontend Setup](/frontend-setup/#react-query) shows how to configure it alongside query and mutation links.

## Tracked Events

Wrap values with `Tracked` to send an ID with each event. Return a channel of `TrackedEvent[Message]`:

```go
// Handler return type: (<-chan trpcgo.TrackedEvent[Message], error)
ch := make(chan trpcgo.TrackedEvent[Message])

// Send from the producer goroutine, checking ctx.Done() as above.
ch <- trpcgo.Tracked("message-42", Message{ID: "42", Text: "hello"})
```

The SSE `data` field contains the wrapped value as JSON. A raw `EventSource` exposes the tracking ID separately as `event.lastEventId`.

With `httpSubscriptionLink`, `onData` receives `{ id, data }`:

```ts
const subscription = client.chat.messages.subscribe({ roomId: 'general' }, {
  onData(event) {
    console.log(event.id);        // "message-42"
    console.log(event.data.text); // "hello"
  },
});

// When you stop listening:
subscription.unsubscribe();
```

Untracked messages reach `onData` as the payload itself.

Use `TrackedEvent` directly when you also need to set the reconnect delay in milliseconds:

```go
ch <- trpcgo.TrackedEvent[Message]{
    ID:    "message-42",
    Retry: 5000,
    Data:  Message{ID: "42", Text: "hello"},
}
```

Use a message ID or database cursor that your handler can use to resume the stream.

## Reconnect Input

When the client reconnects, it sends the last event ID. Add `lastEventId` to your input struct so your handler can read it:

```go
type StreamInput struct {
    LastEventID string `json:"lastEventId,omitempty"`
}
```

Sources are checked in this order:

- `Last-Event-Id` header.
- `lastEventId` query parameter.
- `Last-Event-Id` query parameter.

Use `input.LastEventID` to load missed messages from your message store. Your application handles storing and replaying events.

If your input struct does not declare `lastEventId`, strict input drops it instead of rejecting the reconnect.

## Final Values

Use `SubscribeWithFinal` when the stream should end with a final value. This small example sends one progress update, then closes the channel:

```go
type JobInput struct {
    ID string `json:"id"`
}

type Progress struct {
    Percent int `json:"percent"`
}

type JobResult struct {
    Done bool `json:"done"`
}

trpcgo.MustSubscribeWithFinal(router, "job.progress", func(ctx context.Context, input JobInput) (<-chan Progress, func() any, error) {
    ch := make(chan Progress, 1)
    ch <- Progress{Percent: 100}
    close(ch)

    final := func() any { return JobResult{Done: true} }
    return ch, final, nil
})
```

The callback runs after the channel closes, and its value is sent in the SSE `return` event. Keep cleanup in the producer goroutine so it also runs when the client disconnects.

To read the final value, handle `return` with a raw `EventSource`:

```ts
source.addEventListener('return', (event) => {
  if (event.data) console.log(JSON.parse(event.data));
  source.close();
});
```

`httpSubscriptionLink` calls `onComplete` when it receives `return`; it does not deliver the final payload to `onData`. If typed clients need the job result, emit it as a normal stream item or expose a query for it.

If you use [output hooks](/procedures/#output-hooks), the final value must match their input type too.

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

By default, the server sends a ping every 10 seconds and ends streams after 30 minutes. `WithSSEMaxConnections` rejects extra streams with `TOO_MANY_REQUESTS`.

Subscribe again after the duration limit if you need to keep listening.
