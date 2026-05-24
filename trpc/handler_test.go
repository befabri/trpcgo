package trpc_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/befabri/trpcgo"
	"github.com/befabri/trpcgo/trpc"
)

type echoInput struct {
	Message string `json:"message"`
}

type echoOutput struct {
	Reply string `json:"reply"`
}

func setupRouter(t *testing.T) *trpcgo.Router {
	t.Helper()
	r := trpcgo.NewRouter()
	if err := trpcgo.Query(r, "echo", func(ctx context.Context, input echoInput) (echoOutput, error) {
		return echoOutput{Reply: "echo: " + input.Message}, nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := trpcgo.VoidQuery(r, "ping", func(ctx context.Context) (string, error) {
		return "pong", nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := trpcgo.Mutation(r, "greet", func(ctx context.Context, input echoInput) (echoOutput, error) {
		return echoOutput{Reply: "hello, " + input.Message}, nil
	}); err != nil {
		t.Fatal(err)
	}
	return r
}

func setupSubscriptionRouter(t *testing.T, called *bool) *trpcgo.Router {
	t.Helper()
	r := trpcgo.NewRouter()
	if err := trpcgo.VoidSubscribe(r, "events", func(ctx context.Context) (<-chan string, error) {
		if called != nil {
			*called = true
		}
		ch := make(chan string, 1)
		ch <- "hello"
		close(ch)
		return ch, nil
	}); err != nil {
		t.Fatal(err)
	}
	return r
}

func TestHandler_QueryGET(t *testing.T) {
	r := setupRouter(t)
	h := trpc.NewHandler(r, "/trpc")

	input, _ := json.Marshal(map[string]string{"message": "world"})
	req := httptest.NewRequest(http.MethodGet, "/trpc/echo?input="+string(input), nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var env struct {
		Result struct {
			Data echoOutput `json:"data"`
		} `json:"result"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("failed to unmarshal: %v\nbody: %s", err, rec.Body.String())
	}
	if env.Result.Data.Reply != "echo: world" {
		t.Fatalf("expected 'echo: world', got %q", env.Result.Data.Reply)
	}
}

func TestHandler_MutationPOST(t *testing.T) {
	r := setupRouter(t)
	h := trpc.NewHandler(r, "/trpc")

	body, _ := json.Marshal(map[string]string{"message": "alice"})
	req := httptest.NewRequest(http.MethodPost, "/trpc/greet", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var env struct {
		Result struct {
			Data echoOutput `json:"data"`
		} `json:"result"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if env.Result.Data.Reply != "hello, alice" {
		t.Fatalf("expected 'hello, alice', got %q", env.Result.Data.Reply)
	}
}

func TestHandler_NotFound(t *testing.T) {
	r := setupRouter(t)
	h := trpc.NewHandler(r, "/trpc")

	req := httptest.NewRequest(http.MethodGet, "/trpc/nonexistent", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestHandler_MethodNotAllowed(t *testing.T) {
	called := false
	r := trpcgo.NewRouter()
	if err := trpcgo.Query(r, "echo", func(ctx context.Context, input echoInput) (echoOutput, error) {
		called = true
		return echoOutput{Reply: "echo: " + input.Message}, nil
	}); err != nil {
		t.Fatal(err)
	}
	h := trpc.NewHandler(r, "/trpc")

	req := httptest.NewRequest(http.MethodPut, "/trpc/echo", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}
	if called {
		t.Fatal("handler should not execute a procedure after rejecting an unsupported HTTP method")
	}
}

func TestHandler_QueryPostNotAllowed(t *testing.T) {
	r := setupRouter(t)
	h := trpc.NewHandler(r, "/trpc")

	body, _ := json.Marshal(map[string]string{"message": "test"})
	req := httptest.NewRequest(http.MethodPost, "/trpc/echo", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405 (queries use GET), got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandler_QueryPostWithMethodOverride(t *testing.T) {
	r := trpcgo.NewRouter(trpcgo.WithMethodOverride(true))
	if err := trpcgo.Query(r, "echo", func(ctx context.Context, input echoInput) (echoOutput, error) {
		return echoOutput{Reply: "echo: " + input.Message}, nil
	}); err != nil {
		t.Fatal(err)
	}
	h := trpc.NewHandler(r, "/trpc")

	body, _ := json.Marshal(map[string]string{"message": "override"})
	req := httptest.NewRequest(http.MethodPost, "/trpc/echo", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 with method override, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandler_PostRequiresJSONContentType(t *testing.T) {
	r := setupRouter(t)
	h := trpc.NewHandler(r, "/trpc")

	req := httptest.NewRequest(http.MethodPost, "/trpc/greet", strings.NewReader(`{"message":"alice"}`))
	req.Header.Set("Content-Type", "text/plain")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("status = %d, want 415: %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/trpc/greet", strings.NewReader(`{"message":"alice"}`))
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 for charset content type: %s", rec.Code, rec.Body.String())
	}
}

func TestHandler_PostWithoutBodyAllowsMissingContentType(t *testing.T) {
	r := trpcgo.NewRouter()
	if err := trpcgo.VoidMutation(r, "ping", func(ctx context.Context) (string, error) {
		return "pong", nil
	}); err != nil {
		t.Fatal(err)
	}
	h := trpc.NewHandler(r, "/trpc")

	req := httptest.NewRequest(http.MethodPost, "/trpc/ping", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 for empty body without content type: %s", rec.Code, rec.Body.String())
	}
}

func TestHandler_CSRFRejectsCrossOriginPost(t *testing.T) {
	called := false
	r := trpcgo.NewRouter()
	if err := trpcgo.Mutation(r, "greet", func(ctx context.Context, input echoInput) (echoOutput, error) {
		called = true
		return echoOutput{Reply: "hello, " + input.Message}, nil
	}); err != nil {
		t.Fatal(err)
	}
	h := trpc.NewHandler(r, "/trpc")

	req := httptest.NewRequest(http.MethodPost, "https://api.example.test/trpc/greet", strings.NewReader(`{"message":"alice"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://evil.example.test")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403: %s", rec.Code, rec.Body.String())
	}
	if called {
		t.Fatal("handler should not run for cross-origin POST")
	}
}

func TestHandler_CSRFAllowsSameOriginPost(t *testing.T) {
	r := setupRouter(t)
	h := trpc.NewHandler(r, "/trpc")

	req := httptest.NewRequest(http.MethodPost, "https://api.example.test/trpc/greet", strings.NewReader(`{"message":"alice"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://api.example.test")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 for same-origin POST: %s", rec.Code, rec.Body.String())
	}
}

func TestHandler_CSRFAllowsSameOriginWithDefaultPort(t *testing.T) {
	r := setupRouter(t)
	h := trpc.NewHandler(r, "/trpc")

	req := httptest.NewRequest(http.MethodPost, "https://api.example.test:443/trpc/greet", strings.NewReader(`{"message":"alice"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://api.example.test")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 for same-origin POST with default port: %s", rec.Code, rec.Body.String())
	}
}

func TestHandler_CSRFRejectsInvalidRequestHost(t *testing.T) {
	r := setupRouter(t)
	h := trpc.NewHandler(r, "/trpc")

	req := httptest.NewRequest(http.MethodPost, "http://api.example.test/trpc/greet", strings.NewReader(`{"message":"alice"}`))
	req.Host = "api.example.test:bad"
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "http://api.example.test")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 for invalid request host: %s", rec.Code, rec.Body.String())
	}
}

func TestHandler_CSRFAllowsSameOriginRefererWithPath(t *testing.T) {
	r := setupRouter(t)
	h := trpc.NewHandler(r, "/trpc")

	req := httptest.NewRequest(http.MethodPost, "https://api.example.test/trpc/greet", strings.NewReader(`{"message":"alice"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Referer", "https://api.example.test/dashboard?tab=users")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 for same-origin referer with path: %s", rec.Code, rec.Body.String())
	}
}

func TestHandler_CSRFRejectsOriginHeaderWithPath(t *testing.T) {
	r := setupRouter(t)
	h := trpc.NewHandler(r, "/trpc")

	req := httptest.NewRequest(http.MethodPost, "https://api.example.test/trpc/greet", strings.NewReader(`{"message":"alice"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://api.example.test/dashboard")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 for Origin header with path: %s", rec.Code, rec.Body.String())
	}
}

func TestHandler_CSRFRejectsRefererWithUserinfo(t *testing.T) {
	r := setupRouter(t)
	h := trpc.NewHandler(r, "/trpc")

	req := httptest.NewRequest(http.MethodPost, "https://api.example.test/trpc/greet", strings.NewReader(`{"message":"alice"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Referer", "https://user@api.example.test/dashboard")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 for referer with userinfo: %s", rec.Code, rec.Body.String())
	}
}

func TestHandler_CSRFPublicOriginAllowsTLSProxySameOriginPost(t *testing.T) {
	r := setupRouter(t)
	h := trpc.NewHandler(r, "/trpc", trpc.WithPublicOrigin("https://api.example.test"))

	req := httptest.NewRequest(http.MethodPost, "http://api.example.test/trpc/greet", strings.NewReader(`{"message":"alice"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://api.example.test")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 for configured public origin behind TLS proxy: %s", rec.Code, rec.Body.String())
	}
}

func TestHandler_CSRFPublicOriginRejectsPathConfiguration(t *testing.T) {
	r := setupRouter(t)
	h := trpc.NewHandler(r, "/trpc", trpc.WithPublicOrigin("https://api.example.test/app"))

	req := httptest.NewRequest(http.MethodPost, "http://api.example.test/trpc/greet", strings.NewReader(`{"message":"alice"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://api.example.test")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 for public origin configured with path: %s", rec.Code, rec.Body.String())
	}
}

func TestHandler_TrustedOriginCanonicalizesDefaultPort(t *testing.T) {
	r := setupRouter(t)
	h := trpc.NewHandler(r, "/trpc", trpc.WithTrustedOrigins("https://app.example.test:443"))

	req := httptest.NewRequest(http.MethodPost, "https://api.example.test/trpc/greet", strings.NewReader(`{"message":"alice"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://app.example.test")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 for trusted origin with default port: %s", rec.Code, rec.Body.String())
	}
}

func TestHandler_CSRFRejectsCookiePostWithoutOriginOrReferer(t *testing.T) {
	called := false
	r := trpcgo.NewRouter()
	if err := trpcgo.Mutation(r, "greet", func(ctx context.Context, input echoInput) (echoOutput, error) {
		called = true
		return echoOutput{Reply: "hello, " + input.Message}, nil
	}); err != nil {
		t.Fatal(err)
	}
	h := trpc.NewHandler(r, "/trpc")

	req := httptest.NewRequest(http.MethodPost, "/trpc/greet", strings.NewReader(`{"message":"alice"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Cookie", "sid=123")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403: %s", rec.Code, rec.Body.String())
	}
	if called {
		t.Fatal("handler should not run for cookie POST without Origin or Referer")
	}
}

func TestHandler_CSRFRequireOriginRejectsPostWithoutOriginOrReferer(t *testing.T) {
	r := setupRouter(t)
	h := trpc.NewHandler(r, "/trpc", trpc.WithCSRFRequireOrigin(true))

	req := httptest.NewRequest(http.MethodPost, "/trpc/greet", strings.NewReader(`{"message":"alice"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403: %s", rec.Code, rec.Body.String())
	}
}

func TestHandler_SubscriptionOriginCheckDisabledByDefault(t *testing.T) {
	called := false
	r := setupSubscriptionRouter(t, &called)
	h := trpc.NewHandler(r, "/trpc")

	req := httptest.NewRequest(http.MethodGet, "https://api.example.test/trpc/events", nil)
	req.Header.Set("Origin", "https://evil.example.test")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 when subscription origin check is disabled: %s", rec.Code, rec.Body.String())
	}
	if !called {
		t.Fatal("subscription should run when origin check is disabled")
	}
}

func TestHandler_SubscriptionOriginCheckRejectsCrossOriginGet(t *testing.T) {
	called := false
	r := setupSubscriptionRouter(t, &called)
	h := trpc.NewHandler(r, "/trpc", trpc.WithSubscriptionOriginCheck(true))

	req := httptest.NewRequest(http.MethodGet, "https://api.example.test/trpc/events", nil)
	req.Header.Set("Origin", "https://evil.example.test")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403: %s", rec.Code, rec.Body.String())
	}
	if called {
		t.Fatal("subscription should not run for rejected cross-origin GET")
	}
}

func TestHandler_SubscriptionOriginCheckRejectsCookieGetWithoutOriginOrReferer(t *testing.T) {
	called := false
	r := setupSubscriptionRouter(t, &called)
	h := trpc.NewHandler(r, "/trpc", trpc.WithSubscriptionOriginCheck(true))

	req := httptest.NewRequest(http.MethodGet, "https://api.example.test/trpc/events", nil)
	req.Header.Set("Cookie", "sid=123")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403: %s", rec.Code, rec.Body.String())
	}
	if called {
		t.Fatal("subscription should not run for cookie GET without Origin or Referer")
	}
}

func TestHandler_SubscriptionOriginCheckAllowsNonCookieGetWithoutOriginOrReferer(t *testing.T) {
	called := false
	r := setupSubscriptionRouter(t, &called)
	h := trpc.NewHandler(r, "/trpc", trpc.WithSubscriptionOriginCheck(true))

	req := httptest.NewRequest(http.MethodGet, "https://api.example.test/trpc/events", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 for non-cookie subscription without browser origin headers: %s", rec.Code, rec.Body.String())
	}
	if !called {
		t.Fatal("subscription should run for non-cookie GET without Origin or Referer")
	}
}

func TestHandler_SubscriptionOriginCheckAllowsSameOriginGet(t *testing.T) {
	called := false
	r := setupSubscriptionRouter(t, &called)
	h := trpc.NewHandler(r, "/trpc", trpc.WithSubscriptionOriginCheck(true))

	req := httptest.NewRequest(http.MethodGet, "https://api.example.test/trpc/events", nil)
	req.Header.Set("Origin", "https://api.example.test")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 for same-origin subscription: %s", rec.Code, rec.Body.String())
	}
	if !called {
		t.Fatal("subscription should run for same-origin GET")
	}
}

func TestHandler_SubscriptionOriginCheckAllowsCORSOrigin(t *testing.T) {
	called := false
	r := setupSubscriptionRouter(t, &called)
	h := trpc.NewHandler(r, "/trpc",
		trpc.WithCORS(trpc.CORSConfig{
			AllowedOrigins:   []string{"https://app.example.test"},
			AllowCredentials: true,
		}),
		trpc.WithSubscriptionOriginCheck(true),
	)

	req := httptest.NewRequest(http.MethodGet, "https://api.example.test/trpc/events", nil)
	req.Header.Set("Origin", "https://app.example.test")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 for CORS-allowed subscription: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://app.example.test" {
		t.Fatalf("Access-Control-Allow-Origin = %q", got)
	}
	if !called {
		t.Fatal("subscription should run for CORS-allowed origin")
	}
}

func TestHandler_SubscriptionOriginCheckAllowsTrustedOriginWithoutCORS(t *testing.T) {
	// External CORS middleware case: no trpc.WithCORS, but the origin is
	// explicitly trusted. The subscription check must honor WithTrustedOrigins.
	called := false
	r := setupSubscriptionRouter(t, &called)
	h := trpc.NewHandler(r, "/trpc",
		trpc.WithTrustedOrigins("https://app.example.test"),
		trpc.WithSubscriptionOriginCheck(true),
	)

	req := httptest.NewRequest(http.MethodGet, "https://api.example.test/trpc/events", nil)
	req.Header.Set("Origin", "https://app.example.test")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 for trusted-origin subscription: %s", rec.Code, rec.Body.String())
	}
	if !called {
		t.Fatal("subscription should run for trusted origin")
	}
}

func TestHandler_SubscriptionOriginCheckAllowsSameOriginRefererWithPath(t *testing.T) {
	called := false
	r := setupSubscriptionRouter(t, &called)
	h := trpc.NewHandler(r, "/trpc", trpc.WithSubscriptionOriginCheck(true))

	req := httptest.NewRequest(http.MethodGet, "https://api.example.test/trpc/events", nil)
	req.Header.Set("Referer", "https://api.example.test/dashboard?tab=live")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 for same-origin referer: %s", rec.Code, rec.Body.String())
	}
	if !called {
		t.Fatal("subscription should run for same-origin referer")
	}
}

func TestHandler_SubscriptionOriginCheckRejectsCrossOriginReferer(t *testing.T) {
	called := false
	r := setupSubscriptionRouter(t, &called)
	h := trpc.NewHandler(r, "/trpc", trpc.WithSubscriptionOriginCheck(true))

	req := httptest.NewRequest(http.MethodGet, "https://api.example.test/trpc/events", nil)
	req.Header.Set("Referer", "https://evil.example.test/page")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 for cross-origin referer: %s", rec.Code, rec.Body.String())
	}
	if called {
		t.Fatal("subscription should not run for cross-origin referer")
	}
}

func TestHandler_SubscriptionOriginCheckAllowsPublicOriginProxy(t *testing.T) {
	// TLS-terminating proxy: the server sees internal http while the browser
	// uses the public https origin.
	called := false
	r := setupSubscriptionRouter(t, &called)
	h := trpc.NewHandler(r, "/trpc",
		trpc.WithPublicOrigin("https://api.example.test"),
		trpc.WithSubscriptionOriginCheck(true),
	)

	req := httptest.NewRequest(http.MethodGet, "http://api.example.test/trpc/events", nil)
	req.Header.Set("Origin", "https://api.example.test")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 for configured public origin: %s", rec.Code, rec.Body.String())
	}
	if !called {
		t.Fatal("subscription should run for public origin behind TLS proxy")
	}
}

func TestHandler_SubscriptionOriginCheckPostRequiresCSRFTrust(t *testing.T) {
	// POST subscriptions pass through the POST CSRF check first, which honors
	// trusted origins but not CORS read access. A CORS-allowed but untrusted
	// origin is therefore rejected before the subscription check runs.
	called := false
	r := setupSubscriptionRouter(t, &called)
	h := trpc.NewHandler(r, "/trpc",
		trpc.WithCORS(trpc.CORSConfig{AllowedOrigins: []string{"https://app.example.test"}}),
		trpc.WithSubscriptionOriginCheck(true),
	)

	req := httptest.NewRequest(http.MethodPost, "https://api.example.test/trpc/events", nil)
	req.Header.Set("Origin", "https://app.example.test")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 for cross-origin POST subscription without trust: %s", rec.Code, rec.Body.String())
	}
	if called {
		t.Fatal("POST subscription should not run for untrusted cross-origin request")
	}
}

func TestHandler_SubscriptionOriginCheckWildcardRejectsCookieRequest(t *testing.T) {
	// Wildcard CORS must not let a cookie-bearing cross-site subscription reach
	// the resolver, even though the browser would block the response read.
	called := false
	r := setupSubscriptionRouter(t, &called)
	h := trpc.NewHandler(r, "/trpc",
		trpc.WithCORS(trpc.CORSConfig{AllowedOrigins: []string{"*"}}),
		trpc.WithSubscriptionOriginCheck(true),
	)

	req := httptest.NewRequest(http.MethodGet, "https://api.example.test/trpc/events", nil)
	req.Header.Set("Origin", "https://evil.example.test")
	req.Header.Set("Cookie", "sid=123")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 for wildcard CORS cookie subscription: %s", rec.Code, rec.Body.String())
	}
	if called {
		t.Fatal("subscription should not run for cookie-bearing cross-site request under wildcard CORS")
	}
}

func TestHandler_SubscriptionOriginCheckWildcardAllowsCookielessRequest(t *testing.T) {
	// Without a cookie there is no ambient credential, so wildcard CORS may
	// still admit an anonymous cross-site subscription.
	called := false
	r := setupSubscriptionRouter(t, &called)
	h := trpc.NewHandler(r, "/trpc",
		trpc.WithCORS(trpc.CORSConfig{AllowedOrigins: []string{"*"}}),
		trpc.WithSubscriptionOriginCheck(true),
	)

	req := httptest.NewRequest(http.MethodGet, "https://api.example.test/trpc/events", nil)
	req.Header.Set("Origin", "https://other.example.test")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 for cookie-less wildcard subscription: %s", rec.Code, rec.Body.String())
	}
	if !called {
		t.Fatal("cookie-less wildcard subscription should run")
	}
}

func TestHandler_CORSPreflight(t *testing.T) {
	r := setupRouter(t)
	h := trpc.NewHandler(r, "/trpc", trpc.WithCORS(trpc.CORSConfig{
		AllowedOrigins:   []string{"https://app.example.test:443"},
		AllowCredentials: true,
	}))

	req := httptest.NewRequest(http.MethodOptions, "/trpc/greet", nil)
	req.Header.Set("Origin", "https://app.example.test:443")
	req.Header.Set("Access-Control-Request-Method", "POST")
	req.Header.Set("Access-Control-Request-Headers", "Content-Type, Authorization")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://app.example.test:443" {
		t.Fatalf("Access-Control-Allow-Origin = %q", got)
	}
	if got := rec.Header().Get("Access-Control-Allow-Credentials"); got != "true" {
		t.Fatalf("Access-Control-Allow-Credentials = %q", got)
	}
	if got := rec.Header().Get("Access-Control-Allow-Methods"); !strings.Contains(got, "POST") {
		t.Fatalf("Access-Control-Allow-Methods = %q", got)
	}
	if vary := rec.Header().Get("Vary"); !strings.Contains(vary, "Origin") || !strings.Contains(vary, "Access-Control-Request-Method") {
		t.Fatalf("Vary = %q", vary)
	}
}

func TestHandler_CORSMaxAge(t *testing.T) {
	preflight := func(t *testing.T, h *trpc.Handler) string {
		t.Helper()
		req := httptest.NewRequest(http.MethodOptions, "/trpc/greet", nil)
		req.Header.Set("Origin", "https://app.example.test")
		req.Header.Set("Access-Control-Request-Method", "POST")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusNoContent {
			t.Fatalf("status = %d, want 204: %s", rec.Code, rec.Body.String())
		}
		return rec.Header().Get("Access-Control-Max-Age")
	}

	t.Run("whole seconds emitted verbatim", func(t *testing.T) {
		r := setupRouter(t)
		h := trpc.NewHandler(r, "/trpc", trpc.WithCORS(trpc.CORSConfig{
			AllowedOrigins: []string{"https://app.example.test"},
			MaxAge:         600 * time.Second,
		}))
		if got := preflight(t, h); got != "600" {
			t.Fatalf("Access-Control-Max-Age = %q, want %q", got, "600")
		}
	})

	t.Run("positive sub-second is not truncated to zero", func(t *testing.T) {
		r := setupRouter(t)
		h := trpc.NewHandler(r, "/trpc", trpc.WithCORS(trpc.CORSConfig{
			AllowedOrigins: []string{"https://app.example.test"},
			MaxAge:         500 * time.Millisecond,
		}))
		// The caller asked for caching; 0 would mean "do not cache" — round up to 1s.
		if got := preflight(t, h); got != "1" {
			t.Fatalf("Access-Control-Max-Age = %q, want %q (sub-second must not become 0)", got, "1")
		}
	})

	t.Run("zero MaxAge omits the header", func(t *testing.T) {
		r := setupRouter(t)
		h := trpc.NewHandler(r, "/trpc", trpc.WithCORS(trpc.CORSConfig{
			AllowedOrigins: []string{"https://app.example.test"},
		}))
		if got := preflight(t, h); got != "" {
			t.Fatalf("Access-Control-Max-Age = %q, want empty when MaxAge unset", got)
		}
	})
}

func TestHandler_CORSRejectsDisallowedPreflightOrigin(t *testing.T) {
	r := setupRouter(t)
	h := trpc.NewHandler(r, "/trpc", trpc.WithCORS(trpc.CORSConfig{
		AllowedOrigins: []string{"https://app.example.test"},
	}))

	req := httptest.NewRequest(http.MethodOptions, "/trpc/greet", nil)
	req.Header.Set("Origin", "https://evil.example.test")
	req.Header.Set("Access-Control-Request-Method", "POST")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("disallowed origin got Access-Control-Allow-Origin = %q", got)
	}
	if vary := rec.Header().Get("Vary"); !strings.Contains(vary, "Origin") {
		t.Fatalf("Vary = %q, want Origin for disallowed preflight", vary)
	}
}

func TestHandler_CORSAllowedOriginDoesNotBypassCSRF(t *testing.T) {
	r := setupRouter(t)
	h := trpc.NewHandler(r, "/trpc", trpc.WithCORS(trpc.CORSConfig{
		AllowedOrigins:   []string{"https://app.example.test"},
		AllowCredentials: true,
	}))

	req := httptest.NewRequest(http.MethodPost, "https://api.example.test/trpc/greet", strings.NewReader(`{"message":"alice"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://app.example.test")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 because CORS does not grant CSRF trust: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://app.example.test" {
		t.Fatalf("Access-Control-Allow-Origin = %q", got)
	}
}

func TestHandler_CORSWithTrustedOriginAllowsCSRF(t *testing.T) {
	r := setupRouter(t)
	h := trpc.NewHandler(r, "/trpc",
		trpc.WithCORS(trpc.CORSConfig{
			AllowedOrigins:   []string{"https://app.example.test"},
			AllowCredentials: true,
		}),
		trpc.WithTrustedOrigins("https://app.example.test"),
	)

	req := httptest.NewRequest(http.MethodPost, "https://api.example.test/trpc/greet", strings.NewReader(`{"message":"alice"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://app.example.test")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 for explicitly trusted CORS origin: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://app.example.test" {
		t.Fatalf("Access-Control-Allow-Origin = %q", got)
	}
}

func TestHandler_TrustedOriginRejectsNonOriginConfiguration(t *testing.T) {
	tests := []string{
		"https://app.example.test/",
		"https://app.example.test/admin",
		"https://app.example.test?debug=true",
		"https://app.example.test#section",
		"https://user@app.example.test",
		"https://app.example.test:bad",
	}

	for _, configuredOrigin := range tests {
		t.Run(configuredOrigin, func(t *testing.T) {
			r := setupRouter(t)
			h := trpc.NewHandler(r, "/trpc", trpc.WithTrustedOrigins(configuredOrigin))

			req := httptest.NewRequest(http.MethodPost, "https://api.example.test/trpc/greet", strings.NewReader(`{"message":"alice"}`))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Origin", "https://app.example.test")
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)

			if rec.Code != http.StatusForbidden {
				t.Fatalf("status = %d, want 403 for trusted origin configured as %q: %s", rec.Code, configuredOrigin, rec.Body.String())
			}
		})
	}
}

func TestHandler_CORSRejectsPathConfiguredOrigin(t *testing.T) {
	r := setupRouter(t)
	h := trpc.NewHandler(r, "/trpc", trpc.WithCORS(trpc.CORSConfig{
		AllowedOrigins: []string{"https://app.example.test/admin"},
	}))

	req := httptest.NewRequest(http.MethodOptions, "/trpc/greet", nil)
	req.Header.Set("Origin", "https://app.example.test")
	req.Header.Set("Access-Control-Request-Method", "POST")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 for CORS origin configured with path", rec.Code)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("Access-Control-Allow-Origin = %q, want empty", got)
	}
}

func TestHandler_BatchGET(t *testing.T) {
	r := setupRouter(t)
	h := trpc.NewHandler(r, "/trpc")

	input, _ := json.Marshal(map[string]json.RawMessage{
		"0": json.RawMessage(`{"message":"a"}`),
		"1": json.RawMessage(`{"message":"b"}`),
	})
	req := httptest.NewRequest(http.MethodGet, "/trpc/echo,echo?batch=1&input="+string(input), nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var results []json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &results); err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
}

func TestHandler_BatchMaxSizeBoundaries(t *testing.T) {
	t.Run("exact limit allowed", func(t *testing.T) {
		called := 0
		r := trpcgo.NewRouter(trpcgo.WithMaxBatchSize(2))
		if err := trpcgo.VoidQuery(r, "ping", func(ctx context.Context) (string, error) {
			called++
			return "pong", nil
		}); err != nil {
			t.Fatal(err)
		}
		h := trpc.NewHandler(r, "/trpc")

		req := httptest.NewRequest(http.MethodGet, "/trpc/ping,ping?batch=1", nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 at batch limit: %s", rec.Code, rec.Body.String())
		}
		if called != 2 {
			t.Fatalf("called = %d, want 2", called)
		}
	})

	t.Run("exceeding limit rejected", func(t *testing.T) {
		called := 0
		r := trpcgo.NewRouter(trpcgo.WithMaxBatchSize(1))
		if err := trpcgo.VoidQuery(r, "ping", func(ctx context.Context) (string, error) {
			called++
			return "pong", nil
		}); err != nil {
			t.Fatal(err)
		}
		h := trpc.NewHandler(r, "/trpc")

		req := httptest.NewRequest(http.MethodGet, "/trpc/ping,ping?batch=1", nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400 above batch limit: %s", rec.Code, rec.Body.String())
		}
		if called != 0 {
			t.Fatalf("called = %d, want 0 after batch limit rejection", called)
		}
	})

	t.Run("unlimited allows batch", func(t *testing.T) {
		called := 0
		r := trpcgo.NewRouter(trpcgo.WithMaxBatchSize(-1))
		if err := trpcgo.VoidQuery(r, "ping", func(ctx context.Context) (string, error) {
			called++
			return "pong", nil
		}); err != nil {
			t.Fatal(err)
		}
		h := trpc.NewHandler(r, "/trpc")

		req := httptest.NewRequest(http.MethodGet, "/trpc/ping,ping?batch=1", nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 with unlimited batch size: %s", rec.Code, rec.Body.String())
		}
		if called != 2 {
			t.Fatalf("called = %d, want 2", called)
		}
	})
}

func TestHandler_BatchRejectsSubscriptionsBeforeExecute(t *testing.T) {
	called := false
	r := trpcgo.NewRouter()
	if err := trpcgo.VoidSubscribe(r, "events", func(ctx context.Context) (<-chan string, error) {
		called = true
		ch := make(chan string)
		close(ch)
		return ch, nil
	}); err != nil {
		t.Fatal(err)
	}
	h := trpc.NewHandler(r, "/trpc")

	req := httptest.NewRequest(http.MethodGet, "/trpc/events,events?batch=1", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for batched subscription: %s", rec.Code, rec.Body.String())
	}
	if called {
		t.Fatal("subscription should not execute after batched subscription rejection")
	}
}

func TestHandler_JSONLBatchGET(t *testing.T) {
	r := setupRouter(t)
	h := trpc.NewHandler(r, "/trpc")

	input, _ := json.Marshal(map[string]json.RawMessage{
		"1": json.RawMessage(`{"message":"jsonl"}`),
	})
	req := httptest.NewRequest(http.MethodGet, "/trpc/ping,echo?batch=1&input="+url.QueryEscape(string(input)), nil)
	req.Header.Set("trpc-accept", "application/jsonl")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", ct)
	}
	if vary := rec.Header().Get("Vary"); vary != "trpc-accept" {
		t.Fatalf("Vary = %q, want trpc-accept", vary)
	}

	lines := strings.Split(strings.TrimSpace(rec.Body.String()), "\n")
	if len(lines) != 3 {
		t.Fatalf("got %d JSONL lines, want 3:\n%s", len(lines), rec.Body.String())
	}
	if !strings.Contains(lines[0], `"0"`) || !strings.Contains(lines[0], `"1"`) {
		t.Fatalf("head line missing batch indexes: %s", lines[0])
	}
	var head map[string][][]any
	if err := json.Unmarshal([]byte(lines[0]), &head); err != nil {
		t.Fatalf("failed to unmarshal JSONL head: %v\n%s", err, lines[0])
	}
	for i := range 2 {
		key := string(rune('0' + i))
		entry := head[key]
		if len(entry) != 2 || len(entry[0]) != 1 || len(entry[1]) != 3 {
			t.Fatalf("head[%q] has unexpected shape: %#v", key, entry)
		}
		if entry[0][0] != float64(0) || entry[1][1] != float64(0) || entry[1][2] != float64(i) {
			t.Fatalf("head[%q] = %#v, want [[0],[nil,0,%d]]", key, entry, i)
		}
	}
	seenChunks := make(map[int]bool)
	for _, line := range lines[1:] {
		var chunk []any
		if err := json.Unmarshal([]byte(line), &chunk); err != nil {
			t.Fatalf("failed to unmarshal JSONL chunk: %v\n%s", err, line)
		}
		if len(chunk) != 3 {
			t.Fatalf("chunk has unexpected shape: %#v", chunk)
		}
		idx, ok := chunk[0].(float64)
		if !ok {
			t.Fatalf("chunk index has type %T, want number", chunk[0])
		}
		if chunk[1] != float64(0) {
			t.Fatalf("chunk control value = %#v, want 0 in %#v", chunk[1], chunk)
		}
		seenChunks[int(idx)] = true
	}
	if !seenChunks[0] || !seenChunks[1] {
		t.Fatalf("JSONL chunks missing indexes 0 and 1: %#v", seenChunks)
	}
	if !strings.Contains(rec.Body.String(), `"pong"`) {
		t.Fatalf("JSONL body missing ping result: %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"echo: jsonl"`) {
		t.Fatalf("JSONL body missing echo result: %s", rec.Body.String())
	}
}

type noFlushResponseWriter struct {
	header http.Header
	status int
	body   strings.Builder
}

func (w *noFlushResponseWriter) Header() http.Header {
	if w.header == nil {
		w.header = make(http.Header)
	}
	return w.header
}

func (w *noFlushResponseWriter) Write(b []byte) (int, error) {
	return w.body.Write(b)
}

func (w *noFlushResponseWriter) WriteHeader(status int) {
	w.status = status
}

func TestHandler_JSONLBatchRequiresFlusher(t *testing.T) {
	r := setupRouter(t)
	h := trpc.NewHandler(r, "/trpc")
	req := httptest.NewRequest(http.MethodGet, "/trpc/ping,ping?batch=1", nil)
	req.Header.Set("trpc-accept", "application/jsonl")
	rec := &noFlushResponseWriter{}

	h.ServeHTTP(rec, req)

	if rec.status != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500; body: %s", rec.status, rec.body.String())
	}
	if !strings.Contains(rec.body.String(), "streaming not supported") {
		t.Fatalf("body missing streaming error: %s", rec.body.String())
	}
}

func TestHandler_Subscription(t *testing.T) {
	r := trpcgo.NewRouter()
	if err := trpcgo.VoidSubscribe(r, "counter", func(ctx context.Context) (<-chan int, error) {
		ch := make(chan int, 3)
		ch <- 1
		ch <- 2
		ch <- 3
		close(ch)
		return ch, nil
	}); err != nil {
		t.Fatal(err)
	}

	h := trpc.NewHandler(r, "/trpc")
	req := httptest.NewRequest(http.MethodGet, "/trpc/counter", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("expected text/event-stream, got %q", ct)
	}

	body := rec.Body.String()

	// tRPC SSE starts with "connected" event.
	if !strings.Contains(body, "event: connected\n") {
		t.Fatalf("expected connected event in: %s", body)
	}

	// Should contain 3 data messages and a return event.
	dataCount := strings.Count(body, "\ndata: ")
	// data lines: 1 for connected + 3 for items + 1 for return = at least 5
	if dataCount < 4 {
		t.Fatalf("expected at least 4 data lines, got %d in: %s", dataCount, body)
	}
	if !strings.Contains(body, "event: return\n") {
		t.Fatalf("expected return event in: %s", body)
	}
}

func TestHandler_LastEventIDMergedOnlyForSubscriptions(t *testing.T) {
	t.Run("subscription receives last event id", func(t *testing.T) {
		type subInput struct {
			LastEventID string `json:"lastEventId"`
		}
		gotID := make(chan string, 1)
		r := trpcgo.NewRouter()
		if err := trpcgo.Subscribe(r, "events", func(ctx context.Context, input subInput) (<-chan string, error) {
			gotID <- input.LastEventID
			ch := make(chan string)
			close(ch)
			return ch, nil
		}); err != nil {
			t.Fatal(err)
		}
		h := trpc.NewHandler(r, "/trpc")

		req := httptest.NewRequest(http.MethodGet, "/trpc/events", nil)
		req.Header.Set("Last-Event-Id", "evt-1")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
		}
		select {
		case id := <-gotID:
			if id != "evt-1" {
				t.Fatalf("lastEventId = %q, want evt-1", id)
			}
		default:
			t.Fatal("subscription handler did not receive input")
		}
	})

	t.Run("query ignores last event id", func(t *testing.T) {
		called := false
		r := trpcgo.NewRouter()
		if err := trpcgo.Query(r, "echo", func(ctx context.Context, input echoInput) (echoOutput, error) {
			called = true
			return echoOutput{Reply: "echo: " + input.Message}, nil
		}); err != nil {
			t.Fatal(err)
		}
		h := trpc.NewHandler(r, "/trpc")
		input, _ := json.Marshal(map[string]string{"message": "world"})

		req := httptest.NewRequest(http.MethodGet, "/trpc/echo?input="+url.QueryEscape(string(input)), nil)
		req.Header.Set("Last-Event-Id", "evt-ignored")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 when query has Last-Event-Id: %s", rec.Code, rec.Body.String())
		}
		if !called {
			t.Fatal("query did not execute")
		}
	})
}

func TestHandler_SSEUnlimitedMaxDurationWaitsForData(t *testing.T) {
	r := trpcgo.NewRouter(trpcgo.WithSSEMaxDuration(-1))
	if err := trpcgo.VoidSubscribe(r, "late", func(ctx context.Context) (<-chan string, error) {
		ch := make(chan string, 1)
		go func() {
			time.Sleep(10 * time.Millisecond)
			ch <- "late"
			close(ch)
		}()
		return ch, nil
	}); err != nil {
		t.Fatal(err)
	}
	h := trpc.NewHandler(r, "/trpc")
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	req := httptest.NewRequest(http.MethodGet, "/trpc/late", nil).WithContext(ctx)
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `data: "late"`) {
		t.Fatalf("SSE body missing delayed item with unlimited max duration: %s", rec.Body.String())
	}
}

func TestHandler_SSEOneNanosecondMaxDurationReturns(t *testing.T) {
	r := trpcgo.NewRouter(
		trpcgo.WithSSEMaxDuration(time.Nanosecond),
		trpcgo.WithSSEPingInterval(time.Hour),
	)
	if err := trpcgo.VoidSubscribe(r, "never", func(ctx context.Context) (<-chan string, error) {
		return make(chan string), nil
	}); err != nil {
		t.Fatal(err)
	}
	h := trpc.NewHandler(r, "/trpc")
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	req := httptest.NewRequest(http.MethodGet, "/trpc/never", nil).WithContext(ctx)
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "event: return\n") {
		t.Fatalf("SSE body missing max-duration return event: %s", rec.Body.String())
	}
}

func TestHandler_SSEStreamPanicWritesSerializedError(t *testing.T) {
	r := trpcgo.NewRouter(trpcgo.WithSSEMaxDuration(10 * time.Millisecond))
	if err := trpcgo.VoidSubscribe(r, "panic", func(ctx context.Context) (<-chan string, error) {
		ch := make(chan string, 1)
		ch <- "boom"
		return ch, nil
	}, trpcgo.WithOutputValidator(func(any) error {
		panic("validator panic")
	})); err != nil {
		t.Fatal(err)
	}
	h := trpc.NewHandler(r, "/trpc")
	req := httptest.NewRequest(http.MethodGet, "/trpc/panic", nil)
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "event: serialized-error\n") {
		t.Fatalf("SSE body missing serialized error after stream panic: %s", rec.Body.String())
	}
}

func TestHandler_SSEConnectionCountDecrementedAfterStream(t *testing.T) {
	r := trpcgo.NewRouter(trpcgo.WithSSEMaxConnections(1))
	if err := trpcgo.VoidSubscribe(r, "events", func(ctx context.Context) (<-chan string, error) {
		ch := make(chan string)
		close(ch)
		return ch, nil
	}); err != nil {
		t.Fatal(err)
	}
	h := trpc.NewHandler(r, "/trpc")
	req := httptest.NewRequest(http.MethodGet, "/trpc/events", nil)
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if got := r.TrackSSEConnection(0); got != 0 {
		t.Fatalf("SSE connection count = %d, want 0 after stream closes", got)
	}
}

func TestHandler_PathTraversal(t *testing.T) {
	r := setupRouter(t)
	h := trpc.NewHandler(r, "/trpc")

	req := httptest.NewRequest(http.MethodGet, "/trpc/../etc/passwd", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest && rec.Code != http.StatusNotFound {
		t.Fatalf("expected 400 or 404 for path traversal, got %d", rec.Code)
	}
}

func TestHandler_MutationGETNotAllowed(t *testing.T) {
	r := setupRouter(t)
	h := trpc.NewHandler(r, "/trpc")

	req := httptest.NewRequest(http.MethodGet, "/trpc/greet", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405 (mutations use POST), got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandler_EmptyBasePath(t *testing.T) {
	r := setupRouter(t)
	h := trpc.NewHandler(r, "")

	req := httptest.NewRequest(http.MethodGet, "/ping", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandler_ErrorCallback(t *testing.T) {
	var calledPath string
	r := trpcgo.NewRouter(
		trpcgo.WithOnError(func(ctx context.Context, err *trpcgo.Error, path string) {
			calledPath = path
		}),
	)
	if err := trpcgo.Query(r, "echo", func(ctx context.Context, input echoInput) (echoOutput, error) {
		return echoOutput{}, nil
	}); err != nil {
		t.Fatal(err)
	}

	h := trpc.NewHandler(r, "/trpc")
	req := httptest.NewRequest(http.MethodGet, "/trpc/nonexistent", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
	if calledPath != "nonexistent" {
		t.Fatalf("expected error callback for path 'nonexistent', got %q", calledPath)
	}
}

func TestHandler_ErrorCallbackForProcedureError(t *testing.T) {
	cause := errors.New("backend failed")
	var callbackPath string
	var callbackErr *trpcgo.Error
	r := trpcgo.NewRouter(
		trpcgo.WithOnError(func(ctx context.Context, err *trpcgo.Error, path string) {
			callbackPath = path
			callbackErr = err
		}),
	)
	if err := trpcgo.VoidQuery(r, "fail", func(ctx context.Context) (string, error) {
		return "", cause
	}); err != nil {
		t.Fatal(err)
	}

	h := trpc.NewHandler(r, "/trpc")
	req := httptest.NewRequest(http.MethodGet, "/trpc/fail", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", rec.Code, rec.Body.String())
	}
	if callbackPath != "fail" {
		t.Fatalf("callback path = %q, want fail", callbackPath)
	}
	if callbackErr == nil || !errors.Is(callbackErr.Cause, cause) {
		t.Fatalf("callback error = %#v, want wrapped cause %v", callbackErr, cause)
	}
}

func TestHandler_BatchDisabled(t *testing.T) {
	called := false
	r := trpcgo.NewRouter(trpcgo.WithBatching(false))
	if err := trpcgo.VoidQuery(r, "ping", func(ctx context.Context) (string, error) {
		called = true
		return "pong", nil
	}); err != nil {
		t.Fatal(err)
	}

	h := trpc.NewHandler(r, "/trpc")
	req := httptest.NewRequest(http.MethodGet, "/trpc/ping,ping?batch=1", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for disabled batching, got %d: %s", rec.Code, rec.Body.String())
	}
	if called {
		t.Fatal("handler should not execute procedures after disabled batch rejection")
	}
}

func TestHandler_VoidQuery(t *testing.T) {
	r := setupRouter(t)
	h := trpc.NewHandler(r, "/trpc")

	req := httptest.NewRequest(http.MethodGet, "/trpc/ping", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var env struct {
		Result struct {
			Data string `json:"data"`
		} `json:"result"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if env.Result.Data != "pong" {
		t.Fatalf("expected 'pong', got %q", env.Result.Data)
	}
}
