package trpcgo_test

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/befabri/trpcgo"
	"github.com/befabri/trpcgo/trpc"
)

func TestProcedureMeta(t *testing.T) {
	type AuthMeta struct {
		RequiresAuth bool
		Role         string
	}

	var gotMeta trpcgo.ProcedureMeta

	router := trpcgo.NewRouter()
	router.Use(func(next trpcgo.HandlerFunc) trpcgo.HandlerFunc {
		return func(ctx context.Context, input any) (any, error) {
			pm, ok := trpcgo.GetProcedureMeta(ctx)
			if !ok {
				t.Error("expected ProcedureMeta in context")
			}
			gotMeta = pm
			return next(ctx, input)
		}
	})
	trpcgo.VoidQuery(router, "hello", func(ctx context.Context) (string, error) {
		return "hi", nil
	}, trpcgo.WithMeta(AuthMeta{RequiresAuth: true, Role: "admin"}))

	server := newTestServer(t, trpc.NewHandler(router, "/trpc"))

	resp := mustGet(t, server, "/trpc/hello")
	_ = resp.Body.Close()

	if gotMeta.Path != "hello" {
		t.Errorf("ProcedureMeta.Path = %q, want %q", gotMeta.Path, "hello")
	}
	if gotMeta.Type != trpcgo.ProcedureQuery {
		t.Errorf("ProcedureMeta.Type = %q, want %q", gotMeta.Type, trpcgo.ProcedureQuery)
	}
	meta, ok := gotMeta.Meta.(AuthMeta)
	if !ok {
		t.Fatalf("ProcedureMeta.Meta type = %T, want AuthMeta", gotMeta.Meta)
	}
	if !meta.RequiresAuth || meta.Role != "admin" {
		t.Errorf("meta = %+v, want {RequiresAuth:true Role:admin}", meta)
	}
}

func TestProcedureMetaNil(t *testing.T) {
	var gotMeta trpcgo.ProcedureMeta

	router := trpcgo.NewRouter()
	router.Use(func(next trpcgo.HandlerFunc) trpcgo.HandlerFunc {
		return func(ctx context.Context, input any) (any, error) {
			pm, _ := trpcgo.GetProcedureMeta(ctx)
			gotMeta = pm
			return next(ctx, input)
		}
	})
	trpcgo.VoidQuery(router, "hello", func(ctx context.Context) (string, error) {
		return "hi", nil
	}) // no WithMeta

	server := newTestServer(t, trpc.NewHandler(router, "/trpc"))

	resp := mustGet(t, server, "/trpc/hello")
	_ = resp.Body.Close()

	if gotMeta.Meta != nil {
		t.Errorf("ProcedureMeta.Meta = %v, want nil", gotMeta.Meta)
	}
	if gotMeta.Path != "hello" {
		t.Errorf("ProcedureMeta.Path = %q, want %q", gotMeta.Path, "hello")
	}
}

func TestProcedureMetaMutation(t *testing.T) {
	var gotType trpcgo.ProcedureType

	router := trpcgo.NewRouter()
	router.Use(func(next trpcgo.HandlerFunc) trpcgo.HandlerFunc {
		return func(ctx context.Context, input any) (any, error) {
			pm, _ := trpcgo.GetProcedureMeta(ctx)
			gotType = pm.Type
			return next(ctx, input)
		}
	})
	trpcgo.Mutation(router, "user.create", func(ctx context.Context, input CreateUserInput) (User, error) {
		return User{ID: "1", Name: input.Name}, nil
	})

	server := newTestServer(t, trpc.NewHandler(router, "/trpc"))

	resp := mustPost(t, server, "/trpc/user.create", `{"name":"Bob"}`)
	_ = resp.Body.Close()

	if gotType != trpcgo.ProcedureMutation {
		t.Errorf("ProcedureMeta.Type = %q, want %q", gotType, trpcgo.ProcedureMutation)
	}
}

func TestProcedureMetaAccessibleInHandler(t *testing.T) {
	var gotMeta trpcgo.ProcedureMeta

	router := trpcgo.NewRouter()
	trpcgo.VoidQuery(router, "check", func(ctx context.Context) (string, error) {
		pm, ok := trpcgo.GetProcedureMeta(ctx)
		if !ok {
			t.Error("expected ProcedureMeta in handler context")
		}
		gotMeta = pm
		return "ok", nil
	}, trpcgo.WithMeta("handler-visible"))

	server := newTestServer(t, trpc.NewHandler(router, "/trpc"))

	resp := mustGet(t, server, "/trpc/check")
	_ = resp.Body.Close()

	if gotMeta.Path != "check" {
		t.Errorf("Path = %q, want %q", gotMeta.Path, "check")
	}
	if gotMeta.Meta != "handler-visible" {
		t.Errorf("Meta = %v, want %q", gotMeta.Meta, "handler-visible")
	}
}

func TestProcedureMetaSubscriptionType(t *testing.T) {
	var gotType trpcgo.ProcedureType

	router := trpcgo.NewRouter()
	router.Use(func(next trpcgo.HandlerFunc) trpcgo.HandlerFunc {
		return func(ctx context.Context, input any) (any, error) {
			pm, _ := trpcgo.GetProcedureMeta(ctx)
			gotType = pm.Type
			return next(ctx, input)
		}
	})
	trpcgo.VoidSubscribe(router, "events", func(ctx context.Context) (<-chan string, error) {
		ch := make(chan string, 1)
		ch <- "hello"
		close(ch)
		return ch, nil
	})

	server := newTestServer(t, trpc.NewHandler(router, "/trpc"))

	// Initiate SSE request, just check that middleware ran.
	req, _ := http.NewRequest("GET", server.URL+"/trpc/events", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request error: %v", err)
	}
	_ = resp.Body.Close()

	if gotType != trpcgo.ProcedureSubscription {
		t.Errorf("ProcedureMeta.Type = %q, want %q", gotType, trpcgo.ProcedureSubscription)
	}
}

func TestProcedureMetaIsolationInBatch(t *testing.T) {
	metas := make(map[string]any)
	var mu sync.Mutex

	router := trpcgo.NewRouter(trpcgo.WithBatching(true))
	router.Use(func(next trpcgo.HandlerFunc) trpcgo.HandlerFunc {
		return func(ctx context.Context, input any) (any, error) {
			pm, _ := trpcgo.GetProcedureMeta(ctx)
			mu.Lock()
			metas[pm.Path] = pm.Meta
			mu.Unlock()
			return next(ctx, input)
		}
	})
	trpcgo.VoidQuery(router, "a", func(ctx context.Context) (string, error) {
		return "a", nil
	}, trpcgo.WithMeta("meta-a"))
	trpcgo.VoidQuery(router, "b", func(ctx context.Context) (string, error) {
		return "b", nil
	}, trpcgo.WithMeta("meta-b"))

	server := newTestServer(t, trpc.NewHandler(router, "/trpc"))

	// Batch request requires ?batch=1
	resp := mustGet(t, server, "/trpc/a,b?batch=1")
	_ = resp.Body.Close()

	mu.Lock()
	defer mu.Unlock()
	if metas["a"] != "meta-a" {
		t.Errorf("meta for 'a' = %v, want 'meta-a'", metas["a"])
	}
	if metas["b"] != "meta-b" {
		t.Errorf("meta for 'b' = %v, want 'meta-b'", metas["b"])
	}
}

func TestSetCookieFromHandler(t *testing.T) {
	router := trpcgo.NewRouter()

	trpcgo.VoidMutation(router, "auth.login", func(ctx context.Context) (string, error) {
		trpcgo.SetCookie(ctx, &http.Cookie{
			Name:  "session",
			Value: "abc123",
			Path:  "/",
		})
		trpcgo.SetCookie(ctx, &http.Cookie{
			Name:  "logged_in",
			Value: "1",
			Path:  "/",
		})
		return "ok", nil
	})

	server := newTestServer(t, trpc.NewHandler(router, "/trpc"))
	resp := mustPost(t, server, "/trpc/auth.login", "")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	cookies := resp.Cookies()
	if len(cookies) != 2 {
		t.Fatalf("got %d cookies, want 2", len(cookies))
	}

	found := map[string]string{}
	for _, c := range cookies {
		found[c.Name] = c.Value
	}
	if found["session"] != "abc123" {
		t.Errorf("session cookie = %q, want %q", found["session"], "abc123")
	}
	if found["logged_in"] != "1" {
		t.Errorf("logged_in cookie = %q, want %q", found["logged_in"], "1")
	}
}

func TestSetCookieFromMiddleware(t *testing.T) {
	router := trpcgo.NewRouter()

	mw := func(next trpcgo.HandlerFunc) trpcgo.HandlerFunc {
		return func(ctx context.Context, input any) (any, error) {
			trpcgo.SetCookie(ctx, &http.Cookie{
				Name:  "mw_cookie",
				Value: "from_middleware",
			})
			return next(ctx, input)
		}
	}

	trpcgo.VoidQuery(router, "test", func(ctx context.Context) (string, error) {
		return "ok", nil
	}, trpcgo.Use(mw))

	server := newTestServer(t, trpc.NewHandler(router, "/trpc"))
	resp := mustGet(t, server, "/trpc/test")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	cookies := resp.Cookies()
	if len(cookies) != 1 {
		t.Fatalf("got %d cookies, want 1", len(cookies))
	}
	if cookies[0].Name != "mw_cookie" || cookies[0].Value != "from_middleware" {
		t.Errorf("cookie = %s=%s, want mw_cookie=from_middleware", cookies[0].Name, cookies[0].Value)
	}
}

func TestSetResponseHeader(t *testing.T) {
	router := trpcgo.NewRouter()

	trpcgo.VoidQuery(router, "test", func(ctx context.Context) (string, error) {
		trpcgo.SetResponseHeader(ctx, "X-Custom", "hello")
		return "ok", nil
	})

	server := newTestServer(t, trpc.NewHandler(router, "/trpc"))
	resp := mustGet(t, server, "/trpc/test")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	if got := resp.Header.Get("X-Custom"); got != "hello" {
		t.Errorf("X-Custom = %q, want %q", got, "hello")
	}
}

func TestSetCookieOnError(t *testing.T) {
	router := trpcgo.NewRouter()

	trpcgo.VoidMutation(router, "auth.logout", func(ctx context.Context) (string, error) {
		// Set cookie to clear session even on error path
		trpcgo.SetCookie(ctx, &http.Cookie{
			Name:   "session",
			Value:  "",
			MaxAge: -1,
		})
		return "", trpcgo.NewError(trpcgo.CodeUnauthorized, "session expired")
	})

	server := newTestServer(t, trpc.NewHandler(router, "/trpc"))
	resp := mustPost(t, server, "/trpc/auth.logout", "")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != 401 {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}

	cookies := resp.Cookies()
	if len(cookies) != 1 {
		t.Fatalf("got %d cookies, want 1", len(cookies))
	}
	if cookies[0].Name != "session" || cookies[0].MaxAge != -1 {
		t.Errorf("cookie = %s maxAge=%d, want session maxAge=-1", cookies[0].Name, cookies[0].MaxAge)
	}
}

func TestSetCookieInBatch(t *testing.T) {
	router := trpcgo.NewRouter(trpcgo.WithBatching(true))

	trpcgo.VoidMutation(router, "noop", func(ctx context.Context) (string, error) {
		return "hi", nil
	})

	trpcgo.VoidMutation(router, "auth.login", func(ctx context.Context) (string, error) {
		trpcgo.SetCookie(ctx, &http.Cookie{
			Name:  "token",
			Value: "batch_token",
		})
		return "ok", nil
	})

	server := newTestServer(t, trpc.NewHandler(router, "/trpc"))
	resp := mustPost(t, server, "/trpc/noop,auth.login?batch=1", `{"0":null,"1":null}`)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	cookies := resp.Cookies()
	if len(cookies) != 1 {
		t.Fatalf("got %d cookies, want 1", len(cookies))
	}
	if cookies[0].Name != "token" || cookies[0].Value != "batch_token" {
		t.Errorf("cookie = %s=%s, want token=batch_token", cookies[0].Name, cookies[0].Value)
	}
}

func TestSetCookieInJSONLBatch(t *testing.T) {
	router := trpcgo.NewRouter(trpcgo.WithBatching(true))

	trpcgo.VoidMutation(router, "noop", func(ctx context.Context) (string, error) {
		return "hi", nil
	})

	trpcgo.VoidMutation(router, "auth.login", func(ctx context.Context) (string, error) {
		trpcgo.SetCookie(ctx, &http.Cookie{
			Name:  "token",
			Value: "jsonl_token",
		})
		return "ok", nil
	})

	server := newTestServer(t, trpc.NewHandler(router, "/trpc"))
	req, _ := http.NewRequest("POST", server.URL+"/trpc/noop,auth.login?batch=1",
		strings.NewReader(`{"0":null,"1":null}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("trpc-accept", "application/jsonl")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	// JSONL streaming sends headers before handlers execute, so cookies set
	// from within handlers cannot be applied — same limitation as tRPC's
	// httpBatchStreamLink where responseMeta runs with eagerGeneration: true.
	cookies := resp.Cookies()
	if len(cookies) != 0 {
		t.Errorf("got %d cookies, want 0 (JSONL sends headers before handlers run)", len(cookies))
	}

	_, chunks := parseJSONLResponse(t, resp)
	if len(chunks) != 2 {
		t.Fatalf("got %d chunks, want 2", len(chunks))
	}
}

func TestSetCookieInSubscription(t *testing.T) {
	router := trpcgo.NewRouter()

	trpcgo.VoidSubscribe(router, "events", func(ctx context.Context) (<-chan string, error) {
		trpcgo.SetCookie(ctx, &http.Cookie{
			Name:  "sub_token",
			Value: "from_subscription",
		})
		ch := make(chan string)
		go func() {
			defer close(ch)
			ch <- "hello"
		}()
		return ch, nil
	})

	server := newTestServer(t, trpc.NewHandler(router, "/trpc"))
	resp := mustGet(t, server, "/trpc/events")
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	cookies := resp.Cookies()
	if len(cookies) != 1 {
		t.Fatalf("got %d cookies, want 1", len(cookies))
	}
	if cookies[0].Name != "sub_token" || cookies[0].Value != "from_subscription" {
		t.Errorf("cookie = %s=%s, want sub_token=from_subscription", cookies[0].Name, cookies[0].Value)
	}

	events := parseSSEEvents(t, resp, 3) // connected + message + return
	if len(events) < 3 {
		t.Fatalf("got %d events, want 3", len(events))
	}
	if events[0].event != "connected" {
		t.Errorf("events[0] = %q, want connected", events[0].event)
	}
}

func TestSetCookieNoopOutsideHandler(t *testing.T) {
	// SetCookie with a plain context should be a no-op, not panic.
	ctx := context.Background()
	trpcgo.SetCookie(ctx, &http.Cookie{Name: "test", Value: "value"})
	trpcgo.SetResponseHeader(ctx, "X-Test", "value")
	// If we got here without panicking, the test passes.
}

func TestSetCookieNilDoesNotPanic(t *testing.T) {
	// SetCookie with nil cookie should be a no-op, not panic.
	ctx := context.Background()
	trpcgo.SetCookie(ctx, nil)
	// If we got here without panicking, the test passes.
}

func TestRawCallSetCookie(t *testing.T) {
	router := trpcgo.NewRouter()

	trpcgo.VoidMutation(router, "auth.login", func(ctx context.Context) (string, error) {
		trpcgo.SetCookie(ctx, &http.Cookie{
			Name:  "session",
			Value: "abc123",
		})
		trpcgo.SetResponseHeader(ctx, "X-Custom", "hello")
		return "ok", nil
	})

	// Pre-inject response metadata so we can read cookies/headers after RawCall.
	ctx := trpcgo.WithResponseMetadata(context.Background())
	result, err := router.RawCall(ctx, "auth.login", nil)
	if err != nil {
		t.Fatalf("RawCall error: %v", err)
	}
	if result != "ok" {
		t.Errorf("result = %v, want ok", result)
	}

	cookies := trpcgo.GetResponseCookies(ctx)
	if len(cookies) != 1 {
		t.Fatalf("got %d cookies, want 1", len(cookies))
	}
	if cookies[0].Name != "session" || cookies[0].Value != "abc123" {
		t.Errorf("cookie = %s=%s, want session=abc123", cookies[0].Name, cookies[0].Value)
	}

	headers := trpcgo.GetResponseHeaders(ctx)
	if got := headers.Get("X-Custom"); got != "hello" {
		t.Errorf("X-Custom = %q, want %q", got, "hello")
	}
}

func TestRawCallWithoutMetadataIsNoop(t *testing.T) {
	router := trpcgo.NewRouter()

	trpcgo.VoidMutation(router, "auth.login", func(ctx context.Context) (string, error) {
		trpcgo.SetCookie(ctx, &http.Cookie{Name: "session", Value: "abc"})
		return "ok", nil
	})

	// Without pre-injecting metadata, RawCall injects its own context.
	// The caller can't access it, but SetCookie must not panic.
	ctx := context.Background()
	result, err := router.RawCall(ctx, "auth.login", nil)
	if err != nil {
		t.Fatalf("RawCall error: %v", err)
	}
	if result != "ok" {
		t.Errorf("result = %v, want ok", result)
	}

	// Cookies are not accessible from the original context.
	cookies := trpcgo.GetResponseCookies(ctx)
	if len(cookies) != 0 {
		t.Errorf("got %d cookies, want 0 (metadata not pre-injected)", len(cookies))
	}
}
