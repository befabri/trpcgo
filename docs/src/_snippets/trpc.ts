export interface CreateUserInput {
  name: string;
  email: string;
  role: Role;
}

/** Role is a user's permission level. */
export type Role = "admin" | "editor" | (string & {});

type AppRouterRecord = {
  user: {
    create: $Mutation<CreateUserInput, User>;
  };
};
