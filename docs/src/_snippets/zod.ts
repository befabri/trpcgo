import { z } from "zod";

// Helpers defined earlier in the generated file.
declare function $trpcgoEmail(value: string): boolean;
declare function $trpcgoIssue(valid: (value: any) => boolean, issue: Record<string, unknown>): z.core.$ZodCheck;

export const RoleSchema = z.string().meta({ id: "Role" });

export const CreateUserInputSchema = z.strictObject({
  name: z.string().min(1).max(100),
  email: z.string().check($trpcgoIssue((value) => $trpcgoEmail(String(value).replace(/\p{Surrogate}/gu, "\uFFFD")), { code: "invalid_format", format: "email" })),
  role: z.enum(["admin", "editor"]),
}).meta({ id: "CreateUserInput" });
