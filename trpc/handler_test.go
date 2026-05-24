package trpc_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

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
	r := setupRouter(t)
	h := trpc.NewHandler(r, "/trpc")

	req := httptest.NewRequest(http.MethodPut, "/trpc/echo", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
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

func TestHandler_BatchDisabled(t *testing.T) {
	r := trpcgo.NewRouter(trpcgo.WithBatching(false))
	if err := trpcgo.VoidQuery(r, "ping", func(ctx context.Context) (string, error) {
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
