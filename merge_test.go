package trpcgo_test

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/befabri/trpcgo"
	"github.com/befabri/trpcgo/trpc"
)

func TestMergeRouters(t *testing.T) {
	r1 := trpcgo.NewRouter()
	trpcgo.VoidQuery(r1, "hello", func(ctx context.Context) (string, error) {
		return "hello", nil
	})

	r2 := trpcgo.NewRouter()
	trpcgo.VoidQuery(r2, "world", func(ctx context.Context) (string, error) {
		return "world", nil
	})

	merged, err := trpcgo.MergeRouters(r1, r2)
	if err != nil {
		t.Fatal(err)
	}
	server := newTestServer(t, trpc.NewHandler(merged, "/trpc"))

	resp1 := mustGet(t, server, "/trpc/hello")
	if resp1.StatusCode != 200 {
		_ = resp1.Body.Close()
		t.Fatalf("hello: status = %d, want 200", resp1.StatusCode)
	}
	if got := resultScalar(t, decodeJSON(t, resp1)); got != "hello" {
		t.Errorf("hello = %v, want hello", got)
	}

	resp2 := mustGet(t, server, "/trpc/world")
	if resp2.StatusCode != 200 {
		_ = resp2.Body.Close()
		t.Fatalf("world: status = %d, want 200", resp2.StatusCode)
	}
	if got := resultScalar(t, decodeJSON(t, resp2)); got != "world" {
		t.Errorf("world = %v, want world", got)
	}
}

func TestMergeRoutersDuplicateError(t *testing.T) {
	r1 := trpcgo.NewRouter()
	trpcgo.VoidQuery(r1, "dup", func(ctx context.Context) (string, error) {
		return "a", nil
	})

	r2 := trpcgo.NewRouter()
	trpcgo.VoidQuery(r2, "dup", func(ctx context.Context) (string, error) {
		return "b", nil
	})

	_, err := trpcgo.MergeRouters(r1, r2)
	if err == nil {
		t.Fatal("expected error on duplicate procedure path")
	}
	if !strings.Contains(err.Error(), "dup") {
		t.Fatalf("expected error mentioning 'dup', got %v", err)
	}
}

func TestMergeRoutersWithGlobalMiddleware(t *testing.T) {
	var calls []string

	r1 := trpcgo.NewRouter()
	trpcgo.VoidQuery(r1, "a", func(ctx context.Context) (string, error) {
		calls = append(calls, "handler-a")
		return "a", nil
	})

	r2 := trpcgo.NewRouter()
	trpcgo.VoidQuery(r2, "b", func(ctx context.Context) (string, error) {
		calls = append(calls, "handler-b")
		return "b", nil
	})

	merged := trpcgo.NewRouter()
	merged.Use(func(next trpcgo.HandlerFunc) trpcgo.HandlerFunc {
		return func(ctx context.Context, input any) (any, error) {
			calls = append(calls, "global-mw")
			return next(ctx, input)
		}
	})
	if err := merged.Merge(r1, r2); err != nil {
		t.Fatal(err)
	}

	server := newTestServer(t, trpc.NewHandler(merged, "/trpc"))

	resp := mustGet(t, server, "/trpc/a")
	_ = resp.Body.Close()

	if len(calls) != 2 || calls[0] != "global-mw" || calls[1] != "handler-a" {
		t.Errorf("calls = %v, want [global-mw, handler-a]", calls)
	}
}

func TestMergePreservesPerProcMiddleware(t *testing.T) {
	mwCalled := false

	sub := trpcgo.NewRouter()
	trpcgo.VoidQuery(sub, "guarded", func(ctx context.Context) (string, error) {
		return "ok", nil
	}, trpcgo.Use(func(next trpcgo.HandlerFunc) trpcgo.HandlerFunc {
		return func(ctx context.Context, input any) (any, error) {
			mwCalled = true
			return next(ctx, input)
		}
	}))

	main := trpcgo.NewRouter()
	if err := main.Merge(sub); err != nil {
		t.Fatal(err)
	}

	server := newTestServer(t, trpc.NewHandler(main, "/trpc"))
	resp := mustGet(t, server, "/trpc/guarded")
	_ = resp.Body.Close()

	if !mwCalled {
		t.Error("per-procedure middleware from source router was not preserved after merge")
	}
}

func TestMergePreservesMeta(t *testing.T) {
	var gotMeta any

	sub := trpcgo.NewRouter()
	trpcgo.VoidQuery(sub, "tagged", func(ctx context.Context) (string, error) {
		pm, _ := trpcgo.GetProcedureMeta(ctx)
		gotMeta = pm.Meta
		return "ok", nil
	}, trpcgo.WithMeta("preserved"))

	main := trpcgo.NewRouter()
	if err := main.Merge(sub); err != nil {
		t.Fatal(err)
	}

	server := newTestServer(t, trpc.NewHandler(main, "/trpc"))
	resp := mustGet(t, server, "/trpc/tagged")
	_ = resp.Body.Close()

	if gotMeta != "preserved" {
		t.Errorf("meta after merge = %v, want %q", gotMeta, "preserved")
	}
}

func TestMergePreservesOutputValidator(t *testing.T) {
	sub := trpcgo.NewRouter()
	trpcgo.MustVoidQuery(sub, "user", func(ctx context.Context) (User, error) {
		return User{}, nil
	}, trpcgo.OutputValidator(func(u User) error {
		if u.ID == "" {
			return fmt.Errorf("id required")
		}
		return nil
	}))

	main := trpcgo.NewRouter()
	if err := main.Merge(sub); err != nil {
		t.Fatal(err)
	}

	server := newTestServer(t, trpc.NewHandler(main, "/trpc"))
	resp := mustGet(t, server, "/trpc/user")
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", resp.StatusCode)
	}
}

func TestMergeEmptyRouters(t *testing.T) {
	r1 := trpcgo.NewRouter()
	r2 := trpcgo.NewRouter()

	merged, err := trpcgo.MergeRouters(r1, r2)
	if err != nil {
		t.Fatal(err)
	}
	server := newTestServer(t, trpc.NewHandler(merged, "/trpc"))

	resp := mustGet(t, server, "/trpc/anything")
	_ = resp.Body.Close()

	if resp.StatusCode != 404 {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}
