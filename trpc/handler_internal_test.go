package trpc

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/befabri/trpcgo"
)

func TestDetermineBatchStatus(t *testing.T) {
	tests := []struct {
		name    string
		results []callResult
		want    int
	}{
		{name: "empty", results: nil, want: http.StatusOK},
		{name: "all same", results: []callResult{{status: http.StatusNotFound}, {status: http.StatusNotFound}}, want: http.StatusNotFound},
		{name: "mixed", results: []callResult{{status: http.StatusOK}, {status: http.StatusNotFound}}, want: http.StatusMultiStatus},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := determineBatchStatus(tt.results); got != tt.want {
				t.Fatalf("determineBatchStatus() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestMergeContextsCarriesValuesAndCancellation(t *testing.T) {
	type ctxKey string

	cancelCtx, cancelParent := context.WithCancel(t.Context())
	valuesCtx := context.WithValue(t.Context(), ctxKey("user"), "alice")
	merged, stop := mergeContexts(cancelCtx, valuesCtx)
	defer stop()

	if got := merged.Value(ctxKey("user")); got != "alice" {
		t.Fatalf("merged context value = %v, want alice", got)
	}

	cancelParent()
	select {
	case <-merged.Done():
	case <-time.After(time.Second):
		t.Fatal("merged context was not cancelled when parent was cancelled")
	}
}

func TestMergeContextsCancelFuncCancelsMergedContext(t *testing.T) {
	merged, stop := mergeContexts(t.Context(), t.Context())
	stop()

	select {
	case <-merged.Done():
	case <-time.After(time.Second):
		t.Fatal("merged context was not cancelled by returned cancel func")
	}
}

func TestWriteSSEDataPreservesIDAndWritesRetry(t *testing.T) {
	rec := httptest.NewRecorder()
	writeSSEData(rec, []byte(`{"ok":true}`), "safe-id", 2500)

	body := rec.Body.String()
	if !strings.Contains(body, "data: {\"ok\":true}\n") {
		t.Fatalf("body missing data line: %q", body)
	}
	if !strings.Contains(body, "id: safe-id\n") {
		t.Fatalf("body did not preserve event id: %q", body)
	}

	if !strings.Contains(body, "retry: 2500\n") {
		t.Fatalf("body missing retry line: %q", body)
	}
}

func TestWriteSSEDataRetryBoundary(t *testing.T) {
	rec := httptest.NewRecorder()
	writeSSEData(rec, []byte(`{"ok":true}`), "", 0)
	if strings.Contains(rec.Body.String(), "retry:") {
		t.Fatalf("retry line written for zero retry: %q", rec.Body.String())
	}

	rec = httptest.NewRecorder()
	writeSSEData(rec, []byte(`{"ok":true}`), "", 1)
	if !strings.Contains(rec.Body.String(), "retry: 1\n") {
		t.Fatalf("retry line missing for retry=1: %q", rec.Body.String())
	}
}

func TestWriteSSENamedEventWritesSingleByteData(t *testing.T) {
	rec := httptest.NewRecorder()
	writeSSENamedEvent(rec, "ping", []byte("x"))

	if got, want := rec.Body.String(), "event: ping\ndata: x\n\n"; got != want {
		t.Fatalf("SSE event = %q, want %q", got, want)
	}
}

func TestWriteStreamReturnWithPayload(t *testing.T) {
	rec := httptest.NewRecorder()
	writeStreamReturn(rec, map[string]string{"cursor": "done"})

	body := rec.Body.String()
	if !strings.Contains(body, "event: return\n") || !strings.Contains(body, `"cursor":"done"`) {
		t.Fatalf("return event with payload not written correctly: %q", body)
	}
}

func TestWriteSingleResultMarshalErrorWrites500(t *testing.T) {
	h := NewHandler(trpcgo.NewRouter(), "/trpc")
	rec := httptest.NewRecorder()

	h.writeSingleResult(t.Context(), rec, callResult{
		response: trpcgo.NewResultEnvelope(func() {}),
		status:   http.StatusOK,
	})

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500; body: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "failed to serialize response") {
		t.Fatalf("body missing serialize error: %s", rec.Body.String())
	}
}

func TestWriteBatchResultsMarshalErrorWrites500(t *testing.T) {
	h := NewHandler(trpcgo.NewRouter(), "/trpc")
	rec := httptest.NewRecorder()

	h.writeBatchResults(t.Context(), rec, []callResult{
		{response: trpcgo.NewResultEnvelope("ok"), status: http.StatusOK},
		{response: trpcgo.NewResultEnvelope(func() {}), status: http.StatusOK},
	})

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500; body: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "failed to serialize response") {
		t.Fatalf("body missing serialize error: %s", rec.Body.String())
	}
}

func TestWriteStreamItemReceiveErrorCallsCallbackAndFormatsSSE(t *testing.T) {
	var callbackPath string
	var callbackErr *trpcgo.Error
	r := trpcgo.NewRouter(
		trpcgo.WithOnError(func(ctx context.Context, err *trpcgo.Error, path string) {
			callbackPath = path
			callbackErr = err
		}),
		trpcgo.WithErrorFormatter(func(input trpcgo.ErrorFormatterInput) any {
			return map[string]any{
				"code":    trpcgo.NameFromCode(input.Error.Code),
				"message": input.Error.Message,
				"path":    input.Path,
				"type":    input.Type,
			}
		}),
	)
	h := NewHandler(r, "/trpc")
	rec := httptest.NewRecorder()
	cause := errors.New("backend stream failed")

	closed := h.writeStreamItem(t.Context(), rec, struct {
		data  any
		id    string
		retry int
		err   error
	}{err: cause}, parsedRequest{path: "events", input: json.RawMessage(`{"cursor":"1"}`)})

	if !closed {
		t.Fatal("writeStreamItem should close stream after receive error")
	}
	if callbackPath != "events" {
		t.Fatalf("callback path = %q, want events", callbackPath)
	}
	if callbackErr == nil || !errors.Is(callbackErr.Cause, cause) {
		t.Fatalf("callback error cause = %v, want %v", callbackErr, cause)
	}
	body := rec.Body.String()
	for _, want := range []string{"event: serialized-error", "INTERNAL_SERVER_ERROR", "events", "subscription"} {
		if !strings.Contains(body, want) {
			t.Fatalf("serialized error body missing %q: %s", want, body)
		}
	}
	if strings.Contains(body, cause.Error()) {
		t.Fatalf("serialized error leaked receive cause: %s", body)
	}
}

func TestWriteStreamItemSerializationErrorCallsCallbackAndFormatsSSE(t *testing.T) {
	var callbackErr *trpcgo.Error
	r := trpcgo.NewRouter(
		trpcgo.WithOnError(func(ctx context.Context, err *trpcgo.Error, path string) {
			callbackErr = err
		}),
		trpcgo.WithErrorFormatter(func(input trpcgo.ErrorFormatterInput) any {
			return input.Shape
		}),
	)
	h := NewHandler(r, "/trpc")
	rec := httptest.NewRecorder()

	closed := h.writeStreamItem(t.Context(), rec, struct {
		data  any
		id    string
		retry int
		err   error
	}{data: struct {
		Fn func() `json:"fn"`
	}{Fn: func() {}}}, parsedRequest{path: "bad"})

	if !closed {
		t.Fatal("writeStreamItem should close stream after serialization error")
	}
	if callbackErr == nil || callbackErr.Message != "failed to serialize subscription data" {
		t.Fatalf("callback error = %#v, want serialization error", callbackErr)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "event: serialized-error") || !strings.Contains(body, "failed to serialize subscription data") {
		t.Fatalf("serialized error body missing serialization message: %s", body)
	}
}

func TestTrackSSEConnectionRejectsWhenLimitAlreadyReached(t *testing.T) {
	r := trpcgo.NewRouter(trpcgo.WithSSEMaxConnections(1))
	h := NewHandler(r, "/trpc")
	if got := r.TrackSSEConnection(1); got != 1 {
		t.Fatalf("initial connection count = %d, want 1", got)
	}
	defer r.TrackSSEConnection(-1)

	rec := httptest.NewRecorder()
	tracked, ok := h.trackSSEConnection(rec, t.Context(), "events")
	if tracked || ok {
		t.Fatalf("trackSSEConnection = (%v, %v), want rejected", tracked, ok)
	}
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429; body: %s", rec.Code, rec.Body.String())
	}
	if got := r.TrackSSEConnection(0); got != 1 {
		t.Fatalf("connection count after rejection = %d, want 1", got)
	}
}

func TestTrackSSEConnectionUnlimitedDoesNotTrack(t *testing.T) {
	r := trpcgo.NewRouter()
	h := NewHandler(r, "/trpc")

	tracked, ok := h.trackSSEConnection(httptest.NewRecorder(), t.Context(), "events")
	if !ok || tracked {
		t.Fatalf("trackSSEConnection = (%v, %v), want untracked success", tracked, ok)
	}
	if got := r.TrackSSEConnection(0); got != 0 {
		t.Fatalf("connection count = %d, want 0", got)
	}
}

func TestTrackSSEConnectionAllowsAtLimit(t *testing.T) {
	r := trpcgo.NewRouter(trpcgo.WithSSEMaxConnections(1))
	h := NewHandler(r, "/trpc")

	tracked, ok := h.trackSSEConnection(httptest.NewRecorder(), t.Context(), "events")
	if !tracked || !ok {
		t.Fatalf("trackSSEConnection = (%v, %v), want tracked success", tracked, ok)
	}
	if got := r.TrackSSEConnection(0); got != 1 {
		t.Fatalf("connection count = %d, want 1", got)
	}
	r.TrackSSEConnection(-1)
}

func TestWriteErrorResponseUsesFormatterWithContext(t *testing.T) {
	type ctxKey struct{}
	r := trpcgo.NewRouter(trpcgo.WithErrorFormatter(func(input trpcgo.ErrorFormatterInput) any {
		return map[string]any{
			"marker": input.Ctx.Value(ctxKey{}),
			"path":   input.Path,
		}
	}))
	h := NewHandler(r, "/trpc")
	rec := httptest.NewRecorder()
	ctx := context.WithValue(t.Context(), ctxKey{}, "from-context")

	h.writeErrorResponse(rec, trpcgo.NewError(trpcgo.CodeBadRequest, "bad"), "events", ctx, trpcgo.ProcedureQuery)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "from-context") || !strings.Contains(body, "events") {
		t.Fatalf("formatter output missing context marker/path: %s", body)
	}
}

func TestRequestContextReturningSameContextKeepsOriginal(t *testing.T) {
	type ctxKey struct{}
	baseCtx := context.WithValue(t.Context(), ctxKey{}, "request")
	r := trpcgo.NewRouter(trpcgo.WithContextCreator(func(ctx context.Context, r *http.Request) context.Context {
		return ctx
	}))
	h := NewHandler(r, "/trpc")
	req := httptest.NewRequest(http.MethodGet, "/trpc/ping", nil).WithContext(baseCtx)

	ctx, cancel := h.requestContext(req)
	defer cancel()

	if ctx != baseCtx {
		t.Fatalf("requestContext returned %T, want original request context", ctx)
	}
	if got := ctx.Value(ctxKey{}); got != "request" {
		t.Fatalf("context value = %v, want request", got)
	}
}

func TestStreamFormatterPreservesCustomErrorFields(t *testing.T) {
	envelope := trpcgo.DefaultErrorEnvelope(trpcgo.NewError(trpcgo.CodeForbidden, "denied"), "events", false)
	for _, tc := range []struct {
		name            string
		formatted, want any
	}{
		{"map", map[string]any{"error": "domain error", "detail": "kept"}, map[string]any{"error": "domain error", "detail": "kept"}},
		{"struct", struct {
			Error  string `json:"error"`
			Detail string `json:"detail"`
		}{"domain error", "kept"}, map[string]any{"error": "domain error", "detail": "kept"}},
		{"raw", json.RawMessage(`{"error":{"nested":true},"detail":"kept"}`), json.RawMessage(`{"error":{"nested":true},"detail":"kept"}`)},
		{"envelope", envelope, envelope.Error},
		{"pointer envelope", &envelope, envelope.Error},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := trpcgo.NewRouter(trpcgo.WithErrorFormatter(func(trpcgo.ErrorFormatterInput) any { return tc.formatted }))
			h := NewHandler(r, "/trpc")
			sse := httptest.NewRecorder()
			h.writeFormattedStreamError(context.Background(), sse, trpcgo.NewError(trpcgo.CodeForbidden, "denied"), parsedRequest{path: "events"})
			want, err := json.Marshal(tc.want)
			if err != nil {
				t.Fatal(err)
			}
			var gotValue, wantValue any
			payload := strings.TrimSuffix(strings.TrimPrefix(sse.Body.String(), "event: serialized-error\ndata: "), "\n\n")
			if err := json.Unmarshal([]byte(payload), &gotValue); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(want, &wantValue); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(gotValue, wantValue) {
				t.Fatalf("SSE changed formatter output: %s", sse.Body.String())
			}
			http := httptest.NewRecorder()
			h.writeErrorResponse(http, trpcgo.NewError(trpcgo.CodeForbidden, "denied"), "events", context.Background(), trpcgo.ProcedureSubscription)
			want, _ = json.Marshal(tc.formatted)
			if http.Body.String() != string(want) {
				t.Fatalf("HTTP changed formatter output: %s", http.Body.String())
			}
		})
	}
}

func TestLateErrorFormatterProcedureMeta(t *testing.T) {
	for _, transport := range []string{"sse", "jsonl"} {
		t.Run(transport, func(t *testing.T) {
			var seen trpcgo.ProcedureMeta
			r := trpcgo.NewRouter(trpcgo.WithErrorFormatter(func(in trpcgo.ErrorFormatterInput) any {
				seen, _ = trpcgo.GetProcedureMeta(in.Ctx)
				return in.Shape
			}))
			trpcgo.MustVoidQuery(r, "value", func(context.Context) (any, error) { return func() {}, nil }, trpcgo.WithMeta("fixture"))
			h := NewHandler(r, "/trpc")
			call := parsedRequest{path: "value"}
			if transport == "sse" {
				h.writeFormattedStreamError(context.Background(), httptest.NewRecorder(), trpcgo.NewError(trpcgo.CodeForbidden, "denied"), call)
			} else {
				data := h.marshalJSONLResult(context.Background(), 0, func() {}, call)
				if !strings.Contains(string(data), "error") {
					t.Fatalf("missing error: %s", data)
				}
			}
			if seen.Path != "value" || seen.Type != trpcgo.ProcedureQuery || seen.Meta != "fixture" {
				t.Fatalf("formatter metadata=%+v", seen)
			}
		})
	}
}
