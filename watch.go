package trpcgo

import (
	"bytes"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/befabri/trpcgo/internal/analysis"
	"github.com/befabri/trpcgo/internal/codegen"
	"github.com/befabri/trpcgo/internal/fsutil"
	"github.com/befabri/trpcgo/internal/typemap"
	"github.com/fsnotify/fsnotify"
)

// watchOpts holds resolved paths for the watcher goroutine.
type watchOpts struct {
	dir       string
	patterns  []string
	output    string
	zodOutput string
	zodStyle  typemap.ZodStyle
}

// startWatcher watches .go files in the current working directory (recursively)
// and regenerates TypeScript types and Zod schemas when changes are detected.
// It uses static analysis (go/packages) to read source files directly, so
// changes are picked up without a server restart.
//
// If the Go code is broken (syntax errors, type errors), the previous
// generated files are preserved.
func (r *Router) startWatcher() {
	output := r.opts.typeOutput
	if output == "" {
		return
	}

	cwd, err := os.Getwd()
	if err != nil {
		log.Printf("trpcgo: watcher: failed to get working directory: %v", err)
		return
	}

	patterns := []string{"."}
	if len(r.opts.watchPackages) > 0 {
		patterns = append([]string(nil), r.opts.watchPackages...)
	}

	output = absPath(output)
	zodOutput := absPath(r.opts.zodOutput)

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Printf("trpcgo: watcher: failed to create: %v", err)
		return
	}

	usingPackageScope := false
	var patternRoots []string
	if len(r.opts.watchPackages) > 0 {
		patternRoots = fsutil.PatternRoots(r.opts.watchPackages, cwd)
		if err := fsutil.WatchScopedRecursive(watcher, patternRoots, cwd); err != nil {
			log.Printf("trpcgo: watcher: failed to watch package-scoped dirs, falling back to full watch: %v", err)
		} else {
			usingPackageScope = true
			log.Printf("trpcgo: watching package-scoped directories under %s (patterns: %s)", cwd, strings.Join(r.opts.watchPackages, ", "))
		}
	}

	if !usingPackageScope {
		// Reset patterns so regeneration uses "." rather than the package
		// patterns that triggered the fallback.
		patterns = []string{"."}
		if err := fsutil.WatchRecursive(watcher, cwd); err != nil {
			log.Printf("trpcgo: watcher: failed to watch %s: %v", cwd, err)
			_ = watcher.Close()
			return
		}
		log.Printf("trpcgo: watching Go directories under %s", cwd)
	}

	// In package-scope mode, restrict newly created dirs to the pattern roots
	// so ancestor create events don't pull in unrelated trees (e.g. frontend).
	handleDirCreate := fsutil.WatchRecursive
	if usingPackageScope {
		handleDirCreate = fsutil.WatchGoInScope(patternRoots)
	}

	zodStyle := typemap.ZodStandard
	if r.opts.zodMini {
		zodStyle = typemap.ZodMini
	}

	opts := watchOpts{
		dir:       cwd,
		patterns:  patterns,
		output:    output,
		zodOutput: zodOutput,
		zodStyle:  zodStyle,
	}

	done := r.done
	go func() {
		defer func() { _ = watcher.Close() }()

		// Run static analysis immediately to enrich reflect-generated types.
		regenerateFromSource(opts)

		var debounce <-chan time.Time
		for {
			select {
			case <-done:
				return

			case event, ok := <-watcher.Events:
				if !ok {
					return
				}
				// Handle directory creation/removal for recursive watching.
				fsutil.HandleDirEventWith(watcher, event, handleDirCreate)

				if filepath.Ext(event.Name) != ".go" {
					continue
				}
				if event.Op&(fsnotify.Write|fsnotify.Create) == 0 {
					continue
				}
				debounce = time.After(fsutil.DebounceInterval)

			case <-debounce:
				debounce = nil
				regenerateFromSource(opts)

			case _, ok := <-watcher.Errors:
				if !ok {
					return
				}
			}
		}
	}()
}

// absPath resolves a path to absolute. Returns "" for empty input.
func absPath(p string) string {
	if p == "" {
		return ""
	}
	if filepath.IsAbs(p) {
		return p
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return p
	}
	return abs
}

// regenerateFromSource uses static analysis to read Go source files and
// regenerate TypeScript types (and optionally Zod schemas). If the source
// has errors, the previous files are preserved.
func regenerateFromSource(opts watchOpts) {
	result, err := analysis.Analyze(opts.patterns, opts.dir)
	if err != nil {
		// Source is broken — keep previous types.
		log.Printf("trpcgo: source has errors, keeping previous types")
		return
	}

	if len(result.Procedures) == 0 {
		return
	}

	var buf bytes.Buffer
	genResult, err := codegen.Generate(&buf, result, result.TypeMetas)
	if err != nil {
		log.Printf("trpcgo: codegen failed: %v", err)
		return
	}

	writeIfChanged(opts.output, buf.Bytes(), "types")

	// Generate Zod schemas if configured.
	if opts.zodOutput != "" && genResult != nil {
		var zodBuf bytes.Buffer
		if err := codegen.WriteZodSchemas(&zodBuf, genResult.Procs, genResult.Defs, opts.zodStyle); err != nil {
			log.Printf("trpcgo: zod codegen failed: %v", err)
			return
		}
		if zodBuf.Len() == 0 {
			// No typed inputs — remove stale file if it exists.
			if err := os.Remove(opts.zodOutput); err == nil {
				log.Printf("trpcgo: removed %s (no typed inputs)", opts.zodOutput)
			}
		} else {
			writeIfChanged(opts.zodOutput, zodBuf.Bytes(), "zod schemas")
		}
	}
}

// writeIfChanged writes data to path only if it differs from the existing
// file contents. This avoids unnecessary writes that would trigger Vite HMR.
func writeIfChanged(path string, data []byte, label string) {
	existing, _ := os.ReadFile(path)
	if bytes.Equal(existing, data) {
		return
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		log.Printf("trpcgo: failed to create output directory: %v", err)
		return
	}

	if err := os.WriteFile(path, data, 0o644); err != nil {
		log.Printf("trpcgo: failed to write %s: %v", label, err)
		return
	}

	log.Printf("trpcgo: %s regenerated → %s", label, path)
}
