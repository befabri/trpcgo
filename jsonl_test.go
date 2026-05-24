package trpcgo_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/befabri/trpcgo"
	"github.com/befabri/trpcgo/trpc"
)

type jsonlChunk struct {
	index    int
	status   int
	envelope map[string]any
}

func parseJSONLResponse(t *testing.T, resp *http.Response) (head map[string]any, chunks []jsonlChunk) {
	t.Helper()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read JSONL body: %v", err)
	}

	lines := strings.Split(strings.TrimSuffix(string(body), "\n"), "\n")
	if len(lines) < 1 {
		t.Fatal("JSONL response has no lines")
	}

	// Parse head line.
	if err := json.Unmarshal([]byte(lines[0]), &head); err != nil {
		t.Fatalf("parse JSONL head: %v\nline: %s", err, lines[0])
	}

	// Parse chunk lines: [chunkId, status, [[envelope]]]
	for _, line := range lines[1:] {
		if line == "" {
			continue
		}
		var parts []json.RawMessage
		if err := json.Unmarshal([]byte(line), &parts); err != nil {
			t.Fatalf("parse JSONL chunk: %v\nline: %s", err, line)
		}
		if len(parts) != 3 {
			t.Fatalf("chunk has %d elements, want 3", len(parts))
		}

		var idx int
		if err := json.Unmarshal(parts[0], &idx); err != nil {
			t.Fatalf("parse chunk index: %v", err)
		}
		var status int
		if err := json.Unmarshal(parts[1], &status); err != nil {
			t.Fatalf("parse chunk status: %v", err)
		}

		// parts[2] is [[envelope]] — unwrap EncodedValue.
		var encoded [][]map[string]any
		if err := json.Unmarshal(parts[2], &encoded); err != nil {
			t.Fatalf("parse chunk[%d] EncodedValue: %v\nraw: %s", idx, err, parts[2])
		}
		if len(encoded) != 1 || len(encoded[0]) != 1 {
			t.Fatalf("chunk[%d] EncodedValue should be [[envelope]], got %s", idx, parts[2])
		}
		chunks = append(chunks, jsonlChunk{index: idx, status: status, envelope: encoded[0][0]})
	}
	return head, chunks
}

func TestJSONLBatchResponse(t *testing.T) {
	tests := []struct {
		name   string
		method string
		path   string
		body   string // POST body (empty for GET)
		input  string // GET input query param
	}{
		{
			name:   "GET batch",
			method: "GET",
			path:   "/trpc/user.getById,user.getById?batch=1",
			input:  `{"0":{"id":"1"},"1":{"id":"2"}}`,
		},
		{
			name:   "POST batch",
			method: "POST",
			path:   "/trpc/user.create,user.create?batch=1",
			body:   `{"0":{"name":"Alice"},"1":{"name":"Bob"}}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := newTestServer(t, trpc.NewHandler(setupRouter(), "/trpc"))

			reqURL := server.URL + tt.path
			if tt.input != "" {
				reqURL += "&input=" + url.QueryEscape(tt.input)
			}
			var body io.Reader
			if tt.body != "" {
				body = strings.NewReader(tt.body)
			}
			req, _ := http.NewRequest(tt.method, reqURL, body)
			req.Header.Set("trpc-accept", "application/jsonl")
			if tt.body != "" {
				req.Header.Set("Content-Type", "application/json")
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = resp.Body.Close() }()

			if resp.StatusCode != 200 {
				t.Fatalf("status = %d, want 200", resp.StatusCode)
			}
			if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
				t.Errorf("Content-Type = %q, want application/json", ct)
			}
			if vary := resp.Header.Get("Vary"); vary != "trpc-accept" {
				t.Errorf("Vary = %q, want trpc-accept", vary)
			}

			head, chunks := parseJSONLResponse(t, resp)

			// Head should have placeholders for 2 results.
			if len(head) != 2 {
				t.Fatalf("head has %d entries, want 2", len(head))
			}

			// Should have 2 chunk lines.
			if len(chunks) != 2 {
				t.Fatalf("got %d chunks, want 2", len(chunks))
			}

			// Both chunks should be FULFILLED (status 0) with result envelopes.
			for _, ch := range chunks {
				if ch.status != 0 {
					t.Errorf("chunk[%d] status = %d, want 0 (FULFILLED)", ch.index, ch.status)
				}
				if _, ok := ch.envelope["result"]; !ok {
					t.Errorf("chunk[%d] missing result key", ch.index)
				}
			}
		})
	}
}

func TestJSONLBatchHeadFormat(t *testing.T) {
	server := newTestServer(t, trpc.NewHandler(setupRouter(), "/trpc"))

	input := url.QueryEscape(`{"0":{"id":"1"},"1":{"id":"2"}}`)
	req, _ := http.NewRequest("GET", server.URL+"/trpc/user.getById,user.getById?batch=1&input="+input, nil)
	req.Header.Set("trpc-accept", "application/jsonl")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()

	head, _ := parseJSONLResponse(t, resp)

	// Verify each head entry is [[placeholder], [null, 0, chunkId]].
	for i := range 2 {
		key := fmt.Sprintf("%d", i)
		entry, ok := head[key]
		if !ok {
			t.Fatalf("head missing key %q", key)
		}
		arr, ok := entry.([]any)
		if !ok || len(arr) != 2 {
			t.Fatalf("head[%q] should be [data, chunkDef], got %v", key, entry)
		}
		// First element: [0] (placeholder)
		dataSlot, ok := arr[0].([]any)
		if !ok || len(dataSlot) != 1 || dataSlot[0] != float64(0) {
			t.Errorf("head[%q] data slot should be [0], got %v", key, arr[0])
		}
		// Second element: [null, 0, chunkId]
		chunkDef, ok := arr[1].([]any)
		if !ok || len(chunkDef) != 3 {
			t.Fatalf("head[%q] chunkDef should be [null,0,N], got %v", key, arr[1])
		}
		if chunkDef[0] != nil {
			t.Errorf("head[%q] chunkDef[0] should be null, got %v", key, chunkDef[0])
		}
		if chunkDef[1] != float64(0) {
			t.Errorf("head[%q] chunkDef[1] (type) should be 0, got %v", key, chunkDef[1])
		}
		if chunkDef[2] != float64(i) {
			t.Errorf("head[%q] chunkDef[2] (chunkId) should be %d, got %v", key, i, chunkDef[2])
		}
	}
}

func TestJSONLBatchVaryHeader(t *testing.T) {
	server := newTestServer(t, trpc.NewHandler(setupRouter(), "/trpc"))

	// Standard batch (no JSONL) should also have Vary header.
	input := url.QueryEscape(`{"0":{"id":"1"}}`)
	resp := mustGet(t, server, "/trpc/user.getById?batch=1&input="+input)
	defer func() { _ = resp.Body.Close() }()

	if vary := resp.Header.Get("Vary"); vary != "trpc-accept" {
		t.Errorf("Vary = %q, want trpc-accept", vary)
	}
}

func TestJSONLBatchWithErrors(t *testing.T) {
	server := newTestServer(t, trpc.NewHandler(setupRouter(), "/trpc"))

	// One success, one error.
	input := url.QueryEscape(`{"0":{"id":"1"},"1":{"id":"404"}}`)
	req, _ := http.NewRequest("GET", server.URL+"/trpc/user.getById,user.getById?batch=1&input="+input, nil)
	req.Header.Set("trpc-accept", "application/jsonl")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != 200 {
		t.Errorf("JSONL status = %d, want 200 (always 200 for JSONL)", resp.StatusCode)
	}

	_, chunks := parseJSONLResponse(t, resp)

	// All chunks should be FULFILLED (status 0) — errors are in the envelope, not rejections.
	byIndex := map[int]map[string]any{}
	for _, ch := range chunks {
		if ch.status != 0 {
			t.Errorf("chunk[%d] status = %d, want 0 (FULFILLED)", ch.index, ch.status)
		}
		byIndex[ch.index] = ch.envelope
	}

	// Entry 0 should be a success.
	data := resultData(t, byIndex[0])
	if data["name"] != "Alice" {
		t.Errorf("chunk[0] name = %v, want Alice", data["name"])
	}

	// Entry 1 should be an error.
	errData := errorData(t, byIndex[1])
	if errData["code"] != "NOT_FOUND" {
		t.Errorf("chunk[1] error code = %v, want NOT_FOUND", errData["code"])
	}
}

func TestJSONLBatchConcurrency(t *testing.T) {
	r := trpcgo.NewRouter(trpcgo.WithBatching(true), trpcgo.WithMethodOverride(true))

	trpcgo.Query(r, "fast", func(ctx context.Context, input GetUserInput) (User, error) {
		return User{ID: "fast", Name: "Fast"}, nil
	})
	trpcgo.Query(r, "slow", func(ctx context.Context, input GetUserInput) (User, error) {
		time.Sleep(50 * time.Millisecond)
		return User{ID: "slow", Name: "Slow"}, nil
	})

	server := newTestServer(t, trpc.NewHandler(r, "/trpc"))

	// Batch: slow first, fast second. With concurrent execution,
	// the fast result should arrive as a chunk before the slow one.
	req, _ := http.NewRequest("POST", server.URL+"/trpc/slow,fast?batch=1",
		strings.NewReader(`{"0":{"id":"1"},"1":{"id":"2"}}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("trpc-accept", "application/jsonl")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()

	_, chunks := parseJSONLResponse(t, resp)
	if len(chunks) != 2 {
		t.Fatalf("got %d chunks, want 2", len(chunks))
	}

	// First chunk should be the fast handler (index 1), not the slow one (index 0).
	if chunks[0].index != 1 {
		t.Errorf("first chunk index = %d, want 1 (fast handler should complete first)", chunks[0].index)
	}
	if chunks[1].index != 0 {
		t.Errorf("second chunk index = %d, want 0 (slow handler)", chunks[1].index)
	}

	// Both should have correct data.
	for _, ch := range chunks {
		data := resultData(t, ch.envelope)
		if ch.index == 0 && data["name"] != "Slow" {
			t.Errorf("chunk[0] name = %v, want Slow", data["name"])
		}
		if ch.index == 1 && data["name"] != "Fast" {
			t.Errorf("chunk[1] name = %v, want Fast", data["name"])
		}
	}
}
