// Package typecontracts defines the Go API exercised by the compiler tests.
// Both static analysis and runtime reflection must preserve its client contract.
package typecontracts

import (
	"context"

	"github.com/befabri/trpcgo"
)

type CreateUserInput struct {
	Name       string   `json:"name" validate:"required,min=1"`
	Email      string   `json:"email" validate:"required,email"`
	Nickname   string   `json:"nickname,omitempty"`
	Labels     []string `json:"labels"`
	InternalID string   `json:"internalId,omitempty" zod_omit:"true"`
}

type GetUserInput struct {
	ID string `json:"id"`
}

type User struct {
	ID          string  `json:"id" tstype:",readonly"`
	Name        string  `json:"name"`
	Email       string  `json:"email"`
	Nickname    *string `json:"nickname,omitempty"`
	Payload     any     `json:"payload"`
	Children    []User  `json:"children"`
	SecretToken string  `json:"-"`
}

func NewRouter(opts ...trpcgo.Option) *trpcgo.Router {
	r := trpcgo.NewRouter(opts...)
	trpcgo.MustMutation(r, "user.create", func(context.Context, CreateUserInput) (User, error) {
		return User{}, nil
	})
	trpcgo.MustQuery(r, "user.get", func(context.Context, GetUserInput) (User, error) {
		return User{}, nil
	})
	trpcgo.MustVoidQuery(r, "user.list", func(context.Context) ([]User, error) {
		return nil, nil
	})
	trpcgo.MustVoidSubscribe(r, "user.events", func(context.Context) (<-chan trpcgo.TrackedEvent[User], error) {
		return nil, nil
	})
	return r
}
