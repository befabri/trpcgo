package trpcgo_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"testing"

	"github.com/befabri/trpcgo"
	"github.com/befabri/trpcgo/trpc"
)

// tRPC's TanStack React Query integration sends direction with every page of
// an infinite query, and browsers resend Last-Event-Id when an EventSource
// reconnects. Strict input drops these keys when the input struct does not
// declare them and keeps rejecting everything else.

type cursorPageInput struct {
	Limit  int     `json:"limit,omitempty"`
	Cursor *string `json:"cursor,omitempty"`
}

type cursorDirectionInput struct {
	Limit     int     `json:"limit,omitempty"`
	Cursor    *string `json:"cursor,omitempty"`
	Direction string  `json:"direction,omitempty"`
}

type cursorUntaggedDirectionInput struct {
	Cursor    *string `json:"cursor,omitempty"`
	Direction string
}

type offsetPageInput struct {
	Limit int `json:"limit,omitempty"`
	Page  int `json:"page,omitempty"`
}

func echoQuery[I any](t *testing.T, r *trpcgo.Router) {
	t.Helper()
	if err := trpcgo.Query(r, "list", func(ctx context.Context, in I) (I, error) { return in, nil }); err != nil {
		t.Fatal(err)
	}
}

func TestStrictInputInfiniteQueryDirection(t *testing.T) {
	tests := []struct {
		name     string
		register func(*testing.T, *trpcgo.Router)
		input    string
		wantData string
		wantCode string
		wantMsg  string
	}{
		{
			name:     "first page without cursor",
			register: echoQuery[cursorPageInput],
			input:    `{"limit":10,"direction":"forward"}`,
			wantData: `{"limit":10}`,
		},
		{
			name:     "next page with cursor",
			register: echoQuery[cursorPageInput],
			input:    `{"limit":10,"cursor":"abc","direction":"forward"}`,
			wantData: `{"limit":10,"cursor":"abc"}`,
		},
		{
			name:     "pointer input",
			register: echoQuery[*cursorPageInput],
			input:    `{"cursor":"abc","direction":"backward"}`,
			wantData: `{"cursor":"abc"}`,
		},
		{
			name:     "declared direction reaches the handler",
			register: echoQuery[cursorDirectionInput],
			input:    `{"cursor":"abc","direction":"backward"}`,
			wantData: `{"cursor":"abc","direction":"backward"}`,
		},
		{
			name:     "direction declared by field name reaches the handler",
			register: echoQuery[cursorUntaggedDirectionInput],
			input:    `{"cursor":"abc","direction":"forward"}`,
			wantData: `{"cursor":"abc","Direction":"forward"}`,
		},
		{
			name:     "query without cursor rejects direction",
			register: echoQuery[offsetPageInput],
			input:    `{"limit":10,"direction":"forward"}`,
			wantCode: "BAD_REQUEST",
			wantMsg:  "unknown field in input",
		},
		{
			name:     "other unknown fields are still rejected",
			register: echoQuery[cursorPageInput],
			input:    `{"cursor":"abc","direction":"forward","isAdmin":true}`,
			wantCode: "BAD_REQUEST",
			wantMsg:  "unknown field in input",
		},
		{
			name:     "differently cased key is not the protocol key",
			register: echoQuery[cursorPageInput],
			input:    `{"cursor":"abc","Direction":"forward"}`,
			wantCode: "BAD_REQUEST",
			wantMsg:  "unknown field in input",
		},
		{
			name:     "type mismatch is still reported",
			register: echoQuery[cursorPageInput],
			input:    `{"limit":"ten","direction":"forward"}`,
			wantCode: "BAD_REQUEST",
			wantMsg:  "invalid input type",
		},
		{
			name:     "trailing tokens are still rejected",
			register: echoQuery[cursorPageInput],
			input:    `{"cursor":"abc","direction":"forward"} 1`,
			wantCode: "PARSE_ERROR",
			wantMsg:  "failed to parse input",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := trpcgo.NewRouter()
			tt.register(t, r)
			server := newTestServer(t, trpc.NewHandler(r, "/trpc"))

			resp := mustGet(t, server, "/trpc/list?input="+url.QueryEscape(tt.input))
			envelope := decodeJSON(t, resp)
			if tt.wantCode != "" {
				if code := errorData(t, envelope)["code"]; code != tt.wantCode {
					t.Fatalf("error code = %v, want %s (envelope %v)", code, tt.wantCode, envelope)
				}
				errEnv, _ := envelope["error"].(map[string]any)
				if msg, _ := errEnv["message"].(string); msg != tt.wantMsg {
					t.Errorf("error message = %q, want %q", msg, tt.wantMsg)
				}
				return
			}
			got, err := json.Marshal(resultScalar(t, envelope))
			if err != nil {
				t.Fatal(err)
			}
			var want any
			if err := json.Unmarshal([]byte(tt.wantData), &want); err != nil {
				t.Fatal(err)
			}
			wantJSON, _ := json.Marshal(want)
			if string(got) != string(wantJSON) {
				t.Errorf("handler input = %s, want %s", got, wantJSON)
			}
		})
	}
}

func TestStrictInputMutationRejectsDirection(t *testing.T) {
	r := trpcgo.NewRouter()
	if err := trpcgo.Mutation(r, "archive", func(ctx context.Context, in cursorPageInput) (cursorPageInput, error) {
		return in, nil
	}); err != nil {
		t.Fatal(err)
	}
	server := newTestServer(t, trpc.NewHandler(r, "/trpc"))

	resp := mustPost(t, server, "/trpc/archive", `{"cursor":"abc","direction":"forward"}`)
	envelope := decodeJSON(t, resp)
	if code := errorData(t, envelope)["code"]; code != "BAD_REQUEST" {
		t.Fatalf("error code = %v, want BAD_REQUEST", code)
	}
}

func TestRawCallDropsInfiniteQueryDirection(t *testing.T) {
	r := trpcgo.NewRouter()
	echoQuery[cursorPageInput](t, r)

	result, err := r.RawCall(context.Background(), "list", json.RawMessage(`{"limit":5,"direction":"forward"}`))
	if err != nil {
		t.Fatalf("RawCall: %v", err)
	}
	if got := result.(cursorPageInput); got.Limit != 5 || got.Cursor != nil {
		t.Errorf("handler input = %+v, want limit 5 and no cursor", got)
	}
}

func TestStrictInputDisabledIgnoresDirection(t *testing.T) {
	r := trpcgo.NewRouter(trpcgo.WithStrictInput(false))
	echoQuery[offsetPageInput](t, r)
	server := newTestServer(t, trpc.NewHandler(r, "/trpc"))

	resp := mustGet(t, server, "/trpc/list?input="+url.QueryEscape(`{"page":2,"direction":"forward"}`))
	if data := resultData(t, decodeJSON(t, resp)); data["page"] != float64(2) {
		t.Errorf("handler input = %v, want page 2", data)
	}
}

func TestStrictInputSubscriptionReconnectWithoutLastEventID(t *testing.T) {
	type channelInput struct {
		Channel string `json:"channel"`
	}
	r := trpcgo.NewRouter()
	got := make(chan channelInput, 1)
	if err := trpcgo.Subscribe(r, "events", func(ctx context.Context, in channelInput) (<-chan string, error) {
		got <- in
		ch := make(chan string)
		go func() {
			defer close(ch)
			ch <- "ok"
		}()
		return ch, nil
	}); err != nil {
		t.Fatal(err)
	}
	server := newTestServer(t, trpc.NewHandler(r, "/trpc"))

	req, _ := http.NewRequest("GET", server.URL+"/trpc/events?input="+url.QueryEscape(`{"channel":"general"}`), nil)
	req.Header.Set("Last-Event-Id", "evt-7")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %v)", resp.StatusCode, decodeJSON(t, resp))
	}
	if in := <-got; in.Channel != "general" {
		t.Errorf("handler input = %+v, want channel general", in)
	}
	parseSSEEvents(t, resp, 3)
}
