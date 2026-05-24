package trpcgo_test

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/befabri/trpcgo"
	"github.com/befabri/trpcgo/trpc"
)

func TestMiddlewareOrdering(t *testing.T) {
	var calls []string

	router := trpcgo.NewRouter()
	router.Use(func(next trpcgo.HandlerFunc) trpcgo.HandlerFunc {
		return func(ctx context.Context, input any) (any, error) {
			calls = append(calls, "global-1")
			return next(ctx, input)
		}
	})
	router.Use(func(next trpcgo.HandlerFunc) trpcgo.HandlerFunc {
		return func(ctx context.Context, input any) (any, error) {
			calls = append(calls, "global-2")
			return next(ctx, input)
		}
	})

	trpcgo.VoidQuery(router, "test", func(ctx context.Context) (string, error) {
		calls = append(calls, "handler")
		return "ok", nil
	}, trpcgo.Use(func(next trpcgo.HandlerFunc) trpcgo.HandlerFunc {
		return func(ctx context.Context, input any) (any, error) {
			calls = append(calls, "per-proc")
			return next(ctx, input)
		}
	}))

	server := newTestServer(t, trpc.NewHandler(router, "/trpc"))

	resp := mustGet(t, server, "/trpc/test")
	_ = resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	want := []string{"global-1", "global-2", "per-proc", "handler"}
	if len(calls) != len(want) {
		t.Fatalf("middleware calls = %v, want %v", calls, want)
	}
	for i := range want {
		if calls[i] != want[i] {
			t.Fatalf("middleware call[%d] = %q, want %q (full: %v)", i, calls[i], want[i], calls)
		}
	}
}

func TestMiddlewareShortCircuit(t *testing.T) {
	handlerReached := false

	router := trpcgo.NewRouter()
	router.Use(func(next trpcgo.HandlerFunc) trpcgo.HandlerFunc {
		return func(ctx context.Context, input any) (any, error) {
			return nil, trpcgo.NewError(trpcgo.CodeUnauthorized, "denied by middleware")
		}
	})
	trpcgo.VoidQuery(router, "secret", func(ctx context.Context) (string, error) {
		handlerReached = true
		return "secret", nil
	})

	server := newTestServer(t, trpc.NewHandler(router, "/trpc"))

	resp := mustGet(t, server, "/trpc/secret")
	if resp.StatusCode != 401 {
		_ = resp.Body.Close()
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}

	ed := errorData(t, decodeJSON(t, resp))
	if ed["code"] != "UNAUTHORIZED" {
		t.Errorf("error code = %v, want UNAUTHORIZED", ed["code"])
	}
	if handlerReached {
		t.Error("handler should not have been called when middleware short-circuits")
	}
}

func TestMiddlewareContextPropagation(t *testing.T) {
	type ctxKey string

	router := trpcgo.NewRouter()
	router.Use(func(next trpcgo.HandlerFunc) trpcgo.HandlerFunc {
		return func(ctx context.Context, input any) (any, error) {
			return next(context.WithValue(ctx, ctxKey("user"), "admin"), input)
		}
	})
	trpcgo.VoidQuery(router, "whoami", func(ctx context.Context) (string, error) {
		return ctx.Value(ctxKey("user")).(string), nil
	})

	server := newTestServer(t, trpc.NewHandler(router, "/trpc"))

	resp := mustGet(t, server, "/trpc/whoami")
	if resp.StatusCode != 200 {
		_ = resp.Body.Close()
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if got := resultScalar(t, decodeJSON(t, resp)); got != "admin" {
		t.Fatalf("result.data = %v, want admin", got)
	}
}

func TestOnErrorCallback(t *testing.T) {
	var mu sync.Mutex
	var gotPath string
	var gotCode trpcgo.ErrorCode

	router := trpcgo.NewRouter(trpcgo.WithOnError(func(ctx context.Context, err *trpcgo.Error, path string) {
		mu.Lock()
		defer mu.Unlock()
		gotPath = path
		gotCode = err.Code
	}))
	trpcgo.VoidQuery(router, "fail", func(ctx context.Context) (string, error) {
		return "", trpcgo.NewError(trpcgo.CodeForbidden, "nope")
	})

	server := newTestServer(t, trpc.NewHandler(router, "/trpc"))

	resp := mustGet(t, server, "/trpc/fail")
	_ = resp.Body.Close()

	mu.Lock()
	defer mu.Unlock()
	if gotPath != "fail" {
		t.Errorf("onError path = %q, want fail", gotPath)
	}
	if gotCode != trpcgo.CodeForbidden {
		t.Errorf("onError code = %v, want CodeForbidden", gotCode)
	}
}

func TestOnErrorNotCalledOnSuccess(t *testing.T) {
	called := false
	router := trpcgo.NewRouter(trpcgo.WithOnError(func(ctx context.Context, err *trpcgo.Error, path string) {
		called = true
	}))
	trpcgo.VoidQuery(router, "ok", func(ctx context.Context) (string, error) {
		return "fine", nil
	})

	server := newTestServer(t, trpc.NewHandler(router, "/trpc"))

	resp := mustGet(t, server, "/trpc/ok")
	_ = resp.Body.Close()

	if called {
		t.Error("onError should not be called when procedure succeeds")
	}
}

func TestCustomContextCreator(t *testing.T) {
	type ctxKey string

	router := trpcgo.NewRouter(trpcgo.WithContextCreator(func(ctx context.Context, r *http.Request) context.Context {
		return context.WithValue(ctx, ctxKey("reqID"), "req-42")
	}))
	trpcgo.VoidQuery(router, "reqid", func(ctx context.Context) (string, error) {
		return ctx.Value(ctxKey("reqID")).(string), nil
	})

	server := newTestServer(t, trpc.NewHandler(router, "/trpc"))

	resp := mustGet(t, server, "/trpc/reqid")
	if resp.StatusCode != 200 {
		_ = resp.Body.Close()
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if got := resultScalar(t, decodeJSON(t, resp)); got != "req-42" {
		t.Fatalf("result.data = %v, want req-42", got)
	}
}

func TestContextCreatorCancellationPropagation(t *testing.T) {
	// Regression: if createContext returns a context NOT derived from r.Context(),
	// SSE subscriptions must still cancel when the client disconnects.
	type ctxKey string
	cancelled := make(chan struct{})

	router := trpcgo.NewRouter(trpcgo.WithContextCreator(func(ctx context.Context, r *http.Request) context.Context {
		// Deliberately not derived from ctx — tests mergeContexts safety net.
		return context.WithValue(context.Background(), ctxKey("user"), "alice")
	}))

	trpcgo.VoidSubscribe(router, "hang", func(ctx context.Context) (<-chan string, error) {
		// Verify the user value was carried through.
		if ctx.Value(ctxKey("user")) != "alice" {
			t.Error("missing user value in context")
		}
		ch := make(chan string)
		go func() {
			defer close(ch)
			// Block until context is cancelled (client disconnect).
			<-ctx.Done()
			close(cancelled)
		}()
		return ch, nil
	})

	server := newTestServer(t, trpc.NewHandler(router, "/trpc"))

	// Start SSE connection then immediately close it.
	resp, err := http.Get(server.URL + "/trpc/hang")
	if err != nil {
		t.Fatal(err)
	}
	// Read the "connected" event to confirm the subscription started.
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		if strings.Contains(scanner.Text(), "connected") {
			break
		}
	}
	// Close = client disconnect.
	_ = resp.Body.Close()

	// The subscription goroutine should be cancelled promptly.
	select {
	case <-cancelled:
		// Success — cancellation propagated.
	case <-time.After(3 * time.Second):
		t.Fatal("subscription was not cancelled after client disconnect (context not propagated)")
	}
}

func TestProcedureOptionUsePlusWithMeta(t *testing.T) {
	var gotMeta trpcgo.ProcedureMeta
	mwCalled := false

	router := trpcgo.NewRouter()
	trpcgo.VoidQuery(router, "both", func(ctx context.Context) (string, error) {
		pm, _ := trpcgo.GetProcedureMeta(ctx)
		gotMeta = pm
		return "ok", nil
	},
		trpcgo.Use(func(next trpcgo.HandlerFunc) trpcgo.HandlerFunc {
			return func(ctx context.Context, input any) (any, error) {
				mwCalled = true
				return next(ctx, input)
			}
		}),
		trpcgo.WithMeta(map[string]string{"role": "admin"}),
	)

	server := newTestServer(t, trpc.NewHandler(router, "/trpc"))

	resp := mustGet(t, server, "/trpc/both")
	_ = resp.Body.Close()

	if !mwCalled {
		t.Error("per-procedure middleware was not called")
	}
	m, ok := gotMeta.Meta.(map[string]string)
	if !ok {
		t.Fatalf("Meta type = %T, want map[string]string", gotMeta.Meta)
	}
	if m["role"] != "admin" {
		t.Errorf("Meta[role] = %q, want %q", m["role"], "admin")
	}
}

func TestProcedureBuilderMiddlewareOrder(t *testing.T) {
	// Middleware registered via Procedure().Use() must execute in the same order
	// as middleware registered directly with Use() on the same call.
	var orderA, orderB []string

	mw := func(tag string, out *[]string) trpcgo.Middleware {
		return func(next trpcgo.HandlerFunc) trpcgo.HandlerFunc {
			return func(ctx context.Context, input any) (any, error) {
				*out = append(*out, tag)
				return next(ctx, input)
			}
		}
	}

	// Path A: flat Use() on registration call
	routerA := trpcgo.NewRouter()
	trpcgo.MustVoidQuery(routerA, "ping", func(ctx context.Context) (string, error) {
		return "ok", nil
	}, trpcgo.Use(mw("1", &orderA), mw("2", &orderA)))

	// Path B: Procedure().Use() builder
	base := trpcgo.Procedure().Use(mw("1", &orderB), mw("2", &orderB))
	routerB := trpcgo.NewRouter()
	trpcgo.MustVoidQuery(routerB, "ping", func(ctx context.Context) (string, error) {
		return "ok", nil
	}, base)

	call := func(r *trpcgo.Router) {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/trpc/ping", nil)
		req.Header.Set("Accept", "application/json")
		w := httptest.NewRecorder()
		trpc.NewHandler(r, "/trpc").ServeHTTP(w, req)
	}

	call(routerA)
	call(routerB)

	if !slices.Equal(orderA, orderB) {
		t.Errorf("middleware order mismatch: flat=%v builder=%v", orderA, orderB)
	}
}

func TestProcedureBuilderImmutable(t *testing.T) {
	// Chaining on a builder must not modify the original.
	base := trpcgo.Procedure().Use(func(next trpcgo.HandlerFunc) trpcgo.HandlerFunc {
		return next
	})

	var calls int
	extraMW := func(next trpcgo.HandlerFunc) trpcgo.HandlerFunc {
		return func(ctx context.Context, input any) (any, error) {
			calls++
			return next(ctx, input)
		}
	}

	derived := base.Use(extraMW)

	// Register using base (should not run extraMW)
	r := trpcgo.NewRouter()
	trpcgo.MustVoidQuery(r, "a", func(ctx context.Context) (string, error) { return "ok", nil }, base)
	trpcgo.MustVoidQuery(r, "b", func(ctx context.Context) (string, error) { return "ok", nil }, derived)

	do := func(path string) {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/trpc/"+path, nil)
		req.Header.Set("Accept", "application/json")
		w := httptest.NewRecorder()
		trpc.NewHandler(r, "/trpc").ServeHTTP(w, req)
	}

	calls = 0
	do("a")
	if calls != 0 {
		t.Errorf("base procedure ran extraMW (calls=%d); builder is not immutable", calls)
	}

	calls = 0
	do("b")
	if calls != 1 {
		t.Errorf("derived procedure should run extraMW once, got calls=%d", calls)
	}
}

func TestProcedureBuilderComposesWithOtherOpts(t *testing.T) {
	// A builder and a plain WithMeta() can be passed together; both apply.
	type role struct{ Admin bool }

	var gotMeta any
	r := trpcgo.NewRouter()
	r.Use(func(next trpcgo.HandlerFunc) trpcgo.HandlerFunc {
		return func(ctx context.Context, input any) (any, error) {
			m, _ := trpcgo.GetProcedureMeta(ctx)
			gotMeta = m.Meta
			return next(ctx, input)
		}
	})

	base := trpcgo.Procedure().Use(func(next trpcgo.HandlerFunc) trpcgo.HandlerFunc { return next })
	trpcgo.MustVoidQuery(r, "check", func(ctx context.Context) (string, error) {
		return "ok", nil
	}, base, trpcgo.WithMeta(role{Admin: true}))

	req := httptest.NewRequest(http.MethodGet, "/trpc/check", nil)
	req.Header.Set("Accept", "application/json")
	w := httptest.NewRecorder()
	trpc.NewHandler(r, "/trpc").ServeHTTP(w, req)

	meta, ok := gotMeta.(role)
	if !ok || !meta.Admin {
		t.Errorf("expected meta role{Admin:true}, got %v", gotMeta)
	}
}

func TestProcedureBuilderNestedComposition(t *testing.T) {
	// Procedure(base) inherits base's middleware; further Use() appends to it.
	var order []string
	mw := func(tag string) trpcgo.Middleware {
		return func(next trpcgo.HandlerFunc) trpcgo.HandlerFunc {
			return func(ctx context.Context, input any) (any, error) {
				order = append(order, tag)
				return next(ctx, input)
			}
		}
	}

	base := trpcgo.Procedure().Use(mw("auth"))
	admin := trpcgo.Procedure(base).Use(mw("admin"))

	r := trpcgo.NewRouter()
	trpcgo.MustVoidQuery(r, "admin.action", func(ctx context.Context) (string, error) {
		return "ok", nil
	}, admin)

	req := httptest.NewRequest(http.MethodGet, "/trpc/admin.action", nil)
	req.Header.Set("Accept", "application/json")
	w := httptest.NewRecorder()
	trpc.NewHandler(r, "/trpc").ServeHTTP(w, req)

	want := []string{"auth", "admin"}
	if !slices.Equal(order, want) {
		t.Errorf("middleware order = %v, want %v", order, want)
	}
}

func TestProcedureOptionNilIgnored(t *testing.T) {
	r := trpcgo.NewRouter()

	var nilOpt trpcgo.ProcedureOption
	trpcgo.MustVoidQuery(r, "ping", func(ctx context.Context) (string, error) {
		return "ok", nil
	}, nilOpt)

	resp := mustGet(t, newTestServer(t, trpc.NewHandler(r, "/trpc")), "/trpc/ping")
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
}

func TestProcedureBuilderNilReceiverIgnored(t *testing.T) {
	r := trpcgo.NewRouter()

	var base *trpcgo.ProcedureBuilder
	trpcgo.MustVoidQuery(r, "ping", func(ctx context.Context) (string, error) {
		return "ok", nil
	}, base, trpcgo.WithMeta("ok"))

	resp := mustGet(t, newTestServer(t, trpc.NewHandler(r, "/trpc")), "/trpc/ping")
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
}

func TestProcedureBuilderContainsNilOptionIgnored(t *testing.T) {
	r := trpcgo.NewRouter()

	var calls int
	mw := func(next trpcgo.HandlerFunc) trpcgo.HandlerFunc {
		return func(ctx context.Context, input any) (any, error) {
			calls++
			return next(ctx, input)
		}
	}

	var nilOpt trpcgo.ProcedureOption
	base := trpcgo.Procedure(nilOpt, trpcgo.Use(mw))
	trpcgo.MustVoidQuery(r, "ping", func(ctx context.Context) (string, error) {
		return "ok", nil
	}, base)

	resp := mustGet(t, newTestServer(t, trpc.NewHandler(r, "/trpc")), "/trpc/ping")
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if calls != 1 {
		t.Fatalf("middleware calls = %d, want 1", calls)
	}
}
