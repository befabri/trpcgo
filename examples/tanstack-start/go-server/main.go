package main

import (
	"context"
	"fmt"
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/trpcgo/trpcgo"
)

// Types — this is the source of truth for both Go and TypeScript.

type User struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Email       string `json:"email"`
	Description string `json:"description"`
	Password    string `json:"password"`
}

type GetUserByIdInput struct {
	ID string `json:"id"`
}

type CreateUserInput struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

// Handlers

type userService struct {
	users []User
}

func (s *userService) GetUserById(ctx context.Context, input GetUserByIdInput) (User, error) {
	for _, u := range s.users {
		if u.ID == input.ID {
			return u, nil
		}
	}
	return User{}, trpcgo.NewError(trpcgo.CodeNotFound, fmt.Sprintf("user %q not found", input.ID))
}

func (s *userService) ListUsers(ctx context.Context) ([]User, error) {
	return s.users, nil
}

func (s *userService) CreateUser(ctx context.Context, input CreateUserInput) (User, error) {
	user := User{
		ID:    fmt.Sprintf("%d", len(s.users)+1),
		Name:  input.Name,
		Email: input.Email,
	}
	s.users = append(s.users, user)
	return user, nil
}

func (s *userService) OnUserCreated(ctx context.Context) (<-chan User, error) {
	ch := make(chan User)
	go func() {
		defer close(ch)
		// In a real app, this would listen to a message bus or database changes.
	}()
	return ch, nil
}

func main() {
	svc := &userService{
		users: []User{
			{ID: "1", Name: "Alice", Email: "alice@example.com"},
			{ID: "2", Name: "Bob", Email: "bob@example.com"},
		},
	}

	router := trpcgo.NewRouter(
		trpcgo.WithBatching(true),
		trpcgo.WithTypeOutput("../web/gen/trpc.ts"),
		trpcgo.WithOnError(func(ctx context.Context, err *trpcgo.Error, path string) {
			log.Printf("tRPC error on %q: %v", path, err)
		}),
	)

	trpcgo.Query(router, "user.getUserById", svc.GetUserById)
	trpcgo.VoidQuery(router, "user.listUsers", svc.ListUsers)
	trpcgo.Mutation(router, "user.createUser", svc.CreateUser)
	trpcgo.VoidSubscribe(router, "user.onCreated", svc.OnUserCreated)

	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	// CORS for dev
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
			if r.Method == "OPTIONS" {
				w.WriteHeader(204)
				return
			}
			next.ServeHTTP(w, r)
		})
	})

	// TypeScript types are written automatically when Handler() is called.
	r.Handle("/trpc/*", router.Handler("/trpc"))

	log.Println("Go tRPC server listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", r))
}
