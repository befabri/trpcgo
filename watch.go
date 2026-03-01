package trpcgo

import (
	"bytes"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/trpcgo/trpcgo/internal/analysis"
	"github.com/trpcgo/trpcgo/internal/codegen"
)

// startWatcher watches .go files in the current working directory and
// regenerates TypeScript types when changes are detected. It uses static
// analysis (go/packages) to read source files directly, so changes are
// picked up without a server restart.
//
// If the Go code is broken (syntax errors, type errors), the previous
// TypeScript file is preserved.
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

	if !filepath.IsAbs(output) {
		abs, err := filepath.Abs(output)
		if err != nil {
			log.Printf("trpcgo: watcher: failed to resolve output path: %v", err)
			return
		}
		output = abs
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Printf("trpcgo: watcher: failed to create: %v", err)
		return
	}

	if err := watcher.Add(cwd); err != nil {
		log.Printf("trpcgo: watcher: failed to watch %s: %v", cwd, err)
		watcher.Close()
		return
	}

	log.Printf("trpcgo: watching %s for Go file changes", cwd)

	go func() {
		defer watcher.Close()

		// Run static analysis immediately to enrich reflect-generated types.
		regenerateFromSource(cwd, output)

		var debounce <-chan time.Time
		for {
			select {
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}
				if filepath.Ext(event.Name) != ".go" {
					continue
				}
				if event.Op&(fsnotify.Write|fsnotify.Create) == 0 {
					continue
				}
				debounce = time.After(300 * time.Millisecond)

			case <-debounce:
				debounce = nil
				regenerateFromSource(cwd, output)

			case _, ok := <-watcher.Errors:
				if !ok {
					return
				}
			}
		}
	}()
}

// regenerateFromSource uses static analysis to read Go source files and
// regenerate TypeScript types. If the source has errors, the previous
// TypeScript file is preserved.
func regenerateFromSource(dir, output string) {
	result, err := analysis.Analyze([]string{"."}, dir)
	if err != nil {
		// Source is broken — keep previous types.
		log.Printf("trpcgo: source has errors, keeping previous types")
		return
	}

	if len(result.Procedures) == 0 {
		return
	}

	var buf bytes.Buffer
	if err := codegen.Generate(&buf, result, result.TypeMetas); err != nil {
		log.Printf("trpcgo: codegen failed: %v", err)
		return
	}

	generated := buf.Bytes()

	// Avoid unnecessary writes (which would trigger Vite HMR for nothing).
	existing, _ := os.ReadFile(output)
	if bytes.Equal(existing, generated) {
		return
	}

	if err := os.MkdirAll(filepath.Dir(output), 0o755); err != nil {
		log.Printf("trpcgo: failed to create output directory: %v", err)
		return
	}

	if err := os.WriteFile(output, generated, 0o644); err != nil {
		log.Printf("trpcgo: failed to write types: %v", err)
		return
	}

	log.Printf("trpcgo: types regenerated → %s", output)
}
