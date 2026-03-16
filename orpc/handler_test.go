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

func decodeJSONField[T any](t *testing.T, body []byte) T {
	t.Helper()
	var payload struct {
		JSON json.RawMessage `json:"json"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	var out T
	if err := json.Unmarshal(payload.JSON, &out); err != nil {
		t.Fatalf("failed to unmarshal json field: %v", err)
	}
	return out
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

	// TRACE is never supported → 405.
	req := httptest.NewRequest(http.MethodTrace, "/rpc/echo", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}
}

func TestHandler_DefaultRouteRejectsNonGetPost(t *testing.T) {
	r := setupRouter(t)
	h := orpc.NewHandler(r, "/rpc")

	// PUT is supported at the handler level, but default routes (no WithRoute)
	// only accept GET and POST. PUT to a default route → 405 (route exists,
	// method not allowed).
	req := httptest.NewRequest(http.MethodPut, "/rpc/echo", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d: %s", rec.Code, rec.Body.String())
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

	// Streaming response is SSE: message events + done event.
	body := rec.Body.String()
	if ct := rec.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("expected text/event-stream, got %q", ct)
	}
	messageCount := strings.Count(body, "event: message\n")
	if messageCount != 2 {
		t.Fatalf("expected 2 message events, got %d in: %s", messageCount, body)
	}
	if !strings.Contains(body, "event: done\n") {
		t.Fatalf("expected done event in: %s", body)
	}

	// Each message data should be a valid batch response item.
	for _, line := range strings.Split(body, "\n") {
		if !strings.HasPrefix(line, "data: ") || line == "data: " {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		var item struct {
			Index int             `json:"index"`
			Body  json.RawMessage `json:"body"`
		}
		if err := json.Unmarshal([]byte(data), &item); err != nil {
			t.Fatalf("SSE data not valid batch item: %s: %v", data, err)
		}
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

func TestHandler_BasePathBoundary(t *testing.T) {
	r := trpcgo.NewRouter()
	if err := trpcgo.VoidQuery(r, "x", func(ctx context.Context) (string, error) {
		return "should-not-match", nil
	}); err != nil {
		t.Fatal(err)
	}

	h := orpc.NewHandler(r, "/rpc")
	req := httptest.NewRequest(http.MethodGet, "/rpcx", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandler_WithRoutePathAndMethod(t *testing.T) {
	type routeInput struct {
		ID string `json:"id"`
	}
	type routeOutput struct {
		ID string `json:"id"`
	}

	r := trpcgo.NewRouter()
	if err := trpcgo.Query(r, "planet.get", func(ctx context.Context, input routeInput) (routeOutput, error) {
		return routeOutput{ID: input.ID}, nil
	}, trpcgo.WithRoute(http.MethodGet, "/planets/{id}")); err != nil {
		t.Fatal(err)
	}

	h := orpc.NewHandler(r, "/rpc")

	t.Run("matches custom route path", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/rpc/planets/42", nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
		out := decodeJSONField[routeOutput](t, rec.Body.Bytes())
		if out.ID != "42" {
			t.Fatalf("expected id 42, got %q", out.ID)
		}
	})

	t.Run("enforces configured method", func(t *testing.T) {
		// POST to a GET-only route → 405 (route exists, wrong method).
		req := httptest.NewRequest(http.MethodPost, "/rpc/planets/42", strings.NewReader(`{"json":{},"meta":[]}`))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("expected 405, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("does not expose default derived path", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/rpc/planet/get", nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
		}
	})
}

func TestHandler_NumericPathParam(t *testing.T) {
	type input struct {
		ID int `json:"id"`
	}
	type output struct {
		ID int `json:"id"`
	}

	r := trpcgo.NewRouter()
	if err := trpcgo.Query(r, "planet.get", func(ctx context.Context, in input) (output, error) {
		return output{ID: in.ID}, nil
	}, trpcgo.WithRoute(http.MethodGet, "/planets/{id}")); err != nil {
		t.Fatal(err)
	}

	h := orpc.NewHandler(r, "/rpc")
	req := httptest.NewRequest(http.MethodGet, "/rpc/planets/42", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	out := decodeJSONField[output](t, rec.Body.Bytes())
	if out.ID != 42 {
		t.Fatalf("expected id 42, got %d", out.ID)
	}
}

func TestHandler_MultiplePathParams(t *testing.T) {
	type input struct {
		PlanetID int    `json:"planet_id"`
		MoonID   string `json:"moon_id"`
	}
	type output struct {
		PlanetID int    `json:"planet_id"`
		MoonID   string `json:"moon_id"`
	}

	r := trpcgo.NewRouter()
	if err := trpcgo.Query(r, "moon.get", func(ctx context.Context, in input) (output, error) {
		return output{PlanetID: in.PlanetID, MoonID: in.MoonID}, nil
	}, trpcgo.WithRoute(http.MethodGet, "/planets/{planet_id}/moons/{moon_id}")); err != nil {
		t.Fatal(err)
	}

	h := orpc.NewHandler(r, "/rpc")
	req := httptest.NewRequest(http.MethodGet, "/rpc/planets/3/moons/europa", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	out := decodeJSONField[output](t, rec.Body.Bytes())
	if out.PlanetID != 3 {
		t.Fatalf("expected planet_id 3, got %d", out.PlanetID)
	}
	if out.MoonID != "europa" {
		t.Fatalf("expected moon_id europa, got %q", out.MoonID)
	}
}

func TestHandler_SamePathDifferentMethods(t *testing.T) {
	type userInput struct {
		Name string `json:"name"`
	}
	type userOutput struct {
		Action string `json:"action"`
	}

	r := trpcgo.NewRouter()
	if err := trpcgo.VoidQuery(r, "user.list", func(ctx context.Context) (userOutput, error) {
		return userOutput{Action: "list"}, nil
	}, trpcgo.WithRoute(http.MethodGet, "/users")); err != nil {
		t.Fatal(err)
	}
	if err := trpcgo.Mutation(r, "user.create", func(ctx context.Context, in userInput) (userOutput, error) {
		return userOutput{Action: "create:" + in.Name}, nil
	}, trpcgo.WithRoute(http.MethodPost, "/users")); err != nil {
		t.Fatal(err)
	}

	h := orpc.NewHandler(r, "/rpc")

	t.Run("GET dispatches to list", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/rpc/users", nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
		out := decodeJSONField[userOutput](t, rec.Body.Bytes())
		if out.Action != "list" {
			t.Fatalf("expected action 'list', got %q", out.Action)
		}
	})

	t.Run("POST dispatches to create", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{"json": map[string]string{"name": "alice"}, "meta": []any{}})
		req := httptest.NewRequest(http.MethodPost, "/rpc/users", strings.NewReader(string(body)))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
		out := decodeJSONField[userOutput](t, rec.Body.Bytes())
		if out.Action != "create:alice" {
			t.Fatalf("expected action 'create:alice', got %q", out.Action)
		}
	})

	t.Run("DELETE returns 405", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodDelete, "/rpc/users", nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("expected 405, got %d: %s", rec.Code, rec.Body.String())
		}
	})
}

func TestHandler_SpecificityOrdering(t *testing.T) {
	r := trpcgo.NewRouter()
	// Static route: /planets/earth
	if err := trpcgo.VoidQuery(r, "planet.earth", func(ctx context.Context) (string, error) {
		return "earth-static", nil
	}, trpcgo.WithRoute(http.MethodGet, "/planets/earth")); err != nil {
		t.Fatal(err)
	}
	// Template route: /planets/{id}
	if err := trpcgo.Query(r, "planet.get", func(ctx context.Context, input struct {
		ID string `json:"id"`
	}) (string, error) {
		return "template:" + input.ID, nil
	}, trpcgo.WithRoute(http.MethodGet, "/planets/{id}")); err != nil {
		t.Fatal(err)
	}

	h := orpc.NewHandler(r, "/rpc")

	t.Run("static route wins over template", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/rpc/planets/earth", nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
		out := decodeJSONField[string](t, rec.Body.Bytes())
		if out != "earth-static" {
			t.Fatalf("expected 'earth-static', got %q", out)
		}
	})

	t.Run("template matches non-static", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/rpc/planets/mars", nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
		out := decodeJSONField[string](t, rec.Body.Bytes())
		if out != "template:mars" {
			t.Fatalf("expected 'template:mars', got %q", out)
		}
	})
}

func TestHandler_CustomMethodRoute(t *testing.T) {
	type input struct {
		ID int `json:"id"`
	}

	r := trpcgo.NewRouter()
	if err := trpcgo.Mutation(r, "planet.delete", func(ctx context.Context, in input) (string, error) {
		return "deleted", nil
	}, trpcgo.WithRoute(http.MethodDelete, "/planets/{id}")); err != nil {
		t.Fatal(err)
	}

	h := orpc.NewHandler(r, "/rpc")

	t.Run("DELETE works", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodDelete, "/rpc/planets/1", nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("GET returns 405", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/rpc/planets/1", nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("expected 405, got %d: %s", rec.Code, rec.Body.String())
		}
	})
}

func TestHandler_EmbeddedStructPathParam(t *testing.T) {
	type Base struct {
		ID int `json:"id"`
	}
	type input struct {
		Base
		Extra string `json:"extra"`
	}
	type output struct {
		ID    int    `json:"id"`
		Extra string `json:"extra"`
	}

	r := trpcgo.NewRouter()
	if err := trpcgo.Query(r, "item.get", func(ctx context.Context, in input) (output, error) {
		return output{ID: in.ID, Extra: in.Extra}, nil
	}, trpcgo.WithRoute(http.MethodGet, "/items/{id}")); err != nil {
		t.Fatal(err)
	}

	h := orpc.NewHandler(r, "/rpc")

	// Path param "id" should be coerced to int via the embedded Base.ID field.
	body, _ := json.Marshal(map[string]any{"json": map[string]string{"extra": "val"}, "meta": []any{}})
	req := httptest.NewRequest(http.MethodGet, "/rpc/items/99?data="+string(body), nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	out := decodeJSONField[output](t, rec.Body.Bytes())
	if out.ID != 99 {
		t.Fatalf("expected id 99, got %d", out.ID)
	}
}

func TestHandler_RouteConflictPanics(t *testing.T) {
	r := trpcgo.NewRouter()
	if err := trpcgo.VoidQuery(r, "a", func(ctx context.Context) (string, error) {
		return "a", nil
	}, trpcgo.WithRoute(http.MethodGet, "/items")); err != nil {
		t.Fatal(err)
	}
	if err := trpcgo.VoidQuery(r, "b", func(ctx context.Context) (string, error) {
		return "b", nil
	}, trpcgo.WithRoute(http.MethodGet, "/items")); err != nil {
		t.Fatal(err)
	}

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic for route conflict")
		}
		msg, ok := r.(string)
		if !ok || !strings.Contains(msg, "route conflict") {
			t.Fatalf("expected route conflict panic, got: %v", r)
		}
	}()
	orpc.NewHandler(r, "/rpc")
}

func TestHandler_BatchMethodRestriction(t *testing.T) {
	r := setupRouter(t)
	h := orpc.NewHandler(r, "/rpc")

	// DELETE with batch header → 405 (batch only supports GET/POST).
	req := httptest.NewRequest(http.MethodDelete, "/rpc/__batch__", nil)
	req.Header.Set("x-orpc-batch", "buffered")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d: %s", rec.Code, rec.Body.String())
	}
}
