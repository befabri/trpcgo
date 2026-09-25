// Role is a user's permission level.
type Role string

const (
  RoleAdmin  Role = "admin"
  RoleEditor Role = "editor"
)

type CreateUserInput struct {
  Name  string `json:"name" validate:"required,min=1,max=100"`
  Email string `json:"email" validate:"required,email"`
  Role  Role   `json:"role" validate:"oneof=admin editor"`
}

trpcgo.MustMutation(
  router, "user.create", createUser,
)
