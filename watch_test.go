package trpcgo

import (
	"bytes"
	"errors"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/befabri/trpcgo/internal/typemap"
	"github.com/fsnotify/fsnotify"
)

func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	old := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(old) })
	return &buf
}

func watchAnalysisFixtureDir(t *testing.T, name string) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Join(filepath.Dir(file), "internal", "analysis", "testdata", name)
}

func writeWatchAnalysisModule(t *testing.T, source string) string {
	t.Helper()
	dir := t.TempDir()
	repoRoot, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	gomod := "module example.com/watched\n\ngo 1.26.0\n\nrequire github.com/befabri/trpcgo v0.0.0\n\nreplace github.com/befabri/trpcgo => " + filepath.ToSlash(repoRoot) + "\n"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(gomod), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "router.go"), []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestAbsPath(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string // empty means check it became absolute
	}{
		{"empty", "", ""},
		{"already absolute", "/usr/local/bin", "/usr/local/bin"},
		{"relative", "gen/trpc.ts", ""}, // will be resolved to cwd + path
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := absPath(tt.input)
			if tt.want != "" {
				if got != tt.want {
					t.Errorf("absPath(%q) = %q, want %q", tt.input, got, tt.want)
				}
			} else if tt.input == "" {
				if got != "" {
					t.Errorf("absPath(%q) = %q, want empty", tt.input, got)
				}
			} else {
				// Relative input should become absolute.
				if !filepath.IsAbs(got) {
					t.Errorf("absPath(%q) = %q, expected absolute path", tt.input, got)
				}
			}
		})
	}
}

func TestWithWatchPackagesFiltersEmptyPatterns(t *testing.T) {
	r := NewRouter(WithWatchPackages("./internal/...", "", "./trpc"))

	want := []string{"./internal/...", "./trpc"}
	if len(r.opts.watchPackages) != len(want) {
		t.Fatalf("watchPackages = %v, want %v", r.opts.watchPackages, want)
	}
	for i := range want {
		if r.opts.watchPackages[i] != want[i] {
			t.Fatalf("watchPackages = %v, want %v", r.opts.watchPackages, want)
		}
	}
}

func TestWithWatchPackagesLeavesUnsetWhenOnlyEmpty(t *testing.T) {
	r := NewRouter(WithWatchPackages("", ""))
	if r.opts.watchPackages != nil {
		t.Fatalf("watchPackages = %v, want nil", r.opts.watchPackages)
	}
}

func TestNewWatcherConfigRequiresTypeOutput(t *testing.T) {
	r := NewRouter()
	if _, err := r.newWatcherConfig(); !errors.Is(err, os.ErrInvalid) {
		t.Fatalf("newWatcherConfig error = %v, want os.ErrInvalid", err)
	}
}

func TestNewWatcherConfigResolvesOutputsAndZodStyle(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	r := NewRouter(
		WithTypeOutput(filepath.Join("gen", "router.ts")),
		WithZodOutput(filepath.Join("gen", "schemas.ts")),
		WithEnumsOutput(filepath.Join("gen", "enums.ts")),
		WithZodMini(true),
	)
	cfg, err := r.newWatcherConfig()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cfg.watcher.Close() }()

	if want := filepath.Join(dir, "gen", "enums.ts"); cfg.opts.enumsOutput != want {
		t.Fatalf("enums output = %q, want %q", cfg.opts.enumsOutput, want)
	}

	if cfg.opts.dir != dir {
		t.Fatalf("watch dir = %q, want %q", cfg.opts.dir, dir)
	}
	if len(cfg.opts.patterns) != 1 || cfg.opts.patterns[0] != "." {
		t.Fatalf("patterns = %v, want [.] for full watch", cfg.opts.patterns)
	}
	if want := filepath.Join(dir, "gen", "router.ts"); cfg.opts.output != want {
		t.Fatalf("type output = %q, want %q", cfg.opts.output, want)
	}
	if want := filepath.Join(dir, "gen", "schemas.ts"); cfg.opts.zodOutput != want {
		t.Fatalf("zod output = %q, want %q", cfg.opts.zodOutput, want)
	}
	if cfg.opts.zodStyle != typemap.ZodMini {
		t.Fatalf("zod style = %v, want ZodMini", cfg.opts.zodStyle)
	}
	if cfg.handleDirCreate == nil {
		t.Fatal("handleDirCreate is nil")
	}
}

func TestConfigureWatchScopeUsesPackageScopedPatterns(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "internal", "api"), 0o755); err != nil {
		t.Fatal(err)
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = watcher.Close() }()

	r := NewRouter(WithWatchPackages("./internal/..."))
	patterns, handleDirCreate, err := r.configureWatchScope(watcher, dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(patterns) != 1 || patterns[0] != "./internal/..." {
		t.Fatalf("patterns = %v, want [./internal/...]", patterns)
	}
	if handleDirCreate == nil {
		t.Fatal("handleDirCreate is nil")
	}
}

func TestRunWatcherLoopRegeneratesInitiallyAndAfterGoWrite(t *testing.T) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	finished := make(chan struct{})
	var calls atomic.Int32
	instantAfter := func(time.Duration) <-chan time.Time {
		ch := make(chan time.Time, 1)
		ch <- time.Now()
		return ch
	}
	regenerate := func(watchOpts) {
		if calls.Add(1) == 2 {
			close(done)
		}
	}

	r := NewRouter()
	go func() {
		r.runWatcherLoop(watcherConfig{
			watcher:         watcher,
			done:            done,
			handleDirCreate: func(*fsnotify.Watcher, string) error { return nil },
		}, instantAfter, regenerate)
		close(finished)
	}()

	watcher.Events <- fsnotify.Event{Name: "router.go", Op: fsnotify.Write}

	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("watcher loop did not stop after second regeneration closed done")
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("regenerate calls = %d, want initial + debounced write", got)
	}
}

func TestRunWatcherLoopUsesDefaultDebounceWhenAfterIsNil(t *testing.T) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	finished := make(chan struct{})
	var calls atomic.Int32
	regenerate := func(watchOpts) {
		if calls.Add(1) == 2 {
			close(done)
		}
	}

	r := NewRouter()
	go func() {
		r.runWatcherLoop(watcherConfig{
			watcher:         watcher,
			done:            done,
			handleDirCreate: func(*fsnotify.Watcher, string) error { return nil },
		}, nil, regenerate)
		close(finished)
	}()

	watcher.Events <- fsnotify.Event{Name: "router.go", Op: fsnotify.Write}

	select {
	case <-finished:
	case <-time.After(2 * time.Second):
		t.Fatal("watcher loop did not use default debounce when after was nil")
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("regenerate calls = %d, want initial + debounced write", got)
	}
}

func TestWriteIfChangedCreatesAndSkipsUnchangedContent(t *testing.T) {
	logs := captureLog(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "router.ts")
	data := []byte("export type Router = {};\n")

	writeIfChanged(path, data, "types")
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(data) {
		t.Fatalf("file content = %q, want %q", got, data)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}

	writeIfChanged(path, data, "types")
	info2, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !info.ModTime().Equal(info2.ModTime()) {
		t.Fatalf("unchanged content should not rewrite file: %v != %v", info.ModTime(), info2.ModTime())
	}

	updated := []byte("export type Router = { ping: string };\n")
	writeIfChanged(path, updated, "types")
	got, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(updated) {
		t.Fatalf("updated file content = %q, want %q", got, updated)
	}
	if !strings.Contains(logs.String(), "trpcgo: types regenerated") {
		t.Fatalf("writeIfChanged did not log successful regeneration, logs:\n%s", logs.String())
	}
}

func TestRegenerateFromSourceWritesMiniZodWithTypedInputs(t *testing.T) {
	dir := t.TempDir()
	typesOut := filepath.Join(dir, "router.ts")
	zodOut := filepath.Join(dir, "schemas.ts")

	regenerateFromSource(watchOpts{
		dir:       watchAnalysisFixtureDir(t, "enhanced"),
		patterns:  []string{"."},
		output:    typesOut,
		zodOutput: zodOut,
		zodStyle:  typemap.ZodMini,
	})

	typesData, err := os.ReadFile(typesOut)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(typesData), "$Mutation<CreateUserInput, User>") {
		t.Fatalf("types output missing user.create mutation:\n%s", string(typesData))
	}

	zodData, err := os.ReadFile(zodOut)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`import * as z from "zod/mini"`, "CreateUserInputSchema", "z.email()"} {
		if !strings.Contains(string(zodData), want) {
			t.Fatalf("zod output missing %q:\n%s", want, string(zodData))
		}
	}
}

func TestRegenerateFromSourceWritesEnums(t *testing.T) {
	dir := t.TempDir()
	typesOut := filepath.Join(dir, "router.ts")
	enumsOut := filepath.Join(dir, "enums.ts")

	regenerateFromSource(watchOpts{
		dir:         watchAnalysisFixtureDir(t, "crosspkg"),
		patterns:    []string{"."},
		output:      typesOut,
		enumsOutput: enumsOut,
	})

	enumsData, err := os.ReadFile(enumsOut)
	if err != nil {
		t.Fatal(err)
	}
	enums := string(enumsData)
	for _, want := range []string{
		"export const RoleEnum = {",
		`viewer: "viewer",`,
		"export const StatusEnum = {",
		"} as const;",
	} {
		if !strings.Contains(enums, want) {
			t.Fatalf("enums output missing %q:\n%s", want, enums)
		}
	}

	if strings.Contains(enums, "DurationEnum") {
		t.Fatalf("stdlib Duration leaked an enum object:\n%s", enums)
	}

	typesData, err := os.ReadFile(typesOut)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(typesData), "export const") {
		t.Fatalf("runtime const leaked into the type-only file:\n%s", string(typesData))
	}
}

func TestRegenerateFromSourceEnumsHeaderOnlyWhenNoEnums(t *testing.T) {
	dir := t.TempDir()
	typesOut := filepath.Join(dir, "router.ts")
	enumsOut := filepath.Join(dir, "enums.ts")

	regenerateFromSource(watchOpts{
		dir:         watchAnalysisFixtureDir(t, "basic"),
		patterns:    []string{"."},
		output:      typesOut,
		enumsOutput: enumsOut,
	})

	data, err := os.ReadFile(enumsOut)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "export const") {
		t.Fatalf("basic fixture has no enums; file should be header-only:\n%s", string(data))
	}
}

func TestRegenerateFromSourceSkipsEnumsWhenOutputUnset(t *testing.T) {
	dir := t.TempDir()
	typesOut := filepath.Join(dir, "router.ts")
	enumsOut := filepath.Join(dir, "enums.ts")

	regenerateFromSource(watchOpts{
		dir:      watchAnalysisFixtureDir(t, "crosspkg"),
		patterns: []string{"."},
		output:   typesOut,
		// enumsOutput intentionally unset
	})

	if _, err := os.Stat(enumsOut); !os.IsNotExist(err) {
		t.Fatalf("enums file should not be written when output unset, stat err = %v", err)
	}
}

func TestRegenerateFromSourcePreservesFilesOnAnalysisError(t *testing.T) {
	dir := t.TempDir()
	typesOut := filepath.Join(dir, "router.ts")
	zodOut := filepath.Join(dir, "schemas.ts")
	if err := os.WriteFile(typesOut, []byte("existing types"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(zodOut, []byte("existing zod"), 0o644); err != nil {
		t.Fatal(err)
	}

	regenerateFromSource(watchOpts{
		dir:       filepath.Join(dir, "missing"),
		patterns:  []string{"."},
		output:    typesOut,
		zodOutput: zodOut,
		zodStyle:  typemap.ZodStandard,
	})

	if got, err := os.ReadFile(typesOut); err != nil || string(got) != "existing types" {
		t.Fatalf("types output = %q, %v; want preserved", got, err)
	}
	if got, err := os.ReadFile(zodOut); err != nil || string(got) != "existing zod" {
		t.Fatalf("zod output = %q, %v; want preserved", got, err)
	}
}

func TestRegenerateFromSourceSkipsZodWhenOutputUnset(t *testing.T) {
	logs := captureLog(t)
	dir := t.TempDir()
	typesOut := filepath.Join(dir, "router.ts")

	regenerateFromSource(watchOpts{
		dir:      watchAnalysisFixtureDir(t, "enhanced"),
		patterns: []string{"."},
		output:   typesOut,
	})

	if data, err := os.ReadFile(typesOut); err != nil || !strings.Contains(string(data), "$Mutation<CreateUserInput, User>") {
		t.Fatalf("types output = %q, %v; want user.create mutation", data, err)
	}
	if strings.Contains(logs.String(), "zod") {
		t.Fatalf("zod generation should be skipped when zod output is unset, logs:\n%s", logs.String())
	}
}

func TestRegenerateFromSourceRemovesStaleZodWhenNoTypedInputs(t *testing.T) {
	logs := captureLog(t)
	moduleDir := writeWatchAnalysisModule(t, `package watched

import (
	"context"

	"github.com/befabri/trpcgo"
)

func Setup() *trpcgo.Router {
	r := trpcgo.NewRouter()
	trpcgo.VoidQuery(r, "health", func(context.Context) (string, error) { return "ok", nil })
	return r
}
`)
	outDir := t.TempDir()
	typesOut := filepath.Join(outDir, "router.ts")
	zodOut := filepath.Join(outDir, "schemas.ts")
	if err := os.WriteFile(zodOut, []byte("stale zod"), 0o644); err != nil {
		t.Fatal(err)
	}

	regenerateFromSource(watchOpts{
		dir:       moduleDir,
		patterns:  []string{"."},
		output:    typesOut,
		zodOutput: zodOut,
		zodStyle:  typemap.ZodStandard,
	})

	if data, err := os.ReadFile(typesOut); err != nil || !strings.Contains(string(data), "$Query<void, string>") {
		t.Fatalf("types output = %q, %v; want health query", data, err)
	}
	if _, err := os.Stat(zodOut); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("zod file stat error = %v, want os.ErrNotExist", err)
	}
	if !strings.Contains(logs.String(), "removed "+zodOut+" (no typed inputs)") {
		t.Fatalf("stale zod removal was not logged, logs:\n%s", logs.String())
	}
}
