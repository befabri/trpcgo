//go:generate go tool trpcgo generate -o ../web/gen/trpc.ts

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/trpcgo/trpcgo"
)

// Types — this is the source of truth for both Go and TypeScript.
// The codegen tool reads these definitions and generates a TypeScript
// AppRouter type that stays in sync automatically.

// Role represents a user's permission level.
type Role string

const (
	RoleAdmin  Role = "admin"
	RoleEditor Role = "editor"
	RoleViewer Role = "viewer"
)

// Status represents a user's account status.
type Status string

const (
	StatusActive    Status = "active"
	StatusInactive  Status = "inactive"
	StatusSuspended Status = "suspended"
)

// Email is a validated email address string.
type Email = string

// Timestamps contains standard audit trail fields.
type Timestamps struct {
	// When the record was created.
	CreatedAt time.Time `json:"createdAt" tstype:",readonly"`
	// When the record was last modified.
	UpdatedAt time.Time `json:"updatedAt" tstype:",readonly"`
}

// User represents a registered user in the system.
type User struct {
	// The unique identifier for this user.
	ID string `json:"id" tstype:",readonly"`

	// The user's display name.
	Name string `json:"name"`

	// The user's email address.
	Email Email `json:"email"`

	// The user's assigned role.
	Role Role `json:"role"`

	// Current account status.
	Status Status `json:"status"`

	// Optional biography text.
	Bio *string `json:"bio,omitempty"`

	// URL to the user's avatar image. Always present in responses.
	AvatarURL *string `json:"avatarUrl" tstype:",required"`

	// Arbitrary user preferences stored as JSON.
	Preferences map[string]any `json:"preferences" tstype:"Record<string, unknown>"`

	// Raw integration data, structure varies.
	ExtraData json.RawMessage `json:"extraData,omitempty"`

	// Key-value tags for categorization.
	Tags map[string]string `json:"tags"`

	// Password hash, never sent to the client.
	PasswordHash string `json:"-"`

	// Internal debug info, excluded from TypeScript types.
	DebugInfo string `json:"debugInfo" tstype:"-"`

	Timestamps
}

// PaginatedList wraps a slice of items with pagination metadata.
type PaginatedList[T any] struct {
	// The items on this page.
	Items []T `json:"items"`
	// Total number of items across all pages.
	Total int `json:"total"`
	// Current page number (1-based).
	Page int `json:"page"`
	// Number of items per page.
	PerPage int `json:"perPage"`
}

type GetUserByIdInput struct {
	ID string `json:"id"`
}

// CreateUserInput contains the fields needed to create a new user.
type CreateUserInput struct {
	// The user's display name.
	Name string `json:"name"`
	// The user's email address.
	Email Email `json:"email"`
	// The role to assign. Defaults to "viewer" on the server.
	Role Role `json:"role,omitempty"`
	// Optional biography.
	Bio *string `json:"bio,omitempty"`
}

// ListUsersInput provides pagination parameters.
type ListUsersInput struct {
	Page    int `json:"page"`
	PerPage int `json:"perPage"`
}

// Handlers

type userService struct {
	users  []User
	nextID int
}

func (s *userService) GetUserById(ctx context.Context, input GetUserByIdInput) (User, error) {
	for _, u := range s.users {
		if u.ID == input.ID {
			return u, nil
		}
	}
	return User{}, trpcgo.NewError(trpcgo.CodeNotFound, fmt.Sprintf("user %q not found", input.ID))
}

func (s *userService) ListUsers(ctx context.Context, input ListUsersInput) (PaginatedList[User], error) {
	total := len(s.users)
	page := input.Page
	perPage := input.PerPage
	if page < 1 {
		page = 1
	}
	if perPage < 1 {
		perPage = 20
	}

	start := (page - 1) * perPage
	if start > total {
		start = total
	}
	end := start + perPage
	if end > total {
		end = total
	}

	return PaginatedList[User]{
		Items:   s.users[start:end],
		Total:   total,
		Page:    page,
		PerPage: perPage,
	}, nil
}

func (s *userService) CreateUser(ctx context.Context, input CreateUserInput) (User, error) {
	s.nextID++
	now := time.Now()

	role := input.Role
	if role == "" {
		role = RoleViewer
	}

	avatar := fmt.Sprintf("https://api.dicebear.com/9.x/initials/svg?seed=%s", input.Name)

	user := User{
		ID:        fmt.Sprintf("%d", s.nextID),
		Name:      input.Name,
		Email:     input.Email,
		Role:      role,
		Status:    StatusActive,
		Bio:       input.Bio,
		AvatarURL: &avatar,
		Preferences: map[string]any{
			"theme":    "system",
			"language": "en",
		},
		Tags: map[string]string{},
		Timestamps: Timestamps{
			CreatedAt: now,
			UpdatedAt: now,
		},
	}
	s.users = append(s.users, user)
	return user, nil
}

func main() {
	aliceBio := "Software engineer who loves distributed systems."
	aliceAvatar := "https://api.dicebear.com/9.x/initials/svg?seed=Alice"
	bobAvatar := "https://api.dicebear.com/9.x/initials/svg?seed=Bob"

	svc := &userService{
		nextID: 2,
		users: []User{
			{
				ID:     "1",
				Name:   "Alice",
				Email:  "alice@example.com",
				Role:   RoleAdmin,
				Status: StatusActive,
				Bio:    &aliceBio,
				AvatarURL: &aliceAvatar,
				Preferences: map[string]any{
					"theme":         "dark",
					"language":      "en",
					"notifications": true,
				},
				ExtraData: json.RawMessage(`{"source":"github","orgId":42}`),
				Tags: map[string]string{
					"department": "engineering",
					"team":       "platform",
				},
				Timestamps: Timestamps{
					CreatedAt: time.Date(2025, 1, 15, 9, 0, 0, 0, time.UTC),
					UpdatedAt: time.Date(2025, 6, 20, 14, 30, 0, 0, time.UTC),
				},
			},
			{
				ID:        "2",
				Name:      "Bob",
				Email:     "bob@example.com",
				Role:      RoleEditor,
				Status:    StatusActive,
				AvatarURL: &bobAvatar,
				Preferences: map[string]any{
					"theme":    "light",
					"language": "en",
				},
				Tags: map[string]string{
					"department": "design",
				},
				Timestamps: Timestamps{
					CreatedAt: time.Date(2025, 3, 1, 12, 0, 0, 0, time.UTC),
					UpdatedAt: time.Date(2025, 3, 1, 12, 0, 0, 0, time.UTC),
				},
			},
		},
	}

	router := trpcgo.NewRouter(
		trpcgo.WithBatching(true),
		trpcgo.WithTypeOutput("../web/gen/trpc.ts"),
		trpcgo.WithOnError(func(ctx context.Context, err *trpcgo.Error, path string) {
			log.Printf("tRPC error on %q: %v", path, err)
		}),
	)

	trpcgo.Query(router, "user.getUserById", svc.GetUserById)
	trpcgo.Query(router, "user.listUsers", svc.ListUsers)
	trpcgo.Mutation(router, "user.createUser", svc.CreateUser)

	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	// CORS for dev
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
			if r.Method == "OPTIONS" {
				w.WriteHeader(204)
				return
			}
			next.ServeHTTP(w, r)
		})
	})

	r.Handle("/trpc/*", router.Handler("/trpc"))

	log.Println("Go tRPC server listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", r))
}
