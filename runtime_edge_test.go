package trpcgo_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/befabri/trpcgo"
	"github.com/befabri/trpcgo/trpc"
)

func TestRawCallNullAnyInput(t *testing.T) {
	for _, raw := range []string{"", "null", " \nnull\t"} {
		t.Run(raw, func(t *testing.T) {
			defer failOnPanic(t)
			r := trpcgo.NewRouter()
			called := false
			trpcgo.MustQuery(r, "echo", func(_ context.Context, input any) (any, error) {
				called = true
				return input, nil
			})

			got, err := r.RawCall(t.Context(), "echo", json.RawMessage(raw))
			if err != nil || got != nil || !called {
				t.Fatalf("RawCall(%q) = (%v, %v), called=%v; want (nil, nil), called=true", raw, got, err, called)
			}
		})
	}
}

func TestSubscriptionNullAnyInput(t *testing.T) {
	defer failOnPanic(t)
	r := trpcgo.NewRouter()
	called := false
	trpcgo.MustSubscribe(r, "events", func(_ context.Context, input any) (<-chan string, error) {
		called = true
		if input != nil {
			t.Errorf("input = %v, want nil", input)
		}
		ch := make(chan string)
		close(ch)
		return ch, nil
	})

	rec := httptest.NewRecorder()
	trpc.NewHandler(r, "/trpc").ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/trpc/events?input=null", nil))
	if !called || rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "event: return\n") {
		t.Fatalf("called=%v, status=%d, body=%s; want a completed subscription", called, rec.Code, rec.Body.String())
	}
}

func TestHTTPInterfaceInput(t *testing.T) {
	for _, kind := range []string{"query", "mutation", "subscription", "subscriptionWithFinal"} {
		for _, raw := range []string{"", "null", " \nnull\t", `"hello"`} {
			t.Run(kind+"/"+raw, func(t *testing.T) {
				defer failOnPanic(t)
				r := trpcgo.NewRouter()
				calls := 0
				check := func(input any) {
					calls++
					var want any
					if raw == `"hello"` {
						want = "hello"
					}
					if input != want {
						t.Errorf("input=%v, want %v", input, want)
					}
				}
				handler := func(_ context.Context, input any) (string, error) { check(input); return "ok", nil }
				stream := func(_ context.Context, input any) (<-chan string, error) {
					check(input)
					ch := make(chan string)
					close(ch)
					return ch, nil
				}
				switch kind {
				case "query":
					trpcgo.MustQuery(r, "check", handler)
				case "mutation":
					trpcgo.MustMutation(r, "check", handler)
				case "subscription":
					trpcgo.MustSubscribe(r, "check", stream)
				case "subscriptionWithFinal":
					trpcgo.MustSubscribeWithFinal(r, "check", func(ctx context.Context, input any) (<-chan string, func() any, error) {
						ch, err := stream(ctx, input)
						return ch, func() any { return "done" }, err
					})
				}
				req := httptest.NewRequest(http.MethodGet, "/trpc/check?input="+url.QueryEscape(raw), nil)
				if kind == "mutation" {
					req = httptest.NewRequest(http.MethodPost, "/trpc/check", strings.NewReader(raw))
					req.Header.Set("Content-Type", "application/json")
				}
				rec := httptest.NewRecorder()
				trpc.NewHandler(r, "/trpc").ServeHTTP(rec, req)
				if rec.Code != http.StatusOK || calls != 1 {
					t.Fatalf("status=%d calls=%d body=%s", rec.Code, calls, rec.Body.String())
				}
			})
		}
	}
}

func TestSubscriptionReconnectPaddedNull(t *testing.T) {
	for _, raw := range []string{"null", " null ", "\tnull\n"} {
		t.Run(raw, func(t *testing.T) {
			defer failOnPanic(t)
			r := trpcgo.NewRouter()
			var lastEventID any
			trpcgo.MustSubscribe(r, "events", func(_ context.Context, input map[string]any) (<-chan string, error) {
				lastEventID = input["lastEventId"]
				ch := make(chan string)
				close(ch)
				return ch, nil
			})

			req := httptest.NewRequest(http.MethodGet, "/trpc/events?input="+url.QueryEscape(raw), nil)
			req.Header.Set("Last-Event-Id", "42")
			rec := httptest.NewRecorder()
			trpc.NewHandler(r, "/trpc").ServeHTTP(rec, req)
			if rec.Code != http.StatusOK || lastEventID != "42" {
				t.Fatalf("status=%d, lastEventId=%v; want 200, 42; body=%s", rec.Code, lastEventID, rec.Body.String())
			}
		})
	}
}

func TestSubscriptionNilItemsRunOutputHooks(t *testing.T) {
	for _, hook := range []string{"parser", "validator"} {
		t.Run(hook, func(t *testing.T) {
			r := trpcgo.NewRouter()
			calls := 0
			var option trpcgo.ProcedureOption
			if hook == "parser" {
				option = trpcgo.OutputParser(func(input any) (string, error) {
					calls++
					return "normalized", nil
				})
			} else {
				option = trpcgo.OutputValidator(func(input any) error {
					calls++
					return errors.New("nil output is invalid")
				})
			}
			trpcgo.MustVoidSubscribe(r, "events", func(_ context.Context) (<-chan any, error) {
				ch := make(chan any, 1)
				ch <- nil
				close(ch)
				return ch, nil
			}, option)

			rec := httptest.NewRecorder()
			trpc.NewHandler(r, "/trpc").ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/trpc/events", nil))
			if calls != 1 {
				t.Errorf("hook called %d times; want once for the nil item and never for EOF", calls)
			}
			body := rec.Body.String()
			if strings.Contains(body, "data: null\n") {
				t.Errorf("unprocessed nil item reached the client: %s", body)
			}
			if hook == "parser" && !strings.Contains(body, "data: \"normalized\"\n") {
				t.Errorf("missing parsed item: %s", body)
			}
			if hook == "validator" && !strings.Contains(body, "event: serialized-error\n") {
				t.Errorf("missing validation error: %s", body)
			}
		})
	}
}

func TestJSONLSerializationFailureProducesErrorChunk(t *testing.T) {
	r := trpcgo.NewRouter()
	trpcgo.MustVoidQuery(r, "bad", func(_ context.Context) (float64, error) {
		return math.NaN(), nil
	})
	trpcgo.MustVoidQuery(r, "good", func(_ context.Context) (string, error) {
		return "ok", nil
	})
	req := httptest.NewRequest(http.MethodGet, "/trpc/bad,good?batch=1", nil)
	req.Header.Set("trpc-accept", "application/jsonl")
	rec := httptest.NewRecorder()
	trpc.NewHandler(r, "/trpc").ServeHTTP(rec, req)
	resp := rec.Result()
	defer resp.Body.Close()
	head, chunks := parseJSONLResponse(t, resp)
	if len(head) != 2 || len(chunks) != 2 {
		t.Fatalf("head promises %d results, got %d chunks; want two completed results: %s", len(head), len(chunks), rec.Body.String())
	}
	byIndex := make(map[int]map[string]any)
	for _, chunk := range chunks {
		byIndex[chunk.index] = chunk.envelope
	}
	bad, ok := byIndex[0]["error"].(map[string]any)
	if !ok || bad["code"] != float64(trpcgo.CodeInternalServerError) {
		t.Errorf("bad result = %v, want INTERNAL_SERVER_ERROR", byIndex[0])
	}
	good, ok := byIndex[1]["result"].(map[string]any)
	if !ok || good["data"] != "ok" {
		t.Errorf("good result = %v, want successful sibling result", byIndex[1])
	}
}

type failingJSONLValue struct{}

func (failingJSONLValue) MarshalJSON() ([]byte, error) {
	return nil, errors.New("private serialization detail")
}

func TestJSONLSerializationFallbacks(t *testing.T) {
	for _, bad := range []any{math.Inf(1), func() {}, failingJSONLValue{}} {
		for _, brokenFormatter := range []bool{false, true} {
			t.Run(fmt.Sprintf("%T/formatterBroken=%t", bad, brokenFormatter), func(t *testing.T) {
				callbacks, formats := 0, 0
				r := trpcgo.NewRouter(
					trpcgo.WithOnError(func(_ context.Context, err *trpcgo.Error, path string) {
						callbacks++
						if path != "bad" || err.Cause == nil {
							t.Errorf("serialization callback: path=%q, error=%v", path, err)
						}
					}),
					trpcgo.WithErrorFormatter(func(in trpcgo.ErrorFormatterInput) any {
						formats++
						if in.Path != "bad" || in.Type != trpcgo.ProcedureQuery {
							t.Errorf("formatter metadata: %+v", in)
						}
						if brokenFormatter {
							return failingJSONLValue{}
						}
						return map[string]any{"error": in.Shape.Error, "custom": true}
					}),
				)
				trpcgo.MustVoidQuery(r, "bad", func(context.Context) (any, error) { return bad, nil })
				trpcgo.MustVoidQuery(r, "good", func(context.Context) (string, error) { return "ok", nil })
				req := httptest.NewRequest(http.MethodGet, "/trpc/good,bad,good?batch=1", nil)
				req.Header.Set("trpc-accept", "application/jsonl")
				rec := httptest.NewRecorder()
				trpc.NewHandler(r, "/trpc").ServeHTTP(rec, req)
				resp := rec.Result()
				defer resp.Body.Close()
				head, chunks := parseJSONLResponse(t, resp)
				if len(head) != 3 || len(chunks) != 3 || callbacks != 1 || formats != 1 {
					t.Fatalf("head=%d chunks=%d callbacks=%d formats=%d", len(head), len(chunks), callbacks, formats)
				}
				seen := map[int]bool{}
				for _, chunk := range chunks {
					if seen[chunk.index] || chunk.status != 0 {
						t.Fatalf("invalid or repeated chunk: %+v", chunk)
					}
					seen[chunk.index] = true
					if chunk.index == 1 {
						shape, ok := chunk.envelope["error"].(map[string]any)
						if !ok || shape["code"] != float64(trpcgo.CodeInternalServerError) || shape["message"] != "internal server error" {
							t.Errorf("invalid error envelope: %v", chunk.envelope)
						}
						if !brokenFormatter && chunk.envelope["custom"] != true {
							t.Error("custom formatter was not preserved")
						}
					} else if result, ok := chunk.envelope["result"].(map[string]any); !ok || result["data"] != "ok" {
						t.Errorf("successful sibling lost: %+v", chunk)
					}
				}
			})
		}
	}
}

// TestRawCallConcurrentUse is meaningful under -race: Use and RawCall must
// share the router's synchronization, including the middleware slice.
func TestRawCallConcurrentUse(t *testing.T) {
	r := trpcgo.NewRouter()
	trpcgo.MustVoidQuery(r, "ping", func(_ context.Context) (string, error) {
		return "pong", nil
	})
	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		for range 1000 {
			r.Use(func(next trpcgo.HandlerFunc) trpcgo.HandlerFunc { return next })
		}
	}()
	go func() {
		defer wg.Done()
		<-start
		for range 1000 {
			got, err := r.RawCall(t.Context(), "ping", nil)
			if err != nil || got != "pong" {
				t.Errorf("RawCall = (%v, %v), want (pong, nil)", got, err)
				return
			}
		}
	}()
	close(start)
	wg.Wait()
}

func TestRawCallMiddlewareSnapshot(t *testing.T) {
	r := trpcgo.NewRouter()
	var order []string
	mw := func(name string) trpcgo.Middleware {
		return func(next trpcgo.HandlerFunc) trpcgo.HandlerFunc {
			return func(ctx context.Context, input any) (any, error) {
				order = append(order, name)
				return next(ctx, input)
			}
		}
	}
	var once sync.Once
	r.Use(func(next trpcgo.HandlerFunc) trpcgo.HandlerFunc {
		once.Do(func() { r.Use(mw("later")) })
		return mw("first")(next)
	})
	trpcgo.MustVoidQuery(r, "ping", func(context.Context) (string, error) { return "pong", nil }, trpcgo.Use(mw("local")))
	for _, want := range []string{"first,local", "first,later,local"} {
		order = nil
		got, err := r.RawCall(t.Context(), "ping", nil)
		if err != nil || got != "pong" || strings.Join(order, ",") != want {
			t.Fatalf("RawCall=(%v,%v), order=%v, want %s", got, err, order, want)
		}
	}
}

// failOnPanic turns a panic into a test failure. Defer it directly in the
// goroutine under test; recover only works in the deferred function itself.
func failOnPanic(t *testing.T) {
	t.Helper()
	if recovered := recover(); recovered != nil {
		t.Fatalf("unexpected panic for valid input: %v", recovered)
	}
}
