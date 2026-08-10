import type { AppRouter } from "./gen/trpc";
import { RoleEnum } from "./gen/enums";
import {
  CreateUserInputSchema,
} from "./gen/zod";

const client = createTRPCClient<AppRouter>({
  links: [httpBatchLink({ url: "/trpc" })],
});

const input = CreateUserInputSchema.parse({
  name: "Ada",
  email: "ada@example.com",
  role: RoleEnum.admin,
});

const user = await
  client.user.create.mutate(input);
