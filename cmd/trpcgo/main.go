package main

import (
	"cmp"
	"errors"
	"flag"
	"fmt"
	"io"
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
	enums    string
	stdout   io.Writer
	stderr   io.Writer
}

var writeOutputFile = fsutil.AtomicWriteFile

var (
	errUsage = errors.New("usage")
	errFlag  = errors.New("flag parse")
)

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		if errors.Is(err, errFlag) {
			os.Exit(2)
		}
		if !errors.Is(err, errUsage) {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		}
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	if len(args) < 1 || args[0] != "generate" {
		fmt.Fprintln(stderr, "Usage: trpcgo generate [flags] [packages]")
		return errUsage
	}
	return runGenerate(args[1:], stdout, stderr)
}

func runGenerate(args []string, stdout, stderr io.Writer) error {
	if stdout == nil {
		stdout = os.Stdout
	}
	if stderr == nil {
		stderr = os.Stderr
	}

	fs := flag.NewFlagSet("generate", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		fmt.Fprintln(stderr, "Usage: trpcgo generate [flags] [packages]")
		fs.PrintDefaults()
	}
	output := fs.String("o", "", "output file path (default: stdout)")
	fs.StringVar(output, "output", "", "output file path (default: stdout)")
	dir := fs.String("dir", ".", "working directory for package resolution")
	watch := fs.Bool("watch", false, "watch Go files and regenerate on change")
	fs.BoolVar(watch, "w", false, "watch Go files and regenerate on change")
	zodOutput := fs.String("zod", "", "output path for Zod 4 validation schemas")
	zodMini := fs.Bool("zod-mini", false, "generate zod/mini functional syntax")
	enumsOutput := fs.String("enums", "", "output path for runtime enum value objects")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return errFlag
	}

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
		enums:    *enumsOutput,
		stdout:   stdout,
		stderr:   stderr,
	}

	// Run once.
	if err := generate(opts); err != nil {
		return err
	}

	if !*watch {
		return nil
	}
	return watchGenerate(opts, *dir)
}

func watchGenerate(opts generateOptions, dir string) error {
	// Watch mode.
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return fmt.Errorf("resolving directory: %w", err)
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("creating watcher: %w", err)
	}
	defer func() { _ = watcher.Close() }()

	if err := fsutil.WatchRecursive(watcher, absDir); err != nil {
		return fmt.Errorf("watching %s: %w", absDir, err)
	}

	log.Printf("Watching directories under %s...", absDir)
	watchGenerateLoop(opts, watcher, nil, time.After, generate)
	return nil
}

func watchGenerateLoop(opts generateOptions, watcher *fsnotify.Watcher, done <-chan struct{}, after func(time.Duration) <-chan time.Time, generateFn func(generateOptions) error) {
	if after == nil {
		after = time.After
	}
	if generateFn == nil {
		generateFn = generate
	}

	// Debounce: regenerate at most once per 200ms.
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
			fsutil.HandleDirEventWith(watcher, event, fsutil.WatchRecursive)

			if !fsutil.IsGoWriteOrCreate(event) {
				continue
			}
			debounce = after(fsutil.DebounceInterval)

		case <-debounce:
			debounce = nil
			log.Println("Change detected, regenerating...")
			if err := generateFn(opts); err != nil {
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

func generate(opts generateOptions) error {
	opts.stdout = cmp.Or(opts.stdout, io.Writer(os.Stdout))
	opts.stderr = cmp.Or(opts.stderr, io.Writer(os.Stderr))

	result, err := analysis.Analyze(opts.patterns, opts.dir)
	if err != nil {
		return fmt.Errorf("analysis: %w", err)
	}

	if len(result.Procedures) == 0 {
		fmt.Fprintln(opts.stderr, "Warning: no tRPC procedure registrations found")
	}

	gen := codegen.Prepare(result, result.TypeMetas)
	writeTypes := func(w io.Writer) error {
		return codegen.WriteAppRouter(w, gen.Procs, gen.Defs)
	}
	if opts.output != "" {
		if err := writeOutputFile(opts.output, 0o644, writeTypes); err != nil {
			return fmt.Errorf("writing output file: %w", err)
		}
	} else {
		if err := writeTypes(opts.stdout); err != nil {
			return fmt.Errorf("writing TypeScript output: %w", err)
		}
	}

	// Generate Zod schemas if requested.
	if opts.zod != "" {
		style := typemap.ZodStandard
		if opts.zodMini {
			style = typemap.ZodMini
		}

		if err := writeOutputFile(opts.zod, 0o644, func(w io.Writer) error {
			return codegen.WriteZodSchemas(w, gen.Procs, gen.Defs, style)
		}); err != nil {
			return fmt.Errorf("writing zod schemas: %w", err)
		}
	}

	if opts.enums != "" {
		if err := writeOutputFile(opts.enums, 0o644, func(w io.Writer) error {
			return codegen.WriteEnums(w, gen.Defs)
		}); err != nil {
			return fmt.Errorf("writing enum values: %w", err)
		}
	}

	return nil
}
