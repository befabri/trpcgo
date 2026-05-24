package trpcgo_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/befabri/trpcgo"
	"github.com/befabri/trpcgo/trpc"
)

func TestQuery(t *testing.T) {
	server := newTestServer(t, trpc.NewHandler(setupRouter(), "/trpc"))

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
	server := newTestServer(t, trpc.NewHandler(setupRouter(), "/trpc"))

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

func TestBasePathBoundaryEnforced(t *testing.T) {
	tests := []struct {
		name     string
		basePath string
	}{
		{name: "base without trailing slash", basePath: "/trpc"},
		{name: "base with trailing slash", basePath: "/trpc/"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router := trpcgo.NewRouter()
			trpcgo.VoidQuery(router, "hello", func(ctx context.Context) (string, error) {
				return "ok", nil
			})
			trpcgo.VoidQuery(router, "x", func(ctx context.Context) (string, error) {
				return "should-not-match", nil
			})

			server := newTestServer(t, trpc.NewHandler(router, tt.basePath))

			// Valid request under the configured base path.
			valid := mustGet(t, server, "/trpc/hello")
			if valid.StatusCode != 200 {
				_ = valid.Body.Close()
				t.Fatalf("valid status = %d, want 200", valid.StatusCode)
			}
			if got := resultScalar(t, decodeJSON(t, valid)); got != "ok" {
				t.Fatalf("valid result.data = %v, want ok", got)
			}

			// Invalid path that shares the same prefix but not a segment boundary.
			// Before the fix, /trpcx incorrectly matched basePath /trpc and invoked
			// procedure path "x".
			invalid := mustGet(t, server, "/trpcx")
			if invalid.StatusCode != 404 {
				_ = invalid.Body.Close()
				t.Fatalf("invalid status = %d, want 404", invalid.StatusCode)
			}
			if code := errorData(t, decodeJSON(t, invalid))["code"]; code != "NOT_FOUND" {
				t.Fatalf("invalid error.data.code = %v, want NOT_FOUND", code)
			}

			// Batch request must follow the same boundary rule.
			invalidBatch := mustGet(t, server, "/trpcx?batch=1")
			if invalidBatch.StatusCode != 404 {
				_ = invalidBatch.Body.Close()
				t.Fatalf("invalid batch status = %d, want 404", invalidBatch.StatusCode)
			}
			if code := errorData(t, decodeJSON(t, invalidBatch))["code"]; code != "NOT_FOUND" {
				t.Fatalf("invalid batch error.data.code = %v, want NOT_FOUND", code)
			}
		})
	}
}

func TestRootBasePathBehavior(t *testing.T) {
	router := trpcgo.NewRouter()
	trpcgo.VoidQuery(router, "hello", func(ctx context.Context) (string, error) {
		return "ok", nil
	})

	server := newTestServer(t, trpc.NewHandler(router, "/"))

	// Root base path should expose procedures directly at /<path>.
	resp := mustGet(t, server, "/hello")
	if resp.StatusCode != 200 {
		_ = resp.Body.Close()
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if got := resultScalar(t, decodeJSON(t, resp)); got != "ok" {
		t.Fatalf("result.data = %v, want ok", got)
	}

	// Requests with extra segments should not match.
	notFound := mustGet(t, server, "/trpc/hello")
	if notFound.StatusCode != 404 {
		_ = notFound.Body.Close()
		t.Fatalf("status = %d, want 404", notFound.StatusCode)
	}
	if code := errorData(t, decodeJSON(t, notFound))["code"]; code != "NOT_FOUND" {
		t.Fatalf("error.data.code = %v, want NOT_FOUND", code)
	}
}

func TestMutation(t *testing.T) {
	server := newTestServer(t, trpc.NewHandler(setupRouter(), "/trpc"))

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

func TestMethodValidation(t *testing.T) {
	server := newTestServer(t, trpc.NewHandler(setupRouter(), "/trpc"))

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
	server := newTestServer(t, trpc.NewHandler(router, "/trpc"))

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

func TestNonTRPCErrorWrappedAs500(t *testing.T) {
	router := trpcgo.NewRouter()
	trpcgo.VoidQuery(router, "fail", func(ctx context.Context) (string, error) {
		return "", fmt.Errorf("unexpected database error")
	})
	server := newTestServer(t, trpc.NewHandler(router, "/trpc"))

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

func TestNoBasePath(t *testing.T) {
	router := trpcgo.NewRouter()
	trpcgo.VoidQuery(router, "hello", func(ctx context.Context) (string, error) {
		return "hi", nil
	})
	server := newTestServer(t, trpc.NewHandler(router, ""))

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
	server := newTestServer(t, trpc.NewHandler(router, "/trpc"))

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
	server := newTestServer(t, trpc.NewHandler(setupRouter(), "/trpc"))

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

func TestConcurrentRequests(t *testing.T) {
	server := newTestServer(t, trpc.NewHandler(setupRouter(), "/trpc"))

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

func TestUnknownErrorCodeDefaults(t *testing.T) {
	unknown := trpcgo.ErrorCode(-99999)
	if got := trpcgo.HTTPStatusFromCode(unknown); got != 500 {
		t.Errorf("HTTPStatusFromCode(unknown) = %d, want 500", got)
	}
	if got := trpcgo.NameFromCode(unknown); got != "INTERNAL_SERVER_ERROR" {
		t.Errorf("NameFromCode(unknown) = %q, want INTERNAL_SERVER_ERROR", got)
	}
}

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

func TestMaxBodySizeEnforced(t *testing.T) {
	router := trpcgo.NewRouter(trpcgo.WithMaxBodySize(50))
	trpcgo.Mutation(router, "echo", func(ctx context.Context, input struct {
		Data string `json:"data"`
	}) (string, error) {
		return input.Data, nil
	})

	server := newTestServer(t, trpc.NewHandler(router, "/trpc"))

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

func TestDuplicateRegistrationReturnsError(t *testing.T) {
	router := trpcgo.NewRouter()
	err := trpcgo.VoidQuery(router, "hello", func(ctx context.Context) (string, error) {
		return "hi", nil
	})
	if err != nil {
		t.Fatalf("first registration failed: %v", err)
	}

	err = trpcgo.VoidQuery(router, "hello", func(ctx context.Context) (string, error) {
		return "hi again", nil
	})
	if err == nil {
		t.Fatal("expected duplicate registration error")
	}
	if !strings.Contains(err.Error(), "hello") || !strings.Contains(err.Error(), "already registered") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestMustRegistrationPanicsOnDuplicate(t *testing.T) {
	tests := []struct {
		name     string
		register func(r *trpcgo.Router)
		dup      func(r *trpcgo.Router)
		wantMsg  string
	}{
		{
			name: "MustQuery",
			register: func(r *trpcgo.Router) {
				trpcgo.MustQuery(r, "a.get", func(ctx context.Context, _ struct{}) (string, error) { return "", nil })
			},
			dup: func(r *trpcgo.Router) {
				trpcgo.MustQuery(r, "a.get", func(ctx context.Context, _ struct{}) (string, error) { return "", nil })
			},
			wantMsg: `MustQuery "a.get"`,
		},
		{
			name: "MustVoidQuery",
			register: func(r *trpcgo.Router) {
				trpcgo.MustVoidQuery(r, "b.get", func(ctx context.Context) (string, error) { return "", nil })
			},
			dup: func(r *trpcgo.Router) {
				trpcgo.MustVoidQuery(r, "b.get", func(ctx context.Context) (string, error) { return "", nil })
			},
			wantMsg: `MustVoidQuery "b.get"`,
		},
		{
			name: "MustMutation",
			register: func(r *trpcgo.Router) {
				trpcgo.MustMutation(r, "c.create", func(ctx context.Context, _ struct{}) (string, error) { return "", nil })
			},
			dup: func(r *trpcgo.Router) {
				trpcgo.MustMutation(r, "c.create", func(ctx context.Context, _ struct{}) (string, error) { return "", nil })
			},
			wantMsg: `MustMutation "c.create"`,
		},
		{
			name: "MustVoidMutation",
			register: func(r *trpcgo.Router) {
				trpcgo.MustVoidMutation(r, "d.reset", func(ctx context.Context) (string, error) { return "", nil })
			},
			dup: func(r *trpcgo.Router) {
				trpcgo.MustVoidMutation(r, "d.reset", func(ctx context.Context) (string, error) { return "", nil })
			},
			wantMsg: `MustVoidMutation "d.reset"`,
		},
		{
			name: "MustSubscribe",
			register: func(r *trpcgo.Router) {
				trpcgo.MustSubscribe(r, "e.stream", func(ctx context.Context, _ struct{}) (<-chan string, error) {
					return make(chan string), nil
				})
			},
			dup: func(r *trpcgo.Router) {
				trpcgo.MustSubscribe(r, "e.stream", func(ctx context.Context, _ struct{}) (<-chan string, error) {
					return make(chan string), nil
				})
			},
			wantMsg: `MustSubscribe "e.stream"`,
		},
		{
			name: "MustVoidSubscribe",
			register: func(r *trpcgo.Router) {
				trpcgo.MustVoidSubscribe(r, "f.stream", func(ctx context.Context) (<-chan string, error) {
					return make(chan string), nil
				})
			},
			dup: func(r *trpcgo.Router) {
				trpcgo.MustVoidSubscribe(r, "f.stream", func(ctx context.Context) (<-chan string, error) {
					return make(chan string), nil
				})
			},
			wantMsg: `MustVoidSubscribe "f.stream"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := trpcgo.NewRouter()
			tt.register(r)
			defer func() {
				v := recover()
				if v == nil {
					t.Fatal("expected panic, got none")
				}
				msg, ok := v.(string)
				if !ok {
					t.Fatalf("panic value is %T: %v", v, v)
				}
				if !strings.Contains(msg, tt.wantMsg) {
					t.Fatalf("panic message %q does not contain %q", msg, tt.wantMsg)
				}
				if !strings.Contains(msg, "already registered") {
					t.Fatalf("panic message %q missing 'already registered'", msg)
				}
			}()
			tt.dup(r)
		})
	}
}

func TestMustRegistrationSucceeds(t *testing.T) {
	r := trpcgo.NewRouter()
	trpcgo.MustQuery(r, "q", func(ctx context.Context, _ struct{}) (string, error) { return "", nil })
	trpcgo.MustVoidQuery(r, "vq", func(ctx context.Context) (string, error) { return "", nil })
	trpcgo.MustMutation(r, "m", func(ctx context.Context, _ struct{}) (string, error) { return "", nil })
	trpcgo.MustVoidMutation(r, "vm", func(ctx context.Context) (string, error) { return "", nil })
	trpcgo.MustSubscribe(r, "s", func(ctx context.Context, _ struct{}) (<-chan string, error) { return make(chan string), nil })
	trpcgo.MustVoidSubscribe(r, "vs", func(ctx context.Context) (<-chan string, error) { return make(chan string), nil })
}

func TestDuplicateRegistrationAcrossProcedureKindsReturnsError(t *testing.T) {
	router := trpcgo.NewRouter()
	err := trpcgo.Query(router, "user.get", func(ctx context.Context, input struct{ ID string }) (string, error) {
		return input.ID, nil
	})
	if err != nil {
		t.Fatalf("first registration failed: %v", err)
	}

	err = trpcgo.Mutation(router, "user.get", func(ctx context.Context, input struct{ ID string }) (string, error) {
		return input.ID, nil
	})
	if err == nil {
		t.Fatal("expected duplicate registration error")
	}
	if !strings.Contains(err.Error(), "user.get") || !strings.Contains(err.Error(), "already registered") {
		t.Fatalf("unexpected error: %v", err)
	}
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
	if err := main.Merge(sub); err != nil {
		t.Fatal(err)
	}

	server := newTestServer(t, trpc.NewHandler(main, "/trpc"))

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

func TestHandlerSnapshotIsolation(t *testing.T) {
	router := trpcgo.NewRouter(trpcgo.WithMethodOverride(true))

	trpcgo.VoidQuery(router, "before", func(ctx context.Context) (string, error) {
		return "ok", nil
	})

	handler := trpc.NewHandler(router, "/trpc")
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
	handler2 := trpc.NewHandler(router, "/trpc")
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

func TestWithOutputParserCodegenReflectionUsesUnknown(t *testing.T) {
	r := trpcgo.NewRouter()
	base := trpcgo.Procedure().WithOutputParser(func(v any) (any, error) {
		u, _ := v.(User)
		return struct {
			ID string `json:"id"`
		}{ID: u.ID}, nil
	})

	trpcgo.MustVoidQuery(r, "strip", func(ctx context.Context) (User, error) {
		return User{ID: "1", Name: "Alice"}, nil
	}, base)

	outputPath := filepath.Join(t.TempDir(), "trpc.ts")
	if err := r.GenerateTS(outputPath); err != nil {
		t.Fatalf("GenerateTS failed: %v", err)
	}
	data, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	ts := string(data)

	if !strings.Contains(ts, "$Query<void, unknown>") {
		t.Errorf("expected $Query<void, unknown> for untyped WithOutputParser, got:\n%s", ts)
	}
	if strings.Contains(ts, "name: string;") {
		t.Errorf("untyped WithOutputParser should not expose handler output shape in generated TS:\n%s", ts)
	}
}
