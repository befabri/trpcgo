// Package trpcgo is a Go-first tRPC framework that lets you define procedures
// in Go and automatically generates TypeScript types for @trpc/client.
//
// Write Go structs and handlers, run [trpcgo generate], and get a fully typed
// TypeScript AppRouter — no manual type definitions needed.
//
// # Quick Start
//
// Define a router with procedures:
//
//	type GetUserInput struct {
//		ID int `json:"id"`
//	}
//
//	type User struct {
//		ID   int    `json:"id"`
//		Name string `json:"name"`
//	}
//
//	r := trpcgo.NewRouter()
//
//	trpcgo.Query(r, "user.get", func(ctx context.Context, input GetUserInput) (User, error) {
//		return User{ID: input.ID, Name: "Alice"}, nil
//	})
//
//	http.Handle("/trpc/", r.Handler("/trpc"))
//
// # Procedures
//
// Six registration functions cover all tRPC procedure types:
//
//   - [Query] and [VoidQuery] for read operations (GET)
//   - [Mutation] and [VoidMutation] for write operations (POST)
//   - [Subscribe] and [VoidSubscribe] for real-time streams (SSE)
//
// Void variants are for procedures that take no input.
//
// # Router Options
//
// Configure the router with functional options:
//
//   - [WithBatching] — enable/disable batch request support
//   - [WithValidator] — input validation (e.g. go-playground/validator)
//   - [WithDev] — development mode with stack traces
//   - [WithErrorFormatter] — custom error response shapes
//   - [WithContextCreator] — custom context per request
//   - [WithTypeOutput] — automatic TypeScript generation at startup
//   - [WithZodOutput] — Zod schema generation alongside types
//
// # Middleware
//
// Global middleware applies to all procedures via [Router.Use].
// Per-procedure middleware is set with the [Use] procedure option.
// Compose multiple middleware with [Chain].
//
// # Error Handling
//
// Return [Error] values from handlers with JSON-RPC 2.0 error codes.
// Use [NewError], [NewErrorf], or [WrapError] to create errors.
// All standard tRPC error codes are provided as constants (e.g.
// [CodeNotFound], [CodeUnauthorized]).
//
// # Merging Routers
//
// Combine procedures from multiple routers with [MergeRouters].
//
// # Server-Side Calls
//
// Invoke procedures from Go without HTTP using [Call] (typed) or
// [Router.RawCall] (untyped). Both run the full middleware chain.
//
// # Code Generation
//
// The trpcgo CLI generates TypeScript types from Go source:
//
//	//go:generate trpcgo generate -output ../web/gen/trpc.ts
//
// Install the CLI with:
//
//	go get -tool github.com/befabri/trpcgo/cmd/trpcgo
package trpcgo
