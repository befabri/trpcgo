---
title: CLI
description: Reference for the trpcgo generate command.
---

```bash
go tool trpcgo generate [flags] [packages]
```

If no package patterns are supplied, the CLI analyzes `.`.

## Flags

| Flag | Description |
| --- | --- |
| `-o`, `-output` | Write generated TypeScript router types to a file. Defaults to stdout. |
| `-dir` | Working directory for Go package resolution. Defaults to `.`. |
| `-w`, `-watch` | Watch Go files and regenerate on changes. |
| `-zod` | Write generated Zod 4 schemas to a file. |
| `-zod-mini` | Generate schemas using `zod/mini` functional syntax. |
| `-enums` | Write runtime enum value objects to a file. |

## Examples

Generate TypeScript router types:

```bash
go tool trpcgo generate -o web/gen/trpc.ts ./...
```

Generate TypeScript router types and Zod schemas:

```bash
go tool trpcgo generate -o web/gen/trpc.ts --zod web/gen/zod.ts ./...
```

Generate runtime enum value objects:

```bash
go tool trpcgo generate -o web/gen/trpc.ts --enums web/gen/enums.ts ./...
```

Generate from another working directory:

```bash
go tool trpcgo generate -dir ./server -o ../web/gen/trpc.ts --zod ../web/gen/zod.ts ./...
```

Watch during development:

```bash
go tool trpcgo generate -o web/gen/trpc.ts --zod web/gen/zod.ts -w ./...
```

## Detection Rules

The static analyzer detects calls to trpcgo registration functions:

- `Query`, `VoidQuery`, `Mutation`, `VoidMutation`, `Subscribe`, `VoidSubscribe`.
- `SubscribeWithFinal`, `VoidSubscribeWithFinal`.
- All `Must*` variants.

Procedure paths must be string literals.

```go
trpcgo.MustQuery(router, "user.get", getUser) // detected

path := "user.get"
trpcgo.MustQuery(router, path, getUser) // not detected by static generation
```

## Output Paths

The CLI creates output files with `os.Create`. Create parent directories first:

```bash
mkdir -p web/gen
go tool trpcgo generate -o web/gen/trpc.ts --zod web/gen/zod.ts --enums web/gen/enums.ts ./...
```

Runtime dev generation creates missing parent directories automatically.

## Watch Mode

Watch mode runs generation once, then watches Go files under `-dir` recursively. It ignores common heavy directories such as `.git`, `vendor`, `node_modules`, `testdata`, `dist`, `build`, and `coverage`.

Only `.go` file create/write events trigger regeneration.
