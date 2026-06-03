package main

import (
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

type outputFile interface {
	io.Writer
	Close() error
}

var createOutputFile = func(path string) (outputFile, error) {
	return os.Create(path)
}

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

func generate(opts generateOptions) (err error) {
	if opts.stdout == nil {
		opts.stdout = os.Stdout
	}
	if opts.stderr == nil {
		opts.stderr = os.Stderr
	}

	result, err := analysis.Analyze(opts.patterns, opts.dir)
	if err != nil {
		return fmt.Errorf("analysis: %w", err)
	}

	if len(result.Procedures) == 0 {
		fmt.Fprintln(opts.stderr, "Warning: no tRPC procedure registrations found")
	}

	var w io.Writer
	if opts.output != "" {
		f, openErr := createOutputFile(opts.output)
		if openErr != nil {
			return fmt.Errorf("creating output file: %w", openErr)
		}
		w = f
		defer func() {
			if cerr := f.Close(); cerr != nil && err == nil {
				err = fmt.Errorf("closing output file: %w", cerr)
			}
		}()
	} else {
		w = opts.stdout
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

		zodFile, openErr := createOutputFile(opts.zod)
		if openErr != nil {
			return fmt.Errorf("creating zod output file: %w", openErr)
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

	if opts.enums != "" && genResult != nil {
		enumsFile, openErr := createOutputFile(opts.enums)
		if openErr != nil {
			return fmt.Errorf("creating enums output file: %w", openErr)
		}
		defer func() {
			if cerr := enumsFile.Close(); cerr != nil && err == nil {
				err = fmt.Errorf("closing enums output file: %w", cerr)
			}
		}()

		if err := codegen.WriteEnums(enumsFile, genResult.Defs); err != nil {
			return fmt.Errorf("writing enum values: %w", err)
		}
	}

	return nil
}
