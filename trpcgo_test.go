package trpcgo_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/befabri/trpcgo"
)

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

func resultScalar(t *testing.T, envelope map[string]any) any {
	t.Helper()
	r, ok := envelope["result"].(map[string]any)
	if !ok {
		t.Fatalf("expected result envelope, got keys %v", keys(envelope))
	}
	return r["data"]
}

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
