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
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/trpcgo/trpcgo"
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
	defer resp.Body.Close()
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
	defer resp.Body.Close()
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
				resp.Body.Close()
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
		resp.Body.Close()
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
				resp.Body.Close()
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
			defer resp.Body.Close()

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
		resp.Body.Close()
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
				resp.Body.Close()
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
		resp.Body.Close()
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
		resp.Body.Close()
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
		resp.Body.Close()
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
		resp.Body.Close()
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
		resp.Body.Close()
		t.Fatalf("status = %d, want 404 (all same error → unified status, not 207)", resp.StatusCode)
	}
}

func TestBatchDifferentProcedures(t *testing.T) {
	server := newTestServer(t, setupRouter().Handler("/trpc"))

	input := url.QueryEscape(`{"0":{},"1":{"id":"5"}}`)
	resp := mustGet(t, server, "/trpc/hello,user.getById?batch=1&input="+input)
	if resp.StatusCode != 200 {
		resp.Body.Close()
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
		resp.Body.Close()
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
	defer resp.Body.Close()
	if resp.StatusCode != 400 {
		t.Fatalf("status = %d, want 400 (batching disabled)", resp.StatusCode)
	}
}

// --- Middleware Tests ---

func TestMiddlewareOrdering(t *testing.T) {
	var calls []string

	router := trpcgo.NewRouter()
	router.Use(func(next trpcgo.HandlerFunc) trpcgo.HandlerFunc {
		return func(ctx context.Context, input json.RawMessage) (any, error) {
			calls = append(calls, "global-1")
			return next(ctx, input)
		}
	})
	router.Use(func(next trpcgo.HandlerFunc) trpcgo.HandlerFunc {
		return func(ctx context.Context, input json.RawMessage) (any, error) {
			calls = append(calls, "global-2")
			return next(ctx, input)
		}
	})

	trpcgo.VoidQuery(router, "test", func(ctx context.Context) (string, error) {
		calls = append(calls, "handler")
		return "ok", nil
	}, func(next trpcgo.HandlerFunc) trpcgo.HandlerFunc {
		return func(ctx context.Context, input json.RawMessage) (any, error) {
			calls = append(calls, "per-proc")
			return next(ctx, input)
		}
	})

	server := newTestServer(t, router.Handler("/trpc"))

	resp := mustGet(t, server, "/trpc/test")
	resp.Body.Close()
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
		return func(ctx context.Context, input json.RawMessage) (any, error) {
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
		resp.Body.Close()
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
		return func(ctx context.Context, input json.RawMessage) (any, error) {
			return next(context.WithValue(ctx, ctxKey("user"), "admin"), input)
		}
	})
	trpcgo.VoidQuery(router, "whoami", func(ctx context.Context) (string, error) {
		return ctx.Value(ctxKey("user")).(string), nil
	})

	server := newTestServer(t, router.Handler("/trpc"))

	resp := mustGet(t, server, "/trpc/whoami")
	if resp.StatusCode != 200 {
		resp.Body.Close()
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
	resp.Body.Close()

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
	resp.Body.Close()

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
		resp.Body.Close()
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if got := resultScalar(t, decodeJSON(t, resp)); got != "req-42" {
		t.Fatalf("result.data = %v, want req-42", got)
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
		resp.Body.Close()
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
		resp.Body.Close()
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
			defer resp.Body.Close()
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
			defer resp.Body.Close()

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
	defer resp.Body.Close()

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
	defer resp.Body.Close()

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
	defer resp.Body.Close()

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
	defer resp.Body.Close()

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
	defer resp.Body.Close()

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
		trpcgo.WithSSEMaxDuration(500 * time.Millisecond),
		trpcgo.WithSSEPingInterval(100 * time.Millisecond),
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
	defer resp.Body.Close()

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
	defer resp.Body.Close()

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
	defer resp.Body.Close()

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
	defer resp.Body.Close()

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
	hasData := false
	for _, line := range lines {
		if line == "data: 42" {
			hasData = true
			break
		}
	}
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
		defer resp.Body.Close()

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
		defer resp.Body.Close()

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
	defer resp.Body.Close()

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
			return func(ctx context.Context, input json.RawMessage) (any, error) {
				// Append to execution trace in context.
				prev, _ := ctx.Value(ctxKey("trace")).([]string)
				ctx = context.WithValue(ctx, ctxKey("trace"), append(prev, name))
				return next(ctx, input)
			}
		}
	}

	t.Run("left to right ordering", func(t *testing.T) {
		combined := trpcgo.Chain(makeMW("a"), makeMW("b"), makeMW("c"))

		handler := combined(func(ctx context.Context, input json.RawMessage) (any, error) {
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

		handler := combined(func(ctx context.Context, input json.RawMessage) (any, error) {
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

		handler := combined(func(ctx context.Context, input json.RawMessage) (any, error) {
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
			return func(ctx context.Context, input json.RawMessage) (any, error) {
				return nil, trpcgo.NewError(trpcgo.CodeUnauthorized, "blocked")
			}
		}
		afterBlocker := makeMW("should-not-run")

		combined := trpcgo.Chain(blocker, afterBlocker)
		handler := combined(func(ctx context.Context, input json.RawMessage) (any, error) {
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
			return func(ctx context.Context, input json.RawMessage) (any, error) {
				return next(context.WithValue(ctx, ctxKey("user"), "admin"), input)
			}
		}
		setRole := func(next trpcgo.HandlerFunc) trpcgo.HandlerFunc {
			return func(ctx context.Context, input json.RawMessage) (any, error) {
				user := ctx.Value(ctxKey("user")).(string)
				return next(context.WithValue(ctx, ctxKey("role"), user+"-superuser"), input)
			}
		}

		combined := trpcgo.Chain(setUser, setRole)
		handler := combined(func(ctx context.Context, input json.RawMessage) (any, error) {
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
			resp.Body.Close()
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
			resp.Body.Close()
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
			resp.Body.Close()
			t.Fatalf("body at limit: status = %d, want 200 (body len=%d)", resp.StatusCode, len(body))
		}
		resp.Body.Close()
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
	defer resp.Body.Close()

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
