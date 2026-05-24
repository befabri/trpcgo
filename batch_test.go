package trpcgo_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/befabri/trpcgo"
	"github.com/befabri/trpcgo/trpc"
)

func TestBatchGET(t *testing.T) {
	server := newTestServer(t, trpc.NewHandler(setupRouter(), "/trpc"))

	input := url.QueryEscape(`{"0":{"id":"1"},"1":{"id":"2"}}`)
	resp := mustGet(t, server, "/trpc/user.getById,user.getById?batch=1&input="+input)
	if resp.StatusCode != 200 {
		_ = resp.Body.Close()
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
	server := newTestServer(t, trpc.NewHandler(setupRouter(), "/trpc"))

	resp := mustPost(t, server, "/trpc/user.create,user.create?batch=1", `{"0":{"name":"Alice"},"1":{"name":"Bob"}}`)
	if resp.StatusCode != 200 {
		_ = resp.Body.Close()
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
	server := newTestServer(t, trpc.NewHandler(setupRouter(), "/trpc"))

	// First succeeds (id=1), second fails (id=404)
	input := url.QueryEscape(`{"0":{"id":"1"},"1":{"id":"404"}}`)
	resp := mustGet(t, server, "/trpc/user.getById,user.getById?batch=1&input="+input)
	if resp.StatusCode != 207 {
		_ = resp.Body.Close()
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
	server := newTestServer(t, trpc.NewHandler(setupRouter(), "/trpc"))

	input := url.QueryEscape(`{"0":{"id":"404"},"1":{"id":"404"}}`)
	resp := mustGet(t, server, "/trpc/user.getById,user.getById?batch=1&input="+input)
	if resp.StatusCode != 404 {
		_ = resp.Body.Close()
		t.Fatalf("status = %d, want 404 (all same error → unified status, not 207)", resp.StatusCode)
	}
}

func TestBatchDifferentProcedures(t *testing.T) {
	server := newTestServer(t, trpc.NewHandler(setupRouter(), "/trpc"))

	input := url.QueryEscape(`{"0":{},"1":{"id":"5"}}`)
	resp := mustGet(t, server, "/trpc/hello,user.getById?batch=1&input="+input)
	if resp.StatusCode != 200 {
		_ = resp.Body.Close()
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
	server := newTestServer(t, trpc.NewHandler(setupRouter(), "/trpc"))

	input := url.QueryEscape(`{"0":{},"1":{}}`)
	resp := mustGet(t, server, "/trpc/hello,nonexistent?batch=1&input="+input)
	if resp.StatusCode != 207 {
		_ = resp.Body.Close()
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
	server := newTestServer(t, trpc.NewHandler(router, "/trpc"))

	resp := mustGet(t, server, "/trpc/hello,hello?batch=1")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != 400 {
		t.Fatalf("status = %d, want 400 (batching disabled)", resp.StatusCode)
	}
}

func TestBatchSizeLimitDefault(t *testing.T) {
	router := trpcgo.NewRouter(trpcgo.WithBatching(true))

	trpcgo.VoidMutation(router, "ping", func(ctx context.Context) (string, error) {
		return "pong", nil
	})

	server := newTestServer(t, trpc.NewHandler(router, "/trpc"))

	// 11 paths exceeds default limit of 10.
	paths := strings.Repeat("ping,", 10) + "ping"
	var inputs strings.Builder
	inputs.WriteString("{")
	for i := range 11 {
		if i > 0 {
			inputs.WriteString(",")
		}
		fmt.Fprintf(&inputs, `"%d":null`, i)
	}
	inputs.WriteString("}")

	resp := mustPost(t, server, "/trpc/"+paths+"?batch=1", inputs.String())
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != 400 {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "batch size") {
		t.Errorf("error should mention batch size, got: %s", body)
	}
}

func TestBatchSizeLimitWithinLimit(t *testing.T) {
	router := trpcgo.NewRouter(trpcgo.WithBatching(true))

	trpcgo.VoidMutation(router, "ping", func(ctx context.Context) (string, error) {
		return "pong", nil
	})

	server := newTestServer(t, trpc.NewHandler(router, "/trpc"))

	// 2 paths is within default limit of 10.
	resp := mustPost(t, server, "/trpc/ping,ping?batch=1", `{"0":null,"1":null}`)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
}

func TestBatchSizeLimitCustom(t *testing.T) {
	router := trpcgo.NewRouter(trpcgo.WithBatching(true), trpcgo.WithMaxBatchSize(2))

	trpcgo.VoidMutation(router, "ping", func(ctx context.Context) (string, error) {
		return "pong", nil
	})

	server := newTestServer(t, trpc.NewHandler(router, "/trpc"))

	// 3 paths exceeds custom limit of 2.
	resp := mustPost(t, server, "/trpc/ping,ping,ping?batch=1", `{"0":null,"1":null,"2":null}`)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != 400 {
		t.Fatalf("status = %d, want 400 for batch exceeding custom limit", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "batch size") {
		t.Errorf("error should mention batch size, got: %s", body)
	}
}

func TestBatchSizeLimitDisabled(t *testing.T) {
	router := trpcgo.NewRouter(trpcgo.WithBatching(true), trpcgo.WithMaxBatchSize(-1))

	trpcgo.VoidMutation(router, "ping", func(ctx context.Context) (string, error) {
		return "pong", nil
	})

	server := newTestServer(t, trpc.NewHandler(router, "/trpc"))

	// 15 paths with limit disabled should succeed.
	paths := strings.Repeat("ping,", 14) + "ping"
	var inputs strings.Builder
	inputs.WriteString("{")
	for i := range 15 {
		if i > 0 {
			inputs.WriteString(",")
		}
		fmt.Fprintf(&inputs, `"%d":null`, i)
	}
	inputs.WriteString("}")

	resp := mustPost(t, server, "/trpc/"+paths+"?batch=1", inputs.String())
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d, want 200 with batch limit disabled", resp.StatusCode)
	}
}

func TestBatchSizeLimitJSONL(t *testing.T) {
	router := trpcgo.NewRouter(trpcgo.WithBatching(true), trpcgo.WithMaxBatchSize(2))

	trpcgo.VoidMutation(router, "ping", func(ctx context.Context) (string, error) {
		return "pong", nil
	})

	server := newTestServer(t, trpc.NewHandler(router, "/trpc"))

	// 3 paths exceeds limit of 2, should be rejected even for JSONL.
	req, _ := http.NewRequest("POST", server.URL+"/trpc/ping,ping,ping?batch=1",
		strings.NewReader(`{"0":null,"1":null,"2":null}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("trpc-accept", "application/jsonl")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != 400 {
		t.Fatalf("status = %d, want 400 for JSONL batch exceeding limit", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "batch size") {
		t.Errorf("error should mention batch size, got: %s", body)
	}
}
