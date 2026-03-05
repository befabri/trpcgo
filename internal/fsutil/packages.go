package fsutil

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/fsnotify/fsnotify"
	"golang.org/x/tools/go/packages"
)

// ResolvePackageDirs resolves Go package patterns to unique directories.
// Patterns follow go/packages syntax (e.g. ".", "./cmd/api", "./internal/...").
func ResolvePackageDirs(patterns []string, dir string) ([]string, error) {
	if len(patterns) == 0 {
		return nil, nil
	}

	cfg := &packages.Config{
		Mode: packages.NeedFiles | packages.NeedCompiledGoFiles,
		Dir:  dir,
	}

	pkgs, err := packages.Load(cfg, patterns...)
	if err != nil {
		return nil, fmt.Errorf("loading packages: %w", err)
	}

	// Don't abort on package-level errors (type errors, parse errors, etc.) —
	// NeedFiles populates GoFiles even for broken packages, so we can still
	// determine which directories to watch. The caller has fallback logic for
	// an empty result.
	set := map[string]bool{}
	for _, pkg := range pkgs {
		for _, f := range pkg.GoFiles {
			set[filepath.Dir(f)] = true
		}
		for _, f := range pkg.CompiledGoFiles {
			set[filepath.Dir(f)] = true
		}
	}

	out := make([]string, 0, len(set))
	for d := range set {
		out = append(out, filepath.Clean(d))
	}
	sort.Strings(out)
	return out, nil
}

// WatchDirsAndAncestors adds watches for each directory in dirs and all of
// their ancestors up to root (inclusive). Directories outside root are ignored.
func WatchDirsAndAncestors(watcher WatchAdder, root string, dirs []string) error {
	root = filepath.Clean(root)
	watchSet := map[string]bool{root: true}

	for _, d := range dirs {
		if d == "" {
			continue
		}
		d = filepath.Clean(d)
		if !filepath.IsAbs(d) {
			d = filepath.Join(root, d)
			d = filepath.Clean(d)
		}
		if !isWithinRoot(root, d) {
			continue
		}

		addAncestors(watchSet, d, root)
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

type WatchAdder interface {
	Add(name string) error
}

// PatternRoots converts Go package patterns to the filesystem directories
// they root into. Recursive patterns ending in /... or standalone ... are
// reduced to their directory portion. Paths are resolved relative to dir.
// Duplicates are removed.
func PatternRoots(patterns []string, dir string) []string {
	dir = filepath.Clean(dir)
	seen := map[string]bool{}
	roots := make([]string, 0, len(patterns))
	for _, p := range patterns {
		p = strings.TrimSuffix(p, "/...")
		if p == "..." {
			p = "."
		}
		p = filepath.FromSlash(p)
		if !filepath.IsAbs(p) {
			p = filepath.Join(dir, p)
		}
		p = filepath.Clean(p)
		if !seen[p] {
			seen[p] = true
			roots = append(roots, p)
		}
	}
	return roots
}

// WatchScopedRecursive watches all non-skip directories under each of the
// given pattern roots. Roots that do not yet exist are silently skipped; their
// nearest existing ancestor up to cwd is watched instead so that the root's
// eventual creation fires a Create event that WatchGoInScope can act on.
// An error is returned only for genuine watch failures on existing directories.
func WatchScopedRecursive(watcher *fsnotify.Watcher, patternRoots []string, cwd string) error {
	cwd = filepath.Clean(cwd)
	for _, root := range patternRoots {
		if _, err := os.Stat(root); os.IsNotExist(err) {
			if anc := nearestExistingAncestor(root, cwd); anc != "" {
				_ = watcher.Add(anc) // best-effort: ensure creation is detectable
			}
			continue
		}
		if err := WatchRecursive(watcher, root); err != nil {
			return err
		}
	}
	return nil
}

// nearestExistingAncestor walks up from dir and returns the first parent
// directory that exists on disk, stopping at limit (inclusive). Returns ""
// if no existing ancestor is found within the limit.
func nearestExistingAncestor(dir, limit string) string {
	for {
		parent := filepath.Dir(dir)
		if parent == dir {
			return "" // filesystem root
		}
		if !isWithinRoot(limit, parent) {
			// Exceeded cwd — watch cwd itself as last resort.
			if _, err := os.Stat(limit); err == nil {
				return limit
			}
			return ""
		}
		if _, err := os.Stat(parent); err == nil {
			return parent
		}
		dir = parent
	}
}

// WatchGoInScope returns a watch function that adds a newly created directory
// to the watcher only when it is relevant to one of the given pattern roots:
//   - If the dir is within a root (or is the root itself), WatchRecursive is called.
//   - If the dir is a strict ancestor of a root, it is added as a single watch
//     so that subsequent intermediate directories can be detected progressively
//     until the pattern root itself is created.
//
// Directories outside all pattern roots are ignored, preventing the scoped
// watcher from drifting into unrelated trees (e.g. frontend build output).
func WatchGoInScope(patternRoots []string) func(*fsnotify.Watcher, string) error {
	return func(watcher *fsnotify.Watcher, dir string) error {
		for _, root := range patternRoots {
			if isWithinRoot(root, dir) {
				return WatchRecursive(watcher, dir)
			}
			if isWithinRoot(dir, root) {
				// dir is a strict ancestor of root — watch it so we detect
				// the next intermediate directory being created toward root.
				_ = watcher.Add(dir)
				return nil
			}
		}
		return nil // outside all pattern roots — ignore
	}
}

func isWithinRoot(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	if rel == "." {
		return true
	}
	return !strings.HasPrefix(rel, "..")
}
