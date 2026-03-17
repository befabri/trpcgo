# trpcgo + tRPC Example

Go server + TanStack Start frontend using `@trpc/client` and `@trpc/tanstack-react-query`.

## Setup

```bash
# 1. Start the Go server
cd server
go mod tidy
go generate ./...
go run main.go

# 2. In another terminal, start the frontend
cd web
npm install
npm run dev
```

Open http://localhost:3000

## What it demonstrates

- **Queries**: user list (paginated), user detail, health check (void)
- **Mutations**: create user, delete user, reset demo (void, uses server-side `Call`)
- **Subscriptions**: live feed via SSE (`EventSource`)
- **Validation**: Go `validate` tags generate Zod schemas, used for client-side form validation
- **Middleware**: global (request timer), per-procedure (log mutation, require X-Request-ID)
- **Error handling**: custom error formatter with timestamp
- **Type safety**: Go types generate TypeScript `AppRouter` for `@trpc/client`
- **Loaders**: TanStack Router loaders pre-fetch data with `ensureQueryData`
