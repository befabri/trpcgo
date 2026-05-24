package trpcgo_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/befabri/trpcgo"
	"github.com/befabri/trpcgo/trpc"
)

func TestErrorResponse(t *testing.T) {
	server := newTestServer(t, trpc.NewHandler(setupRouter(), "/trpc"))

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
	server := newTestServer(t, trpc.NewHandler(setupRouter(), "/trpc"))

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

func TestInternalErrorNotLeaked(t *testing.T) {
	var capturedErr *trpcgo.Error
	router := trpcgo.NewRouter(trpcgo.WithOnError(func(ctx context.Context, err *trpcgo.Error, path string) {
		capturedErr = err
	}))
	trpcgo.VoidQuery(router, "fail", func(ctx context.Context) (string, error) {
		return "", fmt.Errorf("secret database password: hunter2")
	})

	server := newTestServer(t, trpc.NewHandler(router, "/trpc"))

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

func TestDevModeStackTrace(t *testing.T) {
	r := trpcgo.NewRouter(trpcgo.WithDev(true))

	trpcgo.Query(r, "user.get", func(ctx context.Context, input GetUserInput) (User, error) {
		return User{}, trpcgo.NewError(trpcgo.CodeNotFound, "not found")
	})

	server := newTestServer(t, trpc.NewHandler(r, "/trpc"))
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

	server := newTestServer(t, trpc.NewHandler(r, "/trpc"))
	resp := mustGet(t, server, "/trpc/user.get?input="+url.QueryEscape(`{"id":"1"}`))
	body := decodeJSON(t, resp)
	data := errorData(t, body)

	if _, ok := data["stack"]; ok {
		t.Fatal("isDev=false should not include stack trace in error data")
	}
}

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

	server := newTestServer(t, trpc.NewHandler(router, "/trpc"))

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
		trpcgo.WithContextCreator(func(ctx context.Context, r *http.Request) context.Context {
			return context.WithValue(ctx, ctxKey("tenant"), "acme")
		}),
		trpcgo.WithErrorFormatter(func(input trpcgo.ErrorFormatterInput) any {
			gotVal, _ = input.Ctx.Value(ctxKey("tenant")).(string)
			return input.Shape // pass through default shape
		}),
	)

	trpcgo.VoidQuery(router, "fail", func(ctx context.Context) (string, error) {
		return "", trpcgo.NewError(trpcgo.CodeBadRequest, "nope")
	})

	server := newTestServer(t, trpc.NewHandler(router, "/trpc"))

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

	server := newTestServer(t, trpc.NewHandler(router, "/trpc"))

	resp := mustGet(t, server, "/trpc/nonexistent")
	_ = resp.Body.Close()

	if gotPath != "nonexistent" {
		t.Errorf("formatter path = %q, want %q", gotPath, "nonexistent")
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

	server := newTestServer(t, trpc.NewHandler(router, "/trpc"))
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

	server := newTestServer(t, trpc.NewHandler(router, "/trpc"))
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

	server := newTestServer(t, trpc.NewHandler(router, "/trpc"))

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

	server := newTestServer(t, trpc.NewHandler(router, "/trpc"))

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

func TestErrorFormatterReceivesInput(t *testing.T) {
	t.Run("query with input", func(t *testing.T) {
		var gotInput json.RawMessage
		router := trpcgo.NewRouter(trpcgo.WithErrorFormatter(func(input trpcgo.ErrorFormatterInput) any {
			gotInput = input.Input
			return input.Shape
		}))

		trpcgo.Query(router, "fail", func(ctx context.Context, in struct {
			ID string `json:"id"`
		}) (string, error) {
			return "", trpcgo.NewError(trpcgo.CodeBadRequest, "bad")
		})

		server := newTestServer(t, trpc.NewHandler(router, "/trpc"))
		resp := mustGet(t, server, `/trpc/fail?input={"id":"42"}`)
		_ = resp.Body.Close()

		if gotInput == nil {
			t.Fatal("ErrorFormatterInput.Input is nil, want raw JSON")
		}
		if string(gotInput) != `{"id":"42"}` {
			t.Errorf("ErrorFormatterInput.Input = %s, want %s", gotInput, `{"id":"42"}`)
		}
	})

	t.Run("mutation with input", func(t *testing.T) {
		var gotInput json.RawMessage
		router := trpcgo.NewRouter(trpcgo.WithErrorFormatter(func(input trpcgo.ErrorFormatterInput) any {
			gotInput = input.Input
			return input.Shape
		}))

		trpcgo.Mutation(router, "fail", func(ctx context.Context, in struct {
			Name string `json:"name"`
		}) (string, error) {
			return "", trpcgo.NewError(trpcgo.CodeBadRequest, "bad")
		})

		server := newTestServer(t, trpc.NewHandler(router, "/trpc"))
		resp := mustPost(t, server, "/trpc/fail", `{"name":"alice"}`)
		_ = resp.Body.Close()

		if gotInput == nil {
			t.Fatal("ErrorFormatterInput.Input is nil, want raw JSON")
		}
		if string(gotInput) != `{"name":"alice"}` {
			t.Errorf("ErrorFormatterInput.Input = %s, want %s", gotInput, `{"name":"alice"}`)
		}
	})

	t.Run("void procedure has nil input", func(t *testing.T) {
		var gotInput json.RawMessage
		formatterCalled := false
		router := trpcgo.NewRouter(trpcgo.WithErrorFormatter(func(input trpcgo.ErrorFormatterInput) any {
			formatterCalled = true
			gotInput = input.Input
			return input.Shape
		}))

		trpcgo.VoidQuery(router, "fail", func(ctx context.Context) (string, error) {
			return "", trpcgo.NewError(trpcgo.CodeBadRequest, "bad")
		})

		server := newTestServer(t, trpc.NewHandler(router, "/trpc"))
		resp := mustGet(t, server, "/trpc/fail")
		_ = resp.Body.Close()

		if !formatterCalled {
			t.Fatal("error formatter was not called")
		}
		if gotInput != nil {
			t.Errorf("ErrorFormatterInput.Input = %s, want nil for void procedure", gotInput)
		}
	})

	t.Run("not found still has input from request", func(t *testing.T) {
		var gotInput json.RawMessage
		formatterCalled := false
		router := trpcgo.NewRouter(trpcgo.WithErrorFormatter(func(input trpcgo.ErrorFormatterInput) any {
			formatterCalled = true
			gotInput = input.Input
			return input.Shape
		}))

		trpcgo.VoidQuery(router, "exists", func(ctx context.Context) (string, error) {
			return "ok", nil
		})

		server := newTestServer(t, trpc.NewHandler(router, "/trpc"))
		resp := mustGet(t, server, `/trpc/missing?input={"x":1}`)
		_ = resp.Body.Close()

		if !formatterCalled {
			t.Fatal("error formatter was not called for not-found")
		}
		if string(gotInput) != `{"x":1}` {
			t.Errorf("ErrorFormatterInput.Input = %s, want %s", gotInput, `{"x":1}`)
		}
	})
}

func TestErrorFormatterInputInJSONLBatch(t *testing.T) {
	var inputs []json.RawMessage
	var mu sync.Mutex

	router := trpcgo.NewRouter(
		trpcgo.WithBatching(true),
		trpcgo.WithErrorFormatter(func(input trpcgo.ErrorFormatterInput) any {
			mu.Lock()
			inputs = append(inputs, input.Input)
			mu.Unlock()
			return input.Shape
		}),
	)

	trpcgo.Query(router, "fail", func(ctx context.Context, in struct {
		ID string `json:"id"`
	}) (string, error) {
		return "", trpcgo.NewError(trpcgo.CodeBadRequest, "bad")
	})

	server := newTestServer(t, trpc.NewHandler(router, "/trpc"))

	req, _ := http.NewRequest("POST", server.URL+"/trpc/fail,fail?batch=1", strings.NewReader(`{"0":{"id":"a"},"1":{"id":"b"}}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("trpc-accept", "application/jsonl")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.ReadAll(resp.Body)
	_ = resp.Body.Close()

	mu.Lock()
	defer mu.Unlock()
	if len(inputs) != 2 {
		t.Fatalf("expected 2 formatter calls, got %d", len(inputs))
	}

	// Inputs arrive in arbitrary order (concurrent), so check both are present.
	got := map[string]bool{string(inputs[0]): true, string(inputs[1]): true}
	if !got[`{"id":"a"}`] || !got[`{"id":"b"}`] {
		t.Errorf("expected inputs {\"id\":\"a\"} and {\"id\":\"b\"}, got %v", got)
	}
}

func TestErrorFormatterBypassedForPreContextErrors(t *testing.T) {
	formatterCalled := false

	router := trpcgo.NewRouter(
		trpcgo.WithBatching(false),
		trpcgo.WithErrorFormatter(func(input trpcgo.ErrorFormatterInput) any {
			formatterCalled = true
			return input.Shape
		}),
	)

	trpcgo.VoidQuery(router, "hello", func(ctx context.Context) (string, error) {
		return "hi", nil
	})

	server := newTestServer(t, trpc.NewHandler(router, "/trpc"))

	// ?batch=1 with batching disabled hits writeErrorResponse with ctx=nil,
	// which takes the defaultErrorEnvelope branch — formatter is never called.
	resp := mustGet(t, server, "/trpc/hello?batch=1")
	_ = resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
	if formatterCalled {
		t.Error("formatter should NOT be called for pre-context errors (ctx is nil)")
	}
}

func TestErrorFormatterInputNilForSSEConnectionLimit(t *testing.T) {
	var gotInput json.RawMessage
	formatterCalled := false

	router := trpcgo.NewRouter(
		trpcgo.WithSSEMaxConnections(1),
		trpcgo.WithSSEMaxDuration(2*time.Second),
		trpcgo.WithErrorFormatter(func(input trpcgo.ErrorFormatterInput) any {
			formatterCalled = true
			gotInput = input.Input
			return input.Shape
		}),
	)

	trpcgo.VoidSubscribe(router, "stream", func(ctx context.Context) (<-chan string, error) {
		ch := make(chan string)
		go func() { <-ctx.Done(); close(ch) }()
		return ch, nil
	})

	server := newTestServer(t, trpc.NewHandler(router, "/trpc"))

	// Fill the single SSE slot.
	resp1, err := http.Get(server.URL + "/trpc/stream")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp1.Body.Close() }()

	// Second connection should be rejected with 429.
	resp2, err := http.Get(server.URL + "/trpc/stream")
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(resp2.Body)
	_ = resp2.Body.Close()

	if resp2.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d: %s", resp2.StatusCode, raw)
	}
	if !formatterCalled {
		t.Fatal("formatter should be called for SSE connection limit (ctx is available)")
	}
	if gotInput != nil {
		t.Errorf("SSE connection limit should pass nil Input, got %s", gotInput)
	}
}

func TestErrorFormatterInputInSSESerializedError(t *testing.T) {
	var gotInput json.RawMessage
	formatterCalled := false

	router := trpcgo.NewRouter(trpcgo.WithErrorFormatter(func(input trpcgo.ErrorFormatterInput) any {
		formatterCalled = true
		gotInput = input.Input
		return input.Shape
	}))

	type BadData struct {
		Ch chan int // channels can't be JSON serialized
	}

	trpcgo.Subscribe(router, "bad", func(ctx context.Context, in struct {
		ID string `json:"id"`
	}) (<-chan BadData, error) {
		ch := make(chan BadData, 1)
		ch <- BadData{Ch: make(chan int)}
		close(ch)
		return ch, nil
	})

	server := newTestServer(t, trpc.NewHandler(router, "/trpc"))

	resp, err := http.Get(server.URL + `/trpc/bad?input={"id":"99"}`)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.ReadAll(resp.Body)
	_ = resp.Body.Close()

	if !formatterCalled {
		t.Fatal("formatter should be called for SSE serialized-error")
	}
	if string(gotInput) != `{"id":"99"}` {
		t.Errorf("SSE error formatter Input = %s, want %s", gotInput, `{"id":"99"}`)
	}
}
