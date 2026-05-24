package trpcgo_test

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/befabri/trpcgo"
	"github.com/befabri/trpcgo/trpc"
)

func parseSSEEvents(t *testing.T, resp *http.Response, maxEvents int) []sseEvent {
	t.Helper()
	var events []sseEvent
	scanner := bufio.NewScanner(resp.Body)

	var currentEvent, currentData, currentID string
	hasData := false
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "event: "):
			currentEvent = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: "):
			currentData = strings.TrimPrefix(line, "data: ")
			hasData = true
		case line == "data: ":
			currentData = ""
			hasData = true
		case strings.HasPrefix(line, "id: "):
			currentID = strings.TrimPrefix(line, "id: ")
		case line == "":
			if hasData || currentEvent != "" {
				evt := currentEvent
				if evt == "" {
					evt = "message" // SSE default event type
				}
				events = append(events, sseEvent{event: evt, data: currentData, id: currentID})
				currentEvent = ""
				currentData = ""
				currentID = ""
				hasData = false
				if len(events) >= maxEvents {
					return events
				}
			}
		}
	}
	return events
}

type sseEvent struct {
	event string
	data  string
	id    string
}

func TestSubscriptionSSE(t *testing.T) {
	router := trpcgo.NewRouter(trpcgo.WithSSEPingInterval(100 * time.Millisecond))

	type SubInput struct {
		Count int `json:"count"`
	}

	trpcgo.Subscribe(router, "counter", func(ctx context.Context, input SubInput) (<-chan int, error) {
		ch := make(chan int)
		go func() {
			defer close(ch)
			for i := 1; i <= input.Count; i++ {
				select {
				case <-ctx.Done():
					return
				case ch <- i:
				}
			}
		}()
		return ch, nil
	})

	server := newTestServer(t, trpc.NewHandler(router, "/trpc"))

	input := url.QueryEscape(`{"count":3}`)
	resp := mustGet(t, server, "/trpc/counter?input="+input)
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("Content-Type = %q, want text/event-stream", ct)
	}

	// Read: connected + 3 messages + return = 5 events
	events := parseSSEEvents(t, resp, 5)

	if len(events) < 5 {
		t.Fatalf("got %d events, want at least 5", len(events))
	}
	if events[0].event != "connected" {
		t.Errorf("events[0] = %q, want connected", events[0].event)
	}
	for i := 1; i <= 3; i++ {
		if events[i].event != "message" {
			t.Errorf("events[%d].event = %q, want message (SSE default)", i, events[i].event)
		}
		if events[i].data != fmt.Sprintf("%d", i) {
			t.Errorf("events[%d].data = %q, want %d", i, events[i].data, i)
		}
		if events[i].id != "" {
			t.Errorf("events[%d].id = %q, want empty (untracked)", i, events[i].id)
		}
	}
	if events[4].event != "return" {
		t.Errorf("events[4] = %q, want return", events[4].event)
	}
}

func TestSubscriptionVoidStream(t *testing.T) {
	router := trpcgo.NewRouter()

	trpcgo.VoidSubscribe(router, "ticks", func(ctx context.Context) (<-chan string, error) {
		ch := make(chan string)
		go func() {
			defer close(ch)
			ch <- "tick"
			ch <- "tock"
		}()
		return ch, nil
	})

	server := newTestServer(t, trpc.NewHandler(router, "/trpc"))

	resp := mustGet(t, server, "/trpc/ticks")
	defer func() { _ = resp.Body.Close() }()

	events := parseSSEEvents(t, resp, 4) // connected + 2 messages + return
	if len(events) < 4 {
		t.Fatalf("got %d events, want 4", len(events))
	}
	if events[1].data != `"tick"` {
		t.Errorf("events[1].data = %q, want tick", events[1].data)
	}
	if events[2].data != `"tock"` {
		t.Errorf("events[2].data = %q, want tock", events[2].data)
	}
}

func TestSubscriptionError(t *testing.T) {
	router := trpcgo.NewRouter()

	trpcgo.Subscribe(router, "fail", func(ctx context.Context, input struct{}) (<-chan string, error) {
		return nil, trpcgo.NewError(trpcgo.CodeUnauthorized, "not allowed")
	})

	server := newTestServer(t, trpc.NewHandler(router, "/trpc"))

	resp := mustGet(t, server, "/trpc/fail")
	defer func() { _ = resp.Body.Close() }()

	// Should get a regular JSON error, not SSE
	if resp.StatusCode != 401 {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
}

func TestSubscriptionCannotBeBatched(t *testing.T) {
	router := trpcgo.NewRouter(trpcgo.WithBatching(true))

	trpcgo.VoidSubscribe(router, "stream", func(ctx context.Context) (<-chan string, error) {
		ch := make(chan string)
		close(ch)
		return ch, nil
	})
	trpcgo.VoidQuery(router, "hello", func(ctx context.Context) (string, error) {
		return "hi", nil
	})

	server := newTestServer(t, trpc.NewHandler(router, "/trpc"))

	resp := mustGet(t, server, "/trpc/stream,hello?batch=1&input="+url.QueryEscape(`{"0":{},"1":{}}`))
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != 400 {
		t.Fatalf("status = %d, want 400 (subscriptions cannot be batched)", resp.StatusCode)
	}
}

func TestSubscriptionPing(t *testing.T) {
	router := trpcgo.NewRouter(trpcgo.WithSSEPingInterval(50 * time.Millisecond))

	trpcgo.VoidSubscribe(router, "slow", func(ctx context.Context) (<-chan string, error) {
		ch := make(chan string)
		go func() {
			defer close(ch)
			time.Sleep(200 * time.Millisecond)
			ch <- "done"
		}()
		return ch, nil
	})

	server := newTestServer(t, trpc.NewHandler(router, "/trpc"))

	resp := mustGet(t, server, "/trpc/slow")
	defer func() { _ = resp.Body.Close() }()

	// Should see: connected, ping(s), message, return
	events := parseSSEEvents(t, resp, 10)

	hasPing := false
	for _, e := range events {
		if e.event == "ping" {
			hasPing = true
			break
		}
	}
	if !hasPing {
		t.Errorf("expected at least one ping event, got events: %v", events)
	}
}

func TestSubscriptionMaxDuration(t *testing.T) {
	router := trpcgo.NewRouter(
		trpcgo.WithSSEMaxDuration(500*time.Millisecond),
		trpcgo.WithSSEPingInterval(100*time.Millisecond),
	)

	// Stream that never closes on its own — only maxDuration can stop it.
	trpcgo.VoidSubscribe(router, "forever", func(ctx context.Context) (<-chan string, error) {
		ch := make(chan string)
		go func() {
			defer close(ch)
			ticker := time.NewTicker(100 * time.Millisecond)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					select {
					case ch <- "tick":
					case <-ctx.Done():
						return
					}
				}
			}
		}()
		return ch, nil
	})

	server := newTestServer(t, trpc.NewHandler(router, "/trpc"))

	start := time.Now()
	resp := mustGet(t, server, "/trpc/forever")
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	events := parseSSEEvents(t, resp, 50)
	elapsed := time.Since(start)

	// Must have received the connected event.
	if len(events) == 0 || events[0].event != "connected" {
		t.Fatalf("first event should be connected, got %v", events)
	}

	// Last event must be "return" — that's how maxDuration ends the stream.
	lastEvent := events[len(events)-1]
	if lastEvent.event != "return" {
		t.Errorf("last event = %q, want return (maxDuration should end stream)", lastEvent.event)
	}

	// Count message events — should have several before maxDuration fires.
	messageCount := 0
	for _, e := range events {
		if e.event == "message" {
			messageCount++
			if e.data != `"tick"` {
				t.Errorf("message data = %q, want \"tick\"", e.data)
			}
		}
	}
	if messageCount == 0 {
		t.Error("expected at least one message event before maxDuration")
	}

	// Sanity: elapsed time should be roughly around maxDuration (500ms ± 300ms).
	if elapsed < 300*time.Millisecond {
		t.Errorf("stream ended too fast (%v), maxDuration should be ~500ms", elapsed)
	}
	if elapsed > 3*time.Second {
		t.Errorf("stream took too long (%v), maxDuration should cap at ~500ms", elapsed)
	}
}

func TestSubscriptionTrackedEvents(t *testing.T) {
	router := trpcgo.NewRouter()

	type Item struct {
		Name string `json:"name"`
	}

	trpcgo.VoidSubscribe(router, "items", func(ctx context.Context) (<-chan trpcgo.TrackedEvent[Item], error) {
		ch := make(chan trpcgo.TrackedEvent[Item])
		go func() {
			defer close(ch)
			ch <- trpcgo.Tracked("evt-1", Item{Name: "first"})
			ch <- trpcgo.Tracked("evt-2", Item{Name: "second"})
		}()
		return ch, nil
	})

	server := newTestServer(t, trpc.NewHandler(router, "/trpc"))

	resp := mustGet(t, server, "/trpc/items")
	defer func() { _ = resp.Body.Close() }()

	// connected + 2 tracked messages + return = 4 events
	events := parseSSEEvents(t, resp, 4)
	if len(events) < 4 {
		t.Fatalf("got %d events, want at least 4, got: %v", len(events), events)
	}

	// First event: connected.
	if events[0].event != "connected" {
		t.Errorf("events[0].event = %q, want connected", events[0].event)
	}

	// Data messages: default "message" event type, with id and data.
	for i, want := range []struct {
		id   string
		data string
	}{
		{"evt-1", `{"name":"first"}`},
		{"evt-2", `{"name":"second"}`},
	} {
		idx := i + 1
		e := events[idx]
		if e.event != "message" {
			t.Errorf("events[%d].event = %q, want message", idx, e.event)
		}
		if e.id != want.id {
			t.Errorf("events[%d].id = %q, want %q", idx, e.id, want.id)
		}
		if e.data != want.data {
			t.Errorf("events[%d].data = %q, want %q", idx, e.data, want.data)
		}
	}

	// Last event: return.
	if events[3].event != "return" {
		t.Errorf("events[3].event = %q, want return", events[3].event)
	}
}

func TestStreamConsumerRecvReturnsFinalValueOnEOF(t *testing.T) {
	router := trpcgo.NewRouter()

	trpcgo.MustVoidSubscribeWithFinal(router, "done", func(ctx context.Context) (<-chan string, func() any, error) {
		ch := make(chan string)
		close(ch)
		return ch, func() any { return "finished" }, nil
	})

	consumer := streamConsumerFor(t, router, "done")
	data, id, retry, err := consumer.Recv(context.Background())
	if err != io.EOF {
		t.Fatalf("Recv error = %v, want io.EOF", err)
	}
	if data != "finished" {
		t.Fatalf("Recv data = %v, want finished", data)
	}
	if id != "" {
		t.Fatalf("Recv id = %q, want empty", id)
	}
	if retry != 0 {
		t.Fatalf("Recv retry = %d, want 0", retry)
	}
}

func TestStreamConsumerRecvReturnsItemWithZeroRetry(t *testing.T) {
	router := trpcgo.NewRouter()

	trpcgo.MustVoidSubscribe(router, "ticks", func(ctx context.Context) (<-chan string, error) {
		ch := make(chan string, 1)
		ch <- "tick"
		close(ch)
		return ch, nil
	})

	consumer := streamConsumerFor(t, router, "ticks")
	data, id, retry, err := consumer.Recv(context.Background())
	if err != nil {
		t.Fatalf("Recv error = %v, want nil", err)
	}
	if data != "tick" {
		t.Fatalf("Recv data = %v, want tick", data)
	}
	if id != "" {
		t.Fatalf("Recv id = %q, want empty", id)
	}
	if retry != 0 {
		t.Fatalf("Recv retry = %d, want 0", retry)
	}
}

func TestStreamConsumerRecvReturnsEOFWithZeroRetry(t *testing.T) {
	router := trpcgo.NewRouter()

	trpcgo.MustVoidSubscribe(router, "empty", func(ctx context.Context) (<-chan string, error) {
		ch := make(chan string)
		close(ch)
		return ch, nil
	})

	consumer := streamConsumerFor(t, router, "empty")
	data, id, retry, err := consumer.Recv(context.Background())
	if err != io.EOF {
		t.Fatalf("Recv error = %v, want io.EOF", err)
	}
	if data != nil {
		t.Fatalf("Recv data = %v, want nil", data)
	}
	if id != "" {
		t.Fatalf("Recv id = %q, want empty", id)
	}
	if retry != 0 {
		t.Fatalf("Recv retry = %d, want 0", retry)
	}
}

func TestStreamConsumerRecvNilItemSkipsOutputHooks(t *testing.T) {
	router := trpcgo.NewRouter()
	called := false

	trpcgo.MustVoidSubscribe(router, "nil-item", func(ctx context.Context) (<-chan any, error) {
		ch := make(chan any, 1)
		ch <- nil
		close(ch)
		return ch, nil
	}, trpcgo.WithOutputParser(func(v any) (any, error) {
		called = true
		return "parsed", nil
	}))

	consumer := streamConsumerFor(t, router, "nil-item")
	data, id, retry, err := consumer.Recv(context.Background())
	if err != nil {
		t.Fatalf("Recv error = %v, want nil", err)
	}
	if called {
		t.Fatal("output parser was called for nil stream item")
	}
	if data != nil {
		t.Fatalf("Recv data = %v, want nil", data)
	}
	if id != "" {
		t.Fatalf("Recv id = %q, want empty", id)
	}
	if retry != 0 {
		t.Fatalf("Recv retry = %d, want 0", retry)
	}
}

func TestStreamConsumerRecvOutputHookErrorHasZeroRetry(t *testing.T) {
	router := trpcgo.NewRouter()
	hookErr := fmt.Errorf("hook failed")

	trpcgo.MustVoidSubscribe(router, "bad-item", func(ctx context.Context) (<-chan string, error) {
		ch := make(chan string, 1)
		ch <- "bad"
		close(ch)
		return ch, nil
	}, trpcgo.WithOutputParser(func(v any) (any, error) {
		return nil, hookErr
	}))

	consumer := streamConsumerFor(t, router, "bad-item")
	data, id, retry, err := consumer.Recv(context.Background())
	if err == nil || !strings.Contains(err.Error(), "hook failed") {
		t.Fatalf("Recv error = %v, want hook failure", err)
	}
	if data != nil {
		t.Fatalf("Recv data = %v, want nil", data)
	}
	if id != "" {
		t.Fatalf("Recv id = %q, want empty", id)
	}
	if retry != 0 {
		t.Fatalf("Recv retry = %d, want 0", retry)
	}
}

func streamConsumerFor(t *testing.T, router *trpcgo.Router, path string) *trpcgo.StreamConsumer {
	t.Helper()

	procedures := router.BuildProcedureMap()
	entry, ok := procedures.Lookup(path)
	if !ok {
		t.Fatal("expected registered procedure")
	}

	result, err := router.ExecuteEntry(context.Background(), entry, nil)
	if err != nil {
		t.Fatalf("ExecuteEntry: %v", err)
	}
	consumer := trpcgo.ConsumeStream(result)
	if consumer == nil {
		t.Fatal("expected stream consumer")
	}
	return consumer
}

func TestSubscriptionUntrackedEventsHaveNoID(t *testing.T) {
	router := trpcgo.NewRouter()

	trpcgo.VoidSubscribe(router, "plain", func(ctx context.Context) (<-chan string, error) {
		ch := make(chan string)
		go func() {
			defer close(ch)
			ch <- "hello"
			ch <- "world"
		}()
		return ch, nil
	})

	server := newTestServer(t, trpc.NewHandler(router, "/trpc"))

	resp := mustGet(t, server, "/trpc/plain")
	defer func() { _ = resp.Body.Close() }()

	events := parseSSEEvents(t, resp, 4)
	if len(events) < 4 {
		t.Fatalf("got %d events, want 4", len(events))
	}

	for i := 1; i <= 2; i++ {
		if events[i].id != "" {
			t.Errorf("events[%d].id = %q, want empty (untracked events must not have id)", i, events[i].id)
		}
	}
}

func TestSubscriptionWireFormat(t *testing.T) {
	// Verify the exact SSE wire format matches tRPC:
	// - Control events (connected, ping, return) have "event:" field
	// - Data messages have NO "event:" field (use SSE default)
	// - Data messages have "data:" field with JSON content
	router := trpcgo.NewRouter()

	trpcgo.VoidSubscribe(router, "single", func(ctx context.Context) (<-chan int, error) {
		ch := make(chan int)
		go func() {
			defer close(ch)
			ch <- 42
		}()
		return ch, nil
	})

	server := newTestServer(t, trpc.NewHandler(router, "/trpc"))

	resp := mustGet(t, server, "/trpc/single")
	defer func() { _ = resp.Body.Close() }()

	// Read all raw lines.
	var lines []string
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}

	// Must not contain "event: message" anywhere.
	for i, line := range lines {
		if line == "event: message" {
			t.Fatalf("line %d: data messages must not include 'event: message'", i)
		}
	}

	// Must contain "data: 42" (the actual data).
	hasData := slices.Contains(lines, "data: 42")
	if !hasData {
		t.Errorf("missing 'data: 42' line in output:\n%s", strings.Join(lines, "\n"))
	}

	// Must contain control events with event: prefix.
	hasConnected := false
	hasReturn := false
	for _, line := range lines {
		if line == "event: connected" {
			hasConnected = true
		}
		if line == "event: return" {
			hasReturn = true
		}
	}
	if !hasConnected {
		t.Error("missing 'event: connected' line")
	}
	if !hasReturn {
		t.Error("missing 'event: return' line")
	}
}

func TestSubscriptionConnectedEventData(t *testing.T) {
	t.Run("with reconnect timeout", func(t *testing.T) {
		router := trpcgo.NewRouter(
			trpcgo.WithSSEReconnectAfterInactivity(3 * time.Second),
		)

		trpcgo.VoidSubscribe(router, "stream", func(ctx context.Context) (<-chan string, error) {
			ch := make(chan string)
			close(ch)
			return ch, nil
		})

		server := newTestServer(t, trpc.NewHandler(router, "/trpc"))
		resp := mustGet(t, server, "/trpc/stream")
		defer func() { _ = resp.Body.Close() }()

		events := parseSSEEvents(t, resp, 2)
		if len(events) == 0 || events[0].event != "connected" {
			t.Fatalf("first event should be connected, got %v", events)
		}

		var connData map[string]any
		if err := json.Unmarshal([]byte(events[0].data), &connData); err != nil {
			t.Fatalf("failed to parse connected data %q: %v", events[0].data, err)
		}
		if v, ok := connData["reconnectAfterInactivityMs"]; !ok {
			t.Error("connected data missing reconnectAfterInactivityMs")
		} else if v != float64(3000) {
			t.Errorf("reconnectAfterInactivityMs = %v, want 3000", v)
		}
	})

	t.Run("without reconnect timeout", func(t *testing.T) {
		router := trpcgo.NewRouter()

		trpcgo.VoidSubscribe(router, "stream", func(ctx context.Context) (<-chan string, error) {
			ch := make(chan string)
			close(ch)
			return ch, nil
		})

		server := newTestServer(t, trpc.NewHandler(router, "/trpc"))
		resp := mustGet(t, server, "/trpc/stream")
		defer func() { _ = resp.Body.Close() }()

		events := parseSSEEvents(t, resp, 2)
		if len(events) == 0 || events[0].event != "connected" {
			t.Fatalf("first event should be connected, got %v", events)
		}

		var connData map[string]any
		if err := json.Unmarshal([]byte(events[0].data), &connData); err != nil {
			t.Fatalf("failed to parse connected data %q: %v", events[0].data, err)
		}
		if _, ok := connData["reconnectAfterInactivityMs"]; ok {
			t.Error("connected data should omit reconnectAfterInactivityMs when 0")
		}
	})
}

func TestSubscriptionTrackedSerializationError(t *testing.T) {
	// TrackedEvent where Data fails to marshal should emit serialized-error.
	router := trpcgo.NewRouter()

	type BadData struct {
		Fn func() `json:"fn"` // functions can't be marshaled
	}

	trpcgo.VoidSubscribe(router, "bad", func(ctx context.Context) (<-chan trpcgo.TrackedEvent[BadData], error) {
		ch := make(chan trpcgo.TrackedEvent[BadData])
		go func() {
			defer close(ch)
			ch <- trpcgo.Tracked("id-1", BadData{Fn: func() {}})
		}()
		return ch, nil
	})

	server := newTestServer(t, trpc.NewHandler(router, "/trpc"))

	resp := mustGet(t, server, "/trpc/bad")
	defer func() { _ = resp.Body.Close() }()

	events := parseSSEEvents(t, resp, 3)

	hasSerialized := false
	for _, e := range events {
		if e.event == "serialized-error" {
			hasSerialized = true
			if !strings.Contains(e.data, "failed to serialize") {
				t.Errorf("serialized-error data = %q, want 'failed to serialize'", e.data)
			}
			break
		}
	}
	if !hasSerialized {
		t.Errorf("expected serialized-error event, got: %v", events)
	}
}

func lastEventIdSubscription(r *trpcgo.Router) <-chan string {
	type subInput struct {
		Channel     string `json:"channel"`
		LastEventId string `json:"lastEventId"`
	}

	gotID := make(chan string, 1)
	trpcgo.Subscribe(r, "events", func(ctx context.Context, input subInput) (<-chan string, error) {
		gotID <- input.LastEventId
		ch := make(chan string)
		go func() {
			defer close(ch)
			ch <- "ok"
		}()
		return ch, nil
	})
	return gotID
}

func TestLastEventId(t *testing.T) {
	tests := []struct {
		name   string
		header string            // Last-Event-Id header value (empty = don't set)
		query  map[string]string // extra query params
		input  string            // input query param (JSON, empty = none)
		wantID string
	}{
		{
			name:   "from header",
			header: "evt-42",
			wantID: "evt-42",
		},
		{
			name:   "from lastEventId query param",
			query:  map[string]string{"lastEventId": "evt-99"},
			wantID: "evt-99",
		},
		{
			name:   "from Last-Event-Id query param",
			query:  map[string]string{"Last-Event-Id": "evt-77"},
			wantID: "evt-77",
		},
		{
			name:   "header takes precedence over query",
			header: "from-header",
			query:  map[string]string{"lastEventId": "from-query"},
			wantID: "from-header",
		},
		{
			name:   "no lastEventId",
			wantID: "",
		},
		{
			name:   "merges with existing input",
			header: "evt-7",
			input:  `{"channel":"general"}`,
			wantID: "evt-7",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := trpcgo.NewRouter()
			gotID := lastEventIdSubscription(r)
			server := newTestServer(t, trpc.NewHandler(r, "/trpc"))

			u := server.URL + "/trpc/events"
			params := url.Values{}
			if tt.input != "" {
				params.Set("input", tt.input)
			}
			for k, v := range tt.query {
				params.Set(k, v)
			}
			if q := params.Encode(); q != "" {
				u += "?" + q
			}

			req, _ := http.NewRequest("GET", u, nil)
			if tt.header != "" {
				req.Header.Set("Last-Event-Id", tt.header)
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = resp.Body.Close() }()

			// Read the captured ID (race-free via channel).
			id := <-gotID

			// Consume SSE to let handler finish cleanly.
			parseSSEEvents(t, resp, 3)

			if id != tt.wantID {
				t.Errorf("lastEventId = %q, want %q", id, tt.wantID)
			}
		})
	}
}

func TestLastEventIdNotMergedForQueries(t *testing.T) {
	r := trpcgo.NewRouter()

	var capturedInput any
	trpcgo.Query(r, "user.get", func(ctx context.Context, input GetUserInput) (User, error) {
		return User{ID: input.ID, Name: "Alice"}, nil
	})

	// Capture the decoded input from middleware.
	r.Use(func(next trpcgo.HandlerFunc) trpcgo.HandlerFunc {
		return func(ctx context.Context, input any) (any, error) {
			capturedInput = input
			return next(ctx, input)
		}
	})

	server := newTestServer(t, trpc.NewHandler(r, "/trpc"))

	inputJSON := url.QueryEscape(`{"id":"1"}`)
	req, _ := http.NewRequest("GET", server.URL+"/trpc/user.get?input="+inputJSON, nil)
	req.Header.Set("Last-Event-Id", "should-not-appear")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()

	// The decoded input is GetUserInput — marshal to JSON to verify no lastEventId.
	data, _ := json.Marshal(capturedInput)
	if strings.Contains(string(data), "lastEventId") {
		t.Errorf("lastEventId should not be merged for queries, got input: %s", data)
	}
}
