// Package fsutil provides filesystem utilities for the trpcgo watcher.
package fsutil

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/fsnotify/fsnotify"
)

// DebounceInterval is the time to wait after a file change before
// triggering regeneration. Shared by both the runtime watcher and CLI.
const DebounceInterval = 300 * time.Millisecond

// skipDirs are directory names that should never be watched.
var skipDirs = map[string]bool{
	".git":         true,
	".claude":      true,
	".turbo":       true,
	".next":        true,
	".cache":       true,
	"vendor":       true,
	"node_modules": true,
	"testdata":     true,
	"dist":         true,
	"build":        true,
	"coverage":     true,
}

// WatchRecursive adds root and all its subdirectories to watcher,
// skipping directories in skipDirs. It does not start an event loop.
func WatchRecursive(watcher *fsnotify.Watcher, root string) error {
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // best-effort: skip unreadable dirs
		}
		if !d.IsDir() {
			return nil
		}
		if shouldSkipDir(d.Name()) {
			return filepath.SkipDir
		}
		return watcher.Add(path)
	})
}

// WatchGoRecursive adds only Go-relevant directories to watcher.
// It watches directories that contain .go files plus their ancestors
// up to root, reducing watch count in non-Go-heavy monorepos.
func WatchGoRecursive(watcher *fsnotify.Watcher, root string) error {
	root = filepath.Clean(root)

	dirsWithGo := map[string]bool{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // best-effort: skip unreadable paths
		}

		if d.IsDir() {
			if shouldSkipDir(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}

		if filepath.Ext(d.Name()) == ".go" {
			dirsWithGo[filepath.Dir(path)] = true
		}
		return nil
	})
	if err != nil {
		return err
	}

	watchSet := map[string]bool{root: true}
	for dir := range dirsWithGo {
		addAncestors(watchSet, dir, root)
	}

	paths := make([]string, 0, len(watchSet))
	for p := range watchSet {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, p := range paths {
		if err := watcher.Add(p); err != nil {
			return err
		}
	}
	return nil
}

// HandleDirEvent processes a watcher event that may involve directory
// creation or removal. On Create it adds the new directory (and any
// subdirectories) to the watcher. On Remove/Rename it is a no-op
// because fsnotify automatically removes watches for deleted paths.
func HandleDirEvent(watcher *fsnotify.Watcher, event fsnotify.Event) {
	HandleDirEventWith(watcher, event, WatchRecursive)
}

// HandleDirEventWith is like HandleDirEvent, but lets callers choose
// the recursive watch strategy.
func HandleDirEventWith(watcher *fsnotify.Watcher, event fsnotify.Event, watchFn func(*fsnotify.Watcher, string) error) {
	if event.Op&fsnotify.Create != 0 && isDir(event.Name) {
		// New directory — watch it and any subdirectories.
		_ = watchFn(watcher, event.Name)
	}
}

// addAncestors adds dir and every ancestor up to (and including) root
// into watchSet. dir must already be filepath.Clean'd.
func addAncestors(watchSet map[string]bool, dir, root string) {
	for cur := dir; ; {
		watchSet[cur] = true
		if cur == root {
			return
		}
		next := filepath.Dir(cur)
		if next == cur {
			return
		}
		cur = next
	}
}

func shouldSkipDir(name string) bool {
	return skipDirs[name]
}

func isDir(path string) bool {
	fi, err := os.Stat(path)
	if err != nil {
		return false
	}
	return fi.IsDir()
}
