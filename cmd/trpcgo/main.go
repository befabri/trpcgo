package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/trpcgo/trpcgo/internal/analysis"
	"github.com/trpcgo/trpcgo/internal/codegen"
	"github.com/trpcgo/trpcgo/internal/typemap"
)

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
	fs.Parse(os.Args[2:])

	patterns := fs.Args()
	if len(patterns) == 0 {
		patterns = []string{"."}
	}

	// Run once.
	if err := generate(patterns, *dir, *output); err != nil {
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
	defer watcher.Close()

	if err := watcher.Add(absDir); err != nil {
		fmt.Fprintf(os.Stderr, "Error watching %s: %v\n", absDir, err)
		os.Exit(1)
	}

	log.Printf("Watching %s for changes...", absDir)

	// Debounce: regenerate at most once per 200ms.
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
			debounce = time.After(200 * time.Millisecond)

		case <-debounce:
			debounce = nil
			log.Println("Change detected, regenerating...")
			if err := generate(patterns, *dir, *output); err != nil {
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

func generate(patterns []string, dir, output string) error {
	procedures, err := analysis.Analyze(patterns, dir)
	if err != nil {
		return fmt.Errorf("analysis: %w", err)
	}

	if len(procedures) == 0 {
		fmt.Fprintln(os.Stderr, "Warning: no tRPC procedure registrations found")
	}

	mapper := typemap.NewMapper()

	var w *os.File
	if output != "" {
		w, err = os.Create(output)
		if err != nil {
			return fmt.Errorf("creating output file: %w", err)
		}
		defer w.Close()
	} else {
		w = os.Stdout
	}

	return codegen.Generate(w, procedures, mapper)
}
