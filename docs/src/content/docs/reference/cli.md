---
title: CLI
description: Reference for the trpcgo generate command.
---

```bash
go tool trpcgo generate [flags] [packages]
```

If no package patterns are supplied, the CLI analyzes `.`. Use `./...` to include subpackages. Put flags before package patterns; flags after the first pattern are treated as package arguments.

These examples assume the CLI is registered as a Go tool. See [Installation](/install/) for setup.

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

Analyze a `server` directory from the repository root, with output in `web/gen`:

```bash
go tool trpcgo generate -dir ./server -o web/gen/trpc.ts --zod web/gen/zod.ts ./...
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

Procedure paths must be string literals. Variables and named constants are not detected, even when their value is a fixed string.

```go
trpcgo.MustQuery(router, "user.get", getUser) // detected

path := "user.get"
trpcgo.MustQuery(router, path, getUser) // not detected by static generation
```

## Output Paths

Output paths are relative to the directory where you run the command. `-dir` changes package resolution and the watch root; it does not change where output files are written.

Create parent directories before generating files:

```bash
mkdir -p web/gen
go tool trpcgo generate -o web/gen/trpc.ts --zod web/gen/zod.ts --enums web/gen/enums.ts ./...
```

The CLI replaces each output file after its contents have been generated successfully. Source analysis errors leave existing files intact. Runtime generation and the dev watcher create missing parent directories automatically.

## Watch Mode

Watch mode runs generation once, then watches Go files under `-dir` recursively. It ignores common heavy directories such as `.git`, `vendor`, `node_modules`, `testdata`, `dist`, `build`, and `coverage`.

Only `.go` file create/write events trigger regeneration. The package patterns still determine which packages are analyzed, even though the watcher scans recursively.

The initial generation must succeed before watching starts. Later generation errors are logged, and the watcher continues running so it can retry after the next edit.
