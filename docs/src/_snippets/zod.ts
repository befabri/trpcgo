import { z } from "zod";

export const RoleSchema = z
  .enum(["admin", "editor"])
  .meta({ id: "Role" });

export const CreateUserInputSchema = z.object({
  name: z.string(),
  email: z.email(),
  role: z.enum(["admin", "editor"]),
}).meta({ id: "CreateUserInput" });
