package trpc

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/befabri/trpcgo"
)

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
