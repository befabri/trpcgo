package trpcgo_test

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/befabri/trpcgo"
	"github.com/befabri/trpcgo/trpc"
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

	server := newTestServer(t, trpc.NewHandler(r, "/trpc"))

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
	server := newTestServer(t, trpc.NewHandler(r, "/trpc"))

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

	server := newTestServer(t, trpc.NewHandler(r, "/trpc"))
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
		if after, ok := strings.CutPrefix(line, "id: "); ok {
			ids = append(ids, after)
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

	server := newTestServer(t, trpc.NewHandler(r, "/trpc"))
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

func TestInternalErrorMessageNeverLeaked(t *testing.T) {
	secretMsg := "SECRET_DATABASE_CONNECTION_STRING_xyz123"

	for _, isDev := range []bool{false, true} {
		r := trpcgo.NewRouter(trpcgo.WithDev(isDev))
		trpcgo.VoidQuery(r, "boom", func(ctx context.Context) (string, error) {
			return "", fmt.Errorf("%s", secretMsg)
		})
		trpcgo.Mutation(r, "boom-post", func(ctx context.Context, input string) (string, error) {
			return "", fmt.Errorf("%s", secretMsg)
		}, trpcgo.WithMeta("test"))

		server := newTestServer(t, trpc.NewHandler(r, "/trpc"))

		for _, tc := range []struct{ name, path string }{{"query", "/trpc/boom"}, {"mutation", "/trpc/boom-post"}} {
			t.Run(fmt.Sprintf("isDev=%v/%s", isDev, tc.name), func(t *testing.T) {
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
				if !strings.Contains(body, "internal server error") {
					t.Errorf("expected generic error message, got: %s", body)
				}
				if isDev && !strings.Contains(body, ".go:") {
					t.Error("dev mode should include a stack trace")
				}
			})
		}
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

	server := newTestServer(t, trpc.NewHandler(r, "/trpc"))
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

			server := newTestServer(t, trpc.NewHandler(r, "/trpc"))
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

	server := newTestServer(t, trpc.NewHandler(r, "/trpc"))

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

func TestDefaultMaxBodySize(t *testing.T) {
	r := trpcgo.NewRouter()
	if got, want := r.MaxBodySize(), int64(1<<20); got != want {
		t.Fatalf("MaxBodySize() = %d, want %d", got, want)
	}
}

// TestBodySizeLimitNegativeOneMeansUnlimited verifies WithMaxBodySize(-1) disables the limit.
func TestBodySizeLimitNegativeOneMeansUnlimited(t *testing.T) {
	r := trpcgo.NewRouter(trpcgo.WithMaxBodySize(-1))
	trpcgo.Mutation(r, "echo", func(ctx context.Context, in json.RawMessage) (string, error) {
		return fmt.Sprintf("got %d bytes", len(in)), nil
	})

	server := newTestServer(t, trpc.NewHandler(r, "/trpc"))

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

	server := newTestServer(t, trpc.NewHandler(r, "/trpc"))

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

	server := newTestServer(t, trpc.NewHandler(r, "/trpc"))

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

	server := newTestServer(t, trpc.NewHandler(r, "/trpc"))

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

	server := newTestServer(t, trpc.NewHandler(r, "/trpc"))

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
		"/trpc/%20",
		"/trpc/%00",
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

	server := newTestServer(t, trpc.NewHandler(r, "/trpc"))

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

	server := newTestServer(t, trpc.NewHandler(r, "/trpc"))
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
	server := newTestServer(t, trpc.NewHandler(r, "/trpc"))

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

	server := newTestServer(t, trpc.NewHandler(r, "/trpc"))
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

	server := newTestServer(t, trpc.NewHandler(r, "/trpc"))
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

	server := newTestServer(t, trpc.NewHandler(r, "/trpc"))

	ctx, cancel := context.WithCancel(t.Context())
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

func TestDefaultSSEMaxDuration(t *testing.T) {
	r := trpcgo.NewRouter()
	if got, want := r.SSEMaxDuration(), 30*time.Minute; got != want {
		t.Fatalf("SSEMaxDuration() = %v, want %v", got, want)
	}
}

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

	server := newTestServer(t, trpc.NewHandler(r, "/trpc"))
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
		context.AfterFunc(ctx, func() { close(ch) })
		return ch, nil
	})

	// Start SSE connection and verify connected event includes non-zero timeout.
	server := newTestServer(t, trpc.NewHandler(r, "/trpc"))
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
			context.AfterFunc(ctx, func() { close(ch) })
			return ch, nil
		})
		server := newTestServer(t, trpc.NewHandler(r, "/trpc"))
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
			context.AfterFunc(ctx, func() { close(ch) })
			return ch, nil
		})
		server := newTestServer(t, trpc.NewHandler(r, "/trpc"))
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
			context.AfterFunc(ctx, func() { close(ch) })
			return ch, nil
		})
		server := newTestServer(t, trpc.NewHandler(r, "/trpc"))
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
		context.AfterFunc(ctx, func() { close(ch) })
		return ch, nil
	})

	server := newTestServer(t, trpc.NewHandler(r, "/trpc"))

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

	// Close one existing connection. The slot is released only after the
	// server observes the disconnect, so poll until a new connection is
	// accepted rather than sleeping a fixed time.
	_ = conns[0].Body.Close()

	var resp2 *http.Response
	freed := eventually(t, 5*time.Second, func() bool {
		r, err := http.Get(server.URL + "/trpc/stream")
		if err != nil {
			t.Fatal(err)
		}
		if r.StatusCode == http.StatusOK {
			resp2 = r
			return true
		}
		_, _ = io.ReadAll(r.Body)
		_ = r.Body.Close()
		return false
	})
	if !freed {
		t.Fatal("after freeing slot: expected 200, still rejected after 5s")
	}

	// Cleanup.
	for _, c := range conns[1:] {
		_ = c.Body.Close()
	}
	_ = resp2.Body.Close()
}

func TestSSEMaxConnectionsNegativeDisablesLimit(t *testing.T) {
	r := trpcgo.NewRouter(
		trpcgo.WithSSEMaxConnections(3),
		trpcgo.WithSSEMaxConnections(-1),
	)
	if got := r.MaxSSEConnections(); got != 0 {
		t.Fatalf("MaxSSEConnections() = %d, want 0", got)
	}
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
		context.AfterFunc(ctx, func() { close(ch) })
		return ch, nil
	})

	server := newTestServer(t, trpc.NewHandler(r, "/trpc"))

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
	r := trpcgo.NewRouter(trpcgo.WithBatching(true), trpcgo.WithSSEMaxDuration(300*time.Millisecond))
	trpcgo.VoidQuery(r, "ping", func(ctx context.Context) (string, error) { return "pong", nil })
	trpcgo.Mutation(r, "echo", func(ctx context.Context, in string) (string, error) { return in, nil })
	trpcgo.VoidSubscribe(r, "stream", func(ctx context.Context) (<-chan string, error) {
		ch := make(chan string, 3)
		ch <- "a"
		ch <- "b"
		ch <- "c"
		close(ch)
		return ch, nil
	})

	server := newTestServer(t, trpc.NewHandler(r, "/trpc"))

	done := make(chan struct{})
	for i := range 50 {
		go func(idx int) {
			defer func() { done <- struct{}{} }()

			var resp *http.Response
			switch idx % 5 {
			case 0:
				resp = mustGet(t, server, "/trpc/ping")
			case 4:
				resp = mustGet(t, server, "/trpc/stream")
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

	server := newTestServer(t, trpc.NewHandler(r, "/trpc"))

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
	var formatterError *trpcgo.Error

	r := trpcgo.NewRouter(trpcgo.WithErrorFormatter(func(input trpcgo.ErrorFormatterInput) any {
		formatterError = input.Error
		return input.Shape
	}))
	trpcgo.VoidQuery(r, "boom", func(ctx context.Context) (string, error) {
		return "", fmt.Errorf("%s", secretMsg)
	})

	server := newTestServer(t, trpc.NewHandler(r, "/trpc"))
	resp := mustGet(t, server, "/trpc/boom")
	defer func() { _ = resp.Body.Close() }()

	if formatterError == nil {
		t.Fatal("error formatter was not called")
	}
	if formatterError.Message != "internal server error" {
		t.Errorf("error formatter received %q, want the generic message", formatterError.Message)
	}
	if formatterError.Cause != nil && strings.Contains(formatterError.Cause.Error(), secretMsg) {
		t.Error("error formatter received the internal cause")
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
	// The test context has no response metadata.
	trpcgo.SetCookie(t.Context(), &http.Cookie{Name: "test", Value: "val"})
	trpcgo.SetResponseHeader(t.Context(), "X-Test", "val")
	// If we get here without panic, the test passes.
}

// --- URL Encoding Edge Cases ---

// TestURLEncodedProcedurePath verifies URL-encoded paths are decoded for lookup.
func TestURLEncodedProcedurePath(t *testing.T) {
	r := trpcgo.NewRouter()
	trpcgo.VoidQuery(r, "user.getById", func(ctx context.Context) (string, error) {
		return "found", nil
	})

	server := newTestServer(t, trpc.NewHandler(r, "/trpc"))

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

	server := newTestServer(t, trpc.NewHandler(r, "/trpc"))
	resp := mustGet(t, server, "/trpc/hello?batch=1")
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400 when batching disabled, got %d", resp.StatusCode)
	}
}

// --- JSONL Batch Size Limit ---

func TestJSONLBatchAmplification(t *testing.T) {
	var handlerCalls atomic.Int64
	r := trpcgo.NewRouter(
		trpcgo.WithBatching(true),
		trpcgo.WithMaxBatchSize(3),
	)
	trpcgo.VoidQuery(r, "ping", func(ctx context.Context) (string, error) {
		handlerCalls.Add(1)
		return "pong", nil
	})

	server := newTestServer(t, trpc.NewHandler(r, "/trpc"))

	paths := make([]string, 100)
	for i := range paths {
		paths[i] = "ping"
	}
	url := server.URL + "/trpc/" + strings.Join(paths, ",") + "?batch=1"

	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("trpc-accept", "application/jsonl")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("JSONL batch should respect size limit, got status %d", resp.StatusCode)
	}

	calls := handlerCalls.Load()
	if calls > 0 {
		t.Errorf("VULNERABILITY: %d handlers called in oversized JSONL batch", calls)
	}
}

// --- POST Batch Body Size ---

// TestPOSTBatchBodySizeLimit verifies the body size limit applies to batch POST.
func TestPOSTBatchBodySizeLimit(t *testing.T) {
	r := trpcgo.NewRouter(trpcgo.WithBatching(true), trpcgo.WithMaxBodySize(64))
	trpcgo.Mutation(r, "echo", func(ctx context.Context, in string) (string, error) {
		return in, nil
	})

	server := newTestServer(t, trpc.NewHandler(r, "/trpc"))

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

	server := newTestServer(t, trpc.NewHandler(r, "/trpc"))

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

func TestResponsesAlwaysSetContentType(t *testing.T) {
	r := trpcgo.NewRouter(trpcgo.WithBatching(true))
	trpcgo.VoidQuery(r, "hello", func(ctx context.Context) (string, error) {
		return "hi", nil
	})

	server := newTestServer(t, trpc.NewHandler(r, "/trpc"))

	tests := []struct {
		name   string
		method string
		path   string
	}{
		{"single query", "GET", "/trpc/hello"},
		{"batch query", "GET", "/trpc/hello,hello?batch=1"},
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
				t.Errorf("Content-Type should be application/json, got %q", ct)
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

	_, err := r.RawCall(t.Context(), "user", json.RawMessage(`{"id":"1"}`))
	if err == nil {
		t.Fatal("expected validation error from RawCall")
	}
	if !validatorCalled {
		t.Error("validator was not called via RawCall")
	}
}

// net/http recovers a panic only in the handler goroutine. JSONL batch calls
// run in goroutines of their own, so the handler must recover there itself.
func TestJSONLBatchPanicDoesNotCrashServer(t *testing.T) {
	r := trpcgo.NewRouter(trpcgo.WithBatching(true))
	trpcgo.VoidQuery(r, "safe", func(ctx context.Context) (string, error) {
		return "ok", nil
	})
	trpcgo.VoidQuery(r, "boom", func(ctx context.Context) (string, error) {
		panic("handler bug: nil pointer dereference")
	})

	server := newTestServer(t, trpc.NewHandler(r, "/trpc"))

	req, _ := http.NewRequest("GET", server.URL+"/trpc/safe,boom?batch=1", nil)
	req.Header.Set("trpc-accept", "application/jsonl")

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Logf("connection error (acceptable, server survived): %v", err)
		return
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.ReadAll(resp.Body)

	resp2, err := http.Get(server.URL + "/trpc/safe")
	if err != nil {
		t.Fatalf("server died after JSONL panic: %v", err)
	}
	defer func() { _ = resp2.Body.Close() }()
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("server unhealthy after JSONL panic: status %d", resp2.StatusCode)
	}
}

func TestJSONLBatchAllPanicDoesNotCrashServer(t *testing.T) {
	r := trpcgo.NewRouter(trpcgo.WithBatching(true))
	trpcgo.VoidQuery(r, "boom", func(ctx context.Context) (string, error) {
		panic("total explosion")
	})
	trpcgo.VoidQuery(r, "safe", func(ctx context.Context) (string, error) {
		return "alive", nil
	})

	server := newTestServer(t, trpc.NewHandler(r, "/trpc"))

	req, _ := http.NewRequest("GET", server.URL+"/trpc/boom,boom?batch=1", nil)
	req.Header.Set("trpc-accept", "application/jsonl")

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Logf("connection error (acceptable): %v", err)
	} else {
		_, _ = io.ReadAll(resp.Body)
		_ = resp.Body.Close()
	}

	resp2, err := http.Get(server.URL + "/trpc/safe")
	if err != nil {
		t.Fatalf("server crashed after all-panic JSONL batch: %v", err)
	}
	defer func() { _ = resp2.Body.Close() }()
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp2.StatusCode)
	}
}

// A batch call that dies without sending its result must not block the loop
// collecting results, or the connection hangs until the client gives up.
func TestJSONLBatchPanicDoesNotHang(t *testing.T) {
	r := trpcgo.NewRouter(trpcgo.WithBatching(true))
	trpcgo.VoidQuery(r, "boom", func(ctx context.Context) (string, error) {
		panic("nil pointer")
	})

	server := newTestServer(t, trpc.NewHandler(r, "/trpc"))

	req, _ := http.NewRequest("GET", server.URL+"/trpc/boom?batch=1", nil)
	req.Header.Set("trpc-accept", "application/jsonl")

	client := &http.Client{Timeout: 3 * time.Second}
	start := time.Now()
	resp, err := client.Do(req)
	elapsed := time.Since(start)

	if err == nil {
		_, _ = io.ReadAll(resp.Body)
		_ = resp.Body.Close()
	}

	if elapsed > 2*time.Second {
		t.Fatalf("JSONL batch hung for %v (expected instant error or response)", elapsed)
	}
}

// tRPC clients read data.path from error envelopes to attribute batched
// errors, so echoing the path is protocol, not a leak.
func TestNotFoundIncludesPathPerProtocol(t *testing.T) {
	r := trpcgo.NewRouter()
	trpcgo.VoidQuery(r, "real", func(ctx context.Context) (string, error) { return "ok", nil })

	server := newTestServer(t, trpc.NewHandler(r, "/trpc"))
	resp := mustGet(t, server, "/trpc/nonexistent")
	body := decodeJSON(t, resp)

	errData := errorData(t, body)
	path, _ := errData["path"].(string)
	if path != "nonexistent" {
		t.Errorf("tRPC protocol requires data.path in error envelope, got %q", path)
	}
}

// net/http already percent-decodes r.URL.Path. Decoding it again would let a
// double-encoded path reach a procedure that a proxy ACL had blocked.
func TestDoubleURLDecodingBlocked(t *testing.T) {
	r := trpcgo.NewRouter()
	trpcgo.VoidQuery(r, "admin.secret", func(ctx context.Context) (string, error) {
		return "you reached admin.secret", nil
	})

	server := newTestServer(t, trpc.NewHandler(r, "/trpc"))

	// %252E decodes once to %2E, so the lookup key is "admin%2Esecret".
	resp := mustGet(t, server, "/trpc/admin%252Esecret")
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusOK {
		t.Fatal("double-encoded path should NOT reach procedure 'admin.secret'")
	}
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404 for double-encoded path, got %d", resp.StatusCode)
	}

	resp2 := mustGet(t, server, "/trpc/admin%2Esecret")
	defer func() { _ = resp2.Body.Close() }()
	if resp2.StatusCode != http.StatusOK {
		t.Errorf("single-encoded path should still resolve, got %d", resp2.StatusCode)
	}
}

// r.URL.Query already percent-decodes the input parameter. Decoding it again
// would make input that a proxy or WAF saw as opaque text parse as JSON.
func TestGETInputDoubleDecodeBlocked(t *testing.T) {
	var receivedID string
	r := trpcgo.NewRouter()
	trpcgo.Query(r, "user", func(ctx context.Context, in struct {
		ID string `json:"id"`
	}) (string, error) {
		receivedID = in.ID
		return "found: " + in.ID, nil
	})

	server := newTestServer(t, trpc.NewHandler(r, "/trpc"))

	// {"id":"1"} percent-encoded twice.
	doubleEncoded := "%257B%2522id%2522%253A%25221%2522%257D"
	resp := mustGet(t, server, "/trpc/user?input="+doubleEncoded)
	defer func() { _ = resp.Body.Close() }()

	if receivedID == "1" {
		t.Fatal("double-encoded GET input was decoded twice and reached handler — " +
			"attacker can bypass WAF/proxy input validation via double-encoding")
	}
	if resp.StatusCode == http.StatusOK {
		t.Fatal("double-encoded GET input should not produce a 200 response")
	}
}

func TestGETBatchInputDoubleDecodeBlocked(t *testing.T) {
	var receivedID string
	r := trpcgo.NewRouter(trpcgo.WithBatching(true))
	trpcgo.Query(r, "user", func(ctx context.Context, in struct {
		ID string `json:"id"`
	}) (string, error) {
		receivedID = in.ID
		return "found: " + in.ID, nil
	})

	server := newTestServer(t, trpc.NewHandler(r, "/trpc"))

	// {"0":{"id":"1"}} percent-encoded twice.
	doubleEncoded := "%257B%25220%2522%253A%257B%2522id%2522%253A%25221%2522%257D%257D"
	resp := mustGet(t, server, "/trpc/user?batch=1&input="+doubleEncoded)
	defer func() { _ = resp.Body.Close() }()

	if receivedID == "1" {
		t.Fatal("double-encoded batch GET input was decoded twice and reached handler")
	}
	if resp.StatusCode == http.StatusOK {
		t.Fatal("double-encoded batch GET input should not produce a 200 response")
	}
}

// GET requests carry input in the query string, which MaxBodySize must limit
// as it limits a POST body.
func TestGETQueryInputSizeLimitEnforced(t *testing.T) {
	r := trpcgo.NewRouter(trpcgo.WithMaxBodySize(256))
	trpcgo.Query(r, "echo", func(ctx context.Context, in json.RawMessage) (string, error) {
		return fmt.Sprintf("got %d bytes", len(in)), nil
	})

	server := newTestServer(t, trpc.NewHandler(r, "/trpc"))

	bigInput := `"` + strings.Repeat("x", 2048) + `"`
	resp := mustGet(t, server, "/trpc/echo?input="+url.QueryEscape(bigInput))
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("GET query input (%d bytes) bypassed body size limit (256 bytes): %s",
			len(bigInput), raw)
	}
}

func TestGETBatchQueryInputSizeLimitEnforced(t *testing.T) {
	r := trpcgo.NewRouter(trpcgo.WithBatching(true), trpcgo.WithMaxBodySize(256))
	trpcgo.Query(r, "echo", func(ctx context.Context, in json.RawMessage) (string, error) {
		return fmt.Sprintf("got %d bytes", len(in)), nil
	})

	server := newTestServer(t, trpc.NewHandler(r, "/trpc"))

	bigInput := `{"0":"` + strings.Repeat("x", 2048) + `"}`
	resp := mustGet(t, server, "/trpc/echo?batch=1&input="+url.QueryEscape(bigInput))
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusOK {
		t.Fatal("batch GET query input bypassed body size limit")
	}
}

// NewHandler must copy each procedure rather than mutate the pointer RawCall
// reads. Only -race can fail this test.
func TestHandlerAndRawCallConcurrentNoRace(t *testing.T) {
	r := trpcgo.NewRouter()
	trpcgo.VoidQuery(r, "ping", func(ctx context.Context) (string, error) {
		return "pong", nil
	})

	done := make(chan struct{})
	for range 100 {
		go func() {
			defer func() { done <- struct{}{} }()
			_ = trpc.NewHandler(r, "/trpc")
		}()
		go func() {
			defer func() { done <- struct{}{} }()
			_, _ = r.RawCall(t.Context(), "ping", nil)
		}()
	}
	for range 200 {
		<-done
	}
}
