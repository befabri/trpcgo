package fsutil

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/fsnotify/fsnotify"
)

func TestWatchRecursive(t *testing.T) {
	root := t.TempDir()

	// Create directory tree:
	//   root/
	//     a/
	//       b/
	//     .git/       (skipped)
	//     vendor/     (skipped)
	//     node_modules/ (skipped)
	//     .claude/    (skipped)
	//     c/
	dirs := []string{
		"a", "a/b", ".git", "vendor", "node_modules", ".claude", "c",
	}
	for _, d := range dirs {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = watcher.Close() }()

	if err := WatchRecursive(watcher, root); err != nil {
		t.Fatal(err)
	}

	watched := map[string]bool{}
	for _, p := range watcher.WatchList() {
		rel, _ := filepath.Rel(root, p)
		watched[rel] = true
	}

	// Should be watched.
	for _, want := range []string{".", "a", filepath.Join("a", "b"), "c"} {
		if !watched[want] {
			t.Errorf("expected %q to be watched, but it wasn't", want)
		}
	}

	// Should NOT be watched.
	for _, skip := range []string{".git", "vendor", "node_modules", ".claude"} {
		if watched[skip] {
			t.Errorf("expected %q to be skipped, but it was watched", skip)
		}
	}
}

func TestHandleDirEvent_Create(t *testing.T) {
	root := t.TempDir()

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = watcher.Close() }()

	if err := watcher.Add(root); err != nil {
		t.Fatal(err)
	}

	// Create a new subdirectory.
	newDir := filepath.Join(root, "newpkg")
	if err := os.Mkdir(newDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Simulate the Create event.
	HandleDirEvent(watcher, fsnotify.Event{
		Name: newDir,
		Op:   fsnotify.Create,
	})

	// newDir should now be watched.
	found := slices.Contains(watcher.WatchList(), newDir)
	if !found {
		t.Errorf("expected %q to be added to watcher after HandleDirEvent", newDir)
	}
}

func TestHandleDirEvent_FileIgnored(t *testing.T) {
	root := t.TempDir()

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = watcher.Close() }()

	if err := watcher.Add(root); err != nil {
		t.Fatal(err)
	}

	// Create a regular file.
	filePath := filepath.Join(root, "main.go")
	if err := os.WriteFile(filePath, []byte("package main"), 0o644); err != nil {
		t.Fatal(err)
	}

	before := len(watcher.WatchList())

	HandleDirEvent(watcher, fsnotify.Event{
		Name: filePath,
		Op:   fsnotify.Create,
	})

	after := len(watcher.WatchList())
	if after != before {
		t.Errorf("file Create event should not add watches, got %d -> %d", before, after)
	}
}

func TestWatchRecursiveIncludesEmptyDirs(t *testing.T) {
	// Regression: pre-existing directories with no .go files must be watched
	// so that creating the first .go file there fires a watcher event.
	root := t.TempDir()

	// A dir that exists but has no .go files.
	emptyDir := filepath.Join(root, "internal", "newpkg")
	if err := os.MkdirAll(emptyDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// An unrelated dir with a .go file, so WatchGoRecursive would watch root.
	goDir := filepath.Join(root, "cmd")
	if err := os.MkdirAll(goDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(goDir, "main.go"), []byte("package main"), 0o644); err != nil {
		t.Fatal(err)
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = watcher.Close() }()

	if err := WatchRecursive(watcher, root); err != nil {
		t.Fatal(err)
	}

	watched := map[string]bool{}
	for _, p := range watcher.WatchList() {
		watched[p] = true
	}

	if !watched[emptyDir] {
		t.Errorf("pre-existing empty dir %q is not watched; first .go file creation would be missed", emptyDir)
	}
}

func TestWatchGoRecursive(t *testing.T) {
	root := t.TempDir()

	mustMkdir := func(rel string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Join(root, rel), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	mustWrite := func(rel string) {
		t.Helper()
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("package p"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	mustMkdir("frontend/src")
	mustMkdir("pkg/a")
	mustMkdir("tools/gen")
	mustMkdir("node_modules/lib")

	mustWrite("pkg/a/a.go")
	mustWrite("tools/gen/main.go")
	mustWrite("node_modules/lib/ignored.go")

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = watcher.Close() }()

	if err := WatchGoRecursive(watcher, root); err != nil {
		t.Fatal(err)
	}

	got := map[string]bool{}
	for _, p := range watcher.WatchList() {
		rel, err := filepath.Rel(root, p)
		if err != nil {
			t.Fatal(err)
		}
		got[rel] = true
	}

	for _, want := range []string{".", "pkg", filepath.Join("pkg", "a"), "tools", filepath.Join("tools", "gen")} {
		if !got[want] {
			t.Errorf("expected %q to be watched", want)
		}
	}

	for _, notWant := range []string{"frontend", filepath.Join("frontend", "src"), "node_modules", filepath.Join("node_modules", "lib")} {
		if got[notWant] {
			t.Errorf("expected %q not to be watched", notWant)
		}
	}
}
