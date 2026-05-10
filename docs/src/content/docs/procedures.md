---
title: Procedures
description: Register typed Go queries, mutations, subscriptions, reusable base procedures, and output hooks.
---

Procedures are the unit of work exposed to tRPC clients. Each procedure has a path, a type, an optional input type, an output type, middleware, metadata, and optional output hooks.

## Queries

Use queries for reads.

```go
type GetUserInput struct {
    ID string `json:"id" validate:"required"`
}

func getUser(ctx context.Context, input GetUserInput) (User, error) {
    return db.FindUser(input.ID)
}

trpcgo.MustQuery(router, "user.get", getUser)
```

Use `VoidQuery` when there is no input:

```go
trpcgo.MustVoidQuery(router, "system.health", func(ctx context.Context) (HealthInfo, error) {
    return HealthInfo{OK: true}, nil
})
```

## Mutations

Use mutations for writes.

```go
trpcgo.MustMutation(router, "user.create", func(ctx context.Context, input CreateUserInput) (User, error) {
    return db.CreateUser(input)
})
```

Use `VoidMutation` when there is no input:

```go
trpcgo.MustVoidMutation(router, "system.reset", func(ctx context.Context) (ResetResult, error) {
    return resetDemoData()
})
```

## Subscriptions

Subscriptions return a receive-only channel and are served as SSE streams.

```go
trpcgo.MustSubscribe(router, "chat.messages", func(ctx context.Context, input RoomInput) (<-chan Message, error) {
    ch := make(chan Message)

    go func() {
        defer close(ch)
        // Send values until ctx is canceled.
    }()

    return ch, nil
})
```

Use `SubscribeWithFinal` to send a final value in the SSE `return` event after the channel closes:

```go
trpcgo.MustSubscribeWithFinal(router, "job.progress", func(ctx context.Context, input JobInput) (<-chan Progress, func() any, error) {
    ch := make(chan Progress)
    final := func() any { return map[string]string{"status": "done"} }
    return ch, final, nil
})
```

## Error Handling At Registration

Non-`Must` functions return duplicate path errors:

```go
if err := trpcgo.Query(router, "user.get", getUser); err != nil {
    return err
}
```

`Must*` variants panic and are intended for application startup code where a duplicate path is a programmer mistake.

## Procedure Options

Every registration function accepts procedure options:

```go
trpcgo.MustMutation(router, "user.create", createUser,
    trpcgo.Use(requireAuth, rateLimit),
    trpcgo.WithMeta(map[string]string{"action": "write"}),
)
```

Available procedure options include:

| Option | Purpose |
| --- | --- |
| `Use(mw...)` | Adds per-procedure middleware. |
| `WithMeta(meta)` | Attaches metadata readable through `GetProcedureMeta` or `GetMeta[T]`. |
| `WithOutputValidator(fn)` | Validates successful outputs without changing their type. |
| `OutputValidator[O](fn)` | Typed output validator. |
| `WithOutputParser(fn)` | Validates or transforms output, but generated output type becomes `unknown`. |
| `OutputParser[O, P](fn)` | Typed output parser; generated output type becomes `P`. |

## Base Procedures

`Procedure()` builds immutable, reusable procedure options. Each chain call returns a new builder.

```go
publicProcedure := trpcgo.Procedure()
authedProcedure := publicProcedure.Use(requireAuth)
adminProcedure := authedProcedure.Use(requireAdmin).WithMeta(RoleMeta{Role: "admin"})

trpcgo.MustQuery(router, "user.list", listUsers, authedProcedure)
trpcgo.MustMutation(router, "user.create", createUser, authedProcedure)
trpcgo.MustMutation(router, "admin.ban", banUser, adminProcedure)
```

Seed one builder from another when composing domains:

```go
orgProcedure := trpcgo.Procedure(authedProcedure).Use(requireOrgAccess)
```

## Output Hooks

Output hooks run after a handler succeeds. Validators run before parsers.

```go
trpcgo.MustQuery(router, "user.get", getUser,
    trpcgo.OutputValidator(func(u User) error {
        if u.ID == "" {
            return errors.New("id required")
        }
        return nil
    }),
)
```

Use typed parsers when the client should see a sanitized shape:

```go
type PublicUser struct {
    ID   string `json:"id"`
    Name string `json:"name"`
}

trpcgo.MustQuery(router, "user.get", getUser,
    trpcgo.OutputParser(func(u User) (PublicUser, error) {
        return PublicUser{ID: u.ID, Name: u.Name}, nil
    }),
)
```

For subscriptions, output validators and parsers run for every emitted item. If an output hook fails, the client receives an SSE `serialized-error` event and the stream closes.

:::caution
Untyped `WithOutputParser` changes runtime output but cannot tell codegen the resulting shape. Use `OutputParser[O, P]` when the TypeScript output type should change.
:::
