package trpcgo_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/befabri/trpcgo"
	"github.com/befabri/trpcgo/trpc"
)

const procedurePanicSecret = "private-procedure-panic-detail"

type panicDecodeInput struct{}

func (*panicDecodeInput) UnmarshalJSON([]byte) error { panic(procedurePanicSecret) }

func TestProcedurePanicRecovery(t *testing.T) {
	for _, stage := range []string{"decode", "validator", "global middleware", "procedure middleware", "middleware after handler", "handler", "output validator", "output parser"} {
		for _, call := range []string{"ExecuteEntry", "RawCall", "HTTP", "HTTP dev"} {
			t.Run(stage+"/"+call, func(t *testing.T) {
				reported := make(chan *trpcgo.Error, 4)
				formatted := 0
				r := trpcgo.NewRouter(
					trpcgo.WithDev(call == "HTTP dev"),
					trpcgo.WithOnError(func(ctx context.Context, err *trpcgo.Error, path string) {
						meta, ok := trpcgo.GetProcedureMeta(ctx)
						if path != "broken" || !ok || meta.Path != path || meta.Type != trpcgo.ProcedureQuery {
							t.Errorf("panic callback lost procedure context: path=%q, meta=%+v", path, meta)
						}
						reported <- err
					}),
					trpcgo.WithErrorFormatter(func(input trpcgo.ErrorFormatterInput) any {
						formatted++
						assertSanitizedInternalError(t, input.Error)
						return input.Shape
					}),
					trpcgo.WithValidator(func(any) error {
						if stage == "validator" {
							panic(procedurePanicSecret)
						}
						return nil
					}),
				)
				t.Cleanup(func() { _ = r.Close() })
				var opts []trpcgo.ProcedureOption
				middleware := func(next trpcgo.HandlerFunc) trpcgo.HandlerFunc {
					return func(ctx context.Context, input any) (any, error) {
						if stage == "middleware after handler" {
							_, _ = next(ctx, input)
						}
						panic(procedurePanicSecret)
					}
				}
				switch stage {
				case "global middleware":
					r.Use(middleware)
				case "procedure middleware", "middleware after handler":
					opts = append(opts, trpcgo.Use(middleware))
				case "output validator":
					opts = append(opts, trpcgo.OutputValidator(func(string) error { panic(procedurePanicSecret) }))
				case "output parser":
					opts = append(opts, trpcgo.OutputParser(func(string) (string, error) { panic(procedurePanicSecret) }))
				}
				if stage == "decode" {
					trpcgo.MustQuery(r, "broken", func(context.Context, panicDecodeInput) (string, error) {
						t.Error("handler ran after decoding panicked")
						return "", nil
					})
				} else {
					trpcgo.MustQuery(r, "broken", func(context.Context, struct{}) (string, error) {
						if stage == "handler" {
							panic(procedurePanicSecret)
						}
						return "partial result must not escape", nil
					}, opts...)
				}
				switch call {
				case "ExecuteEntry":
					entry, _ := r.BuildProcedureMap().Lookup("broken")
					result, err := r.ExecuteEntry(t.Context(), entry, []byte(`{}`))
					if result != nil {
						t.Fatalf("partial result escaped: %v", result)
					}
					assertPanicCause(t, err)
				case "RawCall":
					result, err := r.RawCall(t.Context(), "broken", []byte(`{}`))
					if result != nil {
						t.Fatalf("partial result escaped: %v", result)
					}
					assertSanitizedInternalError(t, err)
				default:
					rec := httptest.NewRecorder()
					trpc.NewHandler(r, "/trpc").ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/trpc/broken?input=%7B%7D", nil))
					if rec.Code != http.StatusInternalServerError {
						t.Fatalf("status=%d, body=%s", rec.Code, rec.Body.String())
					}
					var envelope trpcgo.ErrorEnvelope
					if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
						t.Fatal(err)
					}
					if envelope.Error.Code != trpcgo.CodeInternalServerError || envelope.Error.Message != "internal server error" || envelope.Error.Data.Path != "broken" {
						t.Fatalf("unexpected error envelope: %+v", envelope)
					}
					if strings.Contains(rec.Body.String(), procedurePanicSecret) || strings.Contains(rec.Body.String(), "partial result") {
						t.Fatalf("response leaked panic or partial output: %s", rec.Body.String())
					}
					if call == "HTTP" && envelope.Error.Data.Stack != "" {
						t.Fatal("production response included a stack")
					}
					if formatted != 1 {
						t.Fatalf("formatter called %d times, want once", formatted)
					}
				}
				if call != "ExecuteEntry" {
					if len(reported) != 1 {
						t.Fatalf("panic reported %d times, want once", len(reported))
					}
					assertPanicCause(t, <-reported)
				}
			})
		}
	}
}

func assertSanitizedInternalError(t *testing.T, err error) {
	t.Helper()
	trpcErr, ok := errors.AsType[*trpcgo.Error](err)
	if !ok || trpcErr.Code != trpcgo.CodeInternalServerError || trpcErr.Message != "internal server error" || trpcErr.Cause != nil {
		t.Fatalf("expected sanitized INTERNAL_SERVER_ERROR, got %v", err)
	}
}

func assertPanicCause(t *testing.T, err error) {
	t.Helper()
	trpcErr, ok := errors.AsType[*trpcgo.Error](err)
	if !ok || trpcErr.Code != trpcgo.CodeInternalServerError {
		t.Fatalf("expected INTERNAL_SERVER_ERROR, got %v", err)
	}
	cause, ok := errors.AsType[*trpcgo.PanicError](err)
	if !ok || cause.Value != procedurePanicSecret || !bytes.Contains(cause.Stack, []byte("dispatch_test.go")) {
		t.Fatalf("lost original panic value or stack: %v", err)
	}
	if !strings.Contains(err.Error(), procedurePanicSecret) || !strings.Contains(err.Error(), "dispatch_test.go") {
		t.Fatalf("logging the error loses diagnostics: %v", err)
	}
}

func TestProcedurePanicValues(t *testing.T) {
	for _, value := range []any{nil, procedurePanicSecret, errors.New(procedurePanicSecret), trpcgo.NewError(trpcgo.CodeBadRequest, procedurePanicSecret), []string{procedurePanicSecret}} {
		r := trpcgo.NewRouter(trpcgo.WithOnError(func(context.Context, *trpcgo.Error, string) {}))
		trpcgo.MustVoidQuery(r, "broken", func(context.Context) (string, error) { panic(value) })
		entry, _ := r.BuildProcedureMap().Lookup("broken")
		result, err := r.ExecuteEntry(t.Context(), entry, nil)
		trpcErr, ok := errors.AsType[*trpcgo.Error](err)
		if result != nil || !ok || trpcErr.Code != trpcgo.CodeInternalServerError {
			t.Fatalf("panic(%T) returned (%v, %v)", value, result, err)
		}
		if original, ok := value.(error); ok && !errors.Is(err, original) {
			t.Fatalf("lost panic's original error: %v", err)
		}
		_, err = trpcgo.Call[struct{}, string](r, t.Context(), "broken", struct{}{})
		assertSanitizedInternalError(t, err)
		_ = r.Close()
	}
}

func TestProcedurePanicDefaultLoggingAndUnwinding(t *testing.T) {
	var logs bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(previous) })
	r := trpcgo.NewRouter()
	t.Cleanup(func() { _ = r.Close() })
	unwound := false
	r.Use(func(next trpcgo.HandlerFunc) trpcgo.HandlerFunc {
		return func(ctx context.Context, input any) (any, error) {
			defer func() { unwound = true }()
			return next(ctx, input)
		}
	})
	trpcgo.MustVoidQuery(r, "broken", func(context.Context) (string, error) { panic(procedurePanicSecret) })
	_, err := r.RawCall(t.Context(), "broken", nil)
	assertSanitizedInternalError(t, err)
	if !unwound {
		t.Fatal("recovery skipped middleware's deferred cleanup")
	}
	for _, want := range []string{`procedure "broken"`, procedurePanicSecret, "dispatch_test.go"} {
		if !strings.Contains(logs.String(), want) {
			t.Errorf("default panic log missing %q: %s", want, logs.String())
		}
	}
}

func TestProcedurePanicSubscriptionSetup(t *testing.T) {
	reported := make(chan *trpcgo.Error, 2)
	r := trpcgo.NewRouter(trpcgo.WithOnError(func(ctx context.Context, err *trpcgo.Error, path string) {
		meta, ok := trpcgo.GetProcedureMeta(ctx)
		if !ok || meta.Type != trpcgo.ProcedureSubscription || path != "broken" {
			t.Errorf("subscription panic lost procedure metadata: %+v, %q", meta, path)
		}
		reported <- err
	}))
	t.Cleanup(func() { _ = r.Close() })
	trpcgo.MustVoidSubscribe(r, "broken", func(context.Context) (<-chan string, error) { panic(procedurePanicSecret) })
	rec := httptest.NewRecorder()
	trpc.NewHandler(r, "/trpc").ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/trpc/broken", nil))
	if rec.Code != http.StatusInternalServerError || strings.Contains(rec.Body.String(), procedurePanicSecret) || strings.Contains(rec.Header().Get("Content-Type"), "event-stream") {
		t.Fatalf("subscription setup panic committed a stream or leaked details: status=%d, body=%s", rec.Code, rec.Body.String())
	}
	if len(reported) != 1 {
		t.Fatalf("reported %d subscription panics, want one", len(reported))
	}
	assertPanicCause(t, <-reported)
}

func TestProcedurePanicBatchIsolation(t *testing.T) {
	for _, jsonl := range []bool{false, true} {
		t.Run(map[bool]string{false: "batch", true: "JSONL"}[jsonl], func(t *testing.T) {
			reported := make(chan *trpcgo.Error, 4)
			r := trpcgo.NewRouter(trpcgo.WithOnError(func(_ context.Context, err *trpcgo.Error, _ string) { reported <- err }))
			t.Cleanup(func() { _ = r.Close() })
			trpcgo.MustVoidQuery(r, "broken", func(context.Context) (string, error) { panic(procedurePanicSecret) })
			trpcgo.MustVoidQuery(r, "invalid", func(context.Context) (string, error) {
				return "", trpcgo.NewError(trpcgo.CodeBadRequest, "invalid input")
			})
			trpcgo.MustVoidQuery(r, "ok", func(context.Context) (string, error) { return "healthy", nil })
			req := httptest.NewRequest(http.MethodGet, "/trpc/broken,invalid,ok?batch=1", nil)
			if jsonl {
				req.Header.Set("trpc-accept", "application/jsonl")
			}
			rec := httptest.NewRecorder()
			h := trpc.NewHandler(r, "/trpc")
			h.ServeHTTP(rec, req)
			var envelopes []map[string]any
			if jsonl {
				resp := rec.Result()
				defer func() { _ = resp.Body.Close() }()
				_, chunks := parseJSONLResponse(t, resp)
				envelopes = make([]map[string]any, 3)
				for _, chunk := range chunks {
					envelopes[chunk.index] = chunk.envelope
				}
			} else {
				if rec.Code != http.StatusMultiStatus {
					t.Fatalf("mixed batch status=%d, want 207", rec.Code)
				}
				if err := json.Unmarshal(rec.Body.Bytes(), &envelopes); err != nil {
					t.Fatal(err)
				}
			}
			if len(envelopes) != 3 {
				t.Fatalf("batch lost results: %v", envelopes)
			}
			if envelopes[0]["error"].(map[string]any)["message"] != "internal server error" || envelopes[1]["error"].(map[string]any)["message"] != "invalid input" || envelopes[2]["result"].(map[string]any)["data"] != "healthy" {
				t.Fatalf("panic affected sibling calls: %v", envelopes)
			}
			if len(reported) != 2 {
				t.Fatalf("reported %d errors, want exactly one panic and one ordinary error", len(reported))
			}
			panics := 0
			for range 2 {
				if err := <-reported; err.Code == trpcgo.CodeInternalServerError {
					assertPanicCause(t, err)
					panics++
				}
			}
			if panics != 1 {
				t.Fatalf("reported %d panics, want one", panics)
			}
			if strings.Contains(rec.Body.String(), procedurePanicSecret) {
				t.Fatal("batch response leaked the panic")
			}
		})
	}
}
