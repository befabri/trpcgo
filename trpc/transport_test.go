package trpc_test

import (
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/befabri/trpcgo/trpc"
)

func TestMethods_MatchWhatTheHandlerServes(t *testing.T) {
	h := trpc.NewHandler(setupRouter(t), "/trpc")
	served := map[string]func() *http.Request{
		http.MethodGet: func() *http.Request {
			return httptest.NewRequest(http.MethodGet, "/trpc/ping", nil)
		},
		http.MethodPost: func() *http.Request {
			req := httptest.NewRequest(http.MethodPost, "/trpc/greet", strings.NewReader(`{"message":"bob"}`))
			req.Header.Set("Content-Type", "application/json")
			return req
		},
	}
	for _, method := range []string{
		http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut, http.MethodPatch,
		http.MethodDelete, http.MethodConnect, http.MethodOptions, http.MethodTrace,
	} {
		t.Run(method, func(t *testing.T) {
			listed := slices.Contains(trpc.Methods(), method)
			build, ok := served[method]
			if listed && !ok {
				t.Fatalf("Methods() lists %s but this test has no request proving the handler serves it", method)
			}
			if !listed {
				build = func() *http.Request { return httptest.NewRequest(method, "/trpc/ping", nil) }
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, build())
			if listed {
				if rec.Code != http.StatusOK {
					t.Fatalf("%s = %d, want 200: %s", method, rec.Code, rec.Body.String())
				}
				if got := rec.Header().Get("Allow"); got != "" {
					t.Fatalf("Allow = %q on a served method, want none", got)
				}
				return
			}
			if rec.Code != http.StatusMethodNotAllowed {
				t.Fatalf("%s = %d, want 405 for a method outside Methods() %v", method, rec.Code, trpc.Methods())
			}
			if got, want := rec.Header().Get("Allow"), strings.Join(trpc.Methods(), ", "); got != want {
				t.Fatalf("Allow = %q, want %q", got, want)
			}
		})
	}
}

func TestRequestHeaders_DriveDefaultCORS(t *testing.T) {
	h := trpc.NewHandler(setupRouter(t), "/trpc", trpc.WithCORS(trpc.CORSConfig{
		AllowedOrigins: []string{"https://app.example.test"},
	}))

	req := httptest.NewRequest(http.MethodOptions, "/trpc/events", nil)
	req.Header.Set("Origin", "https://app.example.test")
	req.Header.Set("Access-Control-Request-Method", http.MethodGet)
	req.Header.Set("Access-Control-Request-Headers", "last-event-id")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("preflight = %d, want 204: %s", rec.Code, rec.Body.String())
	}
	if got, want := rec.Header().Get("Access-Control-Allow-Methods"), strings.Join(trpc.Methods(), ", "); got != want {
		t.Fatalf("Access-Control-Allow-Methods = %q, want %q", got, want)
	}
	if got, want := rec.Header().Get("Access-Control-Allow-Headers"), strings.Join(trpc.RequestHeaders(), ", "); got != want {
		t.Fatalf("Access-Control-Allow-Headers = %q, want %q", got, want)
	}
	// Every header the handler reads must stay in the list.
	for _, header := range []string{"Content-Type", "Last-Event-Id", "trpc-accept"} {
		if !slices.ContainsFunc(trpc.RequestHeaders(), func(h string) bool { return strings.EqualFold(h, header) }) {
			t.Fatalf("RequestHeaders() = %v, missing %s which the handler reads", trpc.RequestHeaders(), header)
		}
	}
}

func TestCORSPreflightForUnlistedMethodCarriesAllow(t *testing.T) {
	h := trpc.NewHandler(setupRouter(t), "/trpc", trpc.WithCORS(trpc.CORSConfig{
		AllowedOrigins: []string{"https://app.example.test"},
	}))

	req := httptest.NewRequest(http.MethodOptions, "/trpc/greet", nil)
	req.Header.Set("Origin", "https://app.example.test")
	req.Header.Set("Access-Control-Request-Method", http.MethodPut)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("preflight for PUT = %d, want 405: %s", rec.Code, rec.Body.String())
	}
	if got, want := rec.Header().Get("Allow"), strings.Join(trpc.Methods(), ", "); got != want {
		t.Fatalf("Allow = %q, want %q", got, want)
	}
}

func TestMethodsAndRequestHeaders_ReturnCopies(t *testing.T) {
	methods, headers := trpc.Methods(), trpc.RequestHeaders()
	methods[0], headers[0] = "BOGUS", "X-Bogus"
	if slices.Contains(trpc.Methods(), "BOGUS") || slices.Contains(trpc.RequestHeaders(), "X-Bogus") {
		t.Fatal("callers mutating the returned slices must not change the contract")
	}
}
