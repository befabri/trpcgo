package ssecompat

import (
	"context"

	"github.com/befabri/trpcgo"
)

type Message struct {
	Text string `json:"text"`
}

type Nested struct {
	Event trpcgo.TrackedEvent[Message] `json:"event"`
}

func Router() *trpcgo.Router {
	r := trpcgo.NewRouter()
	// The a/b prefixes make the string and int payloads reflect before the
	// struct ones, so confusing ID with the payload type is caught.
	trpcgo.MustVoidQuery(r, "aString", func(context.Context) (trpcgo.TrackedEvent[string], error) {
		return trpcgo.Tracked("42", "hello"), nil
	})
	trpcgo.MustVoidQuery(r, "bInt", func(context.Context) (trpcgo.TrackedEvent[int], error) {
		return trpcgo.Tracked("43", 99), nil
	})
	trpcgo.MustVoidSubscribe(r, "tracked", func(context.Context) (<-chan trpcgo.TrackedEvent[Message], error) {
		return nil, nil
	})
	trpcgo.MustVoidSubscribe(r, "pointer", func(context.Context) (<-chan *trpcgo.TrackedEvent[Message], error) {
		return nil, nil
	})
	trpcgo.MustVoidSubscribe(r, "plain", func(context.Context) (<-chan Message, error) {
		return nil, nil
	})
	trpcgo.MustVoidSubscribe(r, "nested", func(context.Context) (<-chan Nested, error) {
		return nil, nil
	})
	trpcgo.MustVoidQuery(r, "query", func(context.Context) (trpcgo.TrackedEvent[Message], error) {
		return trpcgo.Tracked("42", Message{Text: "hello"}), nil
	})
	return r
}
