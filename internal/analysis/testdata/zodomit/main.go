package zodomit

import (
	"context"

	"github.com/befabri/trpcgo"
)

type UpdateUserInput struct {
	ID     string `json:"id" zod_omit:"true"`
	Name   string `json:"name" validate:"required,min=1"`
	Active bool   `json:"active"`
}

type UpdateWithRefine struct {
	ID     int32 `json:"id" zod_omit:"true"`
	MinVal int32 `json:"min_val" validate:"min=1,gtefield=ID"`
	MaxVal int32 `json:"max_val" validate:"min=1,gtefield=MinVal"`
}

type Result struct {
	OK bool `json:"ok"`
}

func updateUser(ctx context.Context, input UpdateUserInput) (Result, error) {
	return Result{}, nil
}

func updateWithRefine(ctx context.Context, input UpdateWithRefine) (Result, error) {
	return Result{}, nil
}

func Setup() *trpcgo.Router {
	router := trpcgo.NewRouter()
	trpcgo.Mutation(router, "user.update", updateUser)
	trpcgo.Mutation(router, "user.updateRefine", updateWithRefine)
	return router
}
