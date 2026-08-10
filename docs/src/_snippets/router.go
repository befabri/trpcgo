router := trpcgo.NewRouter(
  trpcgo.WithValidator(validate.Struct),
  trpcgo.WithTypeOutput("../web/gen/trpc.ts"),
  trpcgo.WithZodOutput("../web/gen/zod.ts"),
  trpcgo.WithEnumsOutput("../web/gen/enums.ts"),
)

trpcgo.MustMutation(router, "user.create", createUser)
