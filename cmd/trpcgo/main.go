package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/befabri/trpcgo/internal/analysis"
	"github.com/befabri/trpcgo/internal/codegen"
	"github.com/befabri/trpcgo/internal/fsutil"
	"github.com/befabri/trpcgo/internal/typemap"
	"github.com/fsnotify/fsnotify"
)

type generateOptions struct {
	patterns []string
	dir      string
	output   string
	zod      string
	zodMini  bool
}

func main() {
	if len(os.Args) < 2 || os.Args[1] != "generate" {
		fmt.Fprintf(os.Stderr, "Usage: trpcgo generate [flags] [packages]\n")
		os.Exit(1)
	}

	fs := flag.NewFlagSet("generate", flag.ExitOnError)
	output := fs.String("o", "", "output file path (default: stdout)")
	fs.StringVar(output, "output", "", "output file path (default: stdout)")
	dir := fs.String("dir", ".", "working directory for package resolution")
	watch := fs.Bool("watch", false, "watch Go files and regenerate on change")
	fs.BoolVar(watch, "w", false, "watch Go files and regenerate on change")
	zodOutput := fs.String("zod", "", "output path for Zod 4 validation schemas")
	zodMini := fs.Bool("zod-mini", false, "generate zod/mini functional syntax")
	_ = fs.Parse(os.Args[2:]) // ExitOnError handles parse errors

	patterns := fs.Args()
	if len(patterns) == 0 {
		patterns = []string{"."}
	}

	opts := generateOptions{
		patterns: patterns,
		dir:      *dir,
		output:   *output,
		zod:      *zodOutput,
		zodMini:  *zodMini,
	}

	// Run once.
	if err := generate(opts); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	if !*watch {
		return
	}

	// Watch mode.
	absDir, err := filepath.Abs(*dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error resolving directory: %v\n", err)
		os.Exit(1)
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating watcher: %v\n", err)
		os.Exit(1)
	}
	defer func() { _ = watcher.Close() }()

	if err := fsutil.WatchRecursive(watcher, absDir); err != nil {
		fmt.Fprintf(os.Stderr, "Error watching %s: %v\n", absDir, err)
		os.Exit(1)
	}

	log.Printf("Watching directories under %s...", absDir)

	// Debounce: regenerate at most once per 200ms.
	var debounce <-chan time.Time
	for {
		select {
		case event, ok := <-watcher.Events:
			if !ok {
				return
			}
			// Handle directory creation/removal for recursive watching.
			fsutil.HandleDirEventWith(watcher, event, fsutil.WatchRecursive)

			if filepath.Ext(event.Name) != ".go" {
				continue
			}
			if event.Op&(fsnotify.Write|fsnotify.Create) == 0 {
				continue
			}
			debounce = time.After(fsutil.DebounceInterval)

		case <-debounce:
			debounce = nil
			log.Println("Change detected, regenerating...")
			if err := generate(opts); err != nil {
				log.Printf("Error: %v", err)
			} else {
				log.Println("Done.")
			}

		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			log.Printf("Watcher error: %v", err)
		}
	}
}

func generate(opts generateOptions) (err error) {
	result, err := analysis.Analyze(opts.patterns, opts.dir)
	if err != nil {
		return fmt.Errorf("analysis: %w", err)
	}

	if len(result.Procedures) == 0 {
		fmt.Fprintln(os.Stderr, "Warning: no tRPC procedure registrations found")
	}

	var w *os.File
	if opts.output != "" {
		w, err = os.Create(opts.output)
		if err != nil {
			return fmt.Errorf("creating output file: %w", err)
		}
		defer func() {
			if cerr := w.Close(); cerr != nil && err == nil {
				err = fmt.Errorf("closing output file: %w", cerr)
			}
		}()
	} else {
		w = os.Stdout
	}

	genResult, err := codegen.Generate(w, result, result.TypeMetas)
	if err != nil {
		return err
	}

	// Generate Zod schemas if requested.
	if opts.zod != "" && genResult != nil {
		style := typemap.ZodStandard
		if opts.zodMini {
			style = typemap.ZodMini
		}

		zodFile, err := os.Create(opts.zod)
		if err != nil {
			return fmt.Errorf("creating zod output file: %w", err)
		}
		defer func() {
			if cerr := zodFile.Close(); cerr != nil && err == nil {
				err = fmt.Errorf("closing zod output file: %w", cerr)
			}
		}()

		if err := codegen.WriteZodSchemas(zodFile, genResult.Procs, genResult.Defs, style); err != nil {
			return fmt.Errorf("writing zod schemas: %w", err)
		}
	}

	return nil
}
