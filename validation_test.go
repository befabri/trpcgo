package trpcgo_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/befabri/trpcgo"
	"github.com/befabri/trpcgo/trpc"
)

func testValidator(v any) error {
	val := reflect.ValueOf(v)
	typ := val.Type()
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		tag := field.Tag.Get("validate")
		if tag == "" {
			continue
		}
		rules := strings.Split(tag, ",")
		fieldVal := val.Field(i)
		for _, rule := range rules {
			parts := strings.SplitN(rule, "=", 2)
			switch parts[0] {
			case "required":
				if fieldVal.IsZero() {
					return fmt.Errorf("field %s is required", field.Name)
				}
			case "min":
				if len(parts) > 1 {
					n, _ := strconv.Atoi(parts[1])
					if fieldVal.Kind() == reflect.String && len(fieldVal.String()) < n {
						return fmt.Errorf("field %s must be at least %d characters", field.Name, n)
					}
				}
			case "max":
				if len(parts) > 1 {
					n, _ := strconv.Atoi(parts[1])
					if fieldVal.Kind() == reflect.String && len(fieldVal.String()) > n {
						return fmt.Errorf("field %s must be at most %d characters", field.Name, n)
					}
				}
			}
		}
	}
	return nil
}

type ValidatedInput struct {
	Name string `json:"name" validate:"required"`
}

type ValidatedOutput struct {
	Greeting string `json:"greeting"`
}

type MinMaxInput struct {
	Username string `json:"username" validate:"min=3,max=10"`
}

func TestValidatorRejectsInvalidInput(t *testing.T) {
	router := trpcgo.NewRouter(trpcgo.WithValidator(testValidator))

	trpcgo.Query(router, "greet", func(ctx context.Context, input ValidatedInput) (ValidatedOutput, error) {
		return ValidatedOutput{Greeting: "Hello, " + input.Name + "!"}, nil
	})

	server := newTestServer(t, trpc.NewHandler(router, "/trpc"))

	// Send request with empty name (missing required field).
	input := url.QueryEscape(`{"name":""}`)
	resp := mustGet(t, server, "/trpc/greet?input="+input)
	if resp.StatusCode != 400 {
		_ = resp.Body.Close()
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}

	body := decodeJSON(t, resp)
	ed := errorData(t, body)
	if ed["code"] != "BAD_REQUEST" {
		t.Errorf("error.data.code = %v, want BAD_REQUEST", ed["code"])
	}

	// Also verify the error message mentions validation.
	errObj := body["error"].(map[string]any)
	msg, _ := errObj["message"].(string)
	if !strings.Contains(msg, "validation") {
		t.Errorf("error message = %q, want it to contain 'validation'", msg)
	}
}

func TestValidatorAcceptsValidInput(t *testing.T) {
	router := trpcgo.NewRouter(trpcgo.WithValidator(testValidator))

	trpcgo.Query(router, "greet", func(ctx context.Context, input ValidatedInput) (ValidatedOutput, error) {
		return ValidatedOutput{Greeting: "Hello, " + input.Name + "!"}, nil
	})

	server := newTestServer(t, trpc.NewHandler(router, "/trpc"))

	// Send request with valid name.
	input := url.QueryEscape(`{"name":"Alice"}`)
	resp := mustGet(t, server, "/trpc/greet?input="+input)
	if resp.StatusCode != 200 {
		_ = resp.Body.Close()
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	data := resultData(t, decodeJSON(t, resp))
	if data["greeting"] != "Hello, Alice!" {
		t.Errorf("greeting = %v, want 'Hello, Alice!'", data["greeting"])
	}
}

func TestValidatorMinMax(t *testing.T) {
	router := trpcgo.NewRouter(trpcgo.WithValidator(testValidator))

	trpcgo.Query(router, "checkUsername", func(ctx context.Context, input MinMaxInput) (string, error) {
		return "ok:" + input.Username, nil
	})

	server := newTestServer(t, trpc.NewHandler(router, "/trpc"))

	// Below min (less than 3 chars) → 400.
	t.Run("below min", func(t *testing.T) {
		input := url.QueryEscape(`{"username":"ab"}`)
		resp := mustGet(t, server, "/trpc/checkUsername?input="+input)
		if resp.StatusCode != 400 {
			_ = resp.Body.Close()
			t.Fatalf("status = %d, want 400 for username below min length", resp.StatusCode)
		}

		body := decodeJSON(t, resp)
		ed := errorData(t, body)
		if ed["code"] != "BAD_REQUEST" {
			t.Errorf("error.data.code = %v, want BAD_REQUEST", ed["code"])
		}
	})

	// Within range (3-10 chars) → 200.
	t.Run("within range", func(t *testing.T) {
		input := url.QueryEscape(`{"username":"alice"}`)
		resp := mustGet(t, server, "/trpc/checkUsername?input="+input)
		if resp.StatusCode != 200 {
			_ = resp.Body.Close()
			t.Fatalf("status = %d, want 200 for valid username", resp.StatusCode)
		}

		body := decodeJSON(t, resp)
		got := resultScalar(t, body)
		if got != "ok:alice" {
			t.Errorf("result = %v, want ok:alice", got)
		}
	})

	// Above max (more than 10 chars) → 400.
	t.Run("above max", func(t *testing.T) {
		input := url.QueryEscape(`{"username":"verylongusername"}`)
		resp := mustGet(t, server, "/trpc/checkUsername?input="+input)
		if resp.StatusCode != 400 {
			_ = resp.Body.Close()
			t.Fatalf("status = %d, want 400 for username above max length", resp.StatusCode)
		}

		body := decodeJSON(t, resp)
		ed := errorData(t, body)
		if ed["code"] != "BAD_REQUEST" {
			t.Errorf("error.data.code = %v, want BAD_REQUEST", ed["code"])
		}
	})
}

func TestValidatorWithErrorFormatter(t *testing.T) {
	var capturedCause string

	router := trpcgo.NewRouter(
		trpcgo.WithValidator(testValidator),
		trpcgo.WithErrorFormatter(func(input trpcgo.ErrorFormatterInput) any {
			// The error formatter receives the *Error which wraps the validation error.
			// Verify we can access the underlying validation error via Unwrap.
			if cause := input.Error.Unwrap(); cause != nil {
				capturedCause = cause.Error()
			}
			return input.Shape
		}),
	)

	trpcgo.Query(router, "greet", func(ctx context.Context, input ValidatedInput) (ValidatedOutput, error) {
		return ValidatedOutput{Greeting: "Hello, " + input.Name + "!"}, nil
	})

	server := newTestServer(t, trpc.NewHandler(router, "/trpc"))

	// Send invalid input to trigger validation error.
	input := url.QueryEscape(`{"name":""}`)
	resp := mustGet(t, server, "/trpc/greet?input="+input)
	if resp.StatusCode != 400 {
		_ = resp.Body.Close()
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	_ = resp.Body.Close()

	// The cause should be the validation error from testValidator.
	if !strings.Contains(capturedCause, "field Name is required") {
		t.Errorf("captured cause = %q, want it to contain 'field Name is required'", capturedCause)
	}
}

func TestValidatorSkipsNonStruct(t *testing.T) {
	router := trpcgo.NewRouter(trpcgo.WithValidator(testValidator))

	// Register a query with primitive string input — validator should be skipped.
	trpcgo.Query(router, "echo", func(ctx context.Context, input string) (string, error) {
		return "echo:" + input, nil
	})

	server := newTestServer(t, trpc.NewHandler(router, "/trpc"))

	input := url.QueryEscape(`"hello"`)
	resp := mustGet(t, server, "/trpc/echo?input="+input)
	if resp.StatusCode != 200 {
		_ = resp.Body.Close()
		t.Fatalf("status = %d, want 200 (validator should skip primitive input)", resp.StatusCode)
	}

	body := decodeJSON(t, resp)
	got := resultScalar(t, body)
	if got != "echo:hello" {
		t.Errorf("result = %v, want echo:hello", got)
	}
}

func TestValidatorSkipsVoidProcedure(t *testing.T) {
	router := trpcgo.NewRouter(trpcgo.WithValidator(func(any) error {
		return fmt.Errorf("validator should not run")
	}))

	trpcgo.MustVoidQuery(router, "ping", func(ctx context.Context) (string, error) {
		return "pong", nil
	})

	got, err := router.RawCall(context.Background(), "ping", nil)
	if err != nil {
		t.Fatalf("RawCall: %v", err)
	}
	if got != "pong" {
		t.Fatalf("RawCall result = %v, want pong", got)
	}
}

func TestValidatorWithRawCall(t *testing.T) {
	router := trpcgo.NewRouter(trpcgo.WithValidator(testValidator))

	trpcgo.Query(router, "greet", func(ctx context.Context, input ValidatedInput) (ValidatedOutput, error) {
		return ValidatedOutput{Greeting: "Hello, " + input.Name + "!"}, nil
	})

	// Call Handler() to pre-compute middleware chains with validation.
	_ = trpc.NewHandler(router, "/trpc")

	// Valid input via Call should succeed.
	t.Run("valid input", func(t *testing.T) {
		result, err := trpcgo.Call[ValidatedInput, ValidatedOutput](
			router, context.Background(), "greet", ValidatedInput{Name: "Bob"},
		)
		if err != nil {
			t.Fatalf("Call error: %v", err)
		}
		if result.Greeting != "Hello, Bob!" {
			t.Errorf("greeting = %v, want 'Hello, Bob!'", result.Greeting)
		}
	})

	// Invalid input via Call should return validation error.
	t.Run("invalid input", func(t *testing.T) {
		_, err := trpcgo.Call[ValidatedInput, ValidatedOutput](
			router, context.Background(), "greet", ValidatedInput{Name: ""},
		)
		if err == nil {
			t.Fatal("expected validation error, got nil")
		}
		trpcErr, ok := err.(*trpcgo.Error)
		if !ok {
			t.Fatalf("error type = %T, want *trpcgo.Error", err)
		}
		if trpcErr.Code != trpcgo.CodeBadRequest {
			t.Errorf("error code = %v, want BAD_REQUEST", trpcErr.Code)
		}
		if !strings.Contains(trpcErr.Message, "validation") {
			t.Errorf("error message = %q, want it to contain 'validation'", trpcErr.Message)
		}
	})
}

func TestValidatorNotSet(t *testing.T) {
	// No validator configured — struct with validate tags should work normally.
	router := trpcgo.NewRouter()

	trpcgo.Query(router, "greet", func(ctx context.Context, input ValidatedInput) (ValidatedOutput, error) {
		return ValidatedOutput{Greeting: "Hello, " + input.Name + "!"}, nil
	})

	server := newTestServer(t, trpc.NewHandler(router, "/trpc"))

	// Send empty name — without a validator, the handler should run and produce a response.
	input := url.QueryEscape(`{"name":""}`)
	resp := mustGet(t, server, "/trpc/greet?input="+input)
	if resp.StatusCode != 200 {
		_ = resp.Body.Close()
		t.Fatalf("status = %d, want 200 (no validator configured, handler should run)", resp.StatusCode)
	}

	data := resultData(t, decodeJSON(t, resp))
	if data["greeting"] != "Hello, !" {
		t.Errorf("greeting = %v, want 'Hello, !'", data["greeting"])
	}
}

func TestOutputValidatorRejectsInvalidOutput(t *testing.T) {
	r := trpcgo.NewRouter()
	trpcgo.MustVoidQuery(r, "bad", func(ctx context.Context) (User, error) {
		return User{}, nil
	}, trpcgo.OutputValidator(func(u User) error {
		if u.ID == "" {
			return fmt.Errorf("id is required")
		}
		return nil
	}))

	server := newTestServer(t, trpc.NewHandler(r, "/trpc"))
	resp := mustGet(t, server, "/trpc/bad")
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	bodyStr := string(body)
	if !strings.Contains(bodyStr, "INTERNAL_SERVER_ERROR") {
		t.Errorf("body missing INTERNAL_SERVER_ERROR, got: %s", body)
	}
	if !strings.Contains(bodyStr, "internal server error") {
		t.Errorf("body missing generic internal server error message, got: %s", body)
	}
	if strings.Contains(bodyStr, "id is required") {
		t.Errorf("body leaked validator details, got: %s", body)
	}
}

func TestOutputValidatorPassesValidOutput(t *testing.T) {
	r := trpcgo.NewRouter()
	trpcgo.MustVoidQuery(r, "good", func(ctx context.Context) (User, error) {
		return User{ID: "1", Name: "Alice"}, nil
	}, trpcgo.OutputValidator(func(u User) error {
		if u.ID == "" {
			return fmt.Errorf("id is required")
		}
		return nil
	}))

	server := newTestServer(t, trpc.NewHandler(r, "/trpc"))
	resp := mustGet(t, server, "/trpc/good")
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	body := decodeJSON(t, resp)
	data := resultData(t, body)
	if data["id"] != "1" {
		t.Fatalf("result.data.id = %v, want 1", data["id"])
	}
	if data["name"] != "Alice" {
		t.Fatalf("result.data.name = %v, want Alice", data["name"])
	}
}

func TestOutputValidatorWithMethodRuntime(t *testing.T) {
	base := trpcgo.Procedure().With(trpcgo.OutputValidator(func(u User) error {
		if u.ID == "" {
			return fmt.Errorf("id is required")
		}
		return nil
	}))

	r := trpcgo.NewRouter()
	trpcgo.MustVoidQuery(r, "ok", func(ctx context.Context) (User, error) {
		return User{ID: "7", Name: "Alice"}, nil
	}, base)
	trpcgo.MustVoidQuery(r, "bad", func(ctx context.Context) (User, error) {
		return User{}, nil
	}, base)

	server := newTestServer(t, trpc.NewHandler(r, "/trpc"))

	resp := mustGet(t, server, "/trpc/ok")
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		t.Fatalf("ok status = %d, want 200", resp.StatusCode)
	}
	body := decodeJSON(t, resp)
	if got := resultData(t, body)["id"]; got != "7" {
		t.Fatalf("ok result.data.id = %v, want 7", got)
	}

	resp = mustGet(t, server, "/trpc/bad")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("bad status = %d, want 500", resp.StatusCode)
	}
}

func TestOutputValidatorBuilderMethodRuntime(t *testing.T) {
	base := trpcgo.Procedure().WithOutputValidator(func(v any) error {
		u, _ := v.(User)
		if u.ID == "" {
			return fmt.Errorf("id is required")
		}
		return nil
	})

	r := trpcgo.NewRouter()
	trpcgo.MustVoidQuery(r, "ok", func(ctx context.Context) (User, error) {
		return User{ID: "8", Name: "Alice"}, nil
	}, base)
	trpcgo.MustVoidQuery(r, "bad", func(ctx context.Context) (User, error) {
		return User{}, nil
	}, base)

	server := newTestServer(t, trpc.NewHandler(r, "/trpc"))

	resp := mustGet(t, server, "/trpc/ok")
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		t.Fatalf("ok status = %d, want 200", resp.StatusCode)
	}
	body := decodeJSON(t, resp)
	if got := resultData(t, body)["id"]; got != "8" {
		t.Fatalf("ok result.data.id = %v, want 8", got)
	}

	resp = mustGet(t, server, "/trpc/bad")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("bad status = %d, want 500", resp.StatusCode)
	}
}

func TestTypedOutputValidatorRunsOnNilAnyOutput(t *testing.T) {
	validatorCalled := false
	r := trpcgo.NewRouter()
	trpcgo.MustVoidQuery(r, "niltyped", func(ctx context.Context) (any, error) {
		return nil, nil
	}, trpcgo.OutputValidator(func(v any) error {
		validatorCalled = true
		if v != nil {
			return fmt.Errorf("expected nil output, got %T", v)
		}
		return nil
	}))

	server := newTestServer(t, trpc.NewHandler(r, "/trpc"))
	resp := mustGet(t, server, "/trpc/niltyped")
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if !validatorCalled {
		t.Fatal("typed output validator should be called for nil output")
	}
	body := decodeJSON(t, resp)
	if got := resultScalar(t, body); got != nil {
		t.Fatalf("result.data = %v, want nil", got)
	}
}

func TestOutputValidatorRunsBeforeParser(t *testing.T) {
	type PublicUser struct {
		ID string `json:"id"`
	}

	var calls []string
	r := trpcgo.NewRouter()
	trpcgo.MustVoidQuery(r, "user", func(ctx context.Context) (User, error) {
		return User{ID: "9", Name: "Alice"}, nil
	},
		trpcgo.OutputValidator(func(u User) error {
			calls = append(calls, "validator")
			if u.Name == "" {
				return fmt.Errorf("name required")
			}
			return nil
		}),
		trpcgo.OutputParser(func(u User) (PublicUser, error) {
			calls = append(calls, "parser")
			return PublicUser{ID: u.ID}, nil
		}),
	)

	server := newTestServer(t, trpc.NewHandler(r, "/trpc"))
	resp := mustGet(t, server, "/trpc/user")
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if got := strings.Join(calls, ","); got != "validator,parser" {
		t.Fatalf("hook order = %q, want validator,parser", got)
	}
	body := decodeJSON(t, resp)
	data := resultData(t, body)
	if _, ok := data["name"]; ok {
		t.Fatalf("result.data should not contain name, got %v", data)
	}
}

func TestOutputValidatorFailureShortCircuitsParser(t *testing.T) {
	parserCalled := false
	r := trpcgo.NewRouter()
	trpcgo.MustVoidQuery(r, "user", func(ctx context.Context) (User, error) {
		return User{}, nil
	},
		trpcgo.OutputValidator(func(u User) error {
			return fmt.Errorf("id required")
		}),
		trpcgo.OutputParser(func(u User) (User, error) {
			parserCalled = true
			return u, nil
		}),
	)

	server := newTestServer(t, trpc.NewHandler(r, "/trpc"))
	resp := mustGet(t, server, "/trpc/user")
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", resp.StatusCode)
	}
	if parserCalled {
		t.Fatal("output parser must not be called when output validator fails")
	}
}

func TestOutputValidatorSubscription(t *testing.T) {
	r := trpcgo.NewRouter()
	trpcgo.MustVoidSubscribe(r, "stream", func(ctx context.Context) (<-chan User, error) {
		ch := make(chan User, 1)
		ch <- User{}
		close(ch)
		return ch, nil
	}, trpcgo.OutputValidator(func(u User) error {
		if u.ID == "" {
			return fmt.Errorf("id is required")
		}
		return nil
	}))

	server := newTestServer(t, trpc.NewHandler(r, "/trpc"))
	resp, err := http.Get(server.URL + "/trpc/stream")
	if err != nil {
		t.Fatalf("request error: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	bodyStr := string(body)

	if !strings.Contains(bodyStr, "serialized-error") {
		t.Fatalf("body missing serialized-error event, got:\n%s", bodyStr)
	}
	if strings.Contains(bodyStr, "id is required") {
		t.Fatalf("SSE body leaked validator details, got:\n%s", bodyStr)
	}
	if !strings.Contains(bodyStr, "internal server error") {
		t.Fatalf("SSE body missing generic internal server error, got:\n%s", bodyStr)
	}
}

func TestOutputValidatorErrorFormatterSanitized(t *testing.T) {
	secret := "validator-secret"
	var formatterErr *trpcgo.Error

	r := trpcgo.NewRouter(trpcgo.WithErrorFormatter(func(input trpcgo.ErrorFormatterInput) any {
		formatterErr = input.Error
		return input.Shape
	}))
	trpcgo.MustVoidQuery(r, "bad", func(ctx context.Context) (User, error) {
		return User{}, nil
	}, trpcgo.OutputValidator(func(u User) error {
		return fmt.Errorf("validator failed: %s", secret)
	}))

	server := newTestServer(t, trpc.NewHandler(r, "/trpc"))
	resp := mustGet(t, server, "/trpc/bad")
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)

	if formatterErr == nil {
		t.Fatal("formatter error was nil")
	}
	if formatterErr.Cause != nil {
		t.Fatalf("formatter should not receive validator cause, got %v", formatterErr.Cause)
	}
	if formatterErr.Message != "internal server error" {
		t.Fatalf("formatter message = %q, want internal server error", formatterErr.Message)
	}
	if strings.Contains(string(body), secret) {
		t.Fatalf("client response leaked validator cause: %s", body)
	}
}

func TestOutputValidatorErrorFormatterSSESanitized(t *testing.T) {
	secret := "validator-sse-secret"
	var formatterErr *trpcgo.Error

	r := trpcgo.NewRouter(trpcgo.WithErrorFormatter(func(input trpcgo.ErrorFormatterInput) any {
		formatterErr = input.Error
		return input.Shape
	}))
	trpcgo.MustVoidSubscribe(r, "stream", func(ctx context.Context) (<-chan User, error) {
		ch := make(chan User, 1)
		ch <- User{}
		close(ch)
		return ch, nil
	}, trpcgo.OutputValidator(func(u User) error {
		return fmt.Errorf("validator failed: %s", secret)
	}))

	server := newTestServer(t, trpc.NewHandler(r, "/trpc"))
	resp, err := http.Get(server.URL + "/trpc/stream")
	if err != nil {
		t.Fatalf("request error: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)

	if formatterErr == nil {
		t.Fatal("formatter error was nil")
	}
	if formatterErr.Cause != nil {
		t.Fatalf("formatter should not receive SSE validator cause, got %v", formatterErr.Cause)
	}
	if formatterErr.Message != "internal server error" {
		t.Fatalf("formatter message = %q, want internal server error", formatterErr.Message)
	}
	if strings.Contains(string(body), secret) {
		t.Fatalf("SSE body leaked validator cause: %s", body)
	}
}

func TestOutputValidatorPanicInSSEDoesNotCrashServer(t *testing.T) {
	secret := "validator-sse-panic-secret"
	r := trpcgo.NewRouter()
	trpcgo.MustVoidQuery(r, "health", func(ctx context.Context) (string, error) {
		return "ok", nil
	})
	trpcgo.MustVoidSubscribe(r, "stream", func(ctx context.Context) (<-chan User, error) {
		ch := make(chan User, 1)
		ch <- User{}
		close(ch)
		return ch, nil
	}, trpcgo.OutputValidator(func(u User) error {
		panic(secret)
	}))

	server := newTestServer(t, trpc.NewHandler(r, "/trpc"))
	resp, err := http.Get(server.URL + "/trpc/stream")
	if err != nil {
		t.Fatalf("request error: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()

	if !strings.Contains(string(body), "serialized-error") {
		t.Fatalf("SSE panic should produce serialized-error, got: %s", body)
	}
	if strings.Contains(string(body), secret) {
		t.Fatalf("SSE panic leaked panic payload: %s", body)
	}

	resp2 := mustGet(t, server, "/trpc/health")
	defer func() { _ = resp2.Body.Close() }()
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("server unhealthy after SSE panic: status %d", resp2.StatusCode)
	}
}

func TestOutputValidatorOnErrorReceivesCause(t *testing.T) {
	secret := "validator-cause"
	var capturedErr *trpcgo.Error

	r := trpcgo.NewRouter(trpcgo.WithOnError(func(ctx context.Context, err *trpcgo.Error, path string) {
		capturedErr = err
	}))
	trpcgo.MustVoidQuery(r, "bad", func(ctx context.Context) (User, error) {
		return User{}, nil
	}, trpcgo.OutputValidator(func(u User) error {
		return fmt.Errorf("validator failed: %s", secret)
	}))

	server := newTestServer(t, trpc.NewHandler(r, "/trpc"))
	resp := mustGet(t, server, "/trpc/bad")
	defer func() { _ = resp.Body.Close() }()

	if capturedErr == nil {
		t.Fatal("onError was not called")
	}
	if capturedErr.Cause == nil || !strings.Contains(capturedErr.Cause.Error(), secret) {
		t.Fatalf("onError cause = %v, want it to contain %q", capturedErr.Cause, secret)
	}
}

func TestOutputValidatorOnErrorReceivesSSECause(t *testing.T) {
	secret := "validator-sse-cause"
	var capturedErr *trpcgo.Error

	r := trpcgo.NewRouter(trpcgo.WithOnError(func(ctx context.Context, err *trpcgo.Error, path string) {
		capturedErr = err
	}))
	trpcgo.MustVoidSubscribe(r, "stream", func(ctx context.Context) (<-chan User, error) {
		ch := make(chan User, 1)
		ch <- User{}
		close(ch)
		return ch, nil
	}, trpcgo.OutputValidator(func(u User) error {
		return fmt.Errorf("validator failed: %s", secret)
	}))

	server := newTestServer(t, trpc.NewHandler(r, "/trpc"))
	resp, err := http.Get(server.URL + "/trpc/stream")
	if err != nil {
		t.Fatalf("request error: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.ReadAll(resp.Body)

	if capturedErr == nil {
		t.Fatal("onError was not called")
	}
	if capturedErr.Cause == nil || !strings.Contains(capturedErr.Cause.Error(), secret) {
		t.Fatalf("onError cause = %v, want it to contain %q", capturedErr.Cause, secret)
	}
}

func TestRawCallOutputValidatorErrorSanitized(t *testing.T) {
	secret := "rawcall-validator-cause"
	r := trpcgo.NewRouter()
	trpcgo.MustVoidQuery(r, "bad", func(ctx context.Context) (User, error) {
		return User{}, nil
	}, trpcgo.OutputValidator(func(u User) error {
		return fmt.Errorf("validator failed: %s", secret)
	}))

	_, err := r.RawCall(context.Background(), "bad", nil)
	if err == nil {
		t.Fatal("expected RawCall error")
	}
	trpcErr, ok := err.(*trpcgo.Error)
	if !ok {
		t.Fatalf("RawCall error type = %T, want *trpcgo.Error", err)
	}
	if trpcErr.Code != trpcgo.CodeInternalServerError {
		t.Fatalf("RawCall error code = %v, want INTERNAL_SERVER_ERROR", trpcErr.Code)
	}
	if trpcErr.Message != "internal server error" {
		t.Fatalf("RawCall error message = %q, want internal server error", trpcErr.Message)
	}
	if trpcErr.Cause != nil {
		t.Fatalf("RawCall error should be sanitized, got cause %v", trpcErr.Cause)
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("RawCall error leaked validator cause: %v", err)
	}
}

func TestOutputValidatorCodegenReflectionKeepsHandlerType(t *testing.T) {
	r := trpcgo.NewRouter()
	trpcgo.MustVoidQuery(r, "user", func(ctx context.Context) (User, error) {
		return User{ID: "1", Name: "Alice"}, nil
	}, trpcgo.OutputValidator(func(u User) error {
		if u.ID == "" {
			return fmt.Errorf("id required")
		}
		return nil
	}))

	outputPath := filepath.Join(t.TempDir(), "trpc.ts")
	if err := r.GenerateTS(outputPath); err != nil {
		t.Fatalf("GenerateTS failed: %v", err)
	}
	data, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	ts := string(data)

	if !strings.Contains(ts, "$Query<void, User>") {
		t.Fatalf("expected validator-only output to keep User type, got:\n%s", ts)
	}
	if strings.Contains(ts, "$Query<void, unknown>") {
		t.Fatalf("validator-only output should not degrade to unknown, got:\n%s", ts)
	}
	if !strings.Contains(ts, "name: string;") {
		t.Fatalf("validator-only output should keep full handler shape, got:\n%s", ts)
	}
}

func TestOutputParserRejectsInvalidOutput(t *testing.T) {
	r := trpcgo.NewRouter()
	trpcgo.MustVoidQuery(r, "bad", func(ctx context.Context) (User, error) {
		return User{}, nil // ID is empty — parser will reject
	}, trpcgo.OutputParser(func(u User) (any, error) {
		if u.ID == "" {
			return nil, fmt.Errorf("id is required")
		}
		return u, nil
	}))

	server := newTestServer(t, trpc.NewHandler(r, "/trpc"))
	resp := mustGet(t, server, "/trpc/bad")
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	bodyStr := string(body)
	if !strings.Contains(bodyStr, "INTERNAL_SERVER_ERROR") {
		t.Errorf("body missing INTERNAL_SERVER_ERROR, got: %s", body)
	}
	if !strings.Contains(bodyStr, "internal server error") {
		t.Errorf("body missing generic internal server error message, got: %s", body)
	}
	if strings.Contains(bodyStr, "output validation failed") {
		t.Errorf("body leaked parser-specific failure message, got: %s", body)
	}
}

func TestOutputParserTransformsOutput(t *testing.T) {
	// Parser can reshape the output — the transformed value is what the client sees.
	type PublicUser struct {
		ID string `json:"id"`
	}
	r := trpcgo.NewRouter()
	trpcgo.MustVoidQuery(r, "strip", func(ctx context.Context) (User, error) {
		return User{ID: "42", Name: "Alice"}, nil
	}, trpcgo.OutputParser(func(u User) (any, error) {
		// Strip the name field before sending to client.
		return PublicUser{ID: u.ID}, nil
	}))

	server := newTestServer(t, trpc.NewHandler(r, "/trpc"))
	resp := mustGet(t, server, "/trpc/strip")
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	bodyStr := string(body)
	if strings.Contains(bodyStr, "Alice") {
		t.Errorf("stripped name must not appear in response, got: %s", bodyStr)
	}
	if !strings.Contains(bodyStr, `"id":"42"`) {
		t.Errorf("id missing from response, got: %s", bodyStr)
	}
}

func TestOutputParserPassesValidOutput(t *testing.T) {
	r := trpcgo.NewRouter()
	trpcgo.MustVoidQuery(r, "good", func(ctx context.Context) (User, error) {
		return User{ID: "1", Name: "Alice"}, nil
	}, trpcgo.OutputParser(func(u User) (any, error) {
		if u.ID == "" {
			return nil, fmt.Errorf("id is required")
		}
		return u, nil
	}))

	server := newTestServer(t, trpc.NewHandler(r, "/trpc"))
	resp := mustGet(t, server, "/trpc/good")
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	body := decodeJSON(t, resp)
	data := resultData(t, body)
	if data["id"] != "1" {
		t.Fatalf("result.data.id = %v, want 1", data["id"])
	}
	if data["name"] != "Alice" {
		t.Fatalf("result.data.name = %v, want Alice", data["name"])
	}
}

func TestOutputParserNotCalledOnHandlerError(t *testing.T) {
	// When the handler itself returns an error, the output parser must not run.
	parserCalled := false
	r := trpcgo.NewRouter()
	trpcgo.MustVoidQuery(r, "fail", func(ctx context.Context) (User, error) {
		return User{}, trpcgo.NewError(trpcgo.CodeNotFound, "not found")
	}, trpcgo.OutputParser(func(u User) (any, error) {
		parserCalled = true
		return u, nil
	}))

	server := newTestServer(t, trpc.NewHandler(r, "/trpc"))
	resp := mustGet(t, server, "/trpc/fail")
	_ = resp.Body.Close()

	if parserCalled {
		t.Error("output parser must not be called when handler returns an error")
	}
}

func TestOutputParserRunsOnNilOutput(t *testing.T) {
	parserCalled := false
	r := trpcgo.NewRouter()
	trpcgo.MustVoidQuery(r, "nil", func(ctx context.Context) (any, error) {
		return nil, nil
	}, trpcgo.WithOutputParser(func(v any) (any, error) {
		parserCalled = true
		if v != nil {
			return nil, fmt.Errorf("expected nil output, got %T", v)
		}
		return "normalized", nil
	}))

	server := newTestServer(t, trpc.NewHandler(r, "/trpc"))
	resp := mustGet(t, server, "/trpc/nil")
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if !parserCalled {
		t.Fatal("output parser should be called for nil output")
	}
	body := decodeJSON(t, resp)
	if got := resultScalar(t, body); got != "normalized" {
		t.Fatalf("result.data = %v, want normalized", got)
	}
}

func TestTypedOutputParserRunsOnNilAnyOutput(t *testing.T) {
	parserCalled := false
	r := trpcgo.NewRouter()
	trpcgo.MustVoidQuery(r, "niltyped", func(ctx context.Context) (any, error) {
		return nil, nil
	}, trpcgo.OutputParser(func(v any) (string, error) {
		parserCalled = true
		if v != nil {
			return "", fmt.Errorf("expected nil output, got %T", v)
		}
		return "normalized", nil
	}))

	server := newTestServer(t, trpc.NewHandler(r, "/trpc"))
	resp := mustGet(t, server, "/trpc/niltyped")
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if !parserCalled {
		t.Fatal("typed output parser should be called for nil output")
	}
	body := decodeJSON(t, resp)
	if got := resultScalar(t, body); got != "normalized" {
		t.Fatalf("result.data = %v, want normalized", got)
	}
}

func TestOutputParserOnBuilder(t *testing.T) {
	// WithOutputParser applied via ProcedureBuilder is propagated to the procedure.
	base := trpcgo.Procedure().WithOutputParser(func(v any) (any, error) {
		u, ok := v.(User)
		if !ok {
			return v, nil
		}
		if u.ID == "" {
			return nil, fmt.Errorf("id required")
		}
		return u, nil
	})

	r := trpcgo.NewRouter()
	trpcgo.MustVoidQuery(r, "a", func(ctx context.Context) (User, error) {
		return User{}, nil // will fail
	}, base)
	trpcgo.MustVoidQuery(r, "b", func(ctx context.Context) (User, error) {
		return User{ID: "1"}, nil // will pass
	}, base)

	server := newTestServer(t, trpc.NewHandler(r, "/trpc"))

	resp := mustGet(t, server, "/trpc/a")
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("a: status = %d, want 500", resp.StatusCode)
	}

	resp = mustGet(t, server, "/trpc/b")
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		t.Errorf("b: status = %d, want 200", resp.StatusCode)
	} else {
		body := decodeJSON(t, resp)
		data := resultData(t, body)
		if data["id"] != "1" {
			t.Errorf("b: result.data.id = %v, want 1", data["id"])
		}
	}
}

func TestOutputParserWithMethodRuntime(t *testing.T) {
	type PublicUser struct {
		ID string `json:"id"`
	}

	r := trpcgo.NewRouter()
	base := trpcgo.Procedure().With(trpcgo.OutputParser(func(u User) (PublicUser, error) {
		return PublicUser{ID: u.ID}, nil
	}))

	trpcgo.MustVoidQuery(r, "strip", func(ctx context.Context) (User, error) {
		return User{ID: "42", Name: "Alice"}, nil
	}, base)

	server := newTestServer(t, trpc.NewHandler(r, "/trpc"))
	resp := mustGet(t, server, "/trpc/strip")
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	body := decodeJSON(t, resp)
	data := resultData(t, body)
	if data["id"] != "42" {
		t.Fatalf("result.data.id = %v, want 42", data["id"])
	}
	if _, ok := data["name"]; ok {
		t.Fatalf("result.data should not contain name, got %v", data)
	}
}

func TestOutputParserQueryWithInput(t *testing.T) {
	type PublicUser struct {
		ID string `json:"id"`
	}

	r := trpcgo.NewRouter()
	trpcgo.MustQuery(r, "user.get", func(ctx context.Context, input GetUserInput) (User, error) {
		return User{ID: input.ID, Name: "Alice"}, nil
	}, trpcgo.OutputParser(func(u User) (PublicUser, error) {
		return PublicUser{ID: u.ID}, nil
	}))

	server := newTestServer(t, trpc.NewHandler(r, "/trpc"))
	resp := mustGet(t, server, "/trpc/user.get?input="+url.QueryEscape(`{"id":"7"}`))
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	body := decodeJSON(t, resp)
	data := resultData(t, body)
	if data["id"] != "7" {
		t.Fatalf("result.data.id = %v, want 7", data["id"])
	}
	if _, ok := data["name"]; ok {
		t.Fatalf("result.data should not contain name, got %v", data)
	}
}

func TestOutputParserSubscription(t *testing.T) {
	// When a subscription emits an item that fails the output parser, the stream
	// must send a serialized-error event and close — same as a marshal failure.
	// When a valid item is emitted, the parser's returned value is what is sent.
	type PublicUser struct {
		ID string `json:"id"`
	}
	r := trpcgo.NewRouter()
	trpcgo.MustVoidSubscribe(r, "stream", func(ctx context.Context) (<-chan User, error) {
		ch := make(chan User, 2)
		ch <- User{ID: "1", Name: "Alice"} // valid — name will be stripped
		ch <- User{}                       // invalid: no ID
		close(ch)
		return ch, nil
	}, trpcgo.OutputParser(func(u User) (any, error) {
		if u.ID == "" {
			return nil, fmt.Errorf("id required")
		}
		return PublicUser{ID: u.ID}, nil // transform: strip name
	}))

	server := newTestServer(t, trpc.NewHandler(r, "/trpc"))
	resp, err := http.Get(server.URL + "/trpc/stream")
	if err != nil {
		t.Fatalf("request error: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, _ := io.ReadAll(resp.Body)
	bodyStr := string(body)

	// First item: emitted with transformed value (no name field).
	if strings.Contains(bodyStr, "Alice") {
		t.Errorf("stripped name must not appear in SSE body, got:\n%s", bodyStr)
	}
	if !strings.Contains(bodyStr, `"id":"1"`) {
		t.Errorf("body missing first valid item id, got:\n%s", bodyStr)
	}
	// Second item: parser returns error → serialized-error event.
	if !strings.Contains(bodyStr, "serialized-error") {
		t.Errorf("body missing serialized-error event, got:\n%s", bodyStr)
	}
	if !strings.Contains(bodyStr, "INTERNAL_SERVER_ERROR") {
		t.Errorf("serialized-error missing INTERNAL_SERVER_ERROR, got:\n%s", bodyStr)
	}
	if strings.Contains(bodyStr, "output validation failed") {
		t.Errorf("serialized-error leaked parser-specific failure message, got:\n%s", bodyStr)
	}
}

func TestOutputParserErrorFormatterSanitized(t *testing.T) {
	secret := "redis://secret-output-parser"
	var formatterErr *trpcgo.Error
	var formatterBody any

	r := trpcgo.NewRouter(trpcgo.WithErrorFormatter(func(input trpcgo.ErrorFormatterInput) any {
		formatterErr = input.Error
		formatterBody = input.Shape
		return input.Shape
	}))
	trpcgo.MustVoidQuery(r, "bad", func(ctx context.Context) (User, error) {
		return User{}, nil
	}, trpcgo.OutputParser(func(u User) (User, error) {
		return User{}, fmt.Errorf("parser failed: %s", secret)
	}))

	server := newTestServer(t, trpc.NewHandler(r, "/trpc"))
	resp := mustGet(t, server, "/trpc/bad")
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)

	if formatterErr == nil {
		t.Fatal("formatter error was nil")
	}
	if formatterErr.Cause != nil {
		t.Fatalf("formatter should not receive internal parser cause, got %v", formatterErr.Cause)
	}
	if got := formatterErr.Message; got != "internal server error" {
		t.Fatalf("formatter message = %q, want internal server error", got)
	}
	if strings.Contains(string(body), secret) {
		t.Fatalf("client response leaked parser cause: %s", body)
	}
	if formatterBody == nil {
		t.Fatal("formatter did not receive a shape")
	}
}

func TestOutputParserErrorFormatterSSESanitized(t *testing.T) {
	secret := "sse-parser-secret"
	var formatterErr *trpcgo.Error

	r := trpcgo.NewRouter(trpcgo.WithErrorFormatter(func(input trpcgo.ErrorFormatterInput) any {
		formatterErr = input.Error
		return input.Shape
	}))
	trpcgo.MustVoidSubscribe(r, "stream", func(ctx context.Context) (<-chan User, error) {
		ch := make(chan User, 1)
		ch <- User{ID: "1"}
		close(ch)
		return ch, nil
	}, trpcgo.OutputParser(func(u User) (User, error) {
		return User{}, fmt.Errorf("parser failed: %s", secret)
	}))

	server := newTestServer(t, trpc.NewHandler(r, "/trpc"))
	resp, err := http.Get(server.URL + "/trpc/stream")
	if err != nil {
		t.Fatalf("request error: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)

	if formatterErr == nil {
		t.Fatal("formatter error was nil")
	}
	if formatterErr.Cause != nil {
		t.Fatalf("formatter should not receive SSE parser cause, got %v", formatterErr.Cause)
	}
	if got := formatterErr.Message; got != "internal server error" {
		t.Fatalf("formatter message = %q, want internal server error", got)
	}
	if strings.Contains(string(body), secret) {
		t.Fatalf("SSE body leaked parser cause: %s", body)
	}
	if strings.Contains(string(body), "output validation failed") {
		t.Fatalf("SSE body leaked parser-specific message: %s", body)
	}
}

func TestOutputParserOnErrorReceivesCause(t *testing.T) {
	secret := "output-parser-cause"
	var capturedErr *trpcgo.Error

	r := trpcgo.NewRouter(trpcgo.WithOnError(func(ctx context.Context, err *trpcgo.Error, path string) {
		capturedErr = err
	}))
	trpcgo.MustVoidQuery(r, "bad", func(ctx context.Context) (User, error) {
		return User{}, nil
	}, trpcgo.OutputParser(func(u User) (User, error) {
		return User{}, fmt.Errorf("parser failed: %s", secret)
	}))

	server := newTestServer(t, trpc.NewHandler(r, "/trpc"))
	resp := mustGet(t, server, "/trpc/bad")
	defer func() { _ = resp.Body.Close() }()

	if capturedErr == nil {
		t.Fatal("onError was not called")
	}
	if capturedErr.Cause == nil || !strings.Contains(capturedErr.Cause.Error(), secret) {
		t.Fatalf("onError cause = %v, want it to contain %q", capturedErr.Cause, secret)
	}
	if capturedErr.Message != "internal server error" {
		t.Fatalf("onError message = %q, want internal server error", capturedErr.Message)
	}
}

func TestOutputParserOnErrorReceivesSSECause(t *testing.T) {
	secret := "output-parser-sse-cause"
	var capturedErr *trpcgo.Error

	r := trpcgo.NewRouter(trpcgo.WithOnError(func(ctx context.Context, err *trpcgo.Error, path string) {
		capturedErr = err
	}))
	trpcgo.MustVoidSubscribe(r, "stream", func(ctx context.Context) (<-chan User, error) {
		ch := make(chan User, 1)
		ch <- User{ID: "1"}
		close(ch)
		return ch, nil
	}, trpcgo.OutputParser(func(u User) (User, error) {
		return User{}, fmt.Errorf("parser failed: %s", secret)
	}))

	server := newTestServer(t, trpc.NewHandler(r, "/trpc"))
	resp, err := http.Get(server.URL + "/trpc/stream")
	if err != nil {
		t.Fatalf("request error: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.ReadAll(resp.Body)

	if capturedErr == nil {
		t.Fatal("onError was not called")
	}
	if capturedErr.Cause == nil || !strings.Contains(capturedErr.Cause.Error(), secret) {
		t.Fatalf("onError cause = %v, want it to contain %q", capturedErr.Cause, secret)
	}
}

func TestRawCallOutputParserErrorSanitized(t *testing.T) {
	secret := "rawcall-parser-cause"
	r := trpcgo.NewRouter()
	trpcgo.MustVoidQuery(r, "bad", func(ctx context.Context) (User, error) {
		return User{}, nil
	}, trpcgo.OutputParser(func(u User) (User, error) {
		return User{}, fmt.Errorf("parser failed: %s", secret)
	}))

	_, err := r.RawCall(context.Background(), "bad", nil)
	if err == nil {
		t.Fatal("expected RawCall error")
	}
	trpcErr, ok := err.(*trpcgo.Error)
	if !ok {
		t.Fatalf("RawCall error type = %T, want *trpcgo.Error", err)
	}
	if trpcErr.Code != trpcgo.CodeInternalServerError {
		t.Fatalf("RawCall error code = %v, want INTERNAL_SERVER_ERROR", trpcErr.Code)
	}
	if trpcErr.Message != "internal server error" {
		t.Fatalf("RawCall error message = %q, want internal server error", trpcErr.Message)
	}
	if trpcErr.Cause != nil {
		t.Fatalf("RawCall error should be sanitized, got cause %v", trpcErr.Cause)
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("RawCall error leaked parser cause: %v", err)
	}
}

func TestOutputParserCodegenReflection(t *testing.T) {
	// When OutputParser[O, P] is used, GenerateTS must emit P as the output type,
	// not the handler's O. This verifies the reflection codegen path.
	type PublicUser struct {
		ID string `json:"id"`
	}

	r := trpcgo.NewRouter()
	trpcgo.MustVoidQuery(r, "strip", func(ctx context.Context) (User, error) {
		return User{}, nil
	}, trpcgo.OutputParser(func(u User) (PublicUser, error) {
		return PublicUser{ID: u.ID}, nil
	}))

	outputPath := filepath.Join(t.TempDir(), "trpc.ts")
	if err := r.GenerateTS(outputPath); err != nil {
		t.Fatalf("GenerateTS failed: %v", err)
	}
	data, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	ts := string(data)

	// Output type must be PublicUser (P), not User (O).
	if !strings.Contains(ts, "$Query<void, PublicUser>") {
		t.Errorf("expected $Query<void, PublicUser> in output, got:\n%s", ts)
	}
	// PublicUser has no name field — if User was emitted instead, name would appear.
	if strings.Contains(ts, "name: string;") {
		t.Errorf("name field must not appear (would indicate User used instead of PublicUser):\n%s", ts)
	}
}

func TestOutputParserPrecedenceReflection(t *testing.T) {
	t.Run("later untyped parser degrades output to unknown", func(t *testing.T) {
		r := trpcgo.NewRouter()
		trpcgo.MustVoidQuery(r, "strip", func(ctx context.Context) (User, error) {
			return User{ID: "1", Name: "Alice"}, nil
		},
			trpcgo.OutputParser(func(u User) (struct {
				ID string `json:"id"`
			}, error) {
				return struct {
					ID string `json:"id"`
				}{ID: u.ID}, nil
			}),
			trpcgo.WithOutputParser(func(v any) (any, error) {
				return v, nil
			}),
		)

		outputPath := filepath.Join(t.TempDir(), "trpc.ts")
		if err := r.GenerateTS(outputPath); err != nil {
			t.Fatalf("GenerateTS failed: %v", err)
		}
		data, err := os.ReadFile(outputPath)
		if err != nil {
			t.Fatal(err)
		}
		ts := string(data)

		if !strings.Contains(ts, "$Query<void, unknown>") {
			t.Fatalf("expected later untyped parser to force unknown output, got:\n%s", ts)
		}
	})

	t.Run("later typed parser overrides earlier untyped parser", func(t *testing.T) {
		type PublicUser struct {
			ID string `json:"id"`
		}

		r := trpcgo.NewRouter()
		trpcgo.MustVoidQuery(r, "strip", func(ctx context.Context) (User, error) {
			return User{ID: "1", Name: "Alice"}, nil
		},
			trpcgo.WithOutputParser(func(v any) (any, error) {
				return v, nil
			}),
			trpcgo.OutputParser(func(u User) (PublicUser, error) {
				return PublicUser{ID: u.ID}, nil
			}),
		)

		outputPath := filepath.Join(t.TempDir(), "trpc.ts")
		if err := r.GenerateTS(outputPath); err != nil {
			t.Fatalf("GenerateTS failed: %v", err)
		}
		data, err := os.ReadFile(outputPath)
		if err != nil {
			t.Fatal(err)
		}
		ts := string(data)

		if !strings.Contains(ts, "$Query<void, PublicUser>") {
			t.Fatalf("expected later typed parser to win, got:\n%s", ts)
		}
	})

	t.Run("later nil untyped parser clears earlier typed parser", func(t *testing.T) {
		r := trpcgo.NewRouter()
		trpcgo.MustVoidQuery(r, "strip", func(ctx context.Context) (User, error) {
			return User{ID: "1", Name: "Alice"}, nil
		},
			trpcgo.OutputParser(func(u User) (struct {
				ID string `json:"id"`
			}, error) {
				return struct {
					ID string `json:"id"`
				}{ID: u.ID}, nil
			}),
			trpcgo.WithOutputParser(nil),
		)

		outputPath := filepath.Join(t.TempDir(), "trpc.ts")
		if err := r.GenerateTS(outputPath); err != nil {
			t.Fatalf("GenerateTS failed: %v", err)
		}
		data, err := os.ReadFile(outputPath)
		if err != nil {
			t.Fatal(err)
		}
		ts := string(data)

		if !strings.Contains(ts, "$Query<void, User>") {
			t.Fatalf("expected later nil untyped parser to clear parser and restore handler output type, got:\n%s", ts)
		}
	})
}
