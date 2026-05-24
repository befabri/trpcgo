package trpc

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
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

	cancelCtx, cancelParent := context.WithCancel(context.Background())
	valuesCtx := context.WithValue(context.Background(), ctxKey("user"), "alice")
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
	merged, stop := mergeContexts(context.Background(), context.Background())
	stop()

	select {
	case <-merged.Done():
	case <-time.After(time.Second):
		t.Fatal("merged context was not cancelled by returned cancel func")
	}
}

func TestWriteSSEDataSanitizesIDAndWritesRetry(t *testing.T) {
	rec := httptest.NewRecorder()
	writeSSEData(rec, []byte(`{"ok":true}`), "safe\ninjected\r-id", 2500)

	body := rec.Body.String()
	if !strings.Contains(body, "data: {\"ok\":true}\n") {
		t.Fatalf("body missing data line: %q", body)
	}
	if !strings.Contains(body, "id: safeinjected-id\n") {
		t.Fatalf("body did not sanitize event id: %q", body)
	}
	if strings.Contains(body, "id: safe\n") || strings.Contains(body, "injected\r") {
		t.Fatalf("body contains unsanitized id injection: %q", body)
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

	h.writeSingleResult(context.Background(), rec, callResult{
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

	h.writeBatchResults(context.Background(), rec, []callResult{
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

	closed := h.writeStreamItem(context.Background(), rec, struct {
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

	closed := h.writeStreamItem(context.Background(), rec, struct {
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
	tracked, ok := h.trackSSEConnection(rec, context.Background(), "events")
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

	tracked, ok := h.trackSSEConnection(httptest.NewRecorder(), context.Background(), "events")
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

	tracked, ok := h.trackSSEConnection(httptest.NewRecorder(), context.Background(), "events")
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
	ctx := context.WithValue(context.Background(), ctxKey{}, "from-context")

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
	baseCtx := context.WithValue(context.Background(), ctxKey{}, "request")
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
