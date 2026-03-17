//go:generate go tool trpcgo generate -format orpc -o ../web/gen/orpc.ts --zod ../web/gen/zod.ts

package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/befabri/trpcgo"
	"github.com/befabri/trpcgo/orpc"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-playground/validator/v10"
)

type ctxKey int

const contextKeyRequestID ctxKey = iota

// Types — source of truth for both Go and TypeScript.

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
	// When the record was created.
	CreatedAt time.Time `json:"createdAt" tstype:",readonly"`
	// When the record was last modified.
	UpdatedAt time.Time `json:"updatedAt" tstype:",readonly"`
}

// PaginatedList wraps a slice of items with pagination metadata.
type PaginatedList[T any] struct {
	Items   []T `json:"items"`
	Total   int `json:"total"`
	Page    int `json:"page"`
	PerPage int `json:"perPage"`
}

type GetUserInput struct {
	ID string `json:"id" validate:"required"`
}

// CreateUserInput contains the fields needed to create a new user.
type CreateUserInput struct {
	Name  string  `json:"name" validate:"required,min=1,max=100"`
	Email Email   `json:"email" validate:"required,email"`
	Role  Role    `json:"role,omitempty" validate:"omitempty,oneof=admin editor viewer"`
	Bio   *string `json:"bio,omitempty" validate:"omitempty,max=500"`
}

type DeleteUserInput struct {
	ID string `json:"id" validate:"required"`
}

// ListUsersInput provides pagination parameters.
type ListUsersInput struct {
	Page    int `json:"page" validate:"required,min=1"`
	PerPage int `json:"perPage" validate:"required,min=1,max=100"`
}

// HealthInfo is returned by the health check VoidQuery.
type HealthInfo struct {
	OK        bool   `json:"ok"`
	Uptime    string `json:"uptime"`
	UserCount int    `json:"userCount"`
}

// ResetResult is returned by the reset VoidMutation.
type ResetResult struct {
	Message   string `json:"message"`
	UserCount int    `json:"userCount"`
}

// DeleteResult is returned by the delete mutation.
type DeleteResult struct {
	Deleted bool `json:"deleted"`
}

// Middleware

func requestTimer(next trpcgo.HandlerFunc) trpcgo.HandlerFunc {
	return func(ctx context.Context, input any) (any, error) {
		meta, _ := trpcgo.GetProcedureMeta(ctx)
		start := time.Now()
		result, err := next(ctx, input)
		log.Printf("[%s] %s took %s", meta.Type, meta.Path, time.Since(start))
		return result, err
	}
}

func logMutation(next trpcgo.HandlerFunc) trpcgo.HandlerFunc {
	return func(ctx context.Context, input any) (any, error) {
		meta, _ := trpcgo.GetProcedureMeta(ctx)
		log.Printf("mutation %s called with %+v", meta.Path, input)
		return next(ctx, input)
	}
}

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
	mu            sync.RWMutex
	users         []User
	nextID        int
	startedAt     time.Time
	skipBroadcast bool

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
		}
	}
}

func (s *userService) GetUser(ctx context.Context, input GetUserInput) (User, error) {
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
	if !s.skipBroadcast {
		s.broadcast(user)
	}
	return user, nil
}

func (s *userService) insertUser(input CreateUserInput) User {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.nextID++
	now := time.Now()

	role := input.Role
	if role == "" {
		role = RoleViewer
	}

	user := User{
		ID:        fmt.Sprintf("%d", s.nextID),
		Name:      input.Name,
		Email:     input.Email,
		Role:      role,
		Status:    StatusActive,
		Bio:       input.Bio,
		CreatedAt: now,
		UpdatedAt: now,
	}
	s.users = append(s.users, user)
	return user
}

func (s *userService) DeleteUser(ctx context.Context, input DeleteUserInput) (DeleteResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, u := range s.users {
		if u.ID == input.ID {
			s.users = append(s.users[:i], s.users[i+1:]...)
			return DeleteResult{Deleted: true}, nil
		}
	}
	return DeleteResult{}, trpcgo.NewError(trpcgo.CodeNotFound, fmt.Sprintf("user %q not found", input.ID))
}

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

func (s *userService) ResetDemo(router *trpcgo.Router) func(ctx context.Context) (ResetResult, error) {
	return func(ctx context.Context) (ResetResult, error) {
		s.mu.Lock()
		s.users = s.users[:0]
		s.nextID = 0
		s.mu.Unlock()

		// Suppress broadcast during seeding so the live feed only shows
		// genuinely new users. Call still runs the full middleware chain.
		s.skipBroadcast = true
		defer func() { s.skipBroadcast = false }()

		seedUsers := []CreateUserInput{
			{Name: "Alice", Email: "alice@example.com", Role: RoleAdmin},
			{Name: "Bob", Email: "bob@example.com", Role: RoleEditor},
		}
		for _, input := range seedUsers {
			if _, err := trpcgo.Call[CreateUserInput, User](router, ctx, "user.create", input); err != nil {
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
				CreatedAt: time.Date(2025, 1, 15, 9, 0, 0, 0, time.UTC),
				UpdatedAt: time.Date(2025, 6, 20, 14, 30, 0, 0, time.UTC),
			},
			{
				ID:        "2",
				Name:      "Bob",
				Email:     "bob@example.com",
				Role:      RoleEditor,
				Status:    StatusActive,
				CreatedAt: time.Date(2025, 3, 1, 12, 0, 0, 0, time.UTC),
				UpdatedAt: time.Date(2025, 3, 1, 12, 0, 0, 0, time.UTC),
			},
		},
	}

	validate := validator.New()

	router := trpcgo.NewRouter(
		trpcgo.WithBatching(true),
		trpcgo.WithDev(true),
		trpcgo.WithMethodOverride(true),
		trpcgo.WithValidator(validate.Struct),
		// Note: dev watcher (WithTypeOutput) only supports tRPC format.
		// Use `go generate` for oRPC types instead.
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
			log.Printf("error on %q: %v", path, err)
		}),
	)

	router.Use(requestTimer)

	// Queries
	// No WithRoute — @orpc/client uses default paths: /user/get, /user/list, etc.
	trpcgo.Query(router, "user.get", svc.GetUser)
	trpcgo.Query(router, "user.list", svc.ListUsers)
	trpcgo.VoidQuery(router, "system.health", svc.ServerHealth,
		trpcgo.OutputValidator(func(info HealthInfo) error {
			if !info.OK {
				return fmt.Errorf("health response must be ok")
			}
			return nil
		}),
	)

	// Mutations
	trpcgo.Mutation(router, "user.create", svc.CreateUser,
		trpcgo.WithSuccessStatus(http.StatusCreated),
		trpcgo.Use(logMutation),
		trpcgo.WithMeta(map[string]string{"action": "write"}),
	)
	trpcgo.Mutation(router, "user.delete", svc.DeleteUser)
	trpcgo.VoidMutation(router, "system.reset", svc.ResetDemo(router),
		trpcgo.Use(requireRequestID),
	)

	// Subscriptions
	trpcgo.VoidSubscribe(router, "user.onCreated", svc.OnUserCreated)

	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, PATCH, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-Request-ID")
			if r.Method == "OPTIONS" {
				w.WriteHeader(204)
				return
			}
			next.ServeHTTP(w, r)
		})
	})

	r.Handle("/rpc/*", orpc.NewHandler(router, "/rpc"))

	var ln net.Listener
	var addr string
	for _, port := range []string{":8080", ":8081", ":8082"} {
		var err error
		ln, err = net.Listen("tcp", port)
		if err == nil {
			addr = port
			break
		}
	}
	if ln == nil {
		log.Fatal("No available port found (tried 8080-8082)")
	}
	if addr != ":8080" {
		log.Printf("WARNING: Listening on %s instead of :8080 — update vite.config.ts proxy to match", addr)
	}
	log.Printf("Server listening on %s", addr)
	log.Fatal(http.Serve(ln, r))
}
