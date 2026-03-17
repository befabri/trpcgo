package trpc

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/befabri/trpcgo"
)

// requireErrorCode asserts that err is a *trpcgo.Error with the expected code.
func requireErrorCode(t *testing.T, err error, want trpcgo.ErrorCode) {
	t.Helper()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var trpcErr *trpcgo.Error
	if !errors.As(err, &trpcErr) {
		t.Fatalf("expected *trpcgo.Error, got %T: %v", err, err)
	}
	if trpcErr.Code != want {
		t.Errorf("error code = %d (%s), want %d (%s)",
			trpcErr.Code, trpcgo.NameFromCode(trpcErr.Code),
			want, trpcgo.NameFromCode(want))
	}
}

func TestStripBasePath(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		basePath string
		want     string
		wantOK   bool
	}{
		{"empty base", "/user.get", "", "user.get", true},
		{"simple base", "/trpc/user.get", "/trpc", "user.get", true},
		{"trailing slash base", "/trpc/user.get", "/trpc/", "user.get", true},
		{"multiple trailing slashes", "/trpc/user.get", "/trpc///", "user.get", true},
		{"no leading slash base", "/trpc/user.get", "trpc", "user.get", true},
		{"root base", "/user.get", "/", "user.get", true},
		{"exact match no proc", "/trpc", "/trpc", "", true},
		{"mismatch", "/api/user.get", "/trpc", "", false},
		{"partial mismatch", "/trpcfoo/user.get", "/trpc", "", false},
		{"nested base", "/api/v1/user.get", "/api/v1", "user.get", true},
		{"nested proc path", "/trpc/user.list.all", "/trpc", "user.list.all", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := stripBasePath(tt.path, tt.basePath)
			if ok != tt.wantOK {
				t.Errorf("stripBasePath(%q, %q) ok = %v, want %v", tt.path, tt.basePath, ok, tt.wantOK)
			}
			if got != tt.want {
				t.Errorf("stripBasePath(%q, %q) = %q, want %q", tt.path, tt.basePath, got, tt.want)
			}
		})
	}
}

func TestContainsTraversal(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"user.get", false},
		{"user.list", false},
		{".", true},
		{"..", true},
		{"../etc/passwd", true},
		{"user/../admin", true},
		{"user/./get", true},
		{"user.get/something", false},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			if got := containsTraversal(tt.path); got != tt.want {
				t.Errorf("containsTraversal(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

func TestIsBatchRequest(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want bool
	}{
		{"no batch param", "/trpc/user.get", false},
		{"batch=1", "/trpc/user.get?batch=1", true},
		{"batch=0", "/trpc/user.get?batch=0", false},
		{"batch=true", "/trpc/user.get?batch=true", false},
		{"batch empty", "/trpc/user.get?batch=", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.url, nil)
			if got := isBatchRequest(req); got != tt.want {
				t.Errorf("isBatchRequest(%q) = %v, want %v", tt.url, got, tt.want)
			}
		})
	}
}

func TestParsePaths(t *testing.T) {
	tests := []struct {
		name     string
		url      string
		basePath string
		want     []string
	}{
		{"single path", "/trpc/user.get", "/trpc", []string{"user.get"}},
		{"batch paths", "/trpc/user.get,user.list", "/trpc", []string{"user.get", "user.list"}},
		{"exact base match returns nil", "/trpc", "/trpc", nil},
		{"wrong base", "/api/user.get", "/trpc", nil},
		// parsePaths does not validate traversal — that's the caller's job
		// (parseRequest checks containsTraversal after calling parsePaths).
		{"returns traversal paths unvalidated", "/trpc/../etc", "/trpc", []string{"../etc"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.url, nil)
			got := parsePaths(req, tt.basePath)
			if tt.want == nil {
				if got != nil {
					t.Errorf("parsePaths() = %v, want nil", got)
				}
				return
			}
			if len(got) != len(tt.want) {
				t.Fatalf("parsePaths() len = %d, want %d", len(got), len(tt.want))
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("parsePaths()[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestParseInput_GET(t *testing.T) {
	t.Run("no input", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/trpc/ping", nil)
		got, err := parseInput(req, 0)
		if err != nil {
			t.Fatal(err)
		}
		if got != nil {
			t.Errorf("expected nil input, got %s", got)
		}
	})

	t.Run("simple value", func(t *testing.T) {
		encoded := url.QueryEscape(`{"message":"hi"}`)
		req := httptest.NewRequest(http.MethodGet, "/trpc/echo?input="+encoded, nil)
		got, err := parseInput(req, 0)
		if err != nil {
			t.Fatal(err)
		}
		var m map[string]string
		if err := json.Unmarshal(got, &m); err != nil {
			t.Fatal(err)
		}
		if m["message"] != "hi" {
			t.Errorf("input message = %q, want %q", m["message"], "hi")
		}
	})

	t.Run("value with spaces", func(t *testing.T) {
		encoded := url.QueryEscape(`{"message":"hello world"}`)
		req := httptest.NewRequest(http.MethodGet, "/trpc/echo?input="+encoded, nil)
		got, err := parseInput(req, 0)
		if err != nil {
			t.Fatal(err)
		}
		var m map[string]string
		if err := json.Unmarshal(got, &m); err != nil {
			t.Fatal(err)
		}
		if m["message"] != "hello world" {
			t.Errorf("input message = %q, want %q", m["message"], "hello world")
		}
	})

	t.Run("exceeds max size returns CodePayloadTooLarge", func(t *testing.T) {
		longInput := strings.Repeat("a", 100)
		req := httptest.NewRequest(http.MethodGet, "/trpc/echo?input="+longInput, nil)
		_, err := parseInput(req, 10)
		requireErrorCode(t, err, trpcgo.CodePayloadTooLarge)
	})

	t.Run("valid JSON exceeds max size", func(t *testing.T) {
		obj := map[string]string{"data": strings.Repeat("x", 200)}
		raw, _ := json.Marshal(obj)
		encoded := url.QueryEscape(string(raw))
		req := httptest.NewRequest(http.MethodGet, "/trpc/echo?input="+encoded, nil)
		_, err := parseInput(req, 50)
		requireErrorCode(t, err, trpcgo.CodePayloadTooLarge)
	})
}

func TestParseInput_POST(t *testing.T) {
	t.Run("with body", func(t *testing.T) {
		body := `{"message":"hello"}`
		req := httptest.NewRequest(http.MethodPost, "/trpc/echo", strings.NewReader(body))
		got, err := parseInput(req, 0)
		if err != nil {
			t.Fatal(err)
		}
		var m map[string]string
		if err := json.Unmarshal(got, &m); err != nil {
			t.Fatal(err)
		}
		if m["message"] != "hello" {
			t.Errorf("input message = %q, want %q", m["message"], "hello")
		}
	})

	t.Run("empty body", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/trpc/ping", strings.NewReader(""))
		got, err := parseInput(req, 0)
		if err != nil {
			t.Fatal(err)
		}
		if got != nil {
			t.Errorf("expected nil input, got %s", got)
		}
	})

	t.Run("body exceeds max size returns CodePayloadTooLarge", func(t *testing.T) {
		body := strings.Repeat("x", 100)
		req := httptest.NewRequest(http.MethodPost, "/trpc/echo", strings.NewReader(body))
		_, err := parseInput(req, 10)
		requireErrorCode(t, err, trpcgo.CodePayloadTooLarge)
	})

	t.Run("valid JSON body exceeds max size", func(t *testing.T) {
		obj := map[string]string{"data": strings.Repeat("z", 200)}
		raw, _ := json.Marshal(obj)
		req := httptest.NewRequest(http.MethodPost, "/trpc/echo", strings.NewReader(string(raw)))
		_, err := parseInput(req, 50)
		requireErrorCode(t, err, trpcgo.CodePayloadTooLarge)
	})

	// PUT/DELETE/PATCH fall through to the readBody path (same as POST).
	t.Run("PUT reads body", func(t *testing.T) {
		body := `{"message":"put"}`
		req := httptest.NewRequest(http.MethodPut, "/trpc/echo", strings.NewReader(body))
		got, err := parseInput(req, 0)
		if err != nil {
			t.Fatal(err)
		}
		var m map[string]string
		if err := json.Unmarshal(got, &m); err != nil {
			t.Fatal(err)
		}
		if m["message"] != "put" {
			t.Errorf("input message = %q, want %q", m["message"], "put")
		}
	})
}

func TestParseRequest_Single(t *testing.T) {
	t.Run("no path returns CodeNotFound", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/trpc", nil)
		_, err := parseRequest(req, "/trpc", false, 0)
		requireErrorCode(t, err, trpcgo.CodeNotFound)
	})

	// In production, net/http cleans paths before dispatch, so ../
	// would rarely reach the handler. This is defense-in-depth.
	t.Run("traversal returns CodeBadRequest", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/trpc/../etc/passwd", nil)
		_, err := parseRequest(req, "/trpc", false, 0)
		requireErrorCode(t, err, trpcgo.CodeBadRequest)
	})

	t.Run("GET with input", func(t *testing.T) {
		encoded := url.QueryEscape(`{"message":"hi"}`)
		req := httptest.NewRequest(http.MethodGet, "/trpc/echo?input="+encoded, nil)
		calls, err := parseRequest(req, "/trpc", false, 0)
		if err != nil {
			t.Fatal(err)
		}
		if len(calls) != 1 {
			t.Fatalf("expected 1 call, got %d", len(calls))
		}
		if calls[0].path != "echo" {
			t.Errorf("path = %q, want %q", calls[0].path, "echo")
		}
	})

	t.Run("POST with body", func(t *testing.T) {
		body := `{"message":"posted"}`
		req := httptest.NewRequest(http.MethodPost, "/trpc/greet", strings.NewReader(body))
		calls, err := parseRequest(req, "/trpc", false, 0)
		if err != nil {
			t.Fatal(err)
		}
		if len(calls) != 1 {
			t.Fatalf("expected 1 call, got %d", len(calls))
		}
		if calls[0].path != "greet" {
			t.Errorf("path = %q, want %q", calls[0].path, "greet")
		}
		var m map[string]string
		if err := json.Unmarshal(calls[0].input, &m); err != nil {
			t.Fatalf("input unmarshal: %v", err)
		}
		if m["message"] != "posted" {
			t.Errorf("message = %q, want %q", m["message"], "posted")
		}
	})

	t.Run("POST with empty body", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/trpc/ping", strings.NewReader(""))
		calls, err := parseRequest(req, "/trpc", false, 0)
		if err != nil {
			t.Fatal(err)
		}
		if len(calls) != 1 {
			t.Fatalf("expected 1 call, got %d", len(calls))
		}
		if calls[0].input != nil {
			t.Errorf("expected nil input, got %s", calls[0].input)
		}
	})
}

func TestParseRequest_Batch(t *testing.T) {
	t.Run("GET batch", func(t *testing.T) {
		input := `{"0":{"message":"a"},"1":{"message":"b"}}`
		req := httptest.NewRequest(http.MethodGet, "/trpc/echo,greet?batch=1&input="+input, nil)
		calls, err := parseRequest(req, "/trpc", true, 0)
		if err != nil {
			t.Fatal(err)
		}
		if len(calls) != 2 {
			t.Fatalf("expected 2 calls, got %d", len(calls))
		}
		if calls[0].path != "echo" {
			t.Errorf("calls[0].path = %q, want %q", calls[0].path, "echo")
		}
		if calls[1].path != "greet" {
			t.Errorf("calls[1].path = %q, want %q", calls[1].path, "greet")
		}
		var m0, m1 map[string]string
		if err := json.Unmarshal(calls[0].input, &m0); err != nil {
			t.Fatalf("calls[0] input unmarshal: %v", err)
		}
		if err := json.Unmarshal(calls[1].input, &m1); err != nil {
			t.Fatalf("calls[1] input unmarshal: %v", err)
		}
		if m0["message"] != "a" {
			t.Errorf("calls[0] message = %q, want %q", m0["message"], "a")
		}
		if m1["message"] != "b" {
			t.Errorf("calls[1] message = %q, want %q", m1["message"], "b")
		}
	})

	t.Run("POST batch", func(t *testing.T) {
		body := `{"0":{"message":"x"}}`
		req := httptest.NewRequest(http.MethodPost, "/trpc/echo?batch=1", strings.NewReader(body))
		calls, err := parseRequest(req, "/trpc", true, 0)
		if err != nil {
			t.Fatal(err)
		}
		if len(calls) != 1 {
			t.Fatalf("expected 1 call, got %d", len(calls))
		}
		var m map[string]string
		if err := json.Unmarshal(calls[0].input, &m); err != nil {
			t.Fatalf("calls[0] input unmarshal: %v", err)
		}
		if m["message"] != "x" {
			t.Errorf("message = %q, want %q", m["message"], "x")
		}
	})

	t.Run("batch with no input", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/trpc/ping,ping?batch=1", nil)
		calls, err := parseRequest(req, "/trpc", true, 0)
		if err != nil {
			t.Fatal(err)
		}
		if len(calls) != 2 {
			t.Fatalf("expected 2 calls, got %d", len(calls))
		}
		if calls[0].input != nil {
			t.Errorf("expected nil input for calls[0], got %s", calls[0].input)
		}
	})

	t.Run("batch traversal returns CodeBadRequest", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/trpc/echo,../etc/passwd?batch=1", nil)
		_, err := parseRequest(req, "/trpc", true, 0)
		requireErrorCode(t, err, trpcgo.CodeBadRequest)
	})

	t.Run("GET batch malformed JSON returns CodeParseError", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/trpc/echo?batch=1&input=not-json", nil)
		_, err := parseRequest(req, "/trpc", true, 0)
		requireErrorCode(t, err, trpcgo.CodeParseError)
	})

	t.Run("POST batch malformed JSON returns CodeParseError", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/trpc/echo?batch=1", strings.NewReader("{broken"))
		_, err := parseRequest(req, "/trpc", true, 0)
		requireErrorCode(t, err, trpcgo.CodeParseError)
	})

	t.Run("GET batch oversized input returns CodePayloadTooLarge", func(t *testing.T) {
		bigInput := `{"0":"` + strings.Repeat("x", 200) + `"}`
		encoded := url.QueryEscape(bigInput)
		req := httptest.NewRequest(http.MethodGet, "/trpc/echo?batch=1&input="+encoded, nil)
		_, err := parseRequest(req, "/trpc", true, 50)
		requireErrorCode(t, err, trpcgo.CodePayloadTooLarge)
	})

	t.Run("batch missing index has nil input", func(t *testing.T) {
		// Two paths but input only for index 0.
		input := `{"0":{"message":"only-first"}}`
		req := httptest.NewRequest(http.MethodGet, "/trpc/echo,ping?batch=1&input="+input, nil)
		calls, err := parseRequest(req, "/trpc", true, 0)
		if err != nil {
			t.Fatal(err)
		}
		if len(calls) != 2 {
			t.Fatalf("expected 2 calls, got %d", len(calls))
		}
		if calls[1].input != nil {
			t.Errorf("expected nil input for calls[1], got %s", calls[1].input)
		}
	})

	// Empty segment from /trpc/echo,,ping produces path "". The parse layer
	// passes it through; the handler rejects it at procedure lookup.
	t.Run("empty segment in batch produces empty path", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/trpc/echo,,ping?batch=1", nil)
		calls, err := parseRequest(req, "/trpc", true, 0)
		if err != nil {
			t.Fatal(err)
		}
		if len(calls) != 3 {
			t.Fatalf("expected 3 calls, got %d", len(calls))
		}
		if calls[0].path != "echo" {
			t.Errorf("calls[0].path = %q, want %q", calls[0].path, "echo")
		}
		if calls[1].path != "" {
			t.Errorf("calls[1].path = %q, want empty string", calls[1].path)
		}
		if calls[2].path != "ping" {
			t.Errorf("calls[2].path = %q, want %q", calls[2].path, "ping")
		}
	})
}

func TestMergeLastEventId(t *testing.T) {
	t.Run("no lastEventId", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/trpc/sub", nil)
		input := json.RawMessage(`{"cursor":"abc"}`)
		got := mergeLastEventId(req, input)
		if string(got) != string(input) {
			t.Errorf("should return input unchanged, got %s", got)
		}
	})

	t.Run("header lastEventId into nil input", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/trpc/sub", nil)
		req.Header.Set("Last-Event-Id", "evt-42")
		got := mergeLastEventId(req, nil)
		var m map[string]string
		if err := json.Unmarshal(got, &m); err != nil {
			t.Fatal(err)
		}
		if m["lastEventId"] != "evt-42" {
			t.Errorf("lastEventId = %q, want %q", m["lastEventId"], "evt-42")
		}
	})

	t.Run("header lastEventId into null input", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/trpc/sub", nil)
		req.Header.Set("Last-Event-Id", "evt-7")
		got := mergeLastEventId(req, json.RawMessage("null"))
		var m map[string]string
		if err := json.Unmarshal(got, &m); err != nil {
			t.Fatal(err)
		}
		if m["lastEventId"] != "evt-7" {
			t.Errorf("lastEventId = %q, want %q", m["lastEventId"], "evt-7")
		}
	})

	t.Run("header merged into existing input", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/trpc/sub", nil)
		req.Header.Set("Last-Event-Id", "evt-99")
		input := json.RawMessage(`{"cursor":"abc"}`)
		got := mergeLastEventId(req, input)
		var m map[string]json.RawMessage
		if err := json.Unmarshal(got, &m); err != nil {
			t.Fatal(err)
		}
		var cursor string
		if err := json.Unmarshal(m["cursor"], &cursor); err != nil {
			t.Fatalf("cursor unmarshal: %v", err)
		}
		if cursor != "abc" {
			t.Errorf("cursor = %q, want %q", cursor, "abc")
		}
		var id string
		if err := json.Unmarshal(m["lastEventId"], &id); err != nil {
			t.Fatalf("lastEventId unmarshal: %v", err)
		}
		if id != "evt-99" {
			t.Errorf("lastEventId = %q, want %q", id, "evt-99")
		}
	})

	t.Run("header overwrites existing lastEventId in input", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/trpc/sub", nil)
		req.Header.Set("Last-Event-Id", "new")
		input := json.RawMessage(`{"lastEventId":"old","cursor":"abc"}`)
		got := mergeLastEventId(req, input)
		var m map[string]json.RawMessage
		if err := json.Unmarshal(got, &m); err != nil {
			t.Fatal(err)
		}
		var id string
		if err := json.Unmarshal(m["lastEventId"], &id); err != nil {
			t.Fatalf("lastEventId unmarshal: %v", err)
		}
		if id != "new" {
			t.Errorf("lastEventId = %q, want %q (header should overwrite)", id, "new")
		}
		var cursor string
		if err := json.Unmarshal(m["cursor"], &cursor); err != nil {
			t.Fatalf("cursor unmarshal: %v", err)
		}
		if cursor != "abc" {
			t.Errorf("cursor = %q, want %q (other fields should be preserved)", cursor, "abc")
		}
	})

	t.Run("query param fallback", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/trpc/sub?lastEventId=q-1", nil)
		got := mergeLastEventId(req, nil)
		var m map[string]string
		if err := json.Unmarshal(got, &m); err != nil {
			t.Fatal(err)
		}
		if m["lastEventId"] != "q-1" {
			t.Errorf("lastEventId = %q, want %q", m["lastEventId"], "q-1")
		}
	})

	t.Run("header takes precedence over query", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/trpc/sub?lastEventId=from-query", nil)
		req.Header.Set("Last-Event-Id", "from-header")
		got := mergeLastEventId(req, nil)
		var m map[string]string
		if err := json.Unmarshal(got, &m); err != nil {
			t.Fatal(err)
		}
		if m["lastEventId"] != "from-header" {
			t.Errorf("lastEventId = %q, want %q (header should take precedence)", m["lastEventId"], "from-header")
		}
	})

	t.Run("non-object input returned unchanged", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/trpc/sub", nil)
		req.Header.Set("Last-Event-Id", "evt-1")
		input := json.RawMessage(`"just a string"`)
		got := mergeLastEventId(req, input)
		if string(got) != string(input) {
			t.Errorf("non-object input should be returned unchanged, got %s", got)
		}
	})

	t.Run("array input returned unchanged", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/trpc/sub", nil)
		req.Header.Set("Last-Event-Id", "evt-1")
		input := json.RawMessage(`[1,2,3]`)
		got := mergeLastEventId(req, input)
		if string(got) != string(input) {
			t.Errorf("array input should be returned unchanged, got %s", got)
		}
	})

	t.Run("Last-Event-Id query param capitalized", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/trpc/sub?Last-Event-Id=cap-1", nil)
		got := mergeLastEventId(req, nil)
		var m map[string]string
		if err := json.Unmarshal(got, &m); err != nil {
			t.Fatal(err)
		}
		if m["lastEventId"] != "cap-1" {
			t.Errorf("lastEventId = %q, want %q", m["lastEventId"], "cap-1")
		}
	})

	t.Run("both query params set uses lowercase first", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/trpc/sub?lastEventId=lower&Last-Event-Id=upper", nil)
		got := mergeLastEventId(req, nil)
		var m map[string]string
		if err := json.Unmarshal(got, &m); err != nil {
			t.Fatal(err)
		}
		// mergeLastEventId checks lastEventId before Last-Event-Id.
		if m["lastEventId"] != "lower" {
			t.Errorf("lastEventId = %q, want %q (lowercase param should win)", m["lastEventId"], "lower")
		}
	})
}

func TestReadBody(t *testing.T) {
	t.Run("normal read", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("hello"))
		body, err := readBody(req, 0)
		if err != nil {
			t.Fatal(err)
		}
		if string(body) != "hello" {
			t.Errorf("body = %q, want %q", body, "hello")
		}
	})

	t.Run("within limit", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("hi"))
		body, err := readBody(req, 100)
		if err != nil {
			t.Fatal(err)
		}
		if string(body) != "hi" {
			t.Errorf("body = %q, want %q", body, "hi")
		}
	})

	t.Run("exceeds limit returns CodePayloadTooLarge", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(strings.Repeat("x", 100)))
		_, err := readBody(req, 10)
		requireErrorCode(t, err, trpcgo.CodePayloadTooLarge)
	})

	t.Run("unlimited reads large body", func(t *testing.T) {
		big := strings.Repeat("y", 10_000)
		req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(big))
		body, err := readBody(req, 0)
		if err != nil {
			t.Fatal(err)
		}
		if len(body) != 10_000 {
			t.Errorf("body len = %d, want %d", len(body), 10_000)
		}
	})

	t.Run("exactly at limit", func(t *testing.T) {
		data := strings.Repeat("z", 50)
		req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(data))
		body, err := readBody(req, 50)
		if err != nil {
			t.Fatal(err)
		}
		if string(body) != data {
			t.Errorf("body = %q, want %q", body, data)
		}
	})

	t.Run("one byte over limit", func(t *testing.T) {
		data := strings.Repeat("z", 51)
		req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(data))
		_, err := readBody(req, 50)
		requireErrorCode(t, err, trpcgo.CodePayloadTooLarge)
	})
}
