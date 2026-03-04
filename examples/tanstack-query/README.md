# tanstack-query example

A full-stack example using trpcgo with a Go server and a React frontend.

## Stack

**Server** (`go-server/`):
- Go + [chi](https://github.com/go-chi/chi) router
- Input validation with [go-playground/validator](https://github.com/go-playground/validator)
- SSE subscriptions for real-time updates
- Custom error formatter, global and per-procedure middleware
- Server-side caller (`trpcgo.Call`)

**Client** (`web/`):
- React 19 + [TanStack Router](https://tanstack.com/router) + [TanStack Query](https://tanstack.com/query)
- [@trpc/react-query](https://trpc.io/docs/client/react) v11
- [Zod](https://zod.dev/) v4 for client-side validation
- [Tailwind CSS](https://tailwindcss.com/) v4

## Getting started

```bash
# Start the Go server
cd go-server
go run .

# In another terminal, start the frontend
cd web
npm install
npm run dev
```

The Go server runs on `http://localhost:8080` and the frontend on `http://localhost:5173`.

Types are regenerated automatically on file save when the server is running in dev mode. You can also regenerate manually:

```bash
cd go-server
go generate ./...
```
