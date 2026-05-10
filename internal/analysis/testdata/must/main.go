package must

import (
	"context"

	"github.com/befabri/trpcgo"
)

type Item struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type GetItemInput struct {
	ID string `json:"id"`
}

func getItem(ctx context.Context, input GetItemInput) (Item, error) { return Item{}, nil }
func listItems(ctx context.Context) ([]Item, error)                 { return nil, nil }
func createItem(ctx context.Context, input Item) (Item, error)      { return Item{}, nil }
func resetItems(ctx context.Context) (string, error)                { return "", nil }
func streamItems(ctx context.Context, input GetItemInput) (<-chan Item, error) {
	return nil, nil
}
func broadcastItems(ctx context.Context) (<-chan Item, error) { return nil, nil }
func streamItemsWithFinal(ctx context.Context, input GetItemInput) (<-chan Item, func() any, error) {
	return nil, func() any { return "done" }, nil
}
func broadcastItemsWithFinal(ctx context.Context) (<-chan Item, func() any, error) {
	return nil, func() any { return "done" }, nil
}

func Setup() *trpcgo.Router {
	router := trpcgo.NewRouter()
	trpcgo.MustQuery(router, "item.get", getItem)
	trpcgo.MustVoidQuery(router, "item.list", listItems)
	trpcgo.MustMutation(router, "item.create", createItem)
	trpcgo.MustVoidMutation(router, "item.reset", resetItems)
	trpcgo.MustSubscribe(router, "item.stream", streamItems)
	trpcgo.MustVoidSubscribe(router, "item.broadcast", broadcastItems)
	trpcgo.MustSubscribeWithFinal(router, "item.streamFinal", streamItemsWithFinal)
	trpcgo.MustVoidSubscribeWithFinal(router, "item.broadcastFinal", broadcastItemsWithFinal)
	return router
}
