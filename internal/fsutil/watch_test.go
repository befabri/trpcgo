package fsutil

import (
	"os"
	"path/filepath"
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
	defer watcher.Close()

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
	defer watcher.Close()

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
	found := false
	for _, p := range watcher.WatchList() {
		if p == newDir {
			found = true
			break
		}
	}
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
	defer watcher.Close()

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
