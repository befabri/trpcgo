package fsutil

import (
	"os"
	"path/filepath"
	"slices"
	"sort"
	"testing"

	"github.com/fsnotify/fsnotify"
)

func TestPatternRoots(t *testing.T) {
	dir := "/project"

	cases := []struct {
		name     string
		patterns []string
		want     []string
	}{
		{"dot", []string{"."}, []string{"/project"}},
		{"dot-ellipsis", []string{"./..."}, []string{"/project"}},
		{"bare-ellipsis", []string{"..."}, []string{"/project"}},
		{"subpackage", []string{"./cmd/api"}, []string{filepath.Join("/project", "cmd", "api")}},
		{"recursive-subdir", []string{"./internal/..."}, []string{filepath.Join("/project", "internal")}},
		{"multiple", []string{"./cmd/api", "./internal/..."}, []string{
			filepath.Join("/project", "cmd", "api"),
			filepath.Join("/project", "internal"),
		}},
		{"dedup", []string{"./internal/...", "./internal/..."}, []string{
			filepath.Join("/project", "internal"),
		}},
		{"absolute", []string{"/abs/path"}, []string{"/abs/path"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := PatternRoots(tc.patterns, dir)
			sort.Strings(got)
			want := append([]string(nil), tc.want...)
			sort.Strings(want)
			if len(got) != len(want) {
				t.Fatalf("got %v, want %v", got, want)
			}
			for i := range want {
				if got[i] != want[i] {
					t.Errorf("[%d] got %q, want %q", i, got[i], want[i])
				}
			}
		})
	}
}

func TestResolvePackageDirs(t *testing.T) {
	root := t.TempDir()

	write := func(rel, content string) {
		t.Helper()
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	write("go.mod", "module example.com/watchtest\n\ngo 1.24\n")
	write("cmd/api/main.go", "package main\nfunc main() {}\n")
	write("internal/svc/svc.go", "package svc\n")
	write("frontend/src/app.ts", "export {}\n")

	dirs, err := ResolvePackageDirs([]string{"./cmd/api", "./internal/..."}, root)
	if err != nil {
		t.Fatalf("ResolvePackageDirs: %v", err)
	}

	wantA := filepath.Join(root, "cmd", "api")
	wantB := filepath.Join(root, "internal", "svc")
	if !slices.Contains(dirs, wantA) {
		t.Fatalf("dirs missing %q: %v", wantA, dirs)
	}
	if !slices.Contains(dirs, wantB) {
		t.Fatalf("dirs missing %q: %v", wantB, dirs)
	}
}

func TestWatchDirsAndAncestors(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "pkg", "a", "b"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "tools", "gen"), 0o755); err != nil {
		t.Fatal(err)
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = watcher.Close() }()

	dirs := []string{
		filepath.Join(root, "pkg", "a", "b"),
		filepath.Join(root, "tools", "gen"),
		filepath.Join(root, "..", "outside"), // should be ignored
	}
	if err := WatchDirsAndAncestors(watcher, root, dirs); err != nil {
		t.Fatalf("WatchDirsAndAncestors: %v", err)
	}

	got := map[string]bool{}
	for _, p := range watcher.WatchList() {
		rel, err := filepath.Rel(root, p)
		if err != nil {
			t.Fatal(err)
		}
		got[rel] = true
	}

	for _, want := range []string{".", "pkg", filepath.Join("pkg", "a"), filepath.Join("pkg", "a", "b"), "tools", filepath.Join("tools", "gen")} {
		if !got[want] {
			t.Errorf("expected %q to be watched", want)
		}
	}

	if got[filepath.Join("..", "outside")] {
		t.Errorf("outside root path should not be watched")
	}
}

func TestWatchScopedRecursiveMissingRoot(t *testing.T) {
	// Regression: a missing pattern root must not cause WatchScopedRecursive
	// to error out and degrade the whole watcher to a full-repo watch.
	// The existing root should still be watched; the missing one is skipped
	// but its nearest existing ancestor (cwd itself) is added so root
	// creation can be detected via a Create event.
	root := t.TempDir()
	existingRoot := filepath.Join(root, "cmd", "api")
	missingRoot := filepath.Join(root, "internal") // intentionally not created

	if err := os.MkdirAll(existingRoot, 0o755); err != nil {
		t.Fatal(err)
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = watcher.Close() }()

	if err := WatchScopedRecursive(watcher, []string{existingRoot, missingRoot}, root); err != nil {
		t.Fatalf("WatchScopedRecursive returned unexpected error: %v", err)
	}

	watched := map[string]bool{}
	for _, p := range watcher.WatchList() {
		watched[p] = true
	}

	if !watched[existingRoot] {
		t.Errorf("expected existing root %q to be watched", existingRoot)
	}
	if watched[missingRoot] {
		t.Errorf("non-existent root %q should not appear in watch list", missingRoot)
	}
	// cwd (root) must be watched so that creating missingRoot fires an event.
	if !watched[root] {
		t.Errorf("cwd %q should be watched as ancestor of missing root", root)
	}
}

func TestWatchScopedRecursiveAllMissing(t *testing.T) {
	// When ALL pattern roots are missing, WatchScopedRecursive must still add
	// the nearest existing ancestor (cwd) to the watch list so the watcher
	// does not sit idle with an empty watch set.
	root := t.TempDir()
	missingA := filepath.Join(root, "internal")
	missingB := filepath.Join(root, "cmd", "api")

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = watcher.Close() }()

	if err := WatchScopedRecursive(watcher, []string{missingA, missingB}, root); err != nil {
		t.Fatalf("WatchScopedRecursive returned unexpected error: %v", err)
	}

	if len(watcher.WatchList()) == 0 {
		t.Error("watch list is empty; watcher would sit idle and never detect root creation")
	}
	watched := map[string]bool{}
	for _, p := range watcher.WatchList() {
		watched[p] = true
	}
	if !watched[root] {
		t.Errorf("cwd %q must be watched as fallback ancestor", root)
	}
}

func TestWatchGoInScope(t *testing.T) {
	root := t.TempDir()
	pkgDir := filepath.Join(root, "pkg", "a")
	outsideDir := filepath.Join(root, "frontend", "src")

	for _, d := range []string{pkgDir, outsideDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	// Write a .go file so WatchGoRecursive actually adds the dir.
	if err := os.WriteFile(filepath.Join(pkgDir, "a.go"), []byte("package a"), 0o644); err != nil {
		t.Fatal(err)
	}

	watchFn := WatchGoInScope([]string{pkgDir})

	t.Run("in-scope dir is watched", func(t *testing.T) {
		watcher, err := fsnotify.NewWatcher()
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = watcher.Close() }()

		if err := watchFn(watcher, pkgDir); err != nil {
			t.Fatalf("WatchGoInScope returned error: %v", err)
		}

		watched := map[string]bool{}
		for _, p := range watcher.WatchList() {
			watched[p] = true
		}
		if !watched[pkgDir] {
			t.Errorf("expected %q to be watched", pkgDir)
		}
	})

	t.Run("out-of-scope dir is ignored", func(t *testing.T) {
		watcher, err := fsnotify.NewWatcher()
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = watcher.Close() }()

		if err := watchFn(watcher, outsideDir); err != nil {
			t.Fatalf("WatchGoInScope returned unexpected error: %v", err)
		}

		if len(watcher.WatchList()) != 0 {
			t.Errorf("expected no watches added for out-of-scope dir, got %v", watcher.WatchList())
		}
	})

	t.Run("ancestor of in-scope root gets single-dir watch", func(t *testing.T) {
		// When a newly created directory is an ancestor of a pattern root
		// (not yet within it), WatchGoInScope should add a single-dir watch
		// so intermediate dirs can be progressively tracked until the root exists.
		ancestorDir := filepath.Join(root, "pkg") // parent of pkgDir (root/pkg/a)
		if err := os.MkdirAll(ancestorDir, 0o755); err != nil {
			t.Fatal(err)
		}

		watcher, err := fsnotify.NewWatcher()
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = watcher.Close() }()

		if err := watchFn(watcher, ancestorDir); err != nil {
			t.Fatalf("WatchGoInScope returned error: %v", err)
		}

		watched := map[string]bool{}
		for _, p := range watcher.WatchList() {
			watched[p] = true
		}
		// Single-dir watch on the ancestor — not recursive.
		if !watched[ancestorDir] {
			t.Errorf("expected ancestor %q to receive a single-dir watch", ancestorDir)
		}
		// pkgDir itself must NOT be recursively watched (it is within scope
		// but that recursive step happens only when pkgDir is created).
		if watched[pkgDir] {
			t.Errorf("pkgDir %q should not be watched yet (only ancestor watch)", pkgDir)
		}
	})

	t.Run("subdir of in-scope root is watched", func(t *testing.T) {
		subDir := filepath.Join(pkgDir, "sub")
		if err := os.MkdirAll(subDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(subDir, "sub.go"), []byte("package a"), 0o644); err != nil {
			t.Fatal(err)
		}

		watcher, err := fsnotify.NewWatcher()
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = watcher.Close() }()

		if err := watchFn(watcher, subDir); err != nil {
			t.Fatalf("WatchGoInScope returned error: %v", err)
		}

		watched := map[string]bool{}
		for _, p := range watcher.WatchList() {
			watched[p] = true
		}
		if !watched[subDir] {
			t.Errorf("expected subdir %q to be watched", subDir)
		}
	})
}
