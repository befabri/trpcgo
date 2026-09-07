package trpcgo_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/befabri/trpcgo"
	"github.com/befabri/trpcgo/internal/analysis"
	"github.com/befabri/trpcgo/internal/codegen"
	"github.com/befabri/trpcgo/testdata/ssecompat"
	"github.com/befabri/trpcgo/trpc"
)

func TestSSETrackedClientTypes(t *testing.T) {
	var static bytes.Buffer
	result, err := analysis.Analyze([]string{"."}, filepath.Join("testdata", "ssecompat"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := codegen.Generate(&static, result, result.TypeMetas); err != nil {
		t.Fatal(err)
	}
	for name, generated := range map[string]string{
		"reflection": generateTS(t, ssecompat.Router()),
		"static":     static.String(),
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			symlinkNodeModules(t, dir)
			check := `import { createTRPCClient } from '@trpc/client';
import type { AppRouter, RouterOutputs } from './router';
const client = createTRPCClient<AppRouter>({ links: [] });
client.tracked.subscribe(undefined, { onData(event) {
  const id: string = event.id;
  const text: string = event.data.text;
  // @ts-expect-error The payload is inside data.
  event.text;
} });
client.pointer.subscribe(undefined, { onData(event) {
  const id: string = event.id;
  const text: string = event.data.text;
} });
client.plain.subscribe(undefined, { onData(event) {
  const text: string = event.text;
  // @ts-expect-error Untracked messages have no transport wrapper.
  event.data;
} });
const tracked: RouterOutputs['tracked'] = { id: '42', data: { text: 'hello' } };
// Nested tracked structs and query results use Go JSON fields, not SSE metadata.
client.nested.subscribe(undefined, { onData(event) {
  const id: string = event.event.ID;
  const retry: number = event.event.Retry;
  const text: string = event.event.Data.text;
} });
client.query.query().then(event => {
  const id: string = event.ID;
  const retry: number = event.Retry;
  const text: string = event.Data.text;
});
const stringEvent: RouterOutputs['aString'] = { ID: '42', Retry: 5000, Data: 'hello' };
const intEvent: RouterOutputs['bInt'] = { ID: '43', Retry: 5000, Data: 99 };
`
			for file, content := range map[string]string{
				"router.ts":     generated,
				"check.ts":      check,
				"tsconfig.json": `{"compilerOptions":{"strict":true,"noEmit":true,"target":"ES2022","module":"ES2022","moduleResolution":"bundler","skipLibCheck":true},"include":["*.ts"]}`,
			} {
				if err := os.WriteFile(filepath.Join(dir, file), []byte(content), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			cmd := exec.CommandContext(t.Context(), typeScriptCompiler(t), "-p", dir)
			if output, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("client types do not match SSE data: %v\n%s", err, output)
			}
		})
	}
}

func TestSSETrackedPrimitiveReflectionOrder(t *testing.T) {
	for _, intFirst := range []bool{false, true} {
		name := "string-first"
		if intFirst {
			name = "int-first"
		}
		t.Run(name, func(t *testing.T) {
			r := trpcgo.NewRouter()
			stringPath, intPath := "a", "b"
			if intFirst {
				stringPath, intPath = intPath, stringPath
			}
			trpcgo.MustVoidQuery(r, stringPath, func(context.Context) (trpcgo.TrackedEvent[string], error) {
				return trpcgo.Tracked("42", "hello"), nil
			})
			trpcgo.MustVoidQuery(r, intPath, func(context.Context) (trpcgo.TrackedEvent[int], error) {
				return trpcgo.Tracked("43", 99), nil
			})
			generated := generateTS(t, r)
			// ID and Retry must stay concrete whichever instantiation is reflected first.
			for _, field := range []string{"ID: string;", "Retry: number;", "Data: string;", "Data: number;"} {
				if !strings.Contains(generated, field) {
					t.Errorf("missing %q in generated types:\n%s", field, generated)
				}
			}
		})
	}
}

// TestSSEClientCompatibility feeds real handler output through the installed
// tRPC client; an EventSource stub replaces only network delivery.
func TestSSEClientCompatibility(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not available")
	}
	dir := t.TempDir()
	symlinkNodeModules(t, dir)
	frames := map[string]string{}
	for _, name := range []string{"default", "envelope", "custom", "bare", "bad-formatter"} {
		var options []trpcgo.Option
		switch name {
		case "envelope":
			options = append(options, trpcgo.WithErrorFormatter(func(in trpcgo.ErrorFormatterInput) any { return in.Shape }))
		case "custom", "bare":
			options = append(options, trpcgo.WithErrorFormatter(func(in trpcgo.ErrorFormatterInput) any {
				shape := map[string]any{"message": in.Shape.Error.Message, "code": in.Shape.Error.Code,
					"data": map[string]any{"code": in.Shape.Error.Data.Code, "httpStatus": in.Shape.Error.Data.HTTPStatus, "path": in.Path, "detail": "custom"}}
				if name == "bare" {
					shape["error"] = "custom-detail"
					return shape
				}
				return shape
			}))
		case "bad-formatter":
			options = append(options, trpcgo.WithErrorFormatter(func(in trpcgo.ErrorFormatterInput) any { return func() {} }))
		}
		r := trpcgo.NewRouter(options...)
		trpcgo.MustVoidSubscribeWithFinal(r, "tracked", func(context.Context) (<-chan trpcgo.TrackedEvent[ssecompat.Message], func() any, error) {
			ch := make(chan trpcgo.TrackedEvent[ssecompat.Message], 1)
			ch <- trpcgo.Tracked("42", ssecompat.Message{Text: "hello"})
			close(ch)
			return ch, func() any { return map[string]bool{"done": true} }, nil
		})
		trpcgo.MustVoidSubscribe(r, "plain", func(context.Context) (<-chan ssecompat.Message, error) {
			ch := make(chan ssecompat.Message, 1)
			ch <- ssecompat.Message{Text: "hello"}
			close(ch)
			return ch, nil
		})
		trpcgo.MustVoidSubscribe(r, "validation", func(context.Context) (<-chan string, error) {
			ch := make(chan string, 1)
			ch <- "denied"
			close(ch)
			return ch, nil
		}, trpcgo.OutputValidator(func(string) error { return trpcgo.NewError(trpcgo.CodeForbidden, "Access denied") }))
		trpcgo.MustVoidSubscribe(r, "serialization", func(context.Context) (<-chan any, error) {
			ch := make(chan any, 1)
			ch <- func() {}
			close(ch)
			return ch, nil
		})
		for _, path := range []string{"tracked", "plain", "validation", "serialization"} {
			rec := httptest.NewRecorder()
			trpc.NewHandler(r, "/trpc").ServeHTTP(rec, httptest.NewRequest("GET", "/trpc/"+path, nil))
			frames[name+"/"+path] = rec.Body.String()
		}
	}
	data, err := json.Marshal(frames)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "frames.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	script, err := os.ReadFile(filepath.Join("testdata", "ssecompat", "client.mjs"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "client.mjs"), script, 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(t.Context(), node, filepath.Join(dir, "client.mjs"))
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("tRPC client compatibility failed: %v\n%s", err, output)
	}
}

func TestSSETrackedInvalidID(t *testing.T) {
	for _, id := range []string{"", "\r\n", "bad\x00id", "page\n2", "page\r2", "\npage", "page\r\n"} {
		t.Run(id, func(t *testing.T) {
			r := trpcgo.NewRouter()
			trpcgo.MustVoidSubscribe(r, "events", func(context.Context) (<-chan trpcgo.TrackedEvent[string], error) {
				ch := make(chan trpcgo.TrackedEvent[string], 1)
				ch <- trpcgo.Tracked(id, "hello")
				close(ch)
				return ch, nil
			})
			rec := httptest.NewRecorder()
			trpc.NewHandler(r, "/trpc").ServeHTTP(rec, httptest.NewRequest("GET", "/trpc/events", nil))
			resp := rec.Result()
			defer resp.Body.Close()
			events := parseSSEEvents(t, resp, 10)
			if len(events) != 2 || events[1].event != "serialized-error" {
				t.Fatalf("invalid ID must fail instead of delivering an unwrapped payload: %#v", events)
			}
			var shape trpcgo.ErrorShape
			if err := json.Unmarshal([]byte(events[1].data), &shape); err != nil {
				t.Fatal(err)
			}
			if shape.Code != trpcgo.CodeInternalServerError {
				t.Fatalf("invalid ID error code = %v", shape.Code)
			}
		})
	}
}
