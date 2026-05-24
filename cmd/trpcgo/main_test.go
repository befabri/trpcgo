package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"
)

func testRepoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

func analysisFixtureDir(t *testing.T, name string) string {
	t.Helper()
	return filepath.Join(testRepoRoot(t), "internal", "analysis", "testdata", name)
}

func TestRunGenerateWritesTypesAndZodMini(t *testing.T) {
	typesOut := filepath.Join(t.TempDir(), "router.ts")
	zodOut := filepath.Join(t.TempDir(), "schemas.ts")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := run([]string{
		"generate",
		"--dir", analysisFixtureDir(t, "basic"),
		"-o", typesOut,
		"--zod", zodOut,
		"--zod-mini",
	}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("run generate: %v\nstderr:\n%s", err, stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty when -o is used", stdout.String())
	}
	if strings.Contains(stderr.String(), "Warning:") {
		t.Fatalf("stderr contains unexpected warning: %s", stderr.String())
	}

	typesData, err := os.ReadFile(typesOut)
	if err != nil {
		t.Fatal(err)
	}
	types := string(typesData)
	for _, want := range []string{
		"export type AppRouter",
		"$Mutation<CreateUserInput, User>",
		"$Subscription<void, User>",
	} {
		if !strings.Contains(types, want) {
			t.Fatalf("generated types missing %q:\n%s", want, types)
		}
	}

	zodData, err := os.ReadFile(zodOut)
	if err != nil {
		t.Fatal(err)
	}
	zod := string(zodData)
	for _, want := range []string{
		`import * as z from "zod/mini"`,
		"CreateUserInputSchema",
		"GetUserByIdInputSchema",
	} {
		if !strings.Contains(zod, want) {
			t.Fatalf("generated zod missing %q:\n%s", want, zod)
		}
	}
}

func TestRunGenerateWritesToStdout(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := run([]string{"generate", "--dir", analysisFixtureDir(t, "basic")}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("run generate: %v\nstderr:\n%s", err, stderr.String())
	}
	if !strings.Contains(stdout.String(), "export type AppRouter") {
		t.Fatalf("stdout missing generated router:\n%s", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
}

func TestRunGenerateWarnsWhenNoProceduresFound(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/empty\n\ngo 1.26.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "empty.go"), []byte("package empty\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := run([]string{"generate", "--dir", dir}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("run generate: %v\nstderr:\n%s", err, stderr.String())
	}
	if !strings.Contains(stderr.String(), "Warning: no tRPC procedure registrations found") {
		t.Fatalf("stderr missing no-procedures warning: %q", stderr.String())
	}
	if !strings.Contains(stdout.String(), "export type AppRouter") {
		t.Fatalf("stdout missing empty router output:\n%s", stdout.String())
	}
}

func TestRunRejectsInvalidCommandsAndFlags(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantErr    error
		wantStderr string
	}{
		{name: "missing command", args: nil, wantErr: errUsage, wantStderr: "Usage: trpcgo generate"},
		{name: "unknown command", args: []string{"version"}, wantErr: errUsage, wantStderr: "Usage: trpcgo generate"},
		{name: "unknown flag", args: []string{"generate", "--definitely-not-a-flag"}, wantErr: errFlag, wantStderr: "flag provided but not defined"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			err := run(tt.args, &stdout, &stderr)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("run() error = %v, want %v", err, tt.wantErr)
			}
			if stdout.Len() != 0 {
				t.Fatalf("stdout = %q, want empty", stdout.String())
			}
			if !strings.Contains(stderr.String(), tt.wantStderr) {
				t.Fatalf("stderr = %q, want substring %q", stderr.String(), tt.wantStderr)
			}
		})
	}
}

func TestRunGenerateHelpReturnsNil(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := run([]string{"generate", "-h"}, &stdout, &stderr); err != nil {
		t.Fatalf("help returned error: %v", err)
	}
	if !strings.Contains(stderr.String(), "Usage: trpcgo generate") {
		t.Fatalf("help output missing usage: %q", stderr.String())
	}
}

func TestWatchGenerateLoopDebouncesGoWrites(t *testing.T) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = watcher.Close() }()

	done := make(chan struct{})
	generated := make(chan struct{}, 1)
	finished := make(chan struct{})
	instantAfter := func(time.Duration) <-chan time.Time {
		ch := make(chan time.Time, 1)
		ch <- time.Now()
		return ch
	}
	generateFn := func(generateOptions) error {
		generated <- struct{}{}
		close(done)
		return nil
	}

	go func() {
		watchGenerateLoop(generateOptions{}, watcher, done, instantAfter, generateFn)
		close(finished)
	}()

	watcher.Events <- fsnotify.Event{Name: "router.go", Op: fsnotify.Write}

	select {
	case <-generated:
	case <-time.After(time.Second):
		t.Fatal("generate was not called for .go write event")
	}
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("watch loop did not stop after done channel closed")
	}
}

func TestWatchGenerateLoopStopsWhenDoneCloses(t *testing.T) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = watcher.Close() }()
	done := make(chan struct{})
	close(done)

	watchGenerateLoop(generateOptions{}, watcher, done, nil, func(generateOptions) error {
		t.Fatal("generate should not be called after done is closed")
		return nil
	})
}
