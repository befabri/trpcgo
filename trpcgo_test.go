package trpcgo_test

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/befabri/trpcgo"
)

// Test models

type User struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type GetUserInput struct {
	ID string `json:"id"`
}

type CreateUserInput struct {
	Name string `json:"name"`
}

// setupRouter creates a Router with a standard set of procedures for testing.
func setupRouter() *trpcgo.Router {
	r := trpcgo.NewRouter(trpcgo.WithBatching(true))

	trpcgo.VoidQuery(r, "hello", func(ctx context.Context) (string, error) {
		return "Hello world!", nil
	})

	trpcgo.Query(r, "user.getById", func(ctx context.Context, input GetUserInput) (User, error) {
		if input.ID == "" {
			return User{}, trpcgo.NewError(trpcgo.CodeBadRequest, "id is required")
		}
		if input.ID == "404" {
			return User{}, trpcgo.NewError(trpcgo.CodeNotFound, "user not found")
		}
		return User{ID: input.ID, Name: "Alice"}, nil
	})

	trpcgo.Mutation(r, "user.create", func(ctx context.Context, input CreateUserInput) (User, error) {
		if input.Name == "" {
			return User{}, trpcgo.NewError(trpcgo.CodeBadRequest, "name is required")
		}
		return User{ID: "new-id", Name: input.Name}, nil
	})

	return r
}

func newTestServer(t *testing.T, handler http.Handler) *httptest.Server {
	t.Helper()
	s := httptest.NewServer(handler)
	t.Cleanup(s.Close)
	return s
}

func mustGet(t *testing.T, server *httptest.Server, path string) *http.Response {
	t.Helper()
	resp, err := http.Get(server.URL + path)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	return resp
}

func mustPost(t *testing.T, server *httptest.Server, path, body string) *http.Response {
	t.Helper()
	resp, err := http.Post(server.URL+path, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST %s: %v", path, err)
	}
	return resp
}

func mustRequest(t *testing.T, server *httptest.Server, method, path string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, server.URL+path, nil)
	if err != nil {
		t.Fatalf("new request %s %s: %v", method, path, err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	return resp
}

// decodeJSON reads the response body and returns it as a generic map.
func decodeJSON(t *testing.T, resp *http.Response) map[string]any {
	t.Helper()
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("unmarshal %q: %v", raw, err)
	}
	return m
}

// decodeJSONArray reads the response body as a JSON array.
func decodeJSONArray(t *testing.T, resp *http.Response) []map[string]any {
	t.Helper()
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	var a []map[string]any
	if err := json.Unmarshal(raw, &a); err != nil {
		t.Fatalf("unmarshal array %q: %v", raw, err)
	}
	return a
}

// resultData extracts result.data as a map from a tRPC success envelope.
func resultData(t *testing.T, envelope map[string]any) map[string]any {
	t.Helper()
	r, ok := envelope["result"].(map[string]any)
	if !ok {
		t.Fatalf("expected result envelope, got keys %v", keys(envelope))
	}
	d, ok := r["data"].(map[string]any)
	if !ok {
		t.Fatalf("expected data to be object, got %T: %v", r["data"], r["data"])
	}
	return d
}

// resultScalar extracts result.data as a scalar value from a tRPC success envelope.
func resultScalar(t *testing.T, envelope map[string]any) any {
	t.Helper()
	r, ok := envelope["result"].(map[string]any)
	if !ok {
		t.Fatalf("expected result envelope, got keys %v", keys(envelope))
	}
	return r["data"]
}

// errorData extracts the error.data object from a tRPC error envelope.
func errorData(t *testing.T, envelope map[string]any) map[string]any {
	t.Helper()
	e, ok := envelope["error"].(map[string]any)
	if !ok {
		t.Fatalf("expected error envelope, got keys %v", keys(envelope))
	}
	d, ok := e["data"].(map[string]any)
	if !ok {
		t.Fatalf("expected error data, got %T: %v", e["data"], e["data"])
	}
	return d
}

func keys(m map[string]any) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	return ks
}

// --- Single Query Tests ---

func TestQuery(t *testing.T) {
	server := newTestServer(t, setupRouter().Handler("/trpc"))

	tests := []struct {
		name       string
		path       string
		wantStatus int
		wantData   any // string for scalar, map key/val for object
	}{
		{
			name:       "void handler returns scalar",
			path:       "/trpc/hello",
			wantStatus: 200,
			wantData:   "Hello world!",
		},
		{
			name:       "handler with input",
			path:       "/trpc/user.getById?input=" + url.QueryEscape(`{"id":"123"}`),
			wantStatus: 200,
		},
		{
			name:       "no input triggers validation",
			path:       "/trpc/user.getById",
			wantStatus: 400,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := mustGet(t, server, tt.path)
			if resp.StatusCode != tt.wantStatus {
				_ = resp.Body.Close()
				t.Fatalf("status = %d, want %d", resp.StatusCode, tt.wantStatus)
			}

			body := decodeJSON(t, resp)

			if tt.wantStatus == 200 && tt.wantData != nil {
				got := resultScalar(t, body)
				if got != tt.wantData {
					t.Fatalf("result.data = %v, want %v", got, tt.wantData)
				}
			}
		})
	}
}

func TestQueryWithInput(t *testing.T) {
	server := newTestServer(t, setupRouter().Handler("/trpc"))

	input := url.QueryEscape(`{"id":"42"}`)
	resp := mustGet(t, server, "/trpc/user.getById?input="+input)
	if resp.StatusCode != 200 {
		_ = resp.Body.Close()
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	data := resultData(t, decodeJSON(t, resp))
	if data["id"] != "42" {
		t.Errorf("id = %v, want 42", data["id"])
	}
	if data["name"] != "Alice" {
		t.Errorf("name = %v, want Alice", data["name"])
	}
}

// --- Mutation Tests ---

func TestMutation(t *testing.T) {
	server := newTestServer(t, setupRouter().Handler("/trpc"))

	tests := []struct {
		name       string
		body       string
		wantStatus int
		wantName   string
	}{
		{"valid input", `{"name":"Bob"}`, 200, "Bob"},
		{"empty body yields validation error", "", 400, ""},
		{"malformed JSON yields parse error", `{bad json}`, 400, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := mustPost(t, server, "/trpc/user.create", tt.body)
			if resp.StatusCode != tt.wantStatus {
				_ = resp.Body.Close()
				t.Fatalf("status = %d, want %d", resp.StatusCode, tt.wantStatus)
			}

			body := decodeJSON(t, resp)

			if tt.wantStatus == 200 {
				data := resultData(t, body)
				if data["name"] != tt.wantName {
					t.Errorf("name = %v, want %v", data["name"], tt.wantName)
				}
			}
		})
	}
}

// --- Method Validation Tests ---

func TestMethodValidation(t *testing.T) {
	server := newTestServer(t, setupRouter().Handler("/trpc"))

	tests := []struct {
		name       string
		method     string
		path       string
		wantStatus int
	}{
		{"GET query ok", "GET", "/trpc/hello", 200},
		{"POST mutation ok", "POST", "/trpc/user.create", 200},
		{"POST to query rejected", "POST", "/trpc/hello", 405},
		{"GET to mutation rejected", "GET", "/trpc/user.create", 405},
		{"PUT rejected", "PUT", "/trpc/hello", 405},
		{"DELETE rejected", "DELETE", "/trpc/hello", 405},
		{"PATCH rejected", "PATCH", "/trpc/hello", 405},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var resp *http.Response
			switch tt.method {
			case "GET":
				resp = mustGet(t, server, tt.path)
			case "POST":
				body := ""
				if strings.Contains(tt.path, "user.create") {
					body = `{"name":"test"}`
				}
				resp = mustPost(t, server, tt.path, body)
			default:
				resp = mustRequest(t, server, tt.method, tt.path)
			}
			defer func() { _ = resp.Body.Close() }()

			if resp.StatusCode != tt.wantStatus {
				t.Fatalf("status = %d, want %d", resp.StatusCode, tt.wantStatus)
			}
		})
	}
}

func TestMethodOverride(t *testing.T) {
	router := trpcgo.NewRouter(trpcgo.WithMethodOverride(true))
	trpcgo.VoidQuery(router, "hello", func(ctx context.Context) (string, error) {
		return "hi", nil
	})
	server := newTestServer(t, router.Handler("/trpc"))

	// POST to a query should succeed with method override
	resp := mustPost(t, server, "/trpc/hello", "")
	if resp.StatusCode != 200 {
		_ = resp.Body.Close()
		t.Fatalf("status = %d, want 200 (method override enabled)", resp.StatusCode)
	}

	body := decodeJSON(t, resp)
	if got := resultScalar(t, body); got != "hi" {
		t.Fatalf("result.data = %v, want hi", got)
	}
}

// --- Error Response Tests ---

func TestErrorResponse(t *testing.T) {
	server := newTestServer(t, setupRouter().Handler("/trpc"))

	tests := []struct {
		name       string
		path       string
		wantStatus int
		wantCode   string
		wantPath   string
	}{
		{
			name:       "procedure not found",
			path:       "/trpc/nonexistent",
			wantStatus: 404,
			wantCode:   "NOT_FOUND",
			wantPath:   "nonexistent",
		},
		{
			name:       "user not found (application error)",
			path:       "/trpc/user.getById?input=" + url.QueryEscape(`{"id":"404"}`),
			wantStatus: 404,
			wantCode:   "NOT_FOUND",
			wantPath:   "user.getById",
		},
		{
			name:       "malformed input query param",
			path:       "/trpc/user.getById?input=not-json",
			wantStatus: 400,
			wantCode:   "PARSE_ERROR",
			wantPath:   "user.getById",
		},
		{
			name:       "empty path",
			path:       "/trpc/",
			wantStatus: 404,
			wantCode:   "NOT_FOUND",
			wantPath:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := mustGet(t, server, tt.path)
			if resp.StatusCode != tt.wantStatus {
				_ = resp.Body.Close()
				t.Fatalf("status = %d, want %d", resp.StatusCode, tt.wantStatus)
			}

			body := decodeJSON(t, resp)
			ed := errorData(t, body)

			if ed["code"] != tt.wantCode {
				t.Errorf("error.data.code = %v, want %v", ed["code"], tt.wantCode)
			}
			if tt.wantPath != "" && ed["path"] != tt.wantPath {
				t.Errorf("error.data.path = %v, want %v", ed["path"], tt.wantPath)
			}
		})
	}
}

func TestErrorEnvelopeShape(t *testing.T) {
	server := newTestServer(t, setupRouter().Handler("/trpc"))

	resp := mustGet(t, server, "/trpc/nonexistent")
	body := decodeJSON(t, resp)

	// Verify full envelope structure per tRPC spec:
	// { "error": { "code": <number>, "message": <string>, "data": { "code": <string>, "httpStatus": <number> } } }
	errObj, ok := body["error"].(map[string]any)
	if !ok {
		t.Fatal("missing error object in response")
	}

	if _, ok := errObj["code"].(float64); !ok {
		t.Errorf("error.code should be a number, got %T", errObj["code"])
	}
	if _, ok := errObj["message"].(string); !ok {
		t.Errorf("error.message should be a string, got %T", errObj["message"])
	}

	data, ok := errObj["data"].(map[string]any)
	if !ok {
		t.Fatal("error.data should be an object")
	}
	if _, ok := data["code"].(string); !ok {
		t.Errorf("error.data.code should be a string, got %T", data["code"])
	}
	if _, ok := data["httpStatus"].(float64); !ok {
		t.Errorf("error.data.httpStatus should be a number, got %T", data["httpStatus"])
	}
}

func TestNonTRPCErrorWrappedAs500(t *testing.T) {
	router := trpcgo.NewRouter()
	trpcgo.VoidQuery(router, "fail", func(ctx context.Context) (string, error) {
		return "", fmt.Errorf("unexpected database error")
	})
	server := newTestServer(t, router.Handler("/trpc"))

	resp := mustGet(t, server, "/trpc/fail")
	if resp.StatusCode != 500 {
		_ = resp.Body.Close()
		t.Fatalf("status = %d, want 500 for non-trpc error", resp.StatusCode)
	}

	ed := errorData(t, decodeJSON(t, resp))
	if ed["code"] != "INTERNAL_SERVER_ERROR" {
		t.Errorf("error.data.code = %v, want INTERNAL_SERVER_ERROR", ed["code"])
	}
}

// --- Batch Tests ---

func TestBatchGET(t *testing.T) {
	server := newTestServer(t, setupRouter().Handler("/trpc"))

	input := url.QueryEscape(`{"0":{"id":"1"},"1":{"id":"2"}}`)
	resp := mustGet(t, server, "/trpc/user.getById,user.getById?batch=1&input="+input)
	if resp.StatusCode != 200 {
		_ = resp.Body.Close()
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	results := decodeJSONArray(t, resp)
	if len(results) != 2 {
		t.Fatalf("got %d results, want 2", len(results))
	}

	for i, item := range results {
		data := resultData(t, item)
		want := fmt.Sprintf("%d", i+1)
		if data["id"] != want {
			t.Errorf("results[%d].id = %v, want %v", i, data["id"], want)
		}
	}
}

func TestBatchPOST(t *testing.T) {
	server := newTestServer(t, setupRouter().Handler("/trpc"))

	resp := mustPost(t, server, "/trpc/user.create,user.create?batch=1", `{"0":{"name":"Alice"},"1":{"name":"Bob"}}`)
	if resp.StatusCode != 200 {
		_ = resp.Body.Close()
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	results := decodeJSONArray(t, resp)
	if len(results) != 2 {
		t.Fatalf("got %d results, want 2", len(results))
	}

	wantNames := []string{"Alice", "Bob"}
	for i, item := range results {
		data := resultData(t, item)
		if data["name"] != wantNames[i] {
			t.Errorf("results[%d].name = %v, want %v", i, data["name"], wantNames[i])
		}
	}
}

func TestBatchMixedStatus(t *testing.T) {
	server := newTestServer(t, setupRouter().Handler("/trpc"))

	// First succeeds (id=1), second fails (id=404)
	input := url.QueryEscape(`{"0":{"id":"1"},"1":{"id":"404"}}`)
	resp := mustGet(t, server, "/trpc/user.getById,user.getById?batch=1&input="+input)
	if resp.StatusCode != 207 {
		_ = resp.Body.Close()
		t.Fatalf("status = %d, want 207 (Multi-Status)", resp.StatusCode)
	}

	results := decodeJSONArray(t, resp)
	if len(results) != 2 {
		t.Fatalf("got %d results, want 2", len(results))
	}

	if _, ok := results[0]["result"]; !ok {
		t.Error("results[0] should be a success")
	}
	if _, ok := results[1]["error"]; !ok {
		t.Error("results[1] should be an error")
	}
}

func TestBatchAllSameError(t *testing.T) {
	server := newTestServer(t, setupRouter().Handler("/trpc"))

	input := url.QueryEscape(`{"0":{"id":"404"},"1":{"id":"404"}}`)
	resp := mustGet(t, server, "/trpc/user.getById,user.getById?batch=1&input="+input)
	if resp.StatusCode != 404 {
		_ = resp.Body.Close()
		t.Fatalf("status = %d, want 404 (all same error → unified status, not 207)", resp.StatusCode)
	}
}

func TestBatchDifferentProcedures(t *testing.T) {
	server := newTestServer(t, setupRouter().Handler("/trpc"))

	input := url.QueryEscape(`{"0":{},"1":{"id":"5"}}`)
	resp := mustGet(t, server, "/trpc/hello,user.getById?batch=1&input="+input)
	if resp.StatusCode != 200 {
		_ = resp.Body.Close()
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	results := decodeJSONArray(t, resp)
	if len(results) != 2 {
		t.Fatalf("got %d results, want 2", len(results))
	}

	if got := resultScalar(t, results[0]); got != "Hello world!" {
		t.Errorf("results[0].data = %v, want Hello world!", got)
	}
	data := resultData(t, results[1])
	if data["id"] != "5" {
		t.Errorf("results[1].data.id = %v, want 5", data["id"])
	}
}

func TestBatchWithNotFoundProcedure(t *testing.T) {
	server := newTestServer(t, setupRouter().Handler("/trpc"))

	input := url.QueryEscape(`{"0":{},"1":{}}`)
	resp := mustGet(t, server, "/trpc/hello,nonexistent?batch=1&input="+input)
	if resp.StatusCode != 207 {
		_ = resp.Body.Close()
		t.Fatalf("status = %d, want 207", resp.StatusCode)
	}

	results := decodeJSONArray(t, resp)
	if _, ok := results[0]["result"]; !ok {
		t.Error("results[0] should succeed")
	}

	ed := errorData(t, results[1])
	if ed["code"] != "NOT_FOUND" {
		t.Errorf("results[1] error code = %v, want NOT_FOUND", ed["code"])
	}
}

func TestBatchDisabled(t *testing.T) {
	router := trpcgo.NewRouter(trpcgo.WithBatching(false))
	trpcgo.VoidQuery(router, "hello", func(ctx context.Context) (string, error) {
		return "hi", nil
	})
	server := newTestServer(t, router.Handler("/trpc"))

	resp := mustGet(t, server, "/trpc/hello,hello?batch=1")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != 400 {
		t.Fatalf("status = %d, want 400 (batching disabled)", resp.StatusCode)
	}
}

// --- Middleware Tests ---

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

	server := newTestServer(t, router.Handler("/trpc"))

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

	server := newTestServer(t, router.Handler("/trpc"))

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

	server := newTestServer(t, router.Handler("/trpc"))

	resp := mustGet(t, server, "/trpc/whoami")
	if resp.StatusCode != 200 {
		_ = resp.Body.Close()
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if got := resultScalar(t, decodeJSON(t, resp)); got != "admin" {
		t.Fatalf("result.data = %v, want admin", got)
	}
}

// --- Callback Tests ---

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

	server := newTestServer(t, router.Handler("/trpc"))

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

	server := newTestServer(t, router.Handler("/trpc"))

	resp := mustGet(t, server, "/trpc/ok")
	_ = resp.Body.Close()

	if called {
		t.Error("onError should not be called when procedure succeeds")
	}
}

// --- Context Creator Tests ---

func TestCustomContextCreator(t *testing.T) {
	type ctxKey string

	router := trpcgo.NewRouter(trpcgo.WithContextCreator(func(r *http.Request) context.Context {
		return context.WithValue(r.Context(), ctxKey("reqID"), "req-42")
	}))
	trpcgo.VoidQuery(router, "reqid", func(ctx context.Context) (string, error) {
		return ctx.Value(ctxKey("reqID")).(string), nil
	})

	server := newTestServer(t, router.Handler("/trpc"))

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

	router := trpcgo.NewRouter(trpcgo.WithContextCreator(func(r *http.Request) context.Context {
		// Deliberately not derived from r.Context().
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

	server := newTestServer(t, router.Handler("/trpc"))

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

// --- Edge Cases ---

func TestNoBasePath(t *testing.T) {
	router := trpcgo.NewRouter()
	trpcgo.VoidQuery(router, "hello", func(ctx context.Context) (string, error) {
		return "hi", nil
	})
	server := newTestServer(t, router.Handler(""))

	resp := mustGet(t, server, "/hello")
	if resp.StatusCode != 200 {
		_ = resp.Body.Close()
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if got := resultScalar(t, decodeJSON(t, resp)); got != "hi" {
		t.Fatalf("result.data = %v, want hi", got)
	}
}

func TestDeepNestedProcedure(t *testing.T) {
	router := trpcgo.NewRouter()
	trpcgo.VoidQuery(router, "a.b.c.d.deep", func(ctx context.Context) (string, error) {
		return "found", nil
	})
	server := newTestServer(t, router.Handler("/trpc"))

	resp := mustGet(t, server, "/trpc/a.b.c.d.deep")
	if resp.StatusCode != 200 {
		_ = resp.Body.Close()
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if got := resultScalar(t, decodeJSON(t, resp)); got != "found" {
		t.Fatalf("result.data = %v, want found", got)
	}
}

func TestContentType(t *testing.T) {
	server := newTestServer(t, setupRouter().Handler("/trpc"))

	tests := []struct {
		name string
		path string
	}{
		{"success response", "/trpc/hello"},
		{"error response", "/trpc/nonexistent"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := mustGet(t, server, tt.path)
			defer func() { _ = resp.Body.Close() }()
			ct := resp.Header.Get("Content-Type")
			if ct != "application/json" {
				t.Fatalf("Content-Type = %q, want application/json", ct)
			}
		})
	}
}

// --- Concurrent Access ---

func TestConcurrentRequests(t *testing.T) {
	server := newTestServer(t, setupRouter().Handler("/trpc"))

	var wg sync.WaitGroup
	var failures atomic.Int32
	n := 50

	for i := range n {
		wg.Go(func() {
			input := url.QueryEscape(fmt.Sprintf(`{"id":"%d"}`, i))
			resp, err := http.Get(server.URL + "/trpc/user.getById?input=" + input)
			if err != nil {
				failures.Add(1)
				return
			}
			defer func() { _ = resp.Body.Close() }()

			if resp.StatusCode != 200 {
				failures.Add(1)
				return
			}

			var body map[string]any
			if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
				failures.Add(1)
				return
			}

			r, ok := body["result"].(map[string]any)
			if !ok {
				failures.Add(1)
				return
			}
			data, ok := r["data"].(map[string]any)
			if !ok {
				failures.Add(1)
				return
			}
			if data["id"] != fmt.Sprintf("%d", i) {
				failures.Add(1)
			}
		})
	}

	wg.Wait()
	if f := failures.Load(); f > 0 {
		t.Fatalf("%d of %d concurrent requests returned wrong results", f, n)
	}
}

// --- Error Code Mapping ---

func TestErrorCodeMapping(t *testing.T) {
	tests := []struct {
		code       trpcgo.ErrorCode
		wantStatus int
		wantName   string
	}{
		{trpcgo.CodeParseError, 400, "PARSE_ERROR"},
		{trpcgo.CodeBadRequest, 400, "BAD_REQUEST"},
		{trpcgo.CodeUnauthorized, 401, "UNAUTHORIZED"},
		{trpcgo.CodeForbidden, 403, "FORBIDDEN"},
		{trpcgo.CodeNotFound, 404, "NOT_FOUND"},
		{trpcgo.CodeMethodNotSupported, 405, "METHOD_NOT_SUPPORTED"},
		{trpcgo.CodeTimeout, 408, "TIMEOUT"},
		{trpcgo.CodeConflict, 409, "CONFLICT"},
		{trpcgo.CodePayloadTooLarge, 413, "PAYLOAD_TOO_LARGE"},
		{trpcgo.CodeTooManyRequests, 429, "TOO_MANY_REQUESTS"},
		{trpcgo.CodeInternalServerError, 500, "INTERNAL_SERVER_ERROR"},
		{trpcgo.CodeServiceUnavailable, 503, "SERVICE_UNAVAILABLE"},
	}

	for _, tt := range tests {
		t.Run(tt.wantName, func(t *testing.T) {
			if got := trpcgo.HTTPStatusFromCode(tt.code); got != tt.wantStatus {
				t.Errorf("HTTPStatusFromCode = %d, want %d", got, tt.wantStatus)
			}
			if got := trpcgo.NameFromCode(tt.code); got != tt.wantName {
				t.Errorf("NameFromCode = %q, want %q", got, tt.wantName)
			}
		})
	}
}

func TestUnknownErrorCodeDefaults(t *testing.T) {
	unknown := trpcgo.ErrorCode(-99999)
	if got := trpcgo.HTTPStatusFromCode(unknown); got != 500 {
		t.Errorf("HTTPStatusFromCode(unknown) = %d, want 500", got)
	}
	if got := trpcgo.NameFromCode(unknown); got != "INTERNAL_SERVER_ERROR" {
		t.Errorf("NameFromCode(unknown) = %q, want INTERNAL_SERVER_ERROR", got)
	}
}

// --- Error Type ---

func TestErrorType(t *testing.T) {
	t.Run("simple error", func(t *testing.T) {
		err := trpcgo.NewError(trpcgo.CodeNotFound, "not here")
		if err.Error() == "" {
			t.Fatal("Error() should not be empty")
		}
		if err.Unwrap() != nil {
			t.Error("Unwrap should be nil for error without cause")
		}
	})

	t.Run("wrapped error", func(t *testing.T) {
		cause := fmt.Errorf("db connection refused")
		err := trpcgo.WrapError(trpcgo.CodeInternalServerError, "query failed", cause)
		if err.Unwrap() != cause {
			t.Error("Unwrap should return the cause")
		}
		if !strings.Contains(err.Error(), "db connection refused") {
			t.Errorf("Error() = %q, should contain cause message", err.Error())
		}
	})

	t.Run("formatted error", func(t *testing.T) {
		err := trpcgo.NewErrorf(trpcgo.CodeBadRequest, "field %q is invalid", "email")
		if !strings.Contains(err.Error(), `"email"`) {
			t.Errorf("Error() = %q, should contain formatted field name", err.Error())
		}
	})
}

// --- SSE Subscription Tests ---

// parseSSEEvents reads SSE events from a response body.
// Returns a slice of parsed events. Data messages without an explicit
// event type use "message" (the SSE default).
func parseSSEEvents(t *testing.T, resp *http.Response, maxEvents int) []sseEvent {
	t.Helper()
	var events []sseEvent
	scanner := bufio.NewScanner(resp.Body)

	var currentEvent, currentData, currentID string
	hasData := false
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "event: "):
			currentEvent = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: "):
			currentData = strings.TrimPrefix(line, "data: ")
			hasData = true
		case line == "data: ":
			currentData = ""
			hasData = true
		case strings.HasPrefix(line, "id: "):
			currentID = strings.TrimPrefix(line, "id: ")
		case line == "":
			if hasData || currentEvent != "" {
				evt := currentEvent
				if evt == "" {
					evt = "message" // SSE default event type
				}
				events = append(events, sseEvent{event: evt, data: currentData, id: currentID})
				currentEvent = ""
				currentData = ""
				currentID = ""
				hasData = false
				if len(events) >= maxEvents {
					return events
				}
			}
		}
	}
	return events
}

type sseEvent struct {
	event string
	data  string
	id    string
}

func TestSubscriptionSSE(t *testing.T) {
	router := trpcgo.NewRouter(trpcgo.WithSSEPingInterval(100 * time.Millisecond))

	type SubInput struct {
		Count int `json:"count"`
	}

	trpcgo.Subscribe(router, "counter", func(ctx context.Context, input SubInput) (<-chan int, error) {
		ch := make(chan int)
		go func() {
			defer close(ch)
			for i := 1; i <= input.Count; i++ {
				select {
				case <-ctx.Done():
					return
				case ch <- i:
				}
			}
		}()
		return ch, nil
	})

	server := newTestServer(t, router.Handler("/trpc"))

	input := url.QueryEscape(`{"count":3}`)
	resp := mustGet(t, server, "/trpc/counter?input="+input)
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("Content-Type = %q, want text/event-stream", ct)
	}

	// Read: connected + 3 messages + return = 5 events
	events := parseSSEEvents(t, resp, 5)

	if len(events) < 5 {
		t.Fatalf("got %d events, want at least 5", len(events))
	}
	if events[0].event != "connected" {
		t.Errorf("events[0] = %q, want connected", events[0].event)
	}
	for i := 1; i <= 3; i++ {
		if events[i].event != "message" {
			t.Errorf("events[%d].event = %q, want message (SSE default)", i, events[i].event)
		}
		if events[i].data != fmt.Sprintf("%d", i) {
			t.Errorf("events[%d].data = %q, want %d", i, events[i].data, i)
		}
		if events[i].id != "" {
			t.Errorf("events[%d].id = %q, want empty (untracked)", i, events[i].id)
		}
	}
	if events[4].event != "return" {
		t.Errorf("events[4] = %q, want return", events[4].event)
	}
}

func TestSubscriptionVoidStream(t *testing.T) {
	router := trpcgo.NewRouter()

	trpcgo.VoidSubscribe(router, "ticks", func(ctx context.Context) (<-chan string, error) {
		ch := make(chan string)
		go func() {
			defer close(ch)
			ch <- "tick"
			ch <- "tock"
		}()
		return ch, nil
	})

	server := newTestServer(t, router.Handler("/trpc"))

	resp := mustGet(t, server, "/trpc/ticks")
	defer func() { _ = resp.Body.Close() }()

	events := parseSSEEvents(t, resp, 4) // connected + 2 messages + return
	if len(events) < 4 {
		t.Fatalf("got %d events, want 4", len(events))
	}
	if events[1].data != `"tick"` {
		t.Errorf("events[1].data = %q, want tick", events[1].data)
	}
	if events[2].data != `"tock"` {
		t.Errorf("events[2].data = %q, want tock", events[2].data)
	}
}

func TestSubscriptionError(t *testing.T) {
	router := trpcgo.NewRouter()

	trpcgo.Subscribe(router, "fail", func(ctx context.Context, input struct{}) (<-chan string, error) {
		return nil, trpcgo.NewError(trpcgo.CodeUnauthorized, "not allowed")
	})

	server := newTestServer(t, router.Handler("/trpc"))

	resp := mustGet(t, server, "/trpc/fail")
	defer func() { _ = resp.Body.Close() }()

	// Should get a regular JSON error, not SSE
	if resp.StatusCode != 401 {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
}

func TestSubscriptionCannotBeBatched(t *testing.T) {
	router := trpcgo.NewRouter(trpcgo.WithBatching(true))

	trpcgo.VoidSubscribe(router, "stream", func(ctx context.Context) (<-chan string, error) {
		ch := make(chan string)
		close(ch)
		return ch, nil
	})
	trpcgo.VoidQuery(router, "hello", func(ctx context.Context) (string, error) {
		return "hi", nil
	})

	server := newTestServer(t, router.Handler("/trpc"))

	resp := mustGet(t, server, "/trpc/stream,hello?batch=1&input="+url.QueryEscape(`{"0":{},"1":{}}`))
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != 400 {
		t.Fatalf("status = %d, want 400 (subscriptions cannot be batched)", resp.StatusCode)
	}
}

func TestSubscriptionPing(t *testing.T) {
	router := trpcgo.NewRouter(trpcgo.WithSSEPingInterval(50 * time.Millisecond))

	trpcgo.VoidSubscribe(router, "slow", func(ctx context.Context) (<-chan string, error) {
		ch := make(chan string)
		go func() {
			defer close(ch)
			time.Sleep(200 * time.Millisecond)
			ch <- "done"
		}()
		return ch, nil
	})

	server := newTestServer(t, router.Handler("/trpc"))

	resp := mustGet(t, server, "/trpc/slow")
	defer func() { _ = resp.Body.Close() }()

	// Should see: connected, ping(s), message, return
	events := parseSSEEvents(t, resp, 10)

	hasPing := false
	for _, e := range events {
		if e.event == "ping" {
			hasPing = true
			break
		}
	}
	if !hasPing {
		t.Errorf("expected at least one ping event, got events: %v", events)
	}
}

func TestSubscriptionMaxDuration(t *testing.T) {
	router := trpcgo.NewRouter(
		trpcgo.WithSSEMaxDuration(500*time.Millisecond),
		trpcgo.WithSSEPingInterval(100*time.Millisecond),
	)

	// Stream that never closes on its own — only maxDuration can stop it.
	trpcgo.VoidSubscribe(router, "forever", func(ctx context.Context) (<-chan string, error) {
		ch := make(chan string)
		go func() {
			defer close(ch)
			ticker := time.NewTicker(100 * time.Millisecond)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					select {
					case ch <- "tick":
					case <-ctx.Done():
						return
					}
				}
			}
		}()
		return ch, nil
	})

	server := newTestServer(t, router.Handler("/trpc"))

	start := time.Now()
	resp := mustGet(t, server, "/trpc/forever")
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	events := parseSSEEvents(t, resp, 50)
	elapsed := time.Since(start)

	// Must have received the connected event.
	if len(events) == 0 || events[0].event != "connected" {
		t.Fatalf("first event should be connected, got %v", events)
	}

	// Last event must be "return" — that's how maxDuration ends the stream.
	lastEvent := events[len(events)-1]
	if lastEvent.event != "return" {
		t.Errorf("last event = %q, want return (maxDuration should end stream)", lastEvent.event)
	}

	// Count message events — should have several before maxDuration fires.
	messageCount := 0
	for _, e := range events {
		if e.event == "message" {
			messageCount++
			if e.data != `"tick"` {
				t.Errorf("message data = %q, want \"tick\"", e.data)
			}
		}
	}
	if messageCount == 0 {
		t.Error("expected at least one message event before maxDuration")
	}

	// Sanity: elapsed time should be roughly around maxDuration (500ms ± 300ms).
	if elapsed < 300*time.Millisecond {
		t.Errorf("stream ended too fast (%v), maxDuration should be ~500ms", elapsed)
	}
	if elapsed > 3*time.Second {
		t.Errorf("stream took too long (%v), maxDuration should cap at ~500ms", elapsed)
	}
}

func TestSubscriptionTrackedEvents(t *testing.T) {
	router := trpcgo.NewRouter()

	type Item struct {
		Name string `json:"name"`
	}

	trpcgo.VoidSubscribe(router, "items", func(ctx context.Context) (<-chan trpcgo.TrackedEvent[Item], error) {
		ch := make(chan trpcgo.TrackedEvent[Item])
		go func() {
			defer close(ch)
			ch <- trpcgo.Tracked("evt-1", Item{Name: "first"})
			ch <- trpcgo.Tracked("evt-2", Item{Name: "second"})
		}()
		return ch, nil
	})

	server := newTestServer(t, router.Handler("/trpc"))

	resp := mustGet(t, server, "/trpc/items")
	defer func() { _ = resp.Body.Close() }()

	// connected + 2 tracked messages + return = 4 events
	events := parseSSEEvents(t, resp, 4)
	if len(events) < 4 {
		t.Fatalf("got %d events, want at least 4, got: %v", len(events), events)
	}

	// First event: connected.
	if events[0].event != "connected" {
		t.Errorf("events[0].event = %q, want connected", events[0].event)
	}

	// Data messages: default "message" event type, with id and data.
	for i, want := range []struct {
		id   string
		data string
	}{
		{"evt-1", `{"name":"first"}`},
		{"evt-2", `{"name":"second"}`},
	} {
		idx := i + 1
		e := events[idx]
		if e.event != "message" {
			t.Errorf("events[%d].event = %q, want message", idx, e.event)
		}
		if e.id != want.id {
			t.Errorf("events[%d].id = %q, want %q", idx, e.id, want.id)
		}
		if e.data != want.data {
			t.Errorf("events[%d].data = %q, want %q", idx, e.data, want.data)
		}
	}

	// Last event: return.
	if events[3].event != "return" {
		t.Errorf("events[3].event = %q, want return", events[3].event)
	}
}

func TestSubscriptionUntrackedEventsHaveNoID(t *testing.T) {
	router := trpcgo.NewRouter()

	trpcgo.VoidSubscribe(router, "plain", func(ctx context.Context) (<-chan string, error) {
		ch := make(chan string)
		go func() {
			defer close(ch)
			ch <- "hello"
			ch <- "world"
		}()
		return ch, nil
	})

	server := newTestServer(t, router.Handler("/trpc"))

	resp := mustGet(t, server, "/trpc/plain")
	defer func() { _ = resp.Body.Close() }()

	events := parseSSEEvents(t, resp, 4)
	if len(events) < 4 {
		t.Fatalf("got %d events, want 4", len(events))
	}

	for i := 1; i <= 2; i++ {
		if events[i].id != "" {
			t.Errorf("events[%d].id = %q, want empty (untracked events must not have id)", i, events[i].id)
		}
	}
}

func TestSubscriptionWireFormat(t *testing.T) {
	// Verify the exact SSE wire format matches tRPC:
	// - Control events (connected, ping, return) have "event:" field
	// - Data messages have NO "event:" field (use SSE default)
	// - Data messages have "data:" field with JSON content
	router := trpcgo.NewRouter()

	trpcgo.VoidSubscribe(router, "single", func(ctx context.Context) (<-chan int, error) {
		ch := make(chan int)
		go func() {
			defer close(ch)
			ch <- 42
		}()
		return ch, nil
	})

	server := newTestServer(t, router.Handler("/trpc"))

	resp := mustGet(t, server, "/trpc/single")
	defer func() { _ = resp.Body.Close() }()

	// Read all raw lines.
	var lines []string
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}

	// Must not contain "event: message" anywhere.
	for i, line := range lines {
		if line == "event: message" {
			t.Fatalf("line %d: data messages must not include 'event: message'", i)
		}
	}

	// Must contain "data: 42" (the actual data).
	hasData := slices.Contains(lines, "data: 42")
	if !hasData {
		t.Errorf("missing 'data: 42' line in output:\n%s", strings.Join(lines, "\n"))
	}

	// Must contain control events with event: prefix.
	hasConnected := false
	hasReturn := false
	for _, line := range lines {
		if line == "event: connected" {
			hasConnected = true
		}
		if line == "event: return" {
			hasReturn = true
		}
	}
	if !hasConnected {
		t.Error("missing 'event: connected' line")
	}
	if !hasReturn {
		t.Error("missing 'event: return' line")
	}
}

func TestSubscriptionConnectedEventData(t *testing.T) {
	t.Run("with reconnect timeout", func(t *testing.T) {
		router := trpcgo.NewRouter(
			trpcgo.WithSSEReconnectAfterInactivity(3 * time.Second),
		)

		trpcgo.VoidSubscribe(router, "stream", func(ctx context.Context) (<-chan string, error) {
			ch := make(chan string)
			close(ch)
			return ch, nil
		})

		server := newTestServer(t, router.Handler("/trpc"))
		resp := mustGet(t, server, "/trpc/stream")
		defer func() { _ = resp.Body.Close() }()

		events := parseSSEEvents(t, resp, 2)
		if len(events) == 0 || events[0].event != "connected" {
			t.Fatalf("first event should be connected, got %v", events)
		}

		var connData map[string]any
		if err := json.Unmarshal([]byte(events[0].data), &connData); err != nil {
			t.Fatalf("failed to parse connected data %q: %v", events[0].data, err)
		}
		if v, ok := connData["reconnectAfterInactivityMs"]; !ok {
			t.Error("connected data missing reconnectAfterInactivityMs")
		} else if v != float64(3000) {
			t.Errorf("reconnectAfterInactivityMs = %v, want 3000", v)
		}
	})

	t.Run("without reconnect timeout", func(t *testing.T) {
		router := trpcgo.NewRouter()

		trpcgo.VoidSubscribe(router, "stream", func(ctx context.Context) (<-chan string, error) {
			ch := make(chan string)
			close(ch)
			return ch, nil
		})

		server := newTestServer(t, router.Handler("/trpc"))
		resp := mustGet(t, server, "/trpc/stream")
		defer func() { _ = resp.Body.Close() }()

		events := parseSSEEvents(t, resp, 2)
		if len(events) == 0 || events[0].event != "connected" {
			t.Fatalf("first event should be connected, got %v", events)
		}

		var connData map[string]any
		if err := json.Unmarshal([]byte(events[0].data), &connData); err != nil {
			t.Fatalf("failed to parse connected data %q: %v", events[0].data, err)
		}
		if _, ok := connData["reconnectAfterInactivityMs"]; ok {
			t.Error("connected data should omit reconnectAfterInactivityMs when 0")
		}
	})
}

func TestSubscriptionTrackedSerializationError(t *testing.T) {
	// TrackedEvent where Data fails to marshal should emit serialized-error.
	router := trpcgo.NewRouter()

	type BadData struct {
		Fn func() `json:"fn"` // functions can't be marshaled
	}

	trpcgo.VoidSubscribe(router, "bad", func(ctx context.Context) (<-chan trpcgo.TrackedEvent[BadData], error) {
		ch := make(chan trpcgo.TrackedEvent[BadData])
		go func() {
			defer close(ch)
			ch <- trpcgo.Tracked("id-1", BadData{Fn: func() {}})
		}()
		return ch, nil
	})

	server := newTestServer(t, router.Handler("/trpc"))

	resp := mustGet(t, server, "/trpc/bad")
	defer func() { _ = resp.Body.Close() }()

	events := parseSSEEvents(t, resp, 3)

	hasSerialized := false
	for _, e := range events {
		if e.event == "serialized-error" {
			hasSerialized = true
			if !strings.Contains(e.data, "failed to serialize") {
				t.Errorf("serialized-error data = %q, want 'failed to serialize'", e.data)
			}
			break
		}
	}
	if !hasSerialized {
		t.Errorf("expected serialized-error event, got: %v", events)
	}
}

// --- Chain() Middleware Tests ---

func TestChain(t *testing.T) {
	type ctxKey string

	makeMW := func(name string) trpcgo.Middleware {
		return func(next trpcgo.HandlerFunc) trpcgo.HandlerFunc {
			return func(ctx context.Context, input any) (any, error) {
				// Append to execution trace in context.
				prev, _ := ctx.Value(ctxKey("trace")).([]string)
				ctx = context.WithValue(ctx, ctxKey("trace"), append(prev, name))
				return next(ctx, input)
			}
		}
	}

	t.Run("left to right ordering", func(t *testing.T) {
		combined := trpcgo.Chain(makeMW("a"), makeMW("b"), makeMW("c"))

		handler := combined(func(ctx context.Context, input any) (any, error) {
			trace, _ := ctx.Value(ctxKey("trace")).([]string)
			return trace, nil
		})

		result, err := handler(context.Background(), nil)
		if err != nil {
			t.Fatal(err)
		}
		got := result.([]string)
		want := []string{"a", "b", "c"}
		if len(got) != len(want) {
			t.Fatalf("trace = %v, want %v", got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("trace[%d] = %q, want %q (full: %v)", i, got[i], want[i], got)
			}
		}
	})

	t.Run("single middleware", func(t *testing.T) {
		combined := trpcgo.Chain(makeMW("only"))

		handler := combined(func(ctx context.Context, input any) (any, error) {
			trace, _ := ctx.Value(ctxKey("trace")).([]string)
			return trace, nil
		})

		result, err := handler(context.Background(), nil)
		if err != nil {
			t.Fatal(err)
		}
		got := result.([]string)
		if len(got) != 1 || got[0] != "only" {
			t.Fatalf("trace = %v, want [only]", got)
		}
	})

	t.Run("empty chain is passthrough", func(t *testing.T) {
		combined := trpcgo.Chain()

		handler := combined(func(ctx context.Context, input any) (any, error) {
			return "reached", nil
		})

		result, err := handler(context.Background(), nil)
		if err != nil {
			t.Fatal(err)
		}
		if result != "reached" {
			t.Fatalf("result = %v, want reached", result)
		}
	})

	t.Run("short circuit stops chain", func(t *testing.T) {
		blocker := func(next trpcgo.HandlerFunc) trpcgo.HandlerFunc {
			return func(ctx context.Context, input any) (any, error) {
				return nil, trpcgo.NewError(trpcgo.CodeUnauthorized, "blocked")
			}
		}
		afterBlocker := makeMW("should-not-run")

		combined := trpcgo.Chain(blocker, afterBlocker)
		handler := combined(func(ctx context.Context, input any) (any, error) {
			t.Error("handler should not be reached after short circuit")
			return nil, nil
		})

		_, err := handler(context.Background(), nil)
		if err == nil {
			t.Fatal("expected error from short circuit")
		}
	})

	t.Run("context propagates through chain", func(t *testing.T) {
		setUser := func(next trpcgo.HandlerFunc) trpcgo.HandlerFunc {
			return func(ctx context.Context, input any) (any, error) {
				return next(context.WithValue(ctx, ctxKey("user"), "admin"), input)
			}
		}
		setRole := func(next trpcgo.HandlerFunc) trpcgo.HandlerFunc {
			return func(ctx context.Context, input any) (any, error) {
				user := ctx.Value(ctxKey("user")).(string)
				return next(context.WithValue(ctx, ctxKey("role"), user+"-superuser"), input)
			}
		}

		combined := trpcgo.Chain(setUser, setRole)
		handler := combined(func(ctx context.Context, input any) (any, error) {
			return ctx.Value(ctxKey("role")), nil
		})

		result, err := handler(context.Background(), nil)
		if err != nil {
			t.Fatal(err)
		}
		if result != "admin-superuser" {
			t.Fatalf("role = %v, want admin-superuser", result)
		}
	})
}

// --- GenerateTS Tests ---

func TestGenerateTS(t *testing.T) {
	type Base struct {
		ID        string     `json:"id"`
		CreatedAt time.Time  `json:"createdAt"`
		DeletedAt *time.Time `json:"deletedAt,omitempty"`
	}
	type UserInput struct {
		Name  string   `json:"name"`
		Email string   `json:"email"`
		Tags  []string `json:"tags,omitempty"`
	}
	type UserOutput struct {
		Base
		Name  string            `json:"name"`
		Email string            `json:"email"`
		Meta  map[string]string `json:"meta,omitempty"`
	}

	router := trpcgo.NewRouter()
	trpcgo.Query(router, "user.getById", func(ctx context.Context, input struct {
		ID string `json:"id"`
	}) (UserOutput, error) {
		return UserOutput{}, nil
	})
	trpcgo.VoidQuery(router, "user.list", func(ctx context.Context) ([]UserOutput, error) {
		return nil, nil
	})
	trpcgo.Mutation(router, "user.create", func(ctx context.Context, input UserInput) (UserOutput, error) {
		return UserOutput{}, nil
	})
	trpcgo.VoidSubscribe(router, "user.events", func(ctx context.Context) (<-chan UserOutput, error) {
		return nil, nil
	})

	outputPath := filepath.Join(t.TempDir(), "trpc.ts")
	if err := router.GenerateTS(outputPath); err != nil {
		t.Fatalf("GenerateTS failed: %v", err)
	}

	data, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("reading output: %v", err)
	}
	output := string(data)

	t.Run("header", func(t *testing.T) {
		if !strings.HasPrefix(output, "// Code generated by trpcgo. DO NOT EDIT.") {
			t.Errorf("missing or wrong header")
		}
	})

	t.Run("UserOutput interface fields", func(t *testing.T) {
		// Must contain the interface with correct field types.
		mustContain := []string{
			"export interface UserOutput {",
			"name: string;",
			"email: string;",
		}
		for _, s := range mustContain {
			if !strings.Contains(output, s) {
				t.Errorf("missing %q in output:\n%s", s, output)
			}
		}
	})

	t.Run("embedded struct fields flattened", func(t *testing.T) {
		// Base fields should appear in UserOutput, not as a separate Base type.
		if !strings.Contains(output, "id: string;") {
			t.Errorf("embedded Base.ID field missing from UserOutput:\n%s", output)
		}
		if !strings.Contains(output, "createdAt: string;") {
			t.Errorf("time.Time should map to string:\n%s", output)
		}
	})

	t.Run("optional fields", func(t *testing.T) {
		// Pointer field and omitempty field should be optional.
		if !strings.Contains(output, "deletedAt?:") {
			t.Errorf("pointer field should be optional:\n%s", output)
		}
		if !strings.Contains(output, "meta?:") {
			t.Errorf("omitempty field should be optional:\n%s", output)
		}
		if !strings.Contains(output, "tags?:") {
			t.Errorf("omitempty tags should be optional:\n%s", output)
		}
	})

	t.Run("UserInput interface", func(t *testing.T) {
		if !strings.Contains(output, "export interface UserInput {") {
			t.Errorf("missing UserInput interface:\n%s", output)
		}
	})

	t.Run("type mappings", func(t *testing.T) {
		// []string → string[]
		if !strings.Contains(output, "string[]") {
			t.Errorf("[]string should map to string[]:\n%s", output)
		}
		// map[string]string → Record<string, string>
		if !strings.Contains(output, "Record<string, string>") {
			t.Errorf("map[string]string should map to Record<string, string>:\n%s", output)
		}
	})

	t.Run("procedure types", func(t *testing.T) {
		if !strings.Contains(output, "$Mutation<UserInput, UserOutput>") {
			t.Errorf("missing mutation type:\n%s", output)
		}
		if !strings.Contains(output, "$Subscription<void, UserOutput>") {
			t.Errorf("missing subscription type:\n%s", output)
		}
	})

	t.Run("nested namespace", func(t *testing.T) {
		if !strings.Contains(output, "user: {") {
			t.Errorf("missing user namespace:\n%s", output)
		}
	})

	t.Run("list returns array type", func(t *testing.T) {
		if !strings.Contains(output, "$Query<void, UserOutput[]>") {
			t.Errorf("list query should return UserOutput[]:\n%s", output)
		}
	})

	t.Run("AppRouter structure", func(t *testing.T) {
		for _, s := range []string{
			"export type AppRouter =",
			"type AppRouterRecord =",
			"procedures: AppRouterRecord",
			"record: AppRouterRecord",
		} {
			if !strings.Contains(output, s) {
				t.Errorf("missing %q:\n%s", s, output)
			}
		}
	})
}

func TestGenerateTSNoProcedures(t *testing.T) {
	router := trpcgo.NewRouter()

	outputPath := filepath.Join(t.TempDir(), "trpc.ts")
	if err := router.GenerateTS(outputPath); err != nil {
		t.Fatalf("GenerateTS failed: %v", err)
	}

	data, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("reading output: %v", err)
	}
	output := string(data)

	// Should still produce valid TypeScript with empty router record.
	if !strings.Contains(output, "export type AppRouter =") {
		t.Errorf("empty router should still produce AppRouter type:\n%s", output)
	}
	// Should not contain any interfaces.
	if strings.Contains(output, "export interface") {
		t.Errorf("empty router should not produce interfaces:\n%s", output)
	}
}

func TestGenerateTSVoidInput(t *testing.T) {
	router := trpcgo.NewRouter()
	trpcgo.VoidQuery(router, "ping", func(ctx context.Context) (string, error) {
		return "pong", nil
	})

	outputPath := filepath.Join(t.TempDir(), "trpc.ts")
	if err := router.GenerateTS(outputPath); err != nil {
		t.Fatalf("GenerateTS failed: %v", err)
	}

	data, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("reading output: %v", err)
	}
	output := string(data)

	if !strings.Contains(output, "$Query<void, string>") {
		t.Errorf("expected $Query<void, string>, got:\n%s", output)
	}
	// Should have no interfaces for primitive types.
	if strings.Contains(output, "export interface") {
		t.Errorf("primitive-only procedures should not generate interfaces:\n%s", output)
	}
}

func TestGenerateTSIdempotent(t *testing.T) {
	router := trpcgo.NewRouter()
	trpcgo.VoidQuery(router, "ping", func(ctx context.Context) (string, error) {
		return "pong", nil
	})

	dir := t.TempDir()
	path1 := filepath.Join(dir, "first.ts")
	path2 := filepath.Join(dir, "second.ts")

	if err := router.GenerateTS(path1); err != nil {
		t.Fatal(err)
	}
	if err := router.GenerateTS(path2); err != nil {
		t.Fatal(err)
	}

	data1, _ := os.ReadFile(path1)
	data2, _ := os.ReadFile(path2)

	if string(data1) != string(data2) {
		t.Errorf("GenerateTS is not idempotent:\nfirst:\n%s\nsecond:\n%s", data1, data2)
	}
}

func TestGenerateTSTrackedEventUnwrap(t *testing.T) {
	type Notification struct {
		Message string `json:"message"`
	}

	router := trpcgo.NewRouter()
	trpcgo.VoidSubscribe(router, "notifications", func(ctx context.Context) (<-chan trpcgo.TrackedEvent[Notification], error) {
		return nil, nil
	})

	outputPath := filepath.Join(t.TempDir(), "trpc.ts")
	if err := router.GenerateTS(outputPath); err != nil {
		t.Fatalf("GenerateTS failed: %v", err)
	}

	data, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("reading output: %v", err)
	}
	output := string(data)

	// The subscription output should be Notification, not TrackedEvent.
	if !strings.Contains(output, "$Subscription<void, Notification>") {
		t.Errorf("TrackedEvent should be unwrapped to Notification:\n%s", output)
	}
	if strings.Contains(output, "TrackedEvent") {
		t.Errorf("TrackedEvent should not appear in output:\n%s", output)
	}
	if !strings.Contains(output, "export interface Notification") {
		t.Errorf("Notification interface should be emitted:\n%s", output)
	}
}

// --- Body Size Limit Tests ---

func TestMaxBodySizeEnforced(t *testing.T) {
	router := trpcgo.NewRouter(trpcgo.WithMaxBodySize(50))
	trpcgo.Mutation(router, "echo", func(ctx context.Context, input struct {
		Data string `json:"data"`
	}) (string, error) {
		return input.Data, nil
	})

	server := newTestServer(t, router.Handler("/trpc"))

	t.Run("within limit succeeds", func(t *testing.T) {
		resp := mustPost(t, server, "/trpc/echo", `{"data":"hi"}`)
		if resp.StatusCode != 200 {
			_ = resp.Body.Close()
			t.Fatalf("status = %d, want 200", resp.StatusCode)
		}
		got := resultScalar(t, decodeJSON(t, resp))
		if got != "hi" {
			t.Errorf("result = %v, want hi", got)
		}
	})

	t.Run("over limit returns 413", func(t *testing.T) {
		largeBody := `{"data":"` + strings.Repeat("x", 100) + `"}`
		resp := mustPost(t, server, "/trpc/echo", largeBody)
		if resp.StatusCode != 413 {
			_ = resp.Body.Close()
			t.Fatalf("status = %d, want 413", resp.StatusCode)
		}
		ed := errorData(t, decodeJSON(t, resp))
		if ed["code"] != "PAYLOAD_TOO_LARGE" {
			t.Errorf("error code = %v, want PAYLOAD_TOO_LARGE", ed["code"])
		}
	})

	t.Run("exactly at limit succeeds", func(t *testing.T) {
		// 50 bytes limit. Build a body that's exactly 50 bytes.
		body := `{"data":"` + strings.Repeat("x", 27) + `"}`
		if len(body) > 50 {
			body = `{"data":"` + strings.Repeat("x", 50-len(`{"data":""}`)) + `"}`
		}
		resp := mustPost(t, server, "/trpc/echo", body)
		if resp.StatusCode != 200 {
			_ = resp.Body.Close()
			t.Fatalf("body at limit: status = %d, want 200 (body len=%d)", resp.StatusCode, len(body))
		}
		_ = resp.Body.Close()
	})
}

// --- Duplicate Registration Test ---

func TestDuplicateRegistrationPanics(t *testing.T) {
	router := trpcgo.NewRouter()
	trpcgo.VoidQuery(router, "hello", func(ctx context.Context) (string, error) {
		return "hi", nil
	})

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic on duplicate registration")
		}
		msg, ok := r.(string)
		if !ok || !strings.Contains(msg, "hello") {
			t.Fatalf("unexpected panic: %v", r)
		}
	}()

	trpcgo.VoidQuery(router, "hello", func(ctx context.Context) (string, error) {
		return "hi again", nil
	})
}

// --- Internal Error Masking Test ---

func TestInternalErrorNotLeaked(t *testing.T) {
	var capturedErr *trpcgo.Error
	router := trpcgo.NewRouter(trpcgo.WithOnError(func(ctx context.Context, err *trpcgo.Error, path string) {
		capturedErr = err
	}))
	trpcgo.VoidQuery(router, "fail", func(ctx context.Context) (string, error) {
		return "", fmt.Errorf("secret database password: hunter2")
	})

	server := newTestServer(t, router.Handler("/trpc"))

	resp := mustGet(t, server, "/trpc/fail")
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != 500 {
		t.Fatalf("status = %d, want 500", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	bodyStr := string(body)

	// Client must NOT see internal details.
	if strings.Contains(bodyStr, "hunter2") {
		t.Error("internal error details leaked to client")
	}
	if strings.Contains(bodyStr, "secret") {
		t.Error("internal error details leaked to client")
	}
	if !strings.Contains(bodyStr, "internal server error") {
		t.Error("expected generic 'internal server error' message")
	}

	// But onError callback should have received the original error for logging.
	if capturedErr == nil {
		t.Fatal("onError should have been called")
	}
	if capturedErr.Unwrap() == nil || !strings.Contains(capturedErr.Unwrap().Error(), "hunter2") {
		t.Error("onError should receive the original error with internal details")
	}
}

// --- isDev Stack Trace Tests ---

func TestDevModeStackTrace(t *testing.T) {
	r := trpcgo.NewRouter(trpcgo.WithDev(true))

	trpcgo.Query(r, "user.get", func(ctx context.Context, input GetUserInput) (User, error) {
		return User{}, trpcgo.NewError(trpcgo.CodeNotFound, "not found")
	})

	server := newTestServer(t, r.Handler("/trpc"))
	resp := mustGet(t, server, "/trpc/user.get?input="+url.QueryEscape(`{"id":"1"}`))
	body := decodeJSON(t, resp)
	data := errorData(t, body)

	stack, ok := data["stack"].(string)
	if !ok || stack == "" {
		t.Fatal("isDev=true should include stack trace in error data")
	}
	if !strings.Contains(stack, "goroutine") {
		t.Errorf("stack trace should contain goroutine info, got: %s", stack[:min(len(stack), 100)])
	}
}

func TestNonDevModeNoStackTrace(t *testing.T) {
	r := trpcgo.NewRouter() // isDev defaults to false

	trpcgo.Query(r, "user.get", func(ctx context.Context, input GetUserInput) (User, error) {
		return User{}, trpcgo.NewError(trpcgo.CodeNotFound, "not found")
	})

	server := newTestServer(t, r.Handler("/trpc"))
	resp := mustGet(t, server, "/trpc/user.get?input="+url.QueryEscape(`{"id":"1"}`))
	body := decodeJSON(t, resp)
	data := errorData(t, body)

	if _, ok := data["stack"]; ok {
		t.Fatal("isDev=false should not include stack trace in error data")
	}
}

// --- lastEventId Tests ---

// lastEventIdSubscription sets up a subscription that captures the received lastEventId
// via a channel (race-free) and returns it to the caller.
func lastEventIdSubscription(r *trpcgo.Router) <-chan string {
	type subInput struct {
		Channel     string `json:"channel"`
		LastEventId string `json:"lastEventId"`
	}

	gotID := make(chan string, 1)
	trpcgo.Subscribe(r, "events", func(ctx context.Context, input subInput) (<-chan string, error) {
		gotID <- input.LastEventId
		ch := make(chan string)
		go func() {
			defer close(ch)
			ch <- "ok"
		}()
		return ch, nil
	})
	return gotID
}

func TestLastEventId(t *testing.T) {
	tests := []struct {
		name   string
		header string            // Last-Event-Id header value (empty = don't set)
		query  map[string]string // extra query params
		input  string            // input query param (JSON, empty = none)
		wantID string
	}{
		{
			name:   "from header",
			header: "evt-42",
			wantID: "evt-42",
		},
		{
			name:   "from lastEventId query param",
			query:  map[string]string{"lastEventId": "evt-99"},
			wantID: "evt-99",
		},
		{
			name:   "from Last-Event-Id query param",
			query:  map[string]string{"Last-Event-Id": "evt-77"},
			wantID: "evt-77",
		},
		{
			name:   "header takes precedence over query",
			header: "from-header",
			query:  map[string]string{"lastEventId": "from-query"},
			wantID: "from-header",
		},
		{
			name:   "no lastEventId",
			wantID: "",
		},
		{
			name:   "merges with existing input",
			header: "evt-7",
			input:  `{"channel":"general"}`,
			wantID: "evt-7",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := trpcgo.NewRouter()
			gotID := lastEventIdSubscription(r)
			server := newTestServer(t, r.Handler("/trpc"))

			u := server.URL + "/trpc/events"
			params := url.Values{}
			if tt.input != "" {
				params.Set("input", tt.input)
			}
			for k, v := range tt.query {
				params.Set(k, v)
			}
			if q := params.Encode(); q != "" {
				u += "?" + q
			}

			req, _ := http.NewRequest("GET", u, nil)
			if tt.header != "" {
				req.Header.Set("Last-Event-Id", tt.header)
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = resp.Body.Close() }()

			// Read the captured ID (race-free via channel).
			id := <-gotID

			// Consume SSE to let handler finish cleanly.
			parseSSEEvents(t, resp, 3)

			if id != tt.wantID {
				t.Errorf("lastEventId = %q, want %q", id, tt.wantID)
			}
		})
	}
}

func TestLastEventIdNotMergedForQueries(t *testing.T) {
	r := trpcgo.NewRouter()

	var capturedInput any
	trpcgo.Query(r, "user.get", func(ctx context.Context, input GetUserInput) (User, error) {
		return User{ID: input.ID, Name: "Alice"}, nil
	})

	// Capture the decoded input from middleware.
	r.Use(func(next trpcgo.HandlerFunc) trpcgo.HandlerFunc {
		return func(ctx context.Context, input any) (any, error) {
			capturedInput = input
			return next(ctx, input)
		}
	})

	server := newTestServer(t, r.Handler("/trpc"))

	inputJSON := url.QueryEscape(`{"id":"1"}`)
	req, _ := http.NewRequest("GET", server.URL+"/trpc/user.get?input="+inputJSON, nil)
	req.Header.Set("Last-Event-Id", "should-not-appear")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()

	// The decoded input is GetUserInput — marshal to JSON to verify no lastEventId.
	data, _ := json.Marshal(capturedInput)
	if strings.Contains(string(data), "lastEventId") {
		t.Errorf("lastEventId should not be merged for queries, got input: %s", data)
	}
}

// --- JSONL Streaming Batch Response Tests ---

// parseJSONLResponse reads a JSONL streaming response and returns the head line
// and all chunk lines. Each chunk is [chunkId, status, [[envelope]]].
type jsonlChunk struct {
	index    int
	status   int
	envelope map[string]any
}

func parseJSONLResponse(t *testing.T, resp *http.Response) (head map[string]any, chunks []jsonlChunk) {
	t.Helper()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read JSONL body: %v", err)
	}

	lines := strings.Split(strings.TrimSuffix(string(body), "\n"), "\n")
	if len(lines) < 1 {
		t.Fatal("JSONL response has no lines")
	}

	// Parse head line.
	if err := json.Unmarshal([]byte(lines[0]), &head); err != nil {
		t.Fatalf("parse JSONL head: %v\nline: %s", err, lines[0])
	}

	// Parse chunk lines: [chunkId, status, [[envelope]]]
	for _, line := range lines[1:] {
		if line == "" {
			continue
		}
		var parts []json.RawMessage
		if err := json.Unmarshal([]byte(line), &parts); err != nil {
			t.Fatalf("parse JSONL chunk: %v\nline: %s", err, line)
		}
		if len(parts) != 3 {
			t.Fatalf("chunk has %d elements, want 3", len(parts))
		}

		var idx int
		if err := json.Unmarshal(parts[0], &idx); err != nil {
			t.Fatalf("parse chunk index: %v", err)
		}
		var status int
		if err := json.Unmarshal(parts[1], &status); err != nil {
			t.Fatalf("parse chunk status: %v", err)
		}

		// parts[2] is [[envelope]] — unwrap EncodedValue.
		var encoded [][]map[string]any
		if err := json.Unmarshal(parts[2], &encoded); err != nil {
			t.Fatalf("parse chunk[%d] EncodedValue: %v\nraw: %s", idx, err, parts[2])
		}
		if len(encoded) != 1 || len(encoded[0]) != 1 {
			t.Fatalf("chunk[%d] EncodedValue should be [[envelope]], got %s", idx, parts[2])
		}
		chunks = append(chunks, jsonlChunk{index: idx, status: status, envelope: encoded[0][0]})
	}
	return head, chunks
}

func TestJSONLBatchResponse(t *testing.T) {
	tests := []struct {
		name   string
		method string
		path   string
		body   string // POST body (empty for GET)
		input  string // GET input query param
	}{
		{
			name:   "GET batch",
			method: "GET",
			path:   "/trpc/user.getById,user.getById?batch=1",
			input:  `{"0":{"id":"1"},"1":{"id":"2"}}`,
		},
		{
			name:   "POST batch",
			method: "POST",
			path:   "/trpc/user.create,user.create?batch=1",
			body:   `{"0":{"name":"Alice"},"1":{"name":"Bob"}}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := newTestServer(t, setupRouter().Handler("/trpc"))

			reqURL := server.URL + tt.path
			if tt.input != "" {
				reqURL += "&input=" + url.QueryEscape(tt.input)
			}
			var body io.Reader
			if tt.body != "" {
				body = strings.NewReader(tt.body)
			}
			req, _ := http.NewRequest(tt.method, reqURL, body)
			req.Header.Set("trpc-accept", "application/jsonl")
			if tt.body != "" {
				req.Header.Set("Content-Type", "application/json")
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = resp.Body.Close() }()

			if resp.StatusCode != 200 {
				t.Fatalf("status = %d, want 200", resp.StatusCode)
			}
			if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
				t.Errorf("Content-Type = %q, want application/json", ct)
			}
			if vary := resp.Header.Get("Vary"); vary != "trpc-accept" {
				t.Errorf("Vary = %q, want trpc-accept", vary)
			}

			head, chunks := parseJSONLResponse(t, resp)

			// Head should have placeholders for 2 results.
			if len(head) != 2 {
				t.Fatalf("head has %d entries, want 2", len(head))
			}

			// Should have 2 chunk lines.
			if len(chunks) != 2 {
				t.Fatalf("got %d chunks, want 2", len(chunks))
			}

			// Both chunks should be FULFILLED (status 0) with result envelopes.
			for _, ch := range chunks {
				if ch.status != 0 {
					t.Errorf("chunk[%d] status = %d, want 0 (FULFILLED)", ch.index, ch.status)
				}
				if _, ok := ch.envelope["result"]; !ok {
					t.Errorf("chunk[%d] missing result key", ch.index)
				}
			}
		})
	}
}

func TestJSONLBatchHeadFormat(t *testing.T) {
	server := newTestServer(t, setupRouter().Handler("/trpc"))

	input := url.QueryEscape(`{"0":{"id":"1"},"1":{"id":"2"}}`)
	req, _ := http.NewRequest("GET", server.URL+"/trpc/user.getById,user.getById?batch=1&input="+input, nil)
	req.Header.Set("trpc-accept", "application/jsonl")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()

	head, _ := parseJSONLResponse(t, resp)

	// Verify each head entry is [[placeholder], [null, 0, chunkId]].
	for i := range 2 {
		key := fmt.Sprintf("%d", i)
		entry, ok := head[key]
		if !ok {
			t.Fatalf("head missing key %q", key)
		}
		arr, ok := entry.([]any)
		if !ok || len(arr) != 2 {
			t.Fatalf("head[%q] should be [data, chunkDef], got %v", key, entry)
		}
		// First element: [0] (placeholder)
		dataSlot, ok := arr[0].([]any)
		if !ok || len(dataSlot) != 1 || dataSlot[0] != float64(0) {
			t.Errorf("head[%q] data slot should be [0], got %v", key, arr[0])
		}
		// Second element: [null, 0, chunkId]
		chunkDef, ok := arr[1].([]any)
		if !ok || len(chunkDef) != 3 {
			t.Fatalf("head[%q] chunkDef should be [null,0,N], got %v", key, arr[1])
		}
		if chunkDef[0] != nil {
			t.Errorf("head[%q] chunkDef[0] should be null, got %v", key, chunkDef[0])
		}
		if chunkDef[1] != float64(0) {
			t.Errorf("head[%q] chunkDef[1] (type) should be 0, got %v", key, chunkDef[1])
		}
		if chunkDef[2] != float64(i) {
			t.Errorf("head[%q] chunkDef[2] (chunkId) should be %d, got %v", key, i, chunkDef[2])
		}
	}
}

func TestJSONLBatchVaryHeader(t *testing.T) {
	server := newTestServer(t, setupRouter().Handler("/trpc"))

	// Standard batch (no JSONL) should also have Vary header.
	input := url.QueryEscape(`{"0":{"id":"1"}}`)
	resp := mustGet(t, server, "/trpc/user.getById?batch=1&input="+input)
	defer func() { _ = resp.Body.Close() }()

	if vary := resp.Header.Get("Vary"); vary != "trpc-accept" {
		t.Errorf("Vary = %q, want trpc-accept", vary)
	}
}

func TestJSONLBatchWithErrors(t *testing.T) {
	server := newTestServer(t, setupRouter().Handler("/trpc"))

	// One success, one error.
	input := url.QueryEscape(`{"0":{"id":"1"},"1":{"id":"404"}}`)
	req, _ := http.NewRequest("GET", server.URL+"/trpc/user.getById,user.getById?batch=1&input="+input, nil)
	req.Header.Set("trpc-accept", "application/jsonl")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != 200 {
		t.Errorf("JSONL status = %d, want 200 (always 200 for JSONL)", resp.StatusCode)
	}

	_, chunks := parseJSONLResponse(t, resp)

	// All chunks should be FULFILLED (status 0) — errors are in the envelope, not rejections.
	byIndex := map[int]map[string]any{}
	for _, ch := range chunks {
		if ch.status != 0 {
			t.Errorf("chunk[%d] status = %d, want 0 (FULFILLED)", ch.index, ch.status)
		}
		byIndex[ch.index] = ch.envelope
	}

	// Entry 0 should be a success.
	data := resultData(t, byIndex[0])
	if data["name"] != "Alice" {
		t.Errorf("chunk[0] name = %v, want Alice", data["name"])
	}

	// Entry 1 should be an error.
	errData := errorData(t, byIndex[1])
	if errData["code"] != "NOT_FOUND" {
		t.Errorf("chunk[1] error code = %v, want NOT_FOUND", errData["code"])
	}
}

func TestJSONLBatchConcurrency(t *testing.T) {
	r := trpcgo.NewRouter(trpcgo.WithBatching(true), trpcgo.WithMethodOverride(true))

	trpcgo.Query(r, "fast", func(ctx context.Context, input GetUserInput) (User, error) {
		return User{ID: "fast", Name: "Fast"}, nil
	})
	trpcgo.Query(r, "slow", func(ctx context.Context, input GetUserInput) (User, error) {
		time.Sleep(50 * time.Millisecond)
		return User{ID: "slow", Name: "Slow"}, nil
	})

	server := newTestServer(t, r.Handler("/trpc"))

	// Batch: slow first, fast second. With concurrent execution,
	// the fast result should arrive as a chunk before the slow one.
	req, _ := http.NewRequest("POST", server.URL+"/trpc/slow,fast?batch=1",
		strings.NewReader(`{"0":{"id":"1"},"1":{"id":"2"}}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("trpc-accept", "application/jsonl")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()

	_, chunks := parseJSONLResponse(t, resp)
	if len(chunks) != 2 {
		t.Fatalf("got %d chunks, want 2", len(chunks))
	}

	// First chunk should be the fast handler (index 1), not the slow one (index 0).
	if chunks[0].index != 1 {
		t.Errorf("first chunk index = %d, want 1 (fast handler should complete first)", chunks[0].index)
	}
	if chunks[1].index != 0 {
		t.Errorf("second chunk index = %d, want 0 (slow handler)", chunks[1].index)
	}

	// Both should have correct data.
	for _, ch := range chunks {
		data := resultData(t, ch.envelope)
		if ch.index == 0 && data["name"] != "Slow" {
			t.Errorf("chunk[0] name = %v, want Slow", data["name"])
		}
		if ch.index == 1 && data["name"] != "Fast" {
			t.Errorf("chunk[1] name = %v, want Fast", data["name"])
		}
	}
}

// --- Procedure Metadata Tests ---

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

	server := newTestServer(t, router.Handler("/trpc"))

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

	server := newTestServer(t, router.Handler("/trpc"))

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

	server := newTestServer(t, router.Handler("/trpc"))

	resp := mustPost(t, server, "/trpc/user.create", `{"name":"Bob"}`)
	_ = resp.Body.Close()

	if gotType != trpcgo.ProcedureMutation {
		t.Errorf("ProcedureMeta.Type = %q, want %q", gotType, trpcgo.ProcedureMutation)
	}
}

// --- Error Formatter Tests ---

func TestErrorFormatter(t *testing.T) {
	router := trpcgo.NewRouter(trpcgo.WithErrorFormatter(func(input trpcgo.ErrorFormatterInput) any {
		return map[string]any{
			"error": map[string]any{
				"code":    input.Shape.Error.Code,
				"message": input.Shape.Error.Message,
				"data":    input.Shape.Error.Data,
				"custom":  "extra-field",
			},
		}
	}))

	trpcgo.VoidQuery(router, "fail", func(ctx context.Context) (string, error) {
		return "", trpcgo.NewError(trpcgo.CodeBadRequest, "invalid input")
	})

	server := newTestServer(t, router.Handler("/trpc"))

	resp := mustGet(t, server, "/trpc/fail")
	if resp.StatusCode != 400 {
		_ = resp.Body.Close()
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}

	body := decodeJSON(t, resp)
	errObj, ok := body["error"].(map[string]any)
	if !ok {
		t.Fatalf("expected error object, got %v", body)
	}
	if errObj["custom"] != "extra-field" {
		t.Errorf("custom field = %v, want extra-field", errObj["custom"])
	}
	if errObj["message"] != "invalid input" {
		t.Errorf("message = %v, want 'invalid input'", errObj["message"])
	}
}

func TestErrorFormatterReceivesContext(t *testing.T) {
	type ctxKey string

	var gotVal string
	router := trpcgo.NewRouter(
		trpcgo.WithContextCreator(func(r *http.Request) context.Context {
			return context.WithValue(r.Context(), ctxKey("tenant"), "acme")
		}),
		trpcgo.WithErrorFormatter(func(input trpcgo.ErrorFormatterInput) any {
			gotVal, _ = input.Ctx.Value(ctxKey("tenant")).(string)
			return input.Shape // pass through default shape
		}),
	)

	trpcgo.VoidQuery(router, "fail", func(ctx context.Context) (string, error) {
		return "", trpcgo.NewError(trpcgo.CodeBadRequest, "nope")
	})

	server := newTestServer(t, router.Handler("/trpc"))

	resp := mustGet(t, server, "/trpc/fail")
	_ = resp.Body.Close()

	if gotVal != "acme" {
		t.Errorf("error formatter ctx value = %q, want %q", gotVal, "acme")
	}
}

func TestErrorFormatterNotFound(t *testing.T) {
	var gotPath string
	router := trpcgo.NewRouter(trpcgo.WithErrorFormatter(func(input trpcgo.ErrorFormatterInput) any {
		gotPath = input.Path
		return input.Shape
	}))

	trpcgo.VoidQuery(router, "exists", func(ctx context.Context) (string, error) {
		return "ok", nil
	})

	server := newTestServer(t, router.Handler("/trpc"))

	resp := mustGet(t, server, "/trpc/nonexistent")
	_ = resp.Body.Close()

	if gotPath != "nonexistent" {
		t.Errorf("formatter path = %q, want %q", gotPath, "nonexistent")
	}
}

// --- Router Merging Tests ---

func TestMergeRouters(t *testing.T) {
	r1 := trpcgo.NewRouter()
	trpcgo.VoidQuery(r1, "hello", func(ctx context.Context) (string, error) {
		return "hello", nil
	})

	r2 := trpcgo.NewRouter()
	trpcgo.VoidQuery(r2, "world", func(ctx context.Context) (string, error) {
		return "world", nil
	})

	merged := trpcgo.MergeRouters(r1, r2)
	server := newTestServer(t, merged.Handler("/trpc"))

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

func TestMergeRoutersDuplicatePanics(t *testing.T) {
	r1 := trpcgo.NewRouter()
	trpcgo.VoidQuery(r1, "dup", func(ctx context.Context) (string, error) {
		return "a", nil
	})

	r2 := trpcgo.NewRouter()
	trpcgo.VoidQuery(r2, "dup", func(ctx context.Context) (string, error) {
		return "b", nil
	})

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic on duplicate procedure path")
		}
		msg, ok := r.(string)
		if !ok || !strings.Contains(msg, "dup") {
			t.Fatalf("expected panic mentioning 'dup', got %v", r)
		}
	}()
	trpcgo.MergeRouters(r1, r2)
}

func TestRouterMergeMethod(t *testing.T) {
	sub := trpcgo.NewRouter()
	trpcgo.VoidQuery(sub, "sub.hello", func(ctx context.Context) (string, error) {
		return "from sub", nil
	})

	main := trpcgo.NewRouter(trpcgo.WithMethodOverride(true))
	trpcgo.VoidQuery(main, "main.hello", func(ctx context.Context) (string, error) {
		return "from main", nil
	})
	main.Merge(sub)

	server := newTestServer(t, main.Handler("/trpc"))

	// sub procedure accessible
	resp := mustGet(t, server, "/trpc/sub.hello")
	if resp.StatusCode != 200 {
		_ = resp.Body.Close()
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if got := resultScalar(t, decodeJSON(t, resp)); got != "from sub" {
		t.Errorf("result = %v, want 'from sub'", got)
	}

	// main options (method override) apply to merged procedures
	resp2 := mustPost(t, server, "/trpc/sub.hello", "")
	if resp2.StatusCode != 200 {
		_ = resp2.Body.Close()
		t.Fatalf("method override status = %d, want 200", resp2.StatusCode)
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
	merged.Merge(r1, r2)

	server := newTestServer(t, merged.Handler("/trpc"))

	resp := mustGet(t, server, "/trpc/a")
	_ = resp.Body.Close()

	if len(calls) != 2 || calls[0] != "global-mw" || calls[1] != "handler-a" {
		t.Errorf("calls = %v, want [global-mw, handler-a]", calls)
	}
}

// --- Server-Side Caller Tests ---

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

// --- Additional coverage for gaps ---

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

	server := newTestServer(t, router.Handler("/trpc"))

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

	server := newTestServer(t, router.Handler("/trpc"))

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

	server := newTestServer(t, router.Handler("/trpc"))

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

	server := newTestServer(t, router.Handler("/trpc"))

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

func TestErrorFormatterReceivesProcedureType(t *testing.T) {
	var gotType trpcgo.ProcedureType

	router := trpcgo.NewRouter(trpcgo.WithErrorFormatter(func(input trpcgo.ErrorFormatterInput) any {
		gotType = input.Type
		return input.Shape
	}))

	trpcgo.Mutation(router, "fail", func(ctx context.Context, input struct{}) (string, error) {
		return "", trpcgo.NewError(trpcgo.CodeBadRequest, "bad")
	})

	server := newTestServer(t, router.Handler("/trpc"))
	resp := mustPost(t, server, "/trpc/fail", `{}`)
	_ = resp.Body.Close()

	if gotType != trpcgo.ProcedureMutation {
		t.Errorf("ErrorFormatterInput.Type = %q, want %q", gotType, trpcgo.ProcedureMutation)
	}
}

func TestErrorFormatterOnInternalError(t *testing.T) {
	formatterCalled := false

	router := trpcgo.NewRouter(trpcgo.WithErrorFormatter(func(input trpcgo.ErrorFormatterInput) any {
		formatterCalled = true
		return input.Shape
	}))

	trpcgo.VoidQuery(router, "panic-ish", func(ctx context.Context) (string, error) {
		// Return a non-trpc error — handler wraps it as INTERNAL_SERVER_ERROR
		return "", fmt.Errorf("unexpected: db connection lost")
	})

	server := newTestServer(t, router.Handler("/trpc"))
	resp := mustGet(t, server, "/trpc/panic-ish")
	_ = resp.Body.Close()

	if resp.StatusCode != 500 {
		t.Errorf("status = %d, want 500", resp.StatusCode)
	}
	if !formatterCalled {
		t.Error("error formatter was not called for internal error")
	}
}

func TestErrorFormatterInBatchResponse(t *testing.T) {
	callCount := atomic.Int32{}

	router := trpcgo.NewRouter(
		trpcgo.WithBatching(true),
		trpcgo.WithErrorFormatter(func(input trpcgo.ErrorFormatterInput) any {
			callCount.Add(1)
			return input.Shape
		}),
	)

	trpcgo.VoidQuery(router, "ok", func(ctx context.Context) (string, error) {
		return "fine", nil
	})
	trpcgo.VoidQuery(router, "fail", func(ctx context.Context) (string, error) {
		return "", trpcgo.NewError(trpcgo.CodeBadRequest, "bad")
	})

	server := newTestServer(t, router.Handler("/trpc"))

	// Batch: ok + fail
	resp := mustGet(t, server, "/trpc/ok,fail")
	_ = resp.Body.Close()

	if callCount.Load() != 1 {
		t.Errorf("formatter called %d times, want 1 (only for the failing procedure)", callCount.Load())
	}
}

func TestErrorFormatterSSESerializedError(t *testing.T) {
	formatterCalled := false

	router := trpcgo.NewRouter(trpcgo.WithErrorFormatter(func(input trpcgo.ErrorFormatterInput) any {
		formatterCalled = true
		return map[string]any{
			"code":    input.Shape.Error.Code,
			"message": input.Shape.Error.Message,
			"custom":  "sse-formatted",
		}
	}))

	type BadData struct {
		Ch chan int // channels can't be JSON serialized
	}

	trpcgo.VoidSubscribe(router, "bad", func(ctx context.Context) (<-chan BadData, error) {
		ch := make(chan BadData, 1)
		ch <- BadData{Ch: make(chan int)}
		close(ch)
		return ch, nil
	})

	server := newTestServer(t, router.Handler("/trpc"))

	resp, err := http.Get(server.URL + "/trpc/bad")
	if err != nil {
		t.Fatalf("request error: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, _ := io.ReadAll(resp.Body)
	bodyStr := string(body)

	if !formatterCalled {
		t.Error("error formatter was not called for SSE serialized-error")
	}
	if !strings.Contains(bodyStr, "sse-formatted") {
		t.Errorf("SSE body missing formatted error, got:\n%s", bodyStr)
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
	main.Merge(sub)

	server := newTestServer(t, main.Handler("/trpc"))
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
	main.Merge(sub)

	server := newTestServer(t, main.Handler("/trpc"))
	resp := mustGet(t, server, "/trpc/tagged")
	_ = resp.Body.Close()

	if gotMeta != "preserved" {
		t.Errorf("meta after merge = %v, want %q", gotMeta, "preserved")
	}
}

func TestMergeEmptyRouters(t *testing.T) {
	r1 := trpcgo.NewRouter()
	r2 := trpcgo.NewRouter()

	merged := trpcgo.MergeRouters(r1, r2)
	server := newTestServer(t, merged.Handler("/trpc"))

	resp := mustGet(t, server, "/trpc/anything")
	_ = resp.Body.Close()

	if resp.StatusCode != 404 {
		t.Errorf("status = %d, want 404", resp.StatusCode)
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
	_ = router.Handler("/trpc")

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

	_ = router.Handler("/trpc") // pre-compute

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

// --- Validator Tests ---

// testValidator is a simple struct validator for tests that checks "required",
// "min", and "max" rules from `validate` struct tags on string fields.
// This avoids importing go-playground/validator.
func testValidator(v any) error {
	val := reflect.ValueOf(v)
	typ := val.Type()
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		tag := field.Tag.Get("validate")
		if tag == "" {
			continue
		}
		rules := strings.Split(tag, ",")
		fieldVal := val.Field(i)
		for _, rule := range rules {
			parts := strings.SplitN(rule, "=", 2)
			switch parts[0] {
			case "required":
				if fieldVal.IsZero() {
					return fmt.Errorf("field %s is required", field.Name)
				}
			case "min":
				if len(parts) > 1 {
					n, _ := strconv.Atoi(parts[1])
					if fieldVal.Kind() == reflect.String && len(fieldVal.String()) < n {
						return fmt.Errorf("field %s must be at least %d characters", field.Name, n)
					}
				}
			case "max":
				if len(parts) > 1 {
					n, _ := strconv.Atoi(parts[1])
					if fieldVal.Kind() == reflect.String && len(fieldVal.String()) > n {
						return fmt.Errorf("field %s must be at most %d characters", field.Name, n)
					}
				}
			}
		}
	}
	return nil
}

type ValidatedInput struct {
	Name string `json:"name" validate:"required"`
}

type ValidatedOutput struct {
	Greeting string `json:"greeting"`
}

type MinMaxInput struct {
	Username string `json:"username" validate:"min=3,max=10"`
}

func TestValidatorRejectsInvalidInput(t *testing.T) {
	router := trpcgo.NewRouter(trpcgo.WithValidator(testValidator))

	trpcgo.Query(router, "greet", func(ctx context.Context, input ValidatedInput) (ValidatedOutput, error) {
		return ValidatedOutput{Greeting: "Hello, " + input.Name + "!"}, nil
	})

	server := newTestServer(t, router.Handler("/trpc"))

	// Send request with empty name (missing required field).
	input := url.QueryEscape(`{"name":""}`)
	resp := mustGet(t, server, "/trpc/greet?input="+input)
	if resp.StatusCode != 400 {
		_ = resp.Body.Close()
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}

	body := decodeJSON(t, resp)
	ed := errorData(t, body)
	if ed["code"] != "BAD_REQUEST" {
		t.Errorf("error.data.code = %v, want BAD_REQUEST", ed["code"])
	}

	// Also verify the error message mentions validation.
	errObj := body["error"].(map[string]any)
	msg, _ := errObj["message"].(string)
	if !strings.Contains(msg, "validation") {
		t.Errorf("error message = %q, want it to contain 'validation'", msg)
	}
}

func TestValidatorAcceptsValidInput(t *testing.T) {
	router := trpcgo.NewRouter(trpcgo.WithValidator(testValidator))

	trpcgo.Query(router, "greet", func(ctx context.Context, input ValidatedInput) (ValidatedOutput, error) {
		return ValidatedOutput{Greeting: "Hello, " + input.Name + "!"}, nil
	})

	server := newTestServer(t, router.Handler("/trpc"))

	// Send request with valid name.
	input := url.QueryEscape(`{"name":"Alice"}`)
	resp := mustGet(t, server, "/trpc/greet?input="+input)
	if resp.StatusCode != 200 {
		_ = resp.Body.Close()
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	data := resultData(t, decodeJSON(t, resp))
	if data["greeting"] != "Hello, Alice!" {
		t.Errorf("greeting = %v, want 'Hello, Alice!'", data["greeting"])
	}
}

func TestValidatorMinMax(t *testing.T) {
	router := trpcgo.NewRouter(trpcgo.WithValidator(testValidator))

	trpcgo.Query(router, "checkUsername", func(ctx context.Context, input MinMaxInput) (string, error) {
		return "ok:" + input.Username, nil
	})

	server := newTestServer(t, router.Handler("/trpc"))

	// Below min (less than 3 chars) → 400.
	t.Run("below min", func(t *testing.T) {
		input := url.QueryEscape(`{"username":"ab"}`)
		resp := mustGet(t, server, "/trpc/checkUsername?input="+input)
		if resp.StatusCode != 400 {
			_ = resp.Body.Close()
			t.Fatalf("status = %d, want 400 for username below min length", resp.StatusCode)
		}

		body := decodeJSON(t, resp)
		ed := errorData(t, body)
		if ed["code"] != "BAD_REQUEST" {
			t.Errorf("error.data.code = %v, want BAD_REQUEST", ed["code"])
		}
	})

	// Within range (3-10 chars) → 200.
	t.Run("within range", func(t *testing.T) {
		input := url.QueryEscape(`{"username":"alice"}`)
		resp := mustGet(t, server, "/trpc/checkUsername?input="+input)
		if resp.StatusCode != 200 {
			_ = resp.Body.Close()
			t.Fatalf("status = %d, want 200 for valid username", resp.StatusCode)
		}

		body := decodeJSON(t, resp)
		got := resultScalar(t, body)
		if got != "ok:alice" {
			t.Errorf("result = %v, want ok:alice", got)
		}
	})

	// Above max (more than 10 chars) → 400.
	t.Run("above max", func(t *testing.T) {
		input := url.QueryEscape(`{"username":"verylongusername"}`)
		resp := mustGet(t, server, "/trpc/checkUsername?input="+input)
		if resp.StatusCode != 400 {
			_ = resp.Body.Close()
			t.Fatalf("status = %d, want 400 for username above max length", resp.StatusCode)
		}

		body := decodeJSON(t, resp)
		ed := errorData(t, body)
		if ed["code"] != "BAD_REQUEST" {
			t.Errorf("error.data.code = %v, want BAD_REQUEST", ed["code"])
		}
	})
}

func TestValidatorWithErrorFormatter(t *testing.T) {
	var capturedCause string

	router := trpcgo.NewRouter(
		trpcgo.WithValidator(testValidator),
		trpcgo.WithErrorFormatter(func(input trpcgo.ErrorFormatterInput) any {
			// The error formatter receives the *Error which wraps the validation error.
			// Verify we can access the underlying validation error via Unwrap.
			if cause := input.Error.Unwrap(); cause != nil {
				capturedCause = cause.Error()
			}
			return input.Shape
		}),
	)

	trpcgo.Query(router, "greet", func(ctx context.Context, input ValidatedInput) (ValidatedOutput, error) {
		return ValidatedOutput{Greeting: "Hello, " + input.Name + "!"}, nil
	})

	server := newTestServer(t, router.Handler("/trpc"))

	// Send invalid input to trigger validation error.
	input := url.QueryEscape(`{"name":""}`)
	resp := mustGet(t, server, "/trpc/greet?input="+input)
	if resp.StatusCode != 400 {
		_ = resp.Body.Close()
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	_ = resp.Body.Close()

	// The cause should be the validation error from testValidator.
	if !strings.Contains(capturedCause, "field Name is required") {
		t.Errorf("captured cause = %q, want it to contain 'field Name is required'", capturedCause)
	}
}

func TestValidatorSkipsNonStruct(t *testing.T) {
	router := trpcgo.NewRouter(trpcgo.WithValidator(testValidator))

	// Register a query with primitive string input — validator should be skipped.
	trpcgo.Query(router, "echo", func(ctx context.Context, input string) (string, error) {
		return "echo:" + input, nil
	})

	server := newTestServer(t, router.Handler("/trpc"))

	input := url.QueryEscape(`"hello"`)
	resp := mustGet(t, server, "/trpc/echo?input="+input)
	if resp.StatusCode != 200 {
		_ = resp.Body.Close()
		t.Fatalf("status = %d, want 200 (validator should skip primitive input)", resp.StatusCode)
	}

	body := decodeJSON(t, resp)
	got := resultScalar(t, body)
	if got != "echo:hello" {
		t.Errorf("result = %v, want echo:hello", got)
	}
}

func TestValidatorWithRawCall(t *testing.T) {
	router := trpcgo.NewRouter(trpcgo.WithValidator(testValidator))

	trpcgo.Query(router, "greet", func(ctx context.Context, input ValidatedInput) (ValidatedOutput, error) {
		return ValidatedOutput{Greeting: "Hello, " + input.Name + "!"}, nil
	})

	// Call Handler() to pre-compute middleware chains with validation.
	_ = router.Handler("/trpc")

	// Valid input via Call should succeed.
	t.Run("valid input", func(t *testing.T) {
		result, err := trpcgo.Call[ValidatedInput, ValidatedOutput](
			router, context.Background(), "greet", ValidatedInput{Name: "Bob"},
		)
		if err != nil {
			t.Fatalf("Call error: %v", err)
		}
		if result.Greeting != "Hello, Bob!" {
			t.Errorf("greeting = %v, want 'Hello, Bob!'", result.Greeting)
		}
	})

	// Invalid input via Call should return validation error.
	t.Run("invalid input", func(t *testing.T) {
		_, err := trpcgo.Call[ValidatedInput, ValidatedOutput](
			router, context.Background(), "greet", ValidatedInput{Name: ""},
		)
		if err == nil {
			t.Fatal("expected validation error, got nil")
		}
		trpcErr, ok := err.(*trpcgo.Error)
		if !ok {
			t.Fatalf("error type = %T, want *trpcgo.Error", err)
		}
		if trpcErr.Code != trpcgo.CodeBadRequest {
			t.Errorf("error code = %v, want BAD_REQUEST", trpcErr.Code)
		}
		if !strings.Contains(trpcErr.Message, "validation") {
			t.Errorf("error message = %q, want it to contain 'validation'", trpcErr.Message)
		}
	})
}

func TestValidatorNotSet(t *testing.T) {
	// No validator configured — struct with validate tags should work normally.
	router := trpcgo.NewRouter()

	trpcgo.Query(router, "greet", func(ctx context.Context, input ValidatedInput) (ValidatedOutput, error) {
		return ValidatedOutput{Greeting: "Hello, " + input.Name + "!"}, nil
	})

	server := newTestServer(t, router.Handler("/trpc"))

	// Send empty name — without a validator, the handler should run and produce a response.
	input := url.QueryEscape(`{"name":""}`)
	resp := mustGet(t, server, "/trpc/greet?input="+input)
	if resp.StatusCode != 200 {
		_ = resp.Body.Close()
		t.Fatalf("status = %d, want 200 (no validator configured, handler should run)", resp.StatusCode)
	}

	data := resultData(t, decodeJSON(t, resp))
	if data["greeting"] != "Hello, !" {
		t.Errorf("greeting = %v, want 'Hello, !'", data["greeting"])
	}
}

// --- Response Metadata Tests ---

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

	server := newTestServer(t, router.Handler("/trpc"))
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

	server := newTestServer(t, router.Handler("/trpc"))
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

	server := newTestServer(t, router.Handler("/trpc"))
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

	server := newTestServer(t, router.Handler("/trpc"))
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

	server := newTestServer(t, router.Handler("/trpc"))
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

	server := newTestServer(t, router.Handler("/trpc"))
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

	server := newTestServer(t, router.Handler("/trpc"))
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

// --- Batch Size Limit Tests ---

func TestBatchSizeLimitDefault(t *testing.T) {
	router := trpcgo.NewRouter(trpcgo.WithBatching(true))

	trpcgo.VoidMutation(router, "ping", func(ctx context.Context) (string, error) {
		return "pong", nil
	})

	server := newTestServer(t, router.Handler("/trpc"))

	// 11 paths exceeds default limit of 10.
	paths := strings.Repeat("ping,", 10) + "ping"
	var inputs strings.Builder
	inputs.WriteString("{")
	for i := range 11 {
		if i > 0 {
			inputs.WriteString(",")
		}
		fmt.Fprintf(&inputs, `"%d":null`, i)
	}
	inputs.WriteString("}")

	resp := mustPost(t, server, "/trpc/"+paths+"?batch=1", inputs.String())
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != 400 {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "batch size") {
		t.Errorf("error should mention batch size, got: %s", body)
	}
}

func TestBatchSizeLimitWithinLimit(t *testing.T) {
	router := trpcgo.NewRouter(trpcgo.WithBatching(true))

	trpcgo.VoidMutation(router, "ping", func(ctx context.Context) (string, error) {
		return "pong", nil
	})

	server := newTestServer(t, router.Handler("/trpc"))

	// 2 paths is within default limit of 10.
	resp := mustPost(t, server, "/trpc/ping,ping?batch=1", `{"0":null,"1":null}`)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
}

func TestBatchSizeLimitCustom(t *testing.T) {
	router := trpcgo.NewRouter(trpcgo.WithBatching(true), trpcgo.WithMaxBatchSize(2))

	trpcgo.VoidMutation(router, "ping", func(ctx context.Context) (string, error) {
		return "pong", nil
	})

	server := newTestServer(t, router.Handler("/trpc"))

	// 3 paths exceeds custom limit of 2.
	resp := mustPost(t, server, "/trpc/ping,ping,ping?batch=1", `{"0":null,"1":null,"2":null}`)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != 400 {
		t.Fatalf("status = %d, want 400 for batch exceeding custom limit", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "batch size") {
		t.Errorf("error should mention batch size, got: %s", body)
	}
}

func TestBatchSizeLimitDisabled(t *testing.T) {
	router := trpcgo.NewRouter(trpcgo.WithBatching(true), trpcgo.WithMaxBatchSize(-1))

	trpcgo.VoidMutation(router, "ping", func(ctx context.Context) (string, error) {
		return "pong", nil
	})

	server := newTestServer(t, router.Handler("/trpc"))

	// 15 paths with limit disabled should succeed.
	paths := strings.Repeat("ping,", 14) + "ping"
	var inputs strings.Builder
	inputs.WriteString("{")
	for i := range 15 {
		if i > 0 {
			inputs.WriteString(",")
		}
		fmt.Fprintf(&inputs, `"%d":null`, i)
	}
	inputs.WriteString("}")

	resp := mustPost(t, server, "/trpc/"+paths+"?batch=1", inputs.String())
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d, want 200 with batch limit disabled", resp.StatusCode)
	}
}

func TestBatchSizeLimitJSONL(t *testing.T) {
	router := trpcgo.NewRouter(trpcgo.WithBatching(true), trpcgo.WithMaxBatchSize(2))

	trpcgo.VoidMutation(router, "ping", func(ctx context.Context) (string, error) {
		return "pong", nil
	})

	server := newTestServer(t, router.Handler("/trpc"))

	// 3 paths exceeds limit of 2, should be rejected even for JSONL.
	req, _ := http.NewRequest("POST", server.URL+"/trpc/ping,ping,ping?batch=1",
		strings.NewReader(`{"0":null,"1":null,"2":null}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("trpc-accept", "application/jsonl")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != 400 {
		t.Fatalf("status = %d, want 400 for JSONL batch exceeding limit", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "batch size") {
		t.Errorf("error should mention batch size, got: %s", body)
	}
}

// --- RawCall Response Metadata Tests ---

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

func TestHandlerSnapshotIsolation(t *testing.T) {
	router := trpcgo.NewRouter(trpcgo.WithMethodOverride(true))

	trpcgo.VoidQuery(router, "before", func(ctx context.Context) (string, error) {
		return "ok", nil
	})

	handler := router.Handler("/trpc")
	server := newTestServer(t, handler)

	// Register a new procedure AFTER Handler() was called.
	trpcgo.VoidQuery(router, "after", func(ctx context.Context) (string, error) {
		return "should not be reachable", nil
	})

	// "before" should still work via the HTTP handler.
	resp := mustGet(t, server, "/trpc/before")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("before: status = %d, want 200", resp.StatusCode)
	}
	_ = resp.Body.Close()

	// "after" should NOT be reachable via the HTTP handler (snapshot isolation).
	resp = mustGet(t, server, "/trpc/after")
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("after: status = %d, want 404", resp.StatusCode)
	}
	_ = resp.Body.Close()

	// But "after" should still be reachable via Call on the router directly.
	result, err := trpcgo.Call[any, string](router, context.Background(), "after", nil)
	if err != nil {
		t.Fatalf("Call(after): %v", err)
	}
	if result != "should not be reachable" {
		t.Errorf("Call(after) = %q, want %q", result, "should not be reachable")
	}

	// A second Handler() call sees the new procedure.
	handler2 := router.Handler("/trpc")
	server2 := newTestServer(t, handler2)
	resp = mustGet(t, server2, "/trpc/after")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("handler2 after: status = %d, want 200", resp.StatusCode)
	}
	_ = resp.Body.Close()

	// Original handler still doesn't see it.
	resp = mustGet(t, server, "/trpc/after")
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("handler1 after (re-check): status = %d, want 404", resp.StatusCode)
	}
	_ = resp.Body.Close()
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
