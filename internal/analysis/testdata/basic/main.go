package basic

import (
	"context"

	"github.com/befabri/trpcgo"
)

type User struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

type GetUserByIdInput struct {
	ID string `json:"id"`
}

type CreateUserInput struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

func getUserById(ctx context.Context, input GetUserByIdInput) (User, error) {
	return User{}, nil
}

func listUsers(ctx context.Context) ([]User, error) {
	return nil, nil
}

func createUser(ctx context.Context, input CreateUserInput) (User, error) {
	return User{}, nil
}

func onUserCreated(ctx context.Context) (<-chan User, error) {
	return nil, nil
}

func Setup() *trpcgo.Router {
	router := trpcgo.NewRouter()
	trpcgo.Query(router, "user.getById", getUserById)
	trpcgo.VoidQuery(router, "user.listUsers", listUsers)
	trpcgo.Mutation(router, "user.createUser", createUser)
	trpcgo.VoidSubscribe(router, "user.onCreated", onUserCreated)
	return router
}
