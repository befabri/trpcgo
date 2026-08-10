// Role is a user's permission level.
type Role string

const (
  RoleAdmin  Role = "admin"
  RoleEditor Role = "editor"
)

type CreateUserInput struct {
  Name  string `json:"name"`
  Email string `json:"email" validate:"email"`
  Role  Role   `json:"role"`
}

trpcgo.MustMutation(
  router, "user.create", createUser,
)
