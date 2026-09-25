router := trpcgo.NewRouter(
  trpcgo.WithDev(true),
  trpcgo.WithValidator(trpcgo.StructValidator(validate.Struct)),
  trpcgo.WithTypeOutput("../web/gen/trpc.ts"),
  trpcgo.WithZodOutput("../web/gen/zod.ts"),
  trpcgo.WithEnumsOutput("../web/gen/enums.ts"),
)
defer router.Close()

trpcgo.MustMutation(router, "user.create", createUser)
