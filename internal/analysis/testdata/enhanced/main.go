package enhanced

import (
	"context"

	"github.com/trpcgo/trpcgo"
)

// Status represents the account status.
type Status string

const (
	StatusActive  Status = "active"
	StatusPending Status = "pending"
	StatusBanned  Status = "banned"
)

// Priority represents task priority levels.
type Priority int

const (
	PriorityLow    Priority = 1
	PriorityMedium Priority = 2
	PriorityHigh   Priority = 3
)

// UserRole is a type alias for string.
type UserRole = string

// User represents a registered user.
type User struct {
	// The unique identifier.
	ID string `json:"id"`
	// Display name of the user.
	Name     string   `json:"name"`
	Email    string   `json:"email"`
	Status   Status   `json:"status"`
	Role     UserRole `json:"role"`
	Priority Priority `json:"priority"`
	// Internal metadata, excluded from TypeScript.
	Secret string `json:"-"`
	// Custom TypeScript type override.
	Metadata map[string]any `json:"metadata" tstype:"Record<string, unknown>"`
	// Readonly field.
	CreatedAt string `json:"createdAt" tstype:",readonly"`
	// Optional pointer field.
	Bio *string `json:"bio,omitempty"`
	// Excluded via tstype skip.
	Debug string `json:"debug" tstype:"-"`
	// Required pointer: normally optional, forced required.
	Avatar *string `json:"avatar" tstype:",required"`
}

// Paginated wraps a list with pagination info.
type Paginated[T any] struct {
	Items []T `json:"items"`
	Total int `json:"total"`
}

type GetUserInput struct {
	ID string `json:"id"`
}

type CreateUserInput struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

func getUser(ctx context.Context, input GetUserInput) (User, error) {
	return User{}, nil
}

func listUsers(ctx context.Context) (Paginated[User], error) {
	return Paginated[User]{}, nil
}

func createUser(ctx context.Context, input CreateUserInput) (User, error) {
	return User{}, nil
}

func Setup() *trpcgo.Router {
	router := trpcgo.NewRouter()
	trpcgo.Query(router, "user.get", getUser)
	trpcgo.VoidQuery(router, "user.list", listUsers)
	trpcgo.Mutation(router, "user.create", createUser)
	return router
}
