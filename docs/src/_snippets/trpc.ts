/** Role is a user's permission level. */
export type Role = "admin" | "editor";

export interface CreateUserInput {
  name: string;
  email: string;
  role: Role;
}

type AppRouterRecord = {
  user: {
    create: $Mutation<CreateUserInput, User>;
  };
};
