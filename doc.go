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
//	r := trpcgo.NewRouter(trpcgo.WithDev(true), trpcgo.WithTypeOutput("gen/trpc.ts"))
//	defer r.Close()
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
// # Router Options
//
// Configure the router with functional options:
//
//   - [WithBatching] — enable/disable batch request support
//   - [WithMethodOverride] — allow POST for queries
//   - [WithMaxBodySize] — request body size limit (default 1 MB)
//   - [WithMaxBatchSize] — max procedures per batch (default 10)
//   - [WithStrictInput] — reject unknown JSON fields
//   - [WithValidator] — input validation (e.g. go-playground/validator)
//   - [WithDev] — development mode with stack traces and file watcher
//   - [WithErrorFormatter] — custom error response shapes
//   - [WithContextCreator] — custom context per request
//   - [WithOnError] — error callback for logging
//   - [WithSSEPingInterval], [WithSSEMaxDuration], [WithSSEMaxConnections] — SSE tuning
//   - [WithTypeOutput], [WithZodOutput], [WithZodMini] — code generation
//
// # Middleware
//
// Global middleware applies to all procedures via [Router.Use].
// Per-procedure middleware is set with the [Use] procedure option.
// Access procedure metadata with [GetProcedureMeta] or the typed [GetMeta].
// Compose multiple middleware with [Chain].
//
// # Error Handling
//
// Return [Error] values from handlers with JSON-RPC 2.0 error codes.
// Use [NewError], [NewErrorf], or [WrapError] to create errors.
// All 20 standard tRPC error codes are provided as constants (e.g.
// [CodeNotFound], [CodeUnauthorized], [CodeTooManyRequests]).
// Use [HTTPStatusFromCode] and [NameFromCode] for code conversions.
//
// # Merging and Lifecycle
//
// Combine procedures from multiple routers with [MergeRouters] or [Router.Merge].
// Call [Router.Close] to stop the file watcher on shutdown.
//
// # Server-Side Calls
//
// Invoke procedures from Go without HTTP using [Call] (typed) or
// [Router.RawCall] (untyped). Both run the full middleware chain.
//
// See the project README for full documentation, frontend setup guides,
// struct tag reference, and working examples:
// https://github.com/befabri/trpcgo
package trpcgo
