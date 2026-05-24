package trpcgo_test

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"sync"
	"testing"

	"github.com/befabri/trpcgo"
	"github.com/befabri/trpcgo/trpc"
)

func TestRawCallInputProcedureAllowsEmptyRawInput(t *testing.T) {
	router := trpcgo.NewRouter()

	type optionalInput struct {
		Name string `json:"name"`
	}

	trpcgo.MustQuery(router, "optional", func(ctx context.Context, input optionalInput) (string, error) {
		if input.Name == "" {
			return "zero", nil
		}
		return input.Name, nil
	})

	got, err := router.RawCall(context.Background(), "optional", nil)
	if err != nil {
		t.Fatalf("RawCall: %v", err)
	}
	if got != "zero" {
		t.Fatalf("RawCall result = %v, want zero", got)
	}
}

func TestRawCallDecodesOneByteInput(t *testing.T) {
	router := trpcgo.NewRouter()

	trpcgo.MustQuery(router, "number", func(ctx context.Context, input int) (int, error) {
		return input, nil
	})

	got, err := router.RawCall(context.Background(), "number", json.RawMessage("1"))
	if err != nil {
		t.Fatalf("RawCall: %v", err)
	}
	if got != 1 {
		t.Fatalf("RawCall result = %v, want 1", got)
	}
}

func TestRawCall(t *testing.T) {
	router := trpcgo.NewRouter()
	trpcgo.Query(router, "user.get", func(ctx context.Context, input GetUserInput) (User, error) {
		return User{ID: input.ID, Name: "Alice"}, nil
	})

	result, err := router.RawCall(context.Background(), "user.get", json.RawMessage(`{"id":"42"}`))
	if err != nil {
		t.Fatalf("RawCall error: %v", err)
	}

	user, ok := result.(User)
	if !ok {
		t.Fatalf("result type = %T, want User", result)
	}
	if user.ID != "42" || user.Name != "Alice" {
		t.Errorf("result = %+v, want {ID:42 Name:Alice}", user)
	}
}

func TestProcedureMapAllLenAndTypes(t *testing.T) {
	router := trpcgo.NewRouter()
	trpcgo.MustQuery(router, "user.get", func(ctx context.Context, input GetUserInput) (User, error) {
		return User{ID: input.ID, Name: "Alice"}, nil
	})
	trpcgo.MustVoidMutation(router, "user.reset", func(ctx context.Context) (string, error) {
		return "ok", nil
	})

	procedures := router.BuildProcedureMap()
	if procedures.Len() != 2 {
		t.Fatalf("Len = %d, want 2", procedures.Len())
	}

	seen := map[string]trpcgo.ProcedureType{}
	for path, entry := range procedures.All() {
		seen[path] = entry.Type()
		if path == "user.get" {
			if entry.InputType() != reflect.TypeFor[GetUserInput]() {
				t.Fatalf("InputType = %v, want GetUserInput", entry.InputType())
			}
			if entry.OutputType() != reflect.TypeFor[User]() {
				t.Fatalf("OutputType = %v, want User", entry.OutputType())
			}
		}
	}

	if seen["user.get"] != trpcgo.ProcedureQuery {
		t.Fatalf("user.get type = %v, want query", seen["user.get"])
	}
	if seen["user.reset"] != trpcgo.ProcedureMutation {
		t.Fatalf("user.reset type = %v, want mutation", seen["user.reset"])
	}

	count := 0
	for range procedures.All() {
		count++
		break
	}
	if count != 1 {
		t.Fatalf("early-stopped count = %d, want 1", count)
	}
}

func TestTypedCall(t *testing.T) {
	router := trpcgo.NewRouter()
	trpcgo.Query(router, "user.get", func(ctx context.Context, input GetUserInput) (User, error) {
		return User{ID: input.ID, Name: "Bob"}, nil
	})

	user, err := trpcgo.Call[GetUserInput, User](router, context.Background(), "user.get", GetUserInput{ID: "99"})
	if err != nil {
		t.Fatalf("Call error: %v", err)
	}
	if user.ID != "99" || user.Name != "Bob" {
		t.Errorf("result = %+v, want {ID:99 Name:Bob}", user)
	}
}

func TestTypedCallInputMarshalError(t *testing.T) {
	router := trpcgo.NewRouter()

	type badInput struct {
		Fn func()
	}

	_, err := trpcgo.Call[badInput, string](router, context.Background(), "unused", badInput{Fn: func() {}})
	if err == nil {
		t.Fatal("expected marshal error")
	}
	trpcErr, ok := err.(*trpcgo.Error)
	if !ok {
		t.Fatalf("error type = %T, want *trpcgo.Error", err)
	}
	if trpcErr.Code != trpcgo.CodeParseError {
		t.Fatalf("error code = %v, want PARSE_ERROR", trpcErr.Code)
	}
	if trpcErr.Message != "failed to marshal input" {
		t.Fatalf("error message = %q, want failed to marshal input", trpcErr.Message)
	}
}

func TestTypedCallJSONFallbackSuccess(t *testing.T) {
	router := trpcgo.NewRouter()

	type publicUser struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}

	trpcgo.VoidQuery(router, "user.map", func(ctx context.Context) (map[string]any, error) {
		return map[string]any{"id": "7", "name": "Ada"}, nil
	})

	user, err := trpcgo.Call[struct{}, publicUser](router, context.Background(), "user.map", struct{}{})
	if err != nil {
		t.Fatalf("Call error: %v", err)
	}
	if user.ID != "7" || user.Name != "Ada" {
		t.Fatalf("user = %+v, want {ID:7 Name:Ada}", user)
	}
}

func TestTypedCallSerializeResultError(t *testing.T) {
	router := trpcgo.NewRouter()
	trpcgo.VoidQuery(router, "bad.result", func(ctx context.Context) (any, error) {
		return func() {}, nil
	})

	_, err := trpcgo.Call[struct{}, string](router, context.Background(), "bad.result", struct{}{})
	if err == nil {
		t.Fatal("expected serialization error")
	}
	trpcErr, ok := err.(*trpcgo.Error)
	if !ok {
		t.Fatalf("error type = %T, want *trpcgo.Error", err)
	}
	if trpcErr.Code != trpcgo.CodeInternalServerError {
		t.Fatalf("error code = %v, want INTERNAL_SERVER_ERROR", trpcErr.Code)
	}
	if trpcErr.Message != "failed to serialize result" {
		t.Fatalf("error message = %q, want failed to serialize result", trpcErr.Message)
	}
}

func TestTypedCallDeserializeResultError(t *testing.T) {
	router := trpcgo.NewRouter()
	trpcgo.VoidQuery(router, "wrong.type", func(ctx context.Context) (string, error) {
		return "not-a-number", nil
	})

	_, err := trpcgo.Call[struct{}, int](router, context.Background(), "wrong.type", struct{}{})
	if err == nil {
		t.Fatal("expected deserialization error")
	}
	trpcErr, ok := err.(*trpcgo.Error)
	if !ok {
		t.Fatalf("error type = %T, want *trpcgo.Error", err)
	}
	if trpcErr.Code != trpcgo.CodeInternalServerError {
		t.Fatalf("error code = %v, want INTERNAL_SERVER_ERROR", trpcErr.Code)
	}
	if trpcErr.Message != "failed to deserialize result" {
		t.Fatalf("error message = %q, want failed to deserialize result", trpcErr.Message)
	}
}

func TestRawCallNotFound(t *testing.T) {
	router := trpcgo.NewRouter()

	_, err := router.RawCall(context.Background(), "nonexistent", nil)
	if err == nil {
		t.Fatal("expected error for nonexistent procedure")
	}

	trpcErr, ok := err.(*trpcgo.Error)
	if !ok {
		t.Fatalf("error type = %T, want *trpcgo.Error", err)
	}
	if trpcErr.Code != trpcgo.CodeNotFound {
		t.Errorf("error code = %v, want NOT_FOUND", trpcErr.Code)
	}
}

func TestRawCallSubscriptionRejected(t *testing.T) {
	router := trpcgo.NewRouter()
	trpcgo.VoidSubscribe(router, "events", func(ctx context.Context) (<-chan string, error) {
		ch := make(chan string)
		close(ch)
		return ch, nil
	})

	_, err := router.RawCall(context.Background(), "events", nil)
	if err == nil {
		t.Fatal("expected error for subscription via RawCall")
	}
}

func TestRawCallRunsMiddleware(t *testing.T) {
	type ctxKey string
	var gotUser string

	router := trpcgo.NewRouter()
	router.Use(func(next trpcgo.HandlerFunc) trpcgo.HandlerFunc {
		return func(ctx context.Context, input any) (any, error) {
			return next(context.WithValue(ctx, ctxKey("user"), "admin"), input)
		}
	})
	trpcgo.VoidQuery(router, "whoami", func(ctx context.Context) (string, error) {
		gotUser = ctx.Value(ctxKey("user")).(string)
		return gotUser, nil
	})

	result, err := router.RawCall(context.Background(), "whoami", nil)
	if err != nil {
		t.Fatalf("RawCall error: %v", err)
	}
	if result != "admin" {
		t.Errorf("result = %v, want admin", result)
	}
	if gotUser != "admin" {
		t.Errorf("gotUser = %v, want admin", gotUser)
	}
}

func TestRawCallWithProcedureMeta(t *testing.T) {
	var gotMeta trpcgo.ProcedureMeta

	router := trpcgo.NewRouter()
	router.Use(func(next trpcgo.HandlerFunc) trpcgo.HandlerFunc {
		return func(ctx context.Context, input any) (any, error) {
			pm, _ := trpcgo.GetProcedureMeta(ctx)
			gotMeta = pm
			return next(ctx, input)
		}
	})
	trpcgo.VoidQuery(router, "test", func(ctx context.Context) (string, error) {
		return "ok", nil
	}, trpcgo.WithMeta("test-meta"))

	_, err := router.RawCall(context.Background(), "test", nil)
	if err != nil {
		t.Fatalf("RawCall error: %v", err)
	}

	if gotMeta.Path != "test" {
		t.Errorf("meta.Path = %q, want %q", gotMeta.Path, "test")
	}
	if gotMeta.Meta != "test-meta" {
		t.Errorf("meta.Meta = %v, want %q", gotMeta.Meta, "test-meta")
	}
}

func TestCallBeforeHandler(t *testing.T) {
	router := trpcgo.NewRouter()
	trpcgo.VoidQuery(router, "ping", func(ctx context.Context) (string, error) {
		return "pong", nil
	})

	// RawCall before Handler() is called — should still work.
	result, err := router.RawCall(context.Background(), "ping", nil)
	if err != nil {
		t.Fatalf("RawCall error: %v", err)
	}
	if result != "pong" {
		t.Errorf("result = %v, want pong", result)
	}
}

func TestCallHandlerError(t *testing.T) {
	router := trpcgo.NewRouter()
	trpcgo.VoidQuery(router, "fail", func(ctx context.Context) (string, error) {
		return "", trpcgo.NewError(trpcgo.CodeForbidden, "denied")
	})

	_, err := trpcgo.Call[struct{}, string](router, context.Background(), "fail", struct{}{})
	if err == nil {
		t.Fatal("expected error")
	}
	trpcErr, ok := err.(*trpcgo.Error)
	if !ok {
		t.Fatalf("error type = %T, want *trpcgo.Error", err)
	}
	if trpcErr.Code != trpcgo.CodeForbidden {
		t.Errorf("error code = %v, want FORBIDDEN", trpcErr.Code)
	}
}

func TestRawCallVoidQuery(t *testing.T) {
	router := trpcgo.NewRouter()
	trpcgo.VoidQuery(router, "ping", func(ctx context.Context) (string, error) {
		return "pong", nil
	})

	// nil input for void query
	result, err := router.RawCall(context.Background(), "ping", nil)
	if err != nil {
		t.Fatalf("RawCall error: %v", err)
	}
	if result != "pong" {
		t.Errorf("result = %v, want pong", result)
	}
}

func TestRawCallAfterHandler(t *testing.T) {
	router := trpcgo.NewRouter()
	trpcgo.VoidQuery(router, "test", func(ctx context.Context) (string, error) {
		return "ok", nil
	})

	// Call Handler() first to pre-compute middleware chains
	_ = trpc.NewHandler(router, "/trpc")

	// RawCall should use the pre-computed chain
	result, err := router.RawCall(context.Background(), "test", nil)
	if err != nil {
		t.Fatalf("RawCall error: %v", err)
	}
	if result != "ok" {
		t.Errorf("result = %v, want ok", result)
	}
}

func TestRawCallConcurrent(t *testing.T) {
	router := trpcgo.NewRouter()
	trpcgo.Query(router, "echo", func(ctx context.Context, input struct{ V int }) (int, error) {
		return input.V, nil
	})

	_ = trpc.NewHandler(router, "/trpc") // pre-compute

	var wg sync.WaitGroup
	errs := make(chan error, 100)
	for i := range 100 {
		wg.Add(1)
		go func(v int) {
			defer wg.Done()
			input, _ := json.Marshal(struct{ V int }{V: v})
			result, err := router.RawCall(context.Background(), "echo", input)
			if err != nil {
				errs <- fmt.Errorf("RawCall(%d): %w", v, err)
				return
			}
			got, ok := result.(int)
			if !ok {
				errs <- fmt.Errorf("RawCall(%d): result type %T, want int", v, result)
				return
			}
			if got != v {
				errs <- fmt.Errorf("RawCall(%d): got %d, want %d", v, got, v)
			}
		}(i)
	}

	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}
