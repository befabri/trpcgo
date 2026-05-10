---
title: Install
description: Add the trpcgo runtime and generator to a Go module.
---

trpcgo ships as a Go runtime package and a Go tool command.

## Add The Runtime

```bash
go get github.com/befabri/trpcgo@latest
```

Import the runtime and HTTP protocol handler separately:

```go
import (
    "github.com/befabri/trpcgo"
    "github.com/befabri/trpcgo/trpc"
)
```

## Add The Generator

With Go 1.26+, add the generator as a tool in `go.mod`:

```go
tool github.com/befabri/trpcgo/cmd/trpcgo
```

Then run it with `go tool`:

```bash
mkdir -p web/gen
go tool trpcgo generate -o web/gen/trpc.ts --zod web/gen/zod.ts ./...
```

The CLI creates output files directly, so parent directories must already exist.

## Frontend Packages

Install the tRPC client packages used by your frontend framework. For a vanilla client:

```bash
npm install @trpc/client @trpc/server
```

For React Query:

```bash
npm install @trpc/client @trpc/server @trpc/react-query @tanstack/react-query
```

For the TanStack React Query helper API shown in [Frontend Setup](/frontend-setup/):

```bash
npm install @trpc/client @trpc/server @trpc/tanstack-react-query @tanstack/react-query
```

Install Zod if you generate schemas:

```bash
npm install zod
```

## Requirements

- Go 1.26 or newer.
- tRPC v11 client packages.
- Zod 4 when using `--zod` or `WithZodOutput`.

## What trpcgo Does Not Install

trpcgo does not add CORS, authentication, persistence, or a web framework. The HTTP handler is plain `net/http`, so mount it behind Chi, Echo, Fiber adapters, standard middleware, or a raw `http.ServeMux`.
