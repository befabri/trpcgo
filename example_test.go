package trpcgo_test

import (
	"context"
	"fmt"
	"log"

	"github.com/befabri/trpcgo"
	"github.com/befabri/trpcgo/trpc"
)

func ExampleNewRouter() {
	r := trpcgo.NewRouter(
		trpcgo.WithBatching(true),
		trpcgo.WithDev(true),
	)

	trpcgo.Query(r, "user.get", func(ctx context.Context, input GetUserInput) (User, error) {
		return User{ID: input.ID, Name: "Alice"}, nil
	})

	fmt.Println("router created")
	// Output: router created
}

func ExampleQuery() {
	r := trpcgo.NewRouter()

	trpcgo.Query(r, "user.get", func(ctx context.Context, input GetUserInput) (User, error) {
		return User{ID: input.ID, Name: "Alice"}, nil
	})

	// Call the procedure from Go (no HTTP needed).
	user, err := trpcgo.Call[GetUserInput, User](r, context.Background(), "user.get", GetUserInput{ID: "1"})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(user.Name)
	// Output: Alice
}

func ExampleVoidQuery() {
	r := trpcgo.NewRouter()

	trpcgo.VoidQuery(r, "user.list", func(ctx context.Context) ([]User, error) {
		return []User{{ID: "1", Name: "Alice"}, {ID: "2", Name: "Bob"}}, nil
	})

	users, err := trpcgo.Call[any, []User](r, context.Background(), "user.list", nil)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(len(users))
	// Output: 2
}

func ExampleMutation() {
	r := trpcgo.NewRouter()

	trpcgo.Mutation(r, "user.create", func(ctx context.Context, input CreateUserInput) (User, error) {
		return User{ID: "1", Name: input.Name}, nil
	})

	user, err := trpcgo.Call[CreateUserInput, User](r, context.Background(), "user.create", CreateUserInput{Name: "Bob"})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(user.Name)
	// Output: Bob
}

func ExampleRouter_Use() {
	r := trpcgo.NewRouter()

	// Add a logging middleware to all procedures.
	r.Use(func(next trpcgo.HandlerFunc) trpcgo.HandlerFunc {
		return func(ctx context.Context, input any) (any, error) {
			meta, _ := trpcgo.GetProcedureMeta(ctx)
			fmt.Printf("calling %s\n", meta.Path)
			return next(ctx, input)
		}
	})

	trpcgo.VoidQuery(r, "health", func(ctx context.Context) (string, error) {
		return "ok", nil
	})

	result, err := trpcgo.Call[any, string](r, context.Background(), "health", nil)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(result)
	// Output:
	// calling health
	// ok
}

func ExampleChain() {
	r := trpcgo.NewRouter()

	logger := func(next trpcgo.HandlerFunc) trpcgo.HandlerFunc {
		return func(ctx context.Context, input any) (any, error) {
			fmt.Println("log")
			return next(ctx, input)
		}
	}

	timer := func(next trpcgo.HandlerFunc) trpcgo.HandlerFunc {
		return func(ctx context.Context, input any) (any, error) {
			fmt.Println("time")
			return next(ctx, input)
		}
	}

	// Chain composes middleware left-to-right.
	r.Use(trpcgo.Chain(logger, timer))

	trpcgo.VoidQuery(r, "ping", func(ctx context.Context) (string, error) {
		return "pong", nil
	})

	result, err := trpcgo.Call[any, string](r, context.Background(), "ping", nil)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(result)
	// Output:
	// log
	// time
	// pong
}

func ExampleNewError() {
	err := trpcgo.NewError(trpcgo.CodeNotFound, "user not found")
	fmt.Println(err)
	// Output: trpc error NOT_FOUND: user not found
}

func ExampleWrapError() {
	cause := fmt.Errorf("connection refused")
	err := trpcgo.WrapError(trpcgo.CodeInternalServerError, "database error", cause)
	fmt.Println(err)
	// Output: trpc error INTERNAL_SERVER_ERROR: database error: connection refused
}

func ExampleHTTPStatusFromCode() {
	status := trpcgo.HTTPStatusFromCode(trpcgo.CodeNotFound)
	fmt.Println(status)
	// Output: 404
}

func ExampleCall() {
	r := trpcgo.NewRouter()

	type GreetInput struct {
		Name string `json:"name"`
	}

	trpcgo.Query(r, "greet", func(ctx context.Context, input GreetInput) (string, error) {
		return "Hello, " + input.Name + "!", nil
	})

	// Call invokes a procedure from Go with full type safety.
	msg, err := trpcgo.Call[GreetInput, string](r, context.Background(), "greet", GreetInput{Name: "World"})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(msg)
	// Output: Hello, World!
}

func ExampleSubscribe() {
	r := trpcgo.NewRouter()

	type EventInput struct {
		Topic string `json:"topic"`
	}

	trpcgo.Subscribe(r, "events", func(ctx context.Context, input EventInput) (<-chan string, error) {
		ch := make(chan string)
		// In production, send events on ch from a goroutine and close when done.
		return ch, nil
	})

	fmt.Println("subscription registered")
	// Output: subscription registered
}

func ExampleMergeRouters() {
	users := trpcgo.NewRouter()
	trpcgo.VoidQuery(users, "user.list", func(ctx context.Context) ([]User, error) {
		return nil, nil
	})

	posts := trpcgo.NewRouter()
	trpcgo.VoidQuery(posts, "post.list", func(ctx context.Context) ([]string, error) {
		return nil, nil
	})

	// Merge combines procedures from multiple routers.
	app, err := trpcgo.MergeRouters(users, posts)
	if err != nil {
		fmt.Println(err)
		return
	}
	_ = trpc.NewHandler(app, "/trpc")

	fmt.Println("merged")
	// Output: merged
}
