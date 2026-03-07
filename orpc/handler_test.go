package orpc_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/befabri/trpcgo"
	"github.com/befabri/trpcgo/orpc"
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
	h := orpc.NewHandler(r, "/rpc")

	// Build GET request with ?data= containing oRPC-wrapped input.
	inputJSON, _ := json.Marshal(map[string]any{
		"json": map[string]string{"message": "world"},
		"meta": []any{},
	})
	req := httptest.NewRequest(http.MethodGet, "/rpc/echo?data="+string(inputJSON), nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var payload struct {
		JSON json.RawMessage `json:"json"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	var out echoOutput
	if err := json.Unmarshal(payload.JSON, &out); err != nil {
		t.Fatalf("failed to unmarshal json field: %v", err)
	}
	if out.Reply != "echo: world" {
		t.Fatalf("expected 'echo: world', got %q", out.Reply)
	}
}

func TestHandler_QueryPOST(t *testing.T) {
	r := setupRouter(t)
	h := orpc.NewHandler(r, "/rpc")

	body, _ := json.Marshal(map[string]any{
		"json": map[string]string{"message": "test"},
		"meta": []any{},
	})
	req := httptest.NewRequest(http.MethodPost, "/rpc/echo", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var payload struct {
		JSON json.RawMessage `json:"json"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	var out echoOutput
	if err := json.Unmarshal(payload.JSON, &out); err != nil {
		t.Fatalf("failed to unmarshal json field: %v", err)
	}
	if out.Reply != "echo: test" {
		t.Fatalf("expected 'echo: test', got %q", out.Reply)
	}
}

func TestHandler_VoidQuery(t *testing.T) {
	r := setupRouter(t)
	h := orpc.NewHandler(r, "/rpc")

	req := httptest.NewRequest(http.MethodGet, "/rpc/ping", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var payload struct {
		JSON json.RawMessage `json:"json"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	var out string
	if err := json.Unmarshal(payload.JSON, &out); err != nil {
		t.Fatalf("failed to unmarshal json field: %v", err)
	}
	if out != "pong" {
		t.Fatalf("expected 'pong', got %q", out)
	}
}

func TestHandler_MutationPOST(t *testing.T) {
	r := setupRouter(t)
	h := orpc.NewHandler(r, "/rpc")

	body, _ := json.Marshal(map[string]any{
		"json": map[string]string{"message": "alice"},
		"meta": []any{},
	})
	req := httptest.NewRequest(http.MethodPost, "/rpc/greet", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var payload struct {
		JSON json.RawMessage `json:"json"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	var out echoOutput
	if err := json.Unmarshal(payload.JSON, &out); err != nil {
		t.Fatalf("failed to unmarshal json field: %v", err)
	}
	if out.Reply != "hello, alice" {
		t.Fatalf("expected 'hello, alice', got %q", out.Reply)
	}
}

func TestHandler_NotFound(t *testing.T) {
	r := setupRouter(t)
	h := orpc.NewHandler(r, "/rpc")

	req := httptest.NewRequest(http.MethodGet, "/rpc/nonexistent", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestHandler_MethodNotAllowed(t *testing.T) {
	r := setupRouter(t)
	h := orpc.NewHandler(r, "/rpc")

	req := httptest.NewRequest(http.MethodPut, "/rpc/echo", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
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

	h := orpc.NewHandler(r, "/rpc")
	req := httptest.NewRequest(http.MethodGet, "/rpc/counter", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("expected text/event-stream, got %q", ct)
	}

	body := rec.Body.String()

	// Should start with initial comment.
	if !strings.HasPrefix(body, ": \n\n") {
		t.Fatalf("expected initial comment, got: %q", body[:min(40, len(body))])
	}

	// Should contain 3 message events and a done event.
	messageCount := strings.Count(body, "event: message\n")
	if messageCount != 3 {
		t.Fatalf("expected 3 message events, got %d in: %s", messageCount, body)
	}
	if !strings.Contains(body, "event: done\n") {
		t.Fatalf("expected done event in: %s", body)
	}

	// Verify data items are oRPC-wrapped.
	lines := strings.Split(body, "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "data: ") && line != "data: " {
			data := strings.TrimPrefix(line, "data: ")
			var payload struct {
				JSON json.RawMessage `json:"json"`
			}
			if err := json.Unmarshal([]byte(data), &payload); err != nil {
				t.Fatalf("SSE data not valid oRPC payload: %s: %v", data, err)
			}
		}
	}
}

func TestHandler_PathMapping(t *testing.T) {
	r := trpcgo.NewRouter()
	if err := trpcgo.VoidQuery(r, "planet.list", func(ctx context.Context) (string, error) {
		return "planets", nil
	}); err != nil {
		t.Fatal(err)
	}

	h := orpc.NewHandler(r, "/api")
	req := httptest.NewRequest(http.MethodGet, "/api/planet/list", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var payload struct {
		JSON json.RawMessage `json:"json"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	var out string
	_ = json.Unmarshal(payload.JSON, &out)
	if out != "planets" {
		t.Fatalf("expected 'planets', got %q", out)
	}
}

func TestHandler_BareJSONFallback(t *testing.T) {
	r := setupRouter(t)
	h := orpc.NewHandler(r, "/rpc")

	// Send plain JSON (not oRPC-wrapped) — should work via fallback.
	body := `{"message":"bare"}`
	req := httptest.NewRequest(http.MethodPost, "/rpc/echo", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var payload struct {
		JSON json.RawMessage `json:"json"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	var out echoOutput
	_ = json.Unmarshal(payload.JSON, &out)
	if out.Reply != "echo: bare" {
		t.Fatalf("expected 'echo: bare', got %q", out.Reply)
	}
}

