// Package fsutil provides filesystem utilities for the trpcgo watcher.
package fsutil

import (
	"io/fs"
	"os"
	"path/filepath"
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
	"vendor":       true,
	"node_modules": true,
	"testdata":     true,
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
		if skipDirs[d.Name()] {
			return filepath.SkipDir
		}
		return watcher.Add(path)
	})
}

// HandleDirEvent processes a watcher event that may involve directory
// creation or removal. On Create it adds the new directory (and any
// subdirectories) to the watcher. On Remove/Rename it is a no-op
// because fsnotify automatically removes watches for deleted paths.
func HandleDirEvent(watcher *fsnotify.Watcher, event fsnotify.Event) {
	if event.Op&fsnotify.Create != 0 && isDir(event.Name) {
		// New directory — watch it and any subdirectories.
		_ = WatchRecursive(watcher, event.Name)
	}
}

func isDir(path string) bool {
	fi, err := os.Stat(path)
	if err != nil {
		return false
	}
	return fi.IsDir()
}
