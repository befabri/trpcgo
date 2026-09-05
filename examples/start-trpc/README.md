# trpcgo + tRPC Example

A Go server with a TanStack Start frontend using `@trpc/client` and `@trpc/tanstack-react-query`.

## Setup

You'll need Go 1.26 or newer, Node.js, and npm. Keep the example inside a clone of this repository: the server's `go.mod` uses the local trpcgo module through a `replace` directive.

From the repository root, start the Go server:

```bash
cd examples/start-trpc/server
go mod download
mkdir -p ../web/gen
go generate ./...
go run .
```

In a second terminal, also starting from the repository root, start the frontend:

```bash
cd examples/start-trpc/web
npm ci
npm run dev
```

Open [http://localhost:3000](http://localhost:3000). Vite proxies `/trpc` requests to the Go server on port 8080.

If port 8080 is busy, the server tries 8081 and 8082. Check its startup log and update the proxy target in `web/vite.config.ts` to match, then restart Vite.

Saving Go files regenerates `web/gen/trpc.ts`, `web/gen/zod.ts`, and `web/gen/enums.ts` while the server is running. Restart the Go server to apply handler changes. Demo data is stored in memory and resets when the server restarts.

## What it demonstrates

- **Queries**: paginated user list, user detail, and a health check with no input
- **Mutations**: create a user, delete a user, and reset the demo using a server-side `Call`
- **Subscriptions**: live feed via SSE (`EventSource`)
- **Validation**: Go `validate` tags drive server-side validation and generated Zod schemas for forms
- **Middleware**: time requests, log mutations, and require an `X-Request-ID` header for the reset mutation
- **Error handling**: custom error formatter with timestamp
- **Type safety**: Go types generate TypeScript `AppRouter` for `@trpc/client`
- **Loaders**: TanStack Router loaders prefetch data with `ensureQueryData`
