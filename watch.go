package trpcgo

import (
	"bytes"
	"io"
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
	dir         string
	patterns    []string
	output      string
	zodOutput   string
	enumsOutput string
	zodStyle    typemap.ZodStyle
	zodOptions  codegen.ZodOptions
}

type watcherConfig struct {
	watcher         *fsnotify.Watcher
	opts            watchOpts
	done            <-chan struct{}
	handleDirCreate func(*fsnotify.Watcher, string) error
}

// startWatcher regenerates the TypeScript types and Zod schemas from source
// whenever a .go file under the working directory changes. Broken source
// keeps the previous output.
func (r *Router) startWatcher() {
	cfg, err := r.newWatcherConfig()
	if err != nil {
		return
	}
	r.runWatcher(cfg)
}

func (r *Router) newWatcherConfig() (watcherConfig, error) {
	if r.opts.typeOutput == "" {
		return watcherConfig{}, os.ErrInvalid
	}
	cwd, err := os.Getwd()
	if err != nil {
		log.Printf("trpcgo: watcher: failed to get working directory: %v", err)
		return watcherConfig{}, err
	}
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Printf("trpcgo: watcher: failed to create: %v", err)
		return watcherConfig{}, err
	}
	patterns, handleDirCreate, err := r.configureWatchScope(watcher, cwd)
	if err != nil {
		_ = watcher.Close()
		return watcherConfig{}, err
	}
	return watcherConfig{
		watcher:         watcher,
		done:            r.done,
		handleDirCreate: handleDirCreate,
		opts: watchOpts{
			dir:         cwd,
			patterns:    patterns,
			output:      absPath(r.opts.typeOutput),
			zodOutput:   absPath(r.opts.zodOutput),
			enumsOutput: absPath(r.opts.enumsOutput),
			zodStyle:    r.zodStyle(),
			zodOptions:  codegen.ZodOptions{AllowUnknownFields: !r.opts.strictInput, Validation: r.opts.zodValidation.Clone()},
		},
	}, nil
}

func (r *Router) configureWatchScope(watcher *fsnotify.Watcher, cwd string) ([]string, func(*fsnotify.Watcher, string) error, error) {
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

	patterns := []string{"."}
	if !usingPackageScope {
		if err := fsutil.WatchRecursive(watcher, cwd); err != nil {
			log.Printf("trpcgo: watcher: failed to watch %s: %v", cwd, err)
			return nil, nil, err
		}
		log.Printf("trpcgo: watching Go directories under %s", cwd)
	} else {
		patterns = append([]string(nil), r.opts.watchPackages...)
	}

	handleDirCreate := fsutil.WatchRecursive
	if usingPackageScope {
		handleDirCreate = fsutil.WatchGoInScope(patternRoots)
	}
	return patterns, handleDirCreate, nil
}

func (r *Router) zodStyle() typemap.ZodStyle {
	if r.opts.zodMini {
		return typemap.ZodMini
	}
	return typemap.ZodStandard
}

func (r *Router) runWatcher(cfg watcherConfig) {
	go func() {
		r.runWatcherLoop(cfg, time.After, regenerateFromSource)
	}()
}

func (r *Router) runWatcherLoop(cfg watcherConfig, after func(time.Duration) <-chan time.Time, regenerate func(watchOpts)) {
	defer func() { _ = cfg.watcher.Close() }()
	if after == nil {
		after = time.After
	}
	if regenerate == nil {
		regenerate = regenerateFromSource
	}

	// The first pass replaces the reflect-generated types with the richer
	// static-analysis output.
	regenerate(cfg.opts)

	var debounce <-chan time.Time
	for {
		select {
		case <-cfg.done:
			return

		case event, ok := <-cfg.watcher.Events:
			if !ok {
				return
			}
			fsutil.HandleDirEventWith(cfg.watcher, event, cfg.handleDirCreate)

			if !fsutil.IsGoWriteOrCreate(event) {
				continue
			}
			debounce = after(fsutil.DebounceInterval)

		case <-debounce:
			debounce = nil
			regenerate(cfg.opts)

		case _, ok := <-cfg.watcher.Errors:
			if !ok {
				return
			}
		}
	}
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

// regenerateFromSource rewrites the generated files from static analysis,
// keeping the previous files when the source has errors.
func regenerateFromSource(opts watchOpts) {
	program, err := typemap.CompileValidation(opts.zodOptions.Validation)
	if err != nil {
		log.Printf("trpcgo: invalid Zod validation configuration: %v", err)
		return
	}
	result, err := analysis.Analyze(opts.patterns, opts.dir)
	if err != nil {
		log.Printf("trpcgo: source has errors, keeping previous types")
		return
	}

	if len(result.Procedures) == 0 {
		return
	}

	var buf bytes.Buffer
	genResult, err := codegen.Generate(&buf, result, result.TypeMetas, program)
	if err != nil {
		log.Printf("trpcgo: codegen failed: %v", err)
		return
	}

	var zodBuf, enumsBuf bytes.Buffer

	if opts.zodOutput != "" && genResult != nil {
		if err := codegen.WriteZodSchemas(&zodBuf, genResult.Procs, genResult.Defs, opts.zodStyle, opts.zodOptions); err != nil {
			log.Printf("trpcgo: zod codegen failed: %v", err)
			return
		}
	}

	if opts.enumsOutput != "" && genResult != nil {
		if err := codegen.WriteEnums(&enumsBuf, genResult.Defs); err != nil {
			log.Printf("trpcgo: enums codegen failed: %v", err)
			return
		}
	}
	// Complete every render before replacing files, so invalid configuration
	// or validation metadata keeps all previous generated outputs together.
	writeIfChanged(opts.output, buf.Bytes(), "types")
	if opts.zodOutput != "" {
		if zodBuf.Len() == 0 {
			if err := os.Remove(opts.zodOutput); err == nil {
				log.Printf("trpcgo: removed %s (no typed inputs)", opts.zodOutput)
			}
		} else {
			writeIfChanged(opts.zodOutput, zodBuf.Bytes(), "zod schemas")
		}
	}
	if opts.enumsOutput != "" {
		writeIfChanged(opts.enumsOutput, enumsBuf.Bytes(), "enum values")
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

	if err := fsutil.AtomicWriteFile(path, 0o644, func(w io.Writer) error {
		_, err := w.Write(data)
		return err
	}); err != nil {
		log.Printf("trpcgo: failed to write %s: %v", label, err)
		return
	}

	log.Printf("trpcgo: %s regenerated → %s", label, path)
}
