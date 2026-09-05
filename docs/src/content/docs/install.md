---
title: Install
description: Add the trpcgo runtime and generator to a Go module.
---

trpcgo includes a Go runtime for serving procedures and a generator for frontend types and schemas. You'll need Go 1.26 or newer.

## Add The Runtime

Run this in an existing Go module. For a new project, create one first with `go mod init example.com/myapp`.

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

Add the generator to your module's tool dependencies:

```bash
go get -tool github.com/befabri/trpcgo/cmd/trpcgo@latest
```

This adds a tool directive to `go.mod`:

```go
tool github.com/befabri/trpcgo/cmd/trpcgo
```

Then run it with `go tool`:

```bash
mkdir -p web/gen
go tool trpcgo generate -o web/gen/trpc.ts --zod web/gen/zod.ts ./...
```

Create output directories first; the CLI does not create missing parent directories.

The generator looks for procedure registrations in the selected Go packages. If you haven't written any yet, continue with [Quick Start](/quick-start/).

## Frontend Packages

Install the tRPC client packages used by your frontend framework. For a vanilla client:

```bash
npm install @trpc/client@11 @trpc/server@11
```

For React Query:

```bash
npm install @trpc/client@11 @trpc/server@11 @trpc/react-query@11 @tanstack/react-query@5
```

For the TanStack React Query helper API shown in [Frontend Setup](/frontend-setup/):

```bash
npm install @trpc/client@11 @trpc/server@11 @trpc/tanstack-react-query@11 @tanstack/react-query@5
```

Keep your `@trpc/*` packages on matching versions. The generated router type imports from `@trpc/server`, so your frontend needs that package even though the API server runs in Go.

Install Zod if you generate schemas:

```bash
npm install zod@4
```

## Requirements

- Go 1.26 or newer.
- tRPC v11 client packages.
- Zod 4 when using `--zod` or `WithZodOutput`.

## Add Your Application Dependencies

Choose the authentication and persistence libraries that fit your app. trpcgo provides a `net/http` handler with optional CORS handling, so you can use `http.ServeMux`, a compatible Go router, or an adapter for your preferred framework.

For server-side validation with `validate` tags, install a validator and pass it to `WithValidator`. [Quick Start](/quick-start/) shows the setup with `go-playground/validator`.
