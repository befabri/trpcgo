//go:generate go tool trpcgo generate -o ../web/gen/trpc.ts --zod ../web/gen/zod.ts

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/befabri/trpcgo"
	"github.com/befabri/trpcgo/orpc"
	"github.com/befabri/trpcgo/trpc"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-playground/validator/v10"
)

type ctxKey int

const contextKeyRequestID ctxKey = iota

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
	ID string `json:"id" validate:"required"`
}

// CreateUserInput contains the fields needed to create a new user.
type CreateUserInput struct {
	// The user's display name.
	Name string `json:"name" validate:"required,min=1,max=100"`
	// The user's email address.
	Email Email `json:"email" validate:"required,email"`
	// The role to assign. Defaults to "viewer" on the server.
	Role Role `json:"role,omitempty" validate:"omitempty,oneof=admin editor viewer"`
	// Optional biography.
	Bio *string `json:"bio,omitempty" validate:"omitempty,max=500"`
}

// ListUsersInput provides pagination parameters.
type ListUsersInput struct {
	Page    int `json:"page" validate:"required,min=1"`
	PerPage int `json:"perPage" validate:"required,min=1,max=100"`
}

// HealthInfo is returned by the health check VoidQuery.
type HealthInfo struct {
	// Whether the service is operational.
	OK bool `json:"ok"`
	// How long the server has been running.
	Uptime string `json:"uptime"`
	// Number of registered users.
	UserCount int `json:"userCount"`
}

// ResetResult is returned by the reset VoidMutation.
type ResetResult struct {
	// Confirmation message.
	Message string `json:"message"`
	// How many users exist after reset.
	UserCount int `json:"userCount"`
}

// Middleware

// requestTimer is a global middleware that logs how long each procedure takes.
func requestTimer(next trpcgo.HandlerFunc) trpcgo.HandlerFunc {
	return func(ctx context.Context, input any) (any, error) {
		meta, _ := trpcgo.GetProcedureMeta(ctx)
		start := time.Now()
		result, err := next(ctx, input)
		log.Printf("[%s] %s took %s", meta.Type, meta.Path, time.Since(start))
		return result, err
	}
}

// logMutation is a per-procedure middleware that logs mutation inputs.
func logMutation(next trpcgo.HandlerFunc) trpcgo.HandlerFunc {
	return func(ctx context.Context, input any) (any, error) {
		meta, _ := trpcgo.GetProcedureMeta(ctx)
		log.Printf("mutation %s called with %+v", meta.Path, input)
		return next(ctx, input)
	}
}

// requireRequestID is a per-procedure middleware that rejects calls without
// an X-Request-ID header (set via WithContextCreator).
func requireRequestID(next trpcgo.HandlerFunc) trpcgo.HandlerFunc {
	return func(ctx context.Context, input any) (any, error) {
		if ctx.Value(contextKeyRequestID) == nil {
			return nil, trpcgo.NewError(trpcgo.CodeBadRequest, "X-Request-ID header required")
		}
		return next(ctx, input)
	}
}

// Handlers

type userService struct {
	mu        sync.RWMutex
	users     []User
	nextID    int
	startedAt time.Time

	// Subscription broadcast: listeners register a channel, CreateUser pushes to all.
	subsMu sync.Mutex
	subs   []chan User
}

func (s *userService) addSubscriber(ch chan User) {
	s.subsMu.Lock()
	s.subs = append(s.subs, ch)
	s.subsMu.Unlock()
}

func (s *userService) removeSubscriber(ch chan User) {
	s.subsMu.Lock()
	for i, sub := range s.subs {
		if sub == ch {
			s.subs = append(s.subs[:i], s.subs[i+1:]...)
			break
		}
	}
	s.subsMu.Unlock()
}

func (s *userService) broadcast(user User) {
	s.subsMu.Lock()
	defer s.subsMu.Unlock()
	for _, ch := range s.subs {
		select {
		case ch <- user:
		default:
			// Slow consumer, skip.
		}
	}
}

func (s *userService) GetUserById(ctx context.Context, input GetUserByIdInput) (User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, u := range s.users {
		if u.ID == input.ID {
			return u, nil
		}
	}
	return User{}, trpcgo.NewError(trpcgo.CodeNotFound, fmt.Sprintf("user %q not found", input.ID))
}

func (s *userService) ListUsers(ctx context.Context, input ListUsersInput) (PaginatedList[User], error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
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
	user := s.insertUser(input)

	// Broadcast to subscription listeners (outside the lock).
	s.broadcast(user)

	return user, nil
}

// insertUser holds the write lock for the minimum scope needed.
func (s *userService) insertUser(input CreateUserInput) User {
	s.mu.Lock()
	defer s.mu.Unlock()

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
	return user
}

// ServerHealth is a VoidQuery — no input required.
func (s *userService) ServerHealth(ctx context.Context) (HealthInfo, error) {
	s.mu.RLock()
	count := len(s.users)
	s.mu.RUnlock()
	return HealthInfo{
		OK:        true,
		Uptime:    time.Since(s.startedAt).Round(time.Second).String(),
		UserCount: count,
	}, nil
}

// ResetDemo is a VoidMutation — no input, resets data to initial seed.
// Uses Call internally to demonstrate the server-side caller.
func (s *userService) ResetDemo(router *trpcgo.Router) func(ctx context.Context) (ResetResult, error) {
	return func(ctx context.Context) (ResetResult, error) {
		s.mu.Lock()
		s.users = s.users[:0]
		s.nextID = 0
		s.mu.Unlock()

		// Use the server-side caller (Call) to re-seed via the createUser procedure.
		// This runs the full middleware chain including validation.
		seedUsers := []CreateUserInput{
			{Name: "Alice", Email: "alice@example.com", Role: RoleAdmin},
			{Name: "Bob", Email: "bob@example.com", Role: RoleEditor},
		}
		for _, input := range seedUsers {
			if _, err := trpcgo.Call[CreateUserInput, User](router, ctx, "user.createUser", input); err != nil {
				return ResetResult{}, fmt.Errorf("seeding user %s: %w", input.Name, err)
			}
		}

		s.mu.RLock()
		count := len(s.users)
		s.mu.RUnlock()

		return ResetResult{
			Message:   "Demo data reset to initial state",
			UserCount: count,
		}, nil
	}
}

// OnUserCreated is a VoidSubscribe — streams new users as they are created.
func (s *userService) OnUserCreated(ctx context.Context) (<-chan User, error) {
	ch := make(chan User, 8)
	s.addSubscriber(ch)

	go func() {
		<-ctx.Done()
		s.removeSubscriber(ch)
		close(ch)
	}()

	return ch, nil
}

func main() {
	aliceBio := "Software engineer who loves distributed systems."
	aliceAvatar := "https://api.dicebear.com/9.x/initials/svg?seed=Alice"
	bobAvatar := "https://api.dicebear.com/9.x/initials/svg?seed=Bob"

	svc := &userService{
		startedAt: time.Now(),
		nextID:    2,
		users: []User{
			{
				ID:        "1",
				Name:      "Alice",
				Email:     "alice@example.com",
				Role:      RoleAdmin,
				Status:    StatusActive,
				Bio:       &aliceBio,
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

	validate := validator.New()

	router := trpcgo.NewRouter(
		trpcgo.WithBatching(true),
		trpcgo.WithDev(true),
		trpcgo.WithMethodOverride(true),
		trpcgo.WithValidator(validate.Struct),
		trpcgo.WithTypeOutput("../web/gen/trpc.ts"),
		trpcgo.WithZodOutput("../web/gen/zod.ts"),
		trpcgo.WithSSEPingInterval(5*time.Second),
		trpcgo.WithSSEMaxDuration(10*time.Minute),
		trpcgo.WithSSEReconnectAfterInactivity(30*time.Second),
		trpcgo.WithErrorFormatter(func(input trpcgo.ErrorFormatterInput) any {
			return map[string]any{
				"error": map[string]any{
					"code":      input.Shape.Error.Code,
					"message":   input.Shape.Error.Message,
					"data":      input.Shape.Error.Data,
					"timestamp": time.Now().UTC().Format(time.RFC3339),
				},
			}
		}),
		trpcgo.WithContextCreator(func(ctx context.Context, r *http.Request) context.Context {
			reqID := r.Header.Get("X-Request-ID")
			if reqID != "" {
				ctx = context.WithValue(ctx, contextKeyRequestID, reqID)
			}
			return ctx
		}),
		trpcgo.WithOnError(func(ctx context.Context, err *trpcgo.Error, path string) {
			log.Printf("tRPC error on %q: %v", path, err)
		}),
	)

	// Global middleware: runs on every procedure.
	router.Use(requestTimer)

	// Queries
	trpcgo.Query(router, "user.getUserById", svc.GetUserById)
	trpcgo.Query(router, "user.listUsers", svc.ListUsers)

	// VoidQuery — no input, with output validation
	trpcgo.VoidQuery(router, "system.health", svc.ServerHealth,
		trpcgo.OutputValidator(func(info HealthInfo) error {
			if !info.OK {
				return fmt.Errorf("health response must be ok")
			}
			if info.UserCount < 0 {
				return fmt.Errorf("user count must be non-negative")
			}
			return nil
		}),
	)

	// Mutation with per-procedure middleware (Use) and metadata (WithMeta)
	trpcgo.Mutation(router, "user.createUser", svc.CreateUser,
		trpcgo.Use(logMutation),
		trpcgo.WithMeta(map[string]string{"action": "write"}),
	)

	// VoidMutation — no input, uses Call internally for server-side caller demo
	trpcgo.VoidMutation(router, "system.resetDemo", svc.ResetDemo(router),
		trpcgo.Use(requireRequestID),
	)

	// VoidSubscribe — SSE subscription, streams new users in real-time
	trpcgo.VoidSubscribe(router, "user.onCreated", svc.OnUserCreated)

	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	// CORS for dev
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-Request-ID")
			if r.Method == "OPTIONS" {
				w.WriteHeader(204)
				return
			}
			next.ServeHTTP(w, r)
		})
	})

	// Serve both tRPC and oRPC from the same router.
	// tRPC: /trpc/user.getUserById  (dot-separated, @trpc/client)
	// oRPC: /rpc/user/getUserById   (slash-separated, @orpc/client)
	r.Handle("/trpc/*", trpc.NewHandler(router, "/trpc"))
	r.Handle("/rpc/*", orpc.NewHandler(router, "/rpc"))

	log.Println("Go tRPC server listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", r))
}
