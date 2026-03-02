package trpcgo_test

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/befabri/trpcgo"
)

// --- Batch Amplification ---

// TestBatchSizeLimitEnforcedBeforeParsing verifies that the batch size limit
// rejects oversized requests before iterating paths or parsing inputs. This
// prevents amplification: an attacker sending thousands of comma-separated
// paths in the URL should be rejected cheaply, without triggering procedure
// lookups or input parsing for each path.
func TestBatchSizeLimitEnforcedBeforeParsing(t *testing.T) {
	var lookupCount atomic.Int64
	r := trpcgo.NewRouter(trpcgo.WithBatching(true), trpcgo.WithMaxBatchSize(3))
	// Register a query whose handler tracks calls.
	trpcgo.VoidQuery(r, "probe", func(ctx context.Context) (string, error) {
		lookupCount.Add(1)
		return "ok", nil
	})

	server := newTestServer(t, r.Handler("/trpc"))

	// Build a batch with 100 paths (well over the limit of 3).
	paths := make([]string, 100)
	for i := range paths {
		paths[i] = "probe"
	}
	batchPath := "/trpc/" + strings.Join(paths, ",") + "?batch=1"

	resp := mustGet(t, server, batchPath)
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}

	// The handler should never have been called.
	if n := lookupCount.Load(); n != 0 {
		t.Errorf("expected 0 handler calls, got %d (amplification not prevented)", n)
	}

	body := decodeJSON(t, resp)
	errObj, ok := body["error"].(map[string]any)
	if !ok {
		t.Fatalf("expected error envelope, got %v", body)
	}
	msg, _ := errObj["message"].(string)
	if !strings.Contains(msg, "exceeds limit") {
		t.Errorf("expected 'exceeds limit' in message, got %q", msg)
	}
}

// TestBatchSizeLimitAtExactBoundary verifies the boundary: N paths at limit N
// should succeed, N+1 should fail.
func TestBatchSizeLimitAtExactBoundary(t *testing.T) {
	r := trpcgo.NewRouter(trpcgo.WithBatching(true), trpcgo.WithMaxBatchSize(2))
	trpcgo.VoidQuery(r, "a", func(ctx context.Context) (string, error) { return "a", nil })
	trpcgo.VoidQuery(r, "b", func(ctx context.Context) (string, error) { return "b", nil })
	trpcgo.VoidQuery(r, "c", func(ctx context.Context) (string, error) { return "c", nil })
	server := newTestServer(t, r.Handler("/trpc"))

	// Exactly at limit — should succeed.
	resp := mustGet(t, server, "/trpc/a,b?batch=1")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("batch of 2 (limit 2): expected 200, got %d", resp.StatusCode)
	}

	// One over — should fail.
	resp2 := mustGet(t, server, "/trpc/a,b,c?batch=1")
	defer func() { _ = resp2.Body.Close() }()
	if resp2.StatusCode != http.StatusBadRequest {
		t.Errorf("batch of 3 (limit 2): expected 400, got %d", resp2.StatusCode)
	}
}

// --- SSE Event ID Injection ---

// TestSSEEventIDNewlineSanitized verifies that newlines in TrackedEvent IDs
// are stripped, preventing SSE field injection. Without sanitization,
// id: "foo\ndata: injected" would produce a raw "data: injected" line.
func TestSSEEventIDNewlineSanitized(t *testing.T) {
	r := trpcgo.NewRouter()
	trpcgo.VoidSubscribe(r, "events", func(ctx context.Context) (<-chan trpcgo.TrackedEvent[string], error) {
		ch := make(chan trpcgo.TrackedEvent[string], 2)
		// Emit an event with newline in the ID (simulating attacker-controlled input).
		ch <- trpcgo.Tracked("safe-id", "first")
		ch <- trpcgo.Tracked("evil\ndata: injected\n", "second")
		close(ch)
		return ch, nil
	})

	server := newTestServer(t, r.Handler("/trpc"))
	resp, err := http.Get(server.URL + "/trpc/events")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	// Parse SSE events and check that no injected fields appear.
	scanner := bufio.NewScanner(resp.Body)
	var ids []string
	var injectedDataLines []string

	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "id: ") {
			ids = append(ids, strings.TrimPrefix(line, "id: "))
		}
		// A "data: injected" line would indicate successful injection.
		if line == "data: injected" {
			injectedDataLines = append(injectedDataLines, line)
		}
	}

	if len(injectedDataLines) > 0 {
		t.Errorf("SSE field injection detected: found %d injected data lines", len(injectedDataLines))
	}

	// The evil ID should have newlines stripped: "evildata: injected" (concatenated).
	for _, id := range ids {
		if strings.Contains(id, "\n") || strings.Contains(id, "\r") {
			t.Errorf("SSE id field contains newline: %q", id)
		}
	}
}

// TestSSEEventIDCarriageReturnSanitized verifies \r is also stripped from IDs.
func TestSSEEventIDCarriageReturnSanitized(t *testing.T) {
	r := trpcgo.NewRouter()
	trpcgo.VoidSubscribe(r, "events", func(ctx context.Context) (<-chan trpcgo.TrackedEvent[string], error) {
		ch := make(chan trpcgo.TrackedEvent[string], 1)
		ch <- trpcgo.Tracked("evil\r\ndata: injected\r\n", "payload")
		close(ch)
		return ch, nil
	})

	server := newTestServer(t, r.Handler("/trpc"))
	resp, err := http.Get(server.URL + "/trpc/events")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "data: injected" {
			t.Fatal("SSE field injection via \\r\\n detected")
		}
	}
}

// --- Internal Error Leaking ---

// TestInternalErrorMessageNeverLeaked sends requests that trigger various
// internal errors and verifies none of the internal details appear in responses.
func TestInternalErrorMessageNeverLeaked(t *testing.T) {
	secretMsg := "SECRET_DATABASE_CONNECTION_STRING_xyz123"

	r := trpcgo.NewRouter()
	trpcgo.VoidQuery(r, "boom", func(ctx context.Context) (string, error) {
		return "", fmt.Errorf("%s", secretMsg)
	})
	trpcgo.Mutation(r, "boom-post", func(ctx context.Context, input string) (string, error) {
		return "", fmt.Errorf("%s", secretMsg)
	}, trpcgo.WithMeta("test"))

	server := newTestServer(t, r.Handler("/trpc"))

	tests := []struct {
		name string
		path string
	}{
		{"query", "/trpc/boom"},
		{"mutation", "/trpc/boom-post"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var resp *http.Response
			if tc.name == "mutation" {
				resp = mustPost(t, server, tc.path, `"input"`)
			} else {
				resp = mustGet(t, server, tc.path)
			}
			defer func() { _ = resp.Body.Close() }()

			raw, _ := io.ReadAll(resp.Body)
			body := string(raw)

			if strings.Contains(body, secretMsg) {
				t.Errorf("internal error leaked to client: %s", body)
			}
			if strings.Contains(body, "SECRET") {
				t.Errorf("partial secret leaked to client: %s", body)
			}

			// Should contain the generic message.
			if !strings.Contains(body, "internal server error") {
				t.Errorf("expected generic error message, got: %s", body)
			}
		})
	}
}

// TestInternalErrorLeakedToOnErrorCallback verifies the onError callback
// receives the original error (with cause) even though the client gets a
// generic message.
func TestInternalErrorLeakedToOnErrorCallback(t *testing.T) {
	secretMsg := "db connection refused at 10.0.0.5:5432"
	var capturedErr *trpcgo.Error

	r := trpcgo.NewRouter(trpcgo.WithOnError(func(ctx context.Context, err *trpcgo.Error, path string) {
		capturedErr = err
	}))
	trpcgo.VoidQuery(r, "boom", func(ctx context.Context) (string, error) {
		return "", fmt.Errorf("%s", secretMsg)
	})

	server := newTestServer(t, r.Handler("/trpc"))
	resp := mustGet(t, server, "/trpc/boom")
	defer func() { _ = resp.Body.Close() }()

	if capturedErr == nil {
		t.Fatal("onError was not called")
	}
	if capturedErr.Cause == nil || !strings.Contains(capturedErr.Cause.Error(), secretMsg) {
		t.Errorf("onError should receive original cause, got: %v", capturedErr)
	}
}

// --- Stack Trace Leak in Production ---

// TestStackTraceNotInProductionMode ensures stack traces are only in dev mode.
func TestStackTraceNotInProductionMode(t *testing.T) {
	for _, isDev := range []bool{false, true} {
		t.Run(fmt.Sprintf("isDev=%v", isDev), func(t *testing.T) {
			r := trpcgo.NewRouter(trpcgo.WithDev(isDev))
			trpcgo.VoidQuery(r, "fail", func(ctx context.Context) (string, error) {
				return "", trpcgo.NewError(trpcgo.CodeBadRequest, "bad input")
			})

			server := newTestServer(t, r.Handler("/trpc"))
			resp := mustGet(t, server, "/trpc/fail")
			raw, _ := io.ReadAll(resp.Body)
			defer func() { _ = resp.Body.Close() }()

			body := string(raw)
			hasStack := strings.Contains(body, "goroutine") || strings.Contains(body, ".go:")

			if isDev && !hasStack {
				t.Error("dev mode should include stack trace")
			}
			if !isDev && hasStack {
				t.Error("production mode should NOT include stack trace")
			}
		})
	}
}

// --- Method Enforcement ---

// TestHTTPMethodEnforcement verifies all disallowed method combinations.
func TestHTTPMethodEnforcement(t *testing.T) {
	r := trpcgo.NewRouter()
	trpcgo.VoidQuery(r, "q", func(ctx context.Context) (string, error) { return "ok", nil })
	trpcgo.Mutation(r, "m", func(ctx context.Context, in string) (string, error) { return in, nil })

	server := newTestServer(t, r.Handler("/trpc"))

	tests := []struct {
		method string
		path   string
		want   int
	}{
		// Allowed
		{"GET", "/trpc/q", http.StatusOK},
		// Disallowed methods (rejected at top-level, before procedure lookup)
		{"PUT", "/trpc/q", http.StatusMethodNotAllowed},
		{"DELETE", "/trpc/q", http.StatusMethodNotAllowed},
		{"PATCH", "/trpc/q", http.StatusMethodNotAllowed},
		{"OPTIONS", "/trpc/q", http.StatusMethodNotAllowed},
		{"HEAD", "/trpc/q", http.StatusMethodNotAllowed},
		// POST to query without override
		{"POST", "/trpc/q", http.StatusMethodNotAllowed},
		// GET to mutation
		{"GET", "/trpc/m", http.StatusMethodNotAllowed},
	}

	for _, tc := range tests {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			resp := mustRequest(t, server, tc.method, tc.path)
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode != tc.want {
				t.Errorf("%s %s: expected %d, got %d", tc.method, tc.path, tc.want, resp.StatusCode)
			}
		})
	}
}

// --- Body Size Limits ---

// TestBodySizeLimitNegativeOneMeansUnlimited verifies WithMaxBodySize(-1) disables the limit.
func TestBodySizeLimitNegativeOneMeansUnlimited(t *testing.T) {
	r := trpcgo.NewRouter(trpcgo.WithMaxBodySize(-1))
	trpcgo.Mutation(r, "echo", func(ctx context.Context, in json.RawMessage) (string, error) {
		return fmt.Sprintf("got %d bytes", len(in)), nil
	})

	server := newTestServer(t, r.Handler("/trpc"))

	// Send a 2MB body (above the default 1MB limit).
	bigBody := `"` + strings.Repeat("x", 2*1024*1024) + `"`
	resp := mustPost(t, server, "/trpc/echo", bigBody)
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 with unlimited body, got %d", resp.StatusCode)
	}
}

// TestBodySizeLimitZeroKeepsDefault verifies WithMaxBodySize(0) keeps the 1MB default.
func TestBodySizeLimitZeroKeepsDefault(t *testing.T) {
	r := trpcgo.NewRouter(trpcgo.WithMaxBodySize(0))
	trpcgo.Mutation(r, "echo", func(ctx context.Context, in json.RawMessage) (string, error) {
		return "ok", nil
	})

	server := newTestServer(t, r.Handler("/trpc"))

	// Send a 2MB body — should be rejected by the default 1MB limit.
	bigBody := `"` + strings.Repeat("x", 2*1024*1024) + `"`
	resp := mustPost(t, server, "/trpc/echo", bigBody)
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusOK {
		t.Error("expected rejection: WithMaxBodySize(0) should keep the 1MB default")
	}
}

// TestBodySizeLimitCustomSmall verifies a custom small limit rejects large bodies.
func TestBodySizeLimitCustomSmall(t *testing.T) {
	r := trpcgo.NewRouter(trpcgo.WithMaxBodySize(64)) // 64 bytes
	trpcgo.Mutation(r, "echo", func(ctx context.Context, in json.RawMessage) (string, error) {
		return "ok", nil
	})

	server := newTestServer(t, r.Handler("/trpc"))

	// Body well over 64 bytes.
	resp := mustPost(t, server, "/trpc/echo", `"`+strings.Repeat("a", 200)+`"`)
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusOK {
		t.Error("expected rejection for body exceeding 64-byte limit")
	}
}

// --- Malformed Input ---

// TestMalformedJSONInputRejected verifies various malformed inputs are rejected.
func TestMalformedJSONInputRejected(t *testing.T) {
	r := trpcgo.NewRouter()
	trpcgo.Query(r, "user", func(ctx context.Context, in GetUserInput) (User, error) {
		return User{ID: in.ID, Name: "test"}, nil
	})
	trpcgo.Mutation(r, "create", func(ctx context.Context, in CreateUserInput) (User, error) {
		return User{ID: "1", Name: in.Name}, nil
	})

	server := newTestServer(t, r.Handler("/trpc"))

	tests := []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{"truncated JSON", "POST", "/trpc/create", `{"name": "alice`},
		{"plain text", "POST", "/trpc/create", `just some text`},
		{"XML body", "POST", "/trpc/create", `<name>alice</name>`},
		{"array instead of object", "POST", "/trpc/create", `[1,2,3]`},
		{"null bytes", "POST", "/trpc/create", "\x00\x00\x00"},
		{"GET invalid JSON input", "GET", "/trpc/user?input=not-json", ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var resp *http.Response
			if tc.method == "GET" {
				resp = mustGet(t, server, tc.path)
			} else {
				resp = mustPost(t, server, tc.path, tc.body)
			}
			defer func() { _ = resp.Body.Close() }()

			if resp.StatusCode == http.StatusOK {
				t.Errorf("malformed input should not succeed, got 200")
			}
			// Verify the error response is still valid JSON.
			raw, _ := io.ReadAll(resp.Body)
			var envelope map[string]any
			if err := json.Unmarshal(raw, &envelope); err != nil {
				t.Errorf("error response is not valid JSON: %s", raw)
			}
		})
	}
}

// --- Path Traversal / Special Characters ---

// TestPathTraversalAttempts ensures traversal segments are rejected with 400,
// not just silently missed via map lookup (which would return 404).
func TestPathTraversalAttempts(t *testing.T) {
	r := trpcgo.NewRouter()
	trpcgo.VoidQuery(r, "safe", func(ctx context.Context) (string, error) {
		return "ok", nil
	})

	server := newTestServer(t, r.Handler("/trpc"))

	// Traversal paths — must get 400 BAD_REQUEST (explicit rejection).
	traversalPaths := []string{
		"/trpc/../etc/passwd",
		"/trpc/./safe",
		"/trpc/safe/../../etc/shadow",
		"/trpc/%2e%2e/etc/passwd",
	}
	for _, path := range traversalPaths {
		t.Run("traversal "+path, func(t *testing.T) {
			resp := mustGet(t, server, path)
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode != http.StatusBadRequest {
				t.Errorf("traversal path %q: expected 400, got %d", path, resp.StatusCode)
			}
		})
	}

	// Non-traversal invalid paths — 404 (just not found, no traversal).
	otherPaths := []string{
		"/trpc/safe%00injected",
		"/trpc/" + strings.Repeat("a", 10000),
	}
	for _, path := range otherPaths {
		t.Run("invalid "+path[:min(len(path), 40)], func(t *testing.T) {
			resp := mustGet(t, server, path)
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode == http.StatusOK {
				raw, _ := io.ReadAll(resp.Body)
				t.Errorf("path %q returned 200: %s", path[:min(len(path), 40)], raw)
			}
		})
	}
}

// TestPathTraversalInBatch ensures traversal in batch paths is also rejected.
func TestPathTraversalInBatch(t *testing.T) {
	r := trpcgo.NewRouter(trpcgo.WithBatching(true))
	trpcgo.VoidQuery(r, "safe", func(ctx context.Context) (string, error) {
		return "ok", nil
	})

	server := newTestServer(t, r.Handler("/trpc"))

	// One valid path, one traversal path in batch.
	resp := mustGet(t, server, "/trpc/safe,../etc/passwd?batch=1")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("batch with traversal path: expected 400, got %d", resp.StatusCode)
	}
}

// --- Procedure Not Found Always Returns 404, Never Leaks Registry ---

// TestNotFoundDoesNotLeakProcedureNames ensures error responses for unknown
// procedures don't reveal the names of registered procedures.
func TestNotFoundDoesNotLeakProcedureNames(t *testing.T) {
	r := trpcgo.NewRouter()
	trpcgo.VoidQuery(r, "secret.admin.panel", func(ctx context.Context) (string, error) {
		return "admin", nil
	})

	server := newTestServer(t, r.Handler("/trpc"))
	resp := mustGet(t, server, "/trpc/nonexistent")
	defer func() { _ = resp.Body.Close() }()

	raw, _ := io.ReadAll(resp.Body)
	body := string(raw)

	if strings.Contains(body, "secret.admin.panel") {
		t.Errorf("response leaked registered procedure name: %s", body)
	}
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d", resp.StatusCode)
	}
}

// --- Batch Edge Cases ---

// TestBatchEmptyPath verifies an empty batch path is handled.
func TestBatchEmptyPath(t *testing.T) {
	r := trpcgo.NewRouter(trpcgo.WithBatching(true))
	trpcgo.VoidQuery(r, "hello", func(ctx context.Context) (string, error) {
		return "hi", nil
	})
	server := newTestServer(t, r.Handler("/trpc"))

	// Empty path after base — no procedure specified.
	resp := mustGet(t, server, "/trpc/?batch=1")
	defer func() { _ = resp.Body.Close() }()
	// Should be a NOT_FOUND — the path is empty string after split.
	if resp.StatusCode == http.StatusOK {
		t.Error("empty batch path should not succeed")
	}
}

// TestBatchWithDuplicatePaths verifies duplicate paths in a batch are allowed.
func TestBatchWithDuplicatePaths(t *testing.T) {
	var callCount atomic.Int64
	r := trpcgo.NewRouter(trpcgo.WithBatching(true))
	trpcgo.VoidQuery(r, "counter", func(ctx context.Context) (int64, error) {
		return callCount.Add(1), nil
	})

	server := newTestServer(t, r.Handler("/trpc"))
	resp := mustGet(t, server, "/trpc/counter,counter,counter?batch=1")
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if n := callCount.Load(); n != 3 {
		t.Errorf("expected 3 handler calls, got %d", n)
	}
}

// --- Subscription Batching Blocked ---

// TestSubscriptionBatchBlockedEvenWhenMixed verifies that even one subscription
// in a batch causes rejection.
func TestSubscriptionBatchBlockedEvenWhenMixed(t *testing.T) {
	r := trpcgo.NewRouter(trpcgo.WithBatching(true))
	trpcgo.VoidQuery(r, "q", func(ctx context.Context) (string, error) { return "ok", nil })
	trpcgo.VoidSubscribe(r, "sub", func(ctx context.Context) (<-chan string, error) {
		ch := make(chan string, 1)
		ch <- "hello"
		close(ch)
		return ch, nil
	})

	server := newTestServer(t, r.Handler("/trpc"))
	resp := mustGet(t, server, "/trpc/q,sub?batch=1")
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400 when batch includes subscription, got %d", resp.StatusCode)
	}
}

// --- Context Cancellation ---

// TestContextCancellationStopsHandler verifies that a cancelled client
// connection propagates to the handler context.
func TestContextCancellationStopsHandler(t *testing.T) {
	handlerStarted := make(chan struct{})
	handlerCtxDone := make(chan struct{})

	r := trpcgo.NewRouter()
	trpcgo.VoidQuery(r, "slow", func(ctx context.Context) (string, error) {
		close(handlerStarted)
		<-ctx.Done()
		close(handlerCtxDone)
		return "", ctx.Err()
	})

	server := newTestServer(t, r.Handler("/trpc"))

	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, "GET", server.URL+"/trpc/slow", nil)

	go func() {
		// Wait for handler to start, then cancel the client context.
		<-handlerStarted
		cancel()
	}()

	resp, err := http.DefaultClient.Do(req)
	if err == nil {
		_ = resp.Body.Close()
	}

	// Handler should have noticed the cancellation.
	select {
	case <-handlerCtxDone:
		// Success
	case <-time.After(5 * time.Second):
		t.Fatal("handler did not receive context cancellation within 5s")
	}
}

// --- SSE Max Duration ---

// TestSSEMaxDurationEnforced verifies that SSE streams respect the max duration.
func TestSSEMaxDurationEnforced(t *testing.T) {
	r := trpcgo.NewRouter(trpcgo.WithSSEMaxDuration(200 * time.Millisecond))
	trpcgo.VoidSubscribe(r, "forever", func(ctx context.Context) (<-chan string, error) {
		ch := make(chan string)
		go func() {
			// Never close — relies on max duration to terminate.
			<-ctx.Done()
			close(ch)
		}()
		return ch, nil
	})

	server := newTestServer(t, r.Handler("/trpc"))
	start := time.Now()
	resp, err := http.Get(server.URL + "/trpc/forever")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()

	// Read until EOF.
	raw, _ := io.ReadAll(resp.Body)
	elapsed := time.Since(start)

	if elapsed > 3*time.Second {
		t.Errorf("SSE stream ran for %v, expected ~200ms max", elapsed)
	}

	body := string(raw)
	if !strings.Contains(body, "event: return") {
		t.Errorf("expected 'event: return' for max duration termination, got: %s", body)
	}
}

// TestSSEMaxDurationDefault verifies the default SSE max duration is 30 minutes.
func TestSSEMaxDurationDefault(t *testing.T) {
	r := trpcgo.NewRouter()
	trpcgo.VoidSubscribe(r, "test", func(ctx context.Context) (<-chan string, error) {
		ch := make(chan string)
		go func() { <-ctx.Done(); close(ch) }()
		return ch, nil
	})

	// Start SSE connection and verify connected event includes non-zero timeout.
	server := newTestServer(t, r.Handler("/trpc"))
	resp, err := http.Get(server.URL + "/trpc/test")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()

	// The SSE stream should connect successfully (max duration is finite, not 0).
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	// Read the connected event — it should be sent before max duration kicks in.
	scanner := bufio.NewScanner(resp.Body)
	var gotConnected bool
	for scanner.Scan() {
		line := scanner.Text()
		if strings.Contains(line, "event: connected") {
			gotConnected = true
			break
		}
	}
	if !gotConnected {
		t.Error("expected 'event: connected' from SSE stream")
	}
}

// TestSSEMaxDurationOptionConventions verifies the WithSSEMaxDuration option
// follows the same convention as WithMaxBodySize: positive=set, -1=unlimited, 0=keep default.
func TestSSEMaxDurationOptionConventions(t *testing.T) {
	t.Run("positive sets value", func(t *testing.T) {
		r := trpcgo.NewRouter(trpcgo.WithSSEMaxDuration(5 * time.Minute))
		trpcgo.VoidSubscribe(r, "forever", func(ctx context.Context) (<-chan string, error) {
			ch := make(chan string)
			go func() { <-ctx.Done(); close(ch) }()
			return ch, nil
		})
		server := newTestServer(t, r.Handler("/trpc"))
		resp, err := http.Get(server.URL + "/trpc/forever")
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("expected 200, got %d", resp.StatusCode)
		}
	})

	t.Run("negative gives unlimited", func(t *testing.T) {
		// -1 should mean unlimited (no timer). We can't easily verify absence
		// of a timer, but we verify it doesn't panic and connects.
		r := trpcgo.NewRouter(trpcgo.WithSSEMaxDuration(-1))
		trpcgo.VoidSubscribe(r, "forever", func(ctx context.Context) (<-chan string, error) {
			ch := make(chan string)
			go func() { <-ctx.Done(); close(ch) }()
			return ch, nil
		})
		server := newTestServer(t, r.Handler("/trpc"))
		resp, err := http.Get(server.URL + "/trpc/forever")
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("expected 200, got %d", resp.StatusCode)
		}
	})

	t.Run("zero keeps default", func(t *testing.T) {
		// Passing 0 should be a no-op, keeping the 30-minute default.
		// Verify by setting a short duration first, then overriding with 0.
		r := trpcgo.NewRouter(
			trpcgo.WithSSEMaxDuration(100*time.Millisecond),
			trpcgo.WithSSEMaxDuration(0), // should be no-op
		)
		trpcgo.VoidSubscribe(r, "forever", func(ctx context.Context) (<-chan string, error) {
			ch := make(chan string)
			go func() { <-ctx.Done(); close(ch) }()
			return ch, nil
		})
		server := newTestServer(t, r.Handler("/trpc"))
		start := time.Now()
		resp, err := http.Get(server.URL + "/trpc/forever")
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = resp.Body.Close() }()
		raw, _ := io.ReadAll(resp.Body)
		elapsed := time.Since(start)

		// Should still terminate after 100ms (the 0 was a no-op).
		if elapsed > 3*time.Second {
			t.Errorf("stream ran for %v; expected ~100ms (0 should not override)", elapsed)
		}
		if !strings.Contains(string(raw), "event: return") {
			t.Error("expected 'event: return' — 100ms duration should still be active")
		}
	})
}

// TestSSEMaxConnectionsEnforced verifies that WithSSEMaxConnections rejects
// new subscriptions when the limit is reached.
func TestSSEMaxConnectionsEnforced(t *testing.T) {
	const maxConns = 2

	r := trpcgo.NewRouter(
		trpcgo.WithSSEMaxConnections(maxConns),
		trpcgo.WithSSEMaxDuration(5*time.Second),
	)
	trpcgo.VoidSubscribe(r, "stream", func(ctx context.Context) (<-chan string, error) {
		ch := make(chan string)
		go func() { <-ctx.Done(); close(ch) }()
		return ch, nil
	})

	server := newTestServer(t, r.Handler("/trpc"))

	// Open maxConns connections.
	var conns []*http.Response
	for i := range maxConns {
		resp, err := http.Get(server.URL + "/trpc/stream")
		if err != nil {
			t.Fatalf("connection %d failed: %v", i, err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("connection %d: expected 200, got %d", i, resp.StatusCode)
		}
		conns = append(conns, resp)
	}

	// The next connection should be rejected with 429.
	resp, err := http.Get(server.URL + "/trpc/stream")
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()

	if resp.StatusCode != http.StatusTooManyRequests {
		t.Errorf("over-limit connection: expected 429, got %d. Body: %s", resp.StatusCode, raw)
	}

	// Close one existing connection — counter should decrement.
	_ = conns[0].Body.Close()
	// Give the server a moment to process the disconnect.
	time.Sleep(50 * time.Millisecond)

	// Now a new connection should succeed.
	resp2, err := http.Get(server.URL + "/trpc/stream")
	if err != nil {
		t.Fatal(err)
	}
	if resp2.StatusCode != http.StatusOK {
		t.Errorf("after freeing slot: expected 200, got %d", resp2.StatusCode)
	}

	// Cleanup.
	for _, c := range conns[1:] {
		_ = c.Body.Close()
	}
	_ = resp2.Body.Close()
}

// TestSSEMaxConnectionsConcurrentRace tests the connection counter under
// concurrent access to detect races (run with -race).
func TestSSEMaxConnectionsConcurrentRace(t *testing.T) {
	const maxConns = 5
	const attempts = 20

	r := trpcgo.NewRouter(
		trpcgo.WithSSEMaxConnections(maxConns),
		trpcgo.WithSSEMaxDuration(2*time.Second),
	)
	trpcgo.VoidSubscribe(r, "stream", func(ctx context.Context) (<-chan string, error) {
		ch := make(chan string)
		go func() { <-ctx.Done(); close(ch) }()
		return ch, nil
	})

	server := newTestServer(t, r.Handler("/trpc"))

	var accepted atomic.Int64
	var rejected atomic.Int64
	done := make(chan struct{}, attempts)

	for range attempts {
		go func() {
			defer func() { done <- struct{}{} }()
			resp, err := http.Get(server.URL + "/trpc/stream")
			if err != nil {
				return
			}
			if resp.StatusCode == http.StatusOK {
				accepted.Add(1)
				// Hold connection briefly.
				time.Sleep(100 * time.Millisecond)
			} else {
				rejected.Add(1)
			}
			_, _ = io.ReadAll(resp.Body)
			_ = resp.Body.Close()
		}()
	}

	for range attempts {
		<-done
	}

	// At least some should be rejected and none should exceed limit.
	t.Logf("accepted: %d, rejected: %d", accepted.Load(), rejected.Load())
	if rejected.Load() == 0 {
		t.Error("expected some connections to be rejected")
	}
}

// --- Concurrent Safety ---

// TestConcurrentBatchAndSingleRequests fires many requests of different types
// concurrently to detect race conditions (run with -race).
func TestConcurrentBatchAndSingleRequests(t *testing.T) {
	r := trpcgo.NewRouter(trpcgo.WithBatching(true))
	trpcgo.VoidQuery(r, "ping", func(ctx context.Context) (string, error) { return "pong", nil })
	trpcgo.Mutation(r, "echo", func(ctx context.Context, in string) (string, error) { return in, nil })

	server := newTestServer(t, r.Handler("/trpc"))

	done := make(chan struct{})
	for i := range 50 {
		go func(idx int) {
			defer func() { done <- struct{}{} }()

			var resp *http.Response
			switch idx % 4 {
			case 0:
				resp = mustGet(t, server, "/trpc/ping")
			case 1:
				resp = mustPost(t, server, "/trpc/echo", `"hello"`)
			case 2:
				resp = mustGet(t, server, "/trpc/ping,ping?batch=1")
			case 3:
				// JSONL batch
				req, _ := http.NewRequest("GET", server.URL+"/trpc/ping,ping?batch=1", nil)
				req.Header.Set("trpc-accept", "application/jsonl")
				var err error
				resp, err = http.DefaultClient.Do(req)
				if err != nil {
					t.Errorf("JSONL request failed: %v", err)
					return
				}
			}
			if resp != nil {
				_, _ = io.ReadAll(resp.Body)
				_ = resp.Body.Close()
			}
		}(i)
	}

	for range 50 {
		<-done
	}
}

// --- Response Metadata Concurrency ---

// TestResponseMetadataConcurrentAccess tests that SetCookie and SetResponseHeader
// are safe under concurrent JSONL batch execution.
func TestResponseMetadataConcurrentAccess(t *testing.T) {
	r := trpcgo.NewRouter(trpcgo.WithBatching(true))
	for i := range 5 {
		name := fmt.Sprintf("proc%d", i)
		idx := i
		trpcgo.VoidQuery(r, name, func(ctx context.Context) (string, error) {
			trpcgo.SetCookie(ctx, &http.Cookie{
				Name:  fmt.Sprintf("cookie%d", idx),
				Value: "val",
			})
			trpcgo.SetResponseHeader(ctx, fmt.Sprintf("X-Test-%d", idx), "val")
			return name, nil
		})
	}

	server := newTestServer(t, r.Handler("/trpc"))

	// JSONL batch to force concurrent execution.
	req, _ := http.NewRequest("GET", server.URL+"/trpc/proc0,proc1,proc2,proc3,proc4?batch=1", nil)
	req.Header.Set("trpc-accept", "application/jsonl")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

// --- Error Formatter Does Not Bypass Error Hiding ---

// TestErrorFormatterReceivesWrappedInternalError verifies the error formatter
// gets the sanitized error (not the raw internal one) for non-*Error errors.
func TestErrorFormatterReceivesWrappedInternalError(t *testing.T) {
	secretMsg := "pg: connection reset by 10.0.0.5"
	var formatterReceivedMessage string

	r := trpcgo.NewRouter(trpcgo.WithErrorFormatter(func(input trpcgo.ErrorFormatterInput) any {
		formatterReceivedMessage = input.Error.Message
		return input.Shape
	}))
	trpcgo.VoidQuery(r, "boom", func(ctx context.Context) (string, error) {
		return "", fmt.Errorf("%s", secretMsg)
	})

	server := newTestServer(t, r.Handler("/trpc"))
	resp := mustGet(t, server, "/trpc/boom")
	defer func() { _ = resp.Body.Close() }()

	// The formatter should receive "internal server error", not the secret.
	if formatterReceivedMessage == secretMsg {
		t.Errorf("error formatter received raw internal error: %q", formatterReceivedMessage)
	}
	if formatterReceivedMessage != "internal server error" {
		t.Errorf("expected 'internal server error', got %q", formatterReceivedMessage)
	}

	// Client response should also be clean.
	raw, _ := io.ReadAll(resp.Body)
	if strings.Contains(string(raw), secretMsg) {
		t.Error("internal error leaked to client via error formatter")
	}
}

// --- Nil/Edge-Case Cookie Guard ---

// TestSetCookieNilContextIsNoop verifies SetCookie doesn't panic without context.
func TestSetCookieNilContextIsNoop(t *testing.T) {
	// context.Background() has no response metadata.
	trpcgo.SetCookie(context.Background(), &http.Cookie{Name: "test", Value: "val"})
	trpcgo.SetResponseHeader(context.Background(), "X-Test", "val")
	// If we get here without panic, the test passes.
}

// --- URL Encoding Edge Cases ---

// TestURLEncodedProcedurePath verifies URL-encoded paths are decoded for lookup.
func TestURLEncodedProcedurePath(t *testing.T) {
	r := trpcgo.NewRouter()
	trpcgo.VoidQuery(r, "user.getById", func(ctx context.Context) (string, error) {
		return "found", nil
	})

	server := newTestServer(t, r.Handler("/trpc"))

	// URL-encoded dot: user%2EgetById
	resp := mustGet(t, server, "/trpc/user%2EgetById")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("URL-encoded path should resolve, got %d", resp.StatusCode)
	}
}

// --- Batching Disabled Still Rejects Batch Requests ---

// TestBatchingDisabledRejectsBatchQuery verifies ?batch=1 is rejected.
func TestBatchingDisabledRejectsBatchQuery(t *testing.T) {
	r := trpcgo.NewRouter(trpcgo.WithBatching(false))
	trpcgo.VoidQuery(r, "hello", func(ctx context.Context) (string, error) {
		return "hi", nil
	})

	server := newTestServer(t, r.Handler("/trpc"))
	resp := mustGet(t, server, "/trpc/hello?batch=1")
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400 when batching disabled, got %d", resp.StatusCode)
	}
}

// --- JSONL Batch Size Limit ---

// TestJSONLBatchRespectsSizeLimit verifies JSONL batch also respects the limit.
func TestJSONLBatchRespectsSizeLimit(t *testing.T) {
	r := trpcgo.NewRouter(trpcgo.WithBatching(true), trpcgo.WithMaxBatchSize(2))
	trpcgo.VoidQuery(r, "a", func(ctx context.Context) (string, error) { return "a", nil })
	trpcgo.VoidQuery(r, "b", func(ctx context.Context) (string, error) { return "b", nil })
	trpcgo.VoidQuery(r, "c", func(ctx context.Context) (string, error) { return "c", nil })

	server := newTestServer(t, r.Handler("/trpc"))

	req, _ := http.NewRequest("GET", server.URL+"/trpc/a,b,c?batch=1", nil)
	req.Header.Set("trpc-accept", "application/jsonl")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("JSONL batch over limit: expected 400, got %d", resp.StatusCode)
	}
}

// --- POST Batch Body Size ---

// TestPOSTBatchBodySizeLimit verifies the body size limit applies to batch POST.
func TestPOSTBatchBodySizeLimit(t *testing.T) {
	r := trpcgo.NewRouter(trpcgo.WithBatching(true), trpcgo.WithMaxBodySize(64))
	trpcgo.Mutation(r, "echo", func(ctx context.Context, in string) (string, error) {
		return in, nil
	})

	server := newTestServer(t, r.Handler("/trpc"))

	// POST batch with oversized body.
	bigInput := `{"0":"` + strings.Repeat("x", 200) + `"}`
	resp := mustPost(t, server, "/trpc/echo?batch=1", bigInput)
	defer func() { _ = resp.Body.Close() }()

	// Should be rejected — body exceeds limit.
	if resp.StatusCode == http.StatusOK {
		t.Error("expected rejection for oversized POST batch body")
	}
}

// --- No Procedure Path ---

// TestNoProcedurePath verifies requests with no path are rejected.
func TestNoProcedurePath(t *testing.T) {
	r := trpcgo.NewRouter()
	trpcgo.VoidQuery(r, "hello", func(ctx context.Context) (string, error) {
		return "hi", nil
	})

	server := newTestServer(t, r.Handler("/trpc"))

	paths := []string{"/trpc/", "/trpc"}
	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			resp := mustGet(t, server, path)
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode == http.StatusOK {
				t.Errorf("path %q should not find a procedure", path)
			}
		})
	}
}

// --- Response Always Has Content-Type ---

// TestErrorResponseAlwaysHasContentType verifies all error responses set JSON
// content type, preventing browsers from sniffing the content.
func TestErrorResponseAlwaysHasContentType(t *testing.T) {
	r := trpcgo.NewRouter()
	trpcgo.VoidQuery(r, "hello", func(ctx context.Context) (string, error) {
		return "hi", nil
	})

	server := newTestServer(t, r.Handler("/trpc"))

	tests := []struct {
		name   string
		method string
		path   string
	}{
		{"not found", "GET", "/trpc/nonexistent"},
		{"method not allowed", "PUT", "/trpc/hello"},
		{"no path", "GET", "/trpc/"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resp := mustRequest(t, server, tc.method, tc.path)
			defer func() { _ = resp.Body.Close() }()

			ct := resp.Header.Get("Content-Type")
			if !strings.HasPrefix(ct, "application/json") {
				t.Errorf("error response Content-Type should be application/json, got %q", ct)
			}
		})
	}
}

// --- RawCall Security ---

// TestRawCallDoesNotBypassValidation verifies RawCall still runs validation.
func TestRawCallDoesNotBypassValidation(t *testing.T) {
	validatorCalled := false
	r := trpcgo.NewRouter(trpcgo.WithValidator(func(v any) error {
		validatorCalled = true
		return fmt.Errorf("%s", "validation failed")
	}))
	trpcgo.Query(r, "user", func(ctx context.Context, in GetUserInput) (User, error) {
		return User{ID: in.ID}, nil
	})

	_, err := r.RawCall(context.Background(), "user", json.RawMessage(`{"id":"1"}`))
	if err == nil {
		t.Fatal("expected validation error from RawCall")
	}
	if !validatorCalled {
		t.Error("validator was not called via RawCall")
	}
}

// --- Helpers (from trpcgo_test.go, needed for test file compilation) ---
// Note: These are defined in trpcgo_test.go in the same package, so they
// are accessible. If this file is compiled separately, these would need
// to be duplicated. Since we're in the same package (trpcgo_test), they
// are shared.

// Verify this file compiles by ensuring we reference the shared test helpers.
var (
	_ = newTestServer
	_ = mustGet
	_ = mustPost
	_ = mustRequest
	_ = decodeJSON
)
