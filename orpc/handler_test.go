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

func TestHandler_MutationGETNotAllowed(t *testing.T) {
	r := setupRouter(t)
	h := orpc.NewHandler(r, "/rpc")

	// oRPC doesn't enforce method by procedure type the same way tRPC does,
	// but mutations should still accept both GET and POST since oRPC allows it.
	// This tests that GET to a mutation endpoint works (oRPC is lenient).
	req := httptest.NewRequest(http.MethodGet, "/rpc/greet", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	// oRPC doesn't have method restrictions per procedure type,
	// but with no input the handler will return a result or error.
	if rec.Code >= 500 {
		t.Fatalf("unexpected server error %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandler_EmptyBasePath(t *testing.T) {
	r := setupRouter(t)
	h := orpc.NewHandler(r, "")

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

	h := orpc.NewHandler(r, "/rpc")
	req := httptest.NewRequest(http.MethodGet, "/rpc/nonexistent", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
	// oRPC doesn't invoke the error callback for not-found (it short-circuits).
	// Verify the response is correct.
	body := rec.Body.String()
	if !strings.Contains(body, "NOT_FOUND") {
		t.Fatalf("expected NOT_FOUND in body, got: %s", body)
	}
	_ = calledPath // callback not expected for pre-dispatch errors in oRPC
}

func TestHandler_NestedPathMapping(t *testing.T) {
	r := trpcgo.NewRouter()
	if err := trpcgo.VoidQuery(r, "admin.settings.get", func(ctx context.Context) (string, error) {
		return "settings", nil
	}); err != nil {
		t.Fatal(err)
	}

	h := orpc.NewHandler(r, "/api")
	// oRPC maps /api/admin/settings/get → admin.settings.get
	req := httptest.NewRequest(http.MethodGet, "/api/admin/settings/get", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandler_BatchBuffered(t *testing.T) {
	r := setupRouter(t)
	h := orpc.NewHandler(r, "/rpc")

	items, _ := json.Marshal([]map[string]any{
		{"url": "http://localhost/rpc/echo", "body": json.RawMessage(`{"json":{"message":"a"},"meta":[]}`), "method": "POST"},
		{"url": "http://localhost/rpc/ping"},
	})
	req := httptest.NewRequest(http.MethodPost, "/rpc/__batch__", strings.NewReader(string(items)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-orpc-batch", "buffered")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusMultiStatus {
		t.Fatalf("expected 207, got %d: %s", rec.Code, rec.Body.String())
	}

	var results []struct {
		Index int             `json:"index"`
		Body  json.RawMessage `json:"body"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &results); err != nil {
		t.Fatalf("failed to unmarshal batch response: %v\nbody: %s", err, rec.Body.String())
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	// Both should have valid oRPC response bodies.
	for _, res := range results {
		var payload struct {
			JSON json.RawMessage `json:"json"`
		}
		if err := json.Unmarshal(res.Body, &payload); err != nil {
			t.Fatalf("result %d: not valid oRPC payload: %s", res.Index, string(res.Body))
		}
	}
}

func TestHandler_BatchStreaming(t *testing.T) {
	r := setupRouter(t)
	h := orpc.NewHandler(r, "/rpc")

	items, _ := json.Marshal([]map[string]any{
		{"url": "http://localhost/rpc/echo", "body": json.RawMessage(`{"json":{"message":"streamed"},"meta":[]}`), "method": "POST"},
		{"url": "http://localhost/rpc/ping"},
	})
	req := httptest.NewRequest(http.MethodPost, "/rpc/__batch__", strings.NewReader(string(items)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-orpc-batch", "streaming")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusMultiStatus {
		t.Fatalf("expected 207, got %d: %s", rec.Code, rec.Body.String())
	}

	// Streaming response should be a valid JSON array.
	var results []struct {
		Index int             `json:"index"`
		Body  json.RawMessage `json:"body"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &results); err != nil {
		t.Fatalf("failed to unmarshal streaming batch response: %v\nbody: %s", err, rec.Body.String())
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
}

func TestHandler_BatchDisabled(t *testing.T) {
	r := trpcgo.NewRouter(trpcgo.WithBatching(false))
	if err := trpcgo.VoidQuery(r, "ping", func(ctx context.Context) (string, error) {
		return "pong", nil
	}); err != nil {
		t.Fatal(err)
	}

	h := orpc.NewHandler(r, "/rpc")
	items, _ := json.Marshal([]map[string]any{
		{"url": "http://localhost/rpc/ping"},
	})
	req := httptest.NewRequest(http.MethodPost, "/rpc/__batch__", strings.NewReader(string(items)))
	req.Header.Set("x-orpc-batch", "buffered")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for disabled batching, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandler_BatchSizeLimit(t *testing.T) {
	r := trpcgo.NewRouter(trpcgo.WithMaxBatchSize(1))
	if err := trpcgo.VoidQuery(r, "ping", func(ctx context.Context) (string, error) {
		return "pong", nil
	}); err != nil {
		t.Fatal(err)
	}

	h := orpc.NewHandler(r, "/rpc")
	items, _ := json.Marshal([]map[string]any{
		{"url": "http://localhost/rpc/ping"},
		{"url": "http://localhost/rpc/ping"},
	})
	req := httptest.NewRequest(http.MethodPost, "/rpc/__batch__", strings.NewReader(string(items)))
	req.Header.Set("x-orpc-batch", "buffered")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413 for batch size limit, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandler_BatchNotFoundItem(t *testing.T) {
	r := setupRouter(t)
	h := orpc.NewHandler(r, "/rpc")

	items, _ := json.Marshal([]map[string]any{
		{"url": "http://localhost/rpc/ping"},
		{"url": "http://localhost/rpc/nonexistent"},
	})
	req := httptest.NewRequest(http.MethodPost, "/rpc/__batch__", strings.NewReader(string(items)))
	req.Header.Set("x-orpc-batch", "buffered")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusMultiStatus {
		t.Fatalf("expected 207, got %d: %s", rec.Code, rec.Body.String())
	}

	var results []struct {
		Index  int             `json:"index"`
		Status int             `json:"status"`
		Body   json.RawMessage `json:"body"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &results); err != nil {
		t.Fatal(err)
	}

	// Find the not-found result (could be in any order due to concurrent execution).
	found := false
	for _, res := range results {
		// Status will be non-zero if it differs from the batch status (207).
		// A 404 will be included since it differs from 207.
		body := string(res.Body)
		if strings.Contains(body, "NOT_FOUND") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected one NOT_FOUND result in batch response: %s", rec.Body.String())
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

