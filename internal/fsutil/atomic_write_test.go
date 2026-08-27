package fsutil

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"unicode/utf8"
)

type injectedAtomicTemp struct {
	writeErr error
	syncErr  error
	closeErr error
	calls    []string
}

func (f *injectedAtomicTemp) Write(p []byte) (int, error) {
	f.calls = append(f.calls, "write")
	if f.writeErr != nil {
		return 0, f.writeErr
	}
	return len(p), nil
}

func (f *injectedAtomicTemp) Sync() error {
	f.calls = append(f.calls, "sync")
	return f.syncErr
}

func (f *injectedAtomicTemp) Close() error {
	f.calls = append(f.calls, "close")
	return f.closeErr
}

func TestAtomicWriteFileCreatesFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "generated.ts")
	if err := AtomicWriteFile(path, 0o600, func(w io.Writer) error {
		_, err := io.WriteString(w, "generated")
		return err
	}); err != nil {
		t.Fatalf("AtomicWriteFile: %v", err)
	}

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "generated" {
		t.Fatalf("content = %q, want %q", content, "generated")
	}
	// The contract is "same as os.WriteFile", so compare against a file
	// os.WriteFile created with the same perm. That keeps the assertion
	// meaningful under any umask and on Windows.
	reference := filepath.Join(filepath.Dir(path), "reference.ts")
	if err := os.WriteFile(reference, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if got, want := fileMode(t, path), fileMode(t, reference); got != want {
		t.Fatalf("permissions = %o, want %o (os.WriteFile with the same perm)", got, want)
	}
	requireNoAtomicTemps(t, path)
}

func TestAtomicWriteFileReplacesAndPreservesPermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "generated.ts")
	if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Chmod is not subject to the umask, so the existing mode is 0640 on Unix
	// regardless of environment; on Windows it maps to "writable".
	if err := os.Chmod(path, 0o640); err != nil {
		t.Fatal(err)
	}
	want := fileMode(t, path)

	// Pass a fallback perm that differs from the existing mode so a failure
	// to preserve it is observable.
	if err := AtomicWriteFile(path, 0o600, func(w io.Writer) error {
		_, err := io.WriteString(w, "new")
		return err
	}); err != nil {
		t.Fatalf("AtomicWriteFile: %v", err)
	}

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "new" {
		t.Fatalf("content = %q, want %q", content, "new")
	}
	if got := fileMode(t, path); got != want {
		t.Fatalf("permissions = %o, want preserved %o", got, want)
	}
	requireNoAtomicTemps(t, path)
}

func TestAtomicWriteFileFailurePreservesExistingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "generated.ts")
	if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	writeErr := errors.New("generation failed")

	err := AtomicWriteFile(path, 0o600, func(w io.Writer) error {
		if _, err := io.WriteString(w, "partial"); err != nil {
			return err
		}
		return writeErr
	})
	if !errors.Is(err, writeErr) {
		t.Fatalf("AtomicWriteFile error = %v, want wrapping %v", err, writeErr)
	}

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "old" {
		t.Fatalf("content = %q, want preserved %q", content, "old")
	}
	requireNoAtomicTemps(t, path)
}

func TestAtomicWriteFileFailureDoesNotCreateDestination(t *testing.T) {
	path := filepath.Join(t.TempDir(), "generated.ts")
	writeErr := errors.New("generation failed")

	err := AtomicWriteFile(path, 0o600, func(io.Writer) error {
		return writeErr
	})
	if !errors.Is(err, writeErr) {
		t.Fatalf("AtomicWriteFile error = %v, want wrapping %v", err, writeErr)
	}
	if _, err := os.Stat(path); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("destination stat error = %v, want not-exist", err)
	}
	requireNoAtomicTemps(t, path)
}

func TestAtomicWriteFileReplaceFailurePreservesExistingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "generated.ts")
	if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	replaceErr := errors.New("replace failed")

	err := atomicWriteFile(path, 0o600, func(w io.Writer) error {
		_, err := io.WriteString(w, "new")
		return err
	}, atomicWriteOps{
		replace:       func(string, string) error { return replaceErr },
		syncDirectory: syncDirectory,
	})
	if !errors.Is(err, replaceErr) {
		t.Fatalf("atomicWriteFile error = %v, want wrapping %v", err, replaceErr)
	}

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "old" {
		t.Fatalf("content = %q, want preserved %q", content, "old")
	}
	requireNoAtomicTemps(t, path)
}

func TestAtomicWriteFileDirectorySyncFailureRetainsReplacement(t *testing.T) {
	path := filepath.Join(t.TempDir(), "generated.ts")
	if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	syncErr := errors.New("directory sync failed")

	err := atomicWriteFile(path, 0o600, func(w io.Writer) error {
		_, err := io.WriteString(w, "new")
		return err
	}, atomicWriteOps{
		replace:       os.Rename,
		syncDirectory: func(string) error { return syncErr },
	})
	if !errors.Is(err, syncErr) {
		t.Fatalf("atomicWriteFile error = %v, want wrapping %v", err, syncErr)
	}
	// The error must not read as "nothing was written": the rename completed.
	if !strings.Contains(err.Error(), "replaced") {
		t.Fatalf("atomicWriteFile error = %q, want it to state the file was replaced", err)
	}

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "new" {
		t.Fatalf("content = %q, want completed replacement %q", content, "new")
	}
	requireNoAtomicTemps(t, path)
}

func TestAtomicWriteFileRejectsNonRegularDestination(t *testing.T) {
	path := filepath.Join(t.TempDir(), "generated.ts")
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}

	err := AtomicWriteFile(path, 0o600, func(io.Writer) error {
		t.Fatal("write callback should not run")
		return nil
	})
	if err == nil || !strings.Contains(err.Error(), "not a regular file") {
		t.Fatalf("AtomicWriteFile error = %v, want non-regular destination error", err)
	}
}

func TestAtomicWriteFileRejectsNilCallback(t *testing.T) {
	err := AtomicWriteFile(filepath.Join(t.TempDir(), "generated.ts"), 0o600, nil)
	if err == nil || !strings.Contains(err.Error(), "nil write callback") {
		t.Fatalf("AtomicWriteFile error = %v, want nil callback error", err)
	}
}

func TestAtomicWriteFileFollowsSymlinkChain(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target.ts")
	middle := filepath.Join(dir, "middle.ts")
	link := filepath.Join(dir, "generated.ts")
	if err := os.WriteFile(target, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Relative link targets must resolve against the link's own directory.
	if err := os.Symlink("target.ts", middle); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := os.Symlink(middle, link); err != nil {
		t.Fatal(err)
	}

	if err := AtomicWriteFile(link, 0o600, func(w io.Writer) error {
		_, err := io.WriteString(w, "new")
		return err
	}); err != nil {
		t.Fatalf("AtomicWriteFile: %v", err)
	}

	requireSymlink(t, link)
	requireSymlink(t, middle)
	content, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "new" {
		t.Fatalf("symlink target content = %q, want %q", content, "new")
	}
	requireNoAtomicTemps(t, target)
}

func TestAtomicWriteFileCreatesDanglingSymlinkTarget(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "gen", "target.ts")
	link := filepath.Join(dir, "generated.ts")
	if err := os.Mkdir(filepath.Dir(target), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	if err := AtomicWriteFile(link, 0o600, func(w io.Writer) error {
		_, err := io.WriteString(w, "created")
		return err
	}); err != nil {
		t.Fatalf("AtomicWriteFile: %v", err)
	}

	requireSymlink(t, link)
	content, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "created" {
		t.Fatalf("symlink target content = %q, want %q", content, "created")
	}
	requireNoAtomicTemps(t, target)
}

func TestAtomicWriteFileRejectsSymlinkToDirectory(t *testing.T) {
	dir := t.TempDir()
	link := filepath.Join(dir, "generated.ts")
	if err := os.Symlink(dir, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	err := AtomicWriteFile(link, 0o600, func(io.Writer) error {
		t.Fatal("write callback should not run")
		return nil
	})
	if err == nil || !strings.Contains(err.Error(), "not a regular file") {
		t.Fatalf("AtomicWriteFile error = %v, want non-regular destination error", err)
	}
}

func TestAtomicWriteFileSymlinkFollowLimitMatchesLinux(t *testing.T) {
	// Linux MAXSYMLINKS is 40: a chain of 40 links resolves, 41 is ELOOP.
	for _, tt := range []struct {
		links  int
		wantOK bool
	}{
		{links: maxSymlinkFollows, wantOK: true},
		{links: maxSymlinkFollows + 1, wantOK: false},
	} {
		t.Run(fmt.Sprintf("%d links", tt.links), func(t *testing.T) {
			dir := t.TempDir()
			target := filepath.Join(dir, "target.ts")
			if err := os.WriteFile(target, []byte("old"), 0o600); err != nil {
				t.Fatal(err)
			}
			next := target
			for i := range tt.links {
				link := filepath.Join(dir, fmt.Sprintf("link%02d.ts", i))
				if err := os.Symlink(next, link); err != nil {
					t.Skipf("symlinks unavailable: %v", err)
				}
				next = link
			}

			err := AtomicWriteFile(next, 0o600, func(w io.Writer) error {
				_, err := io.WriteString(w, "new")
				return err
			})
			if tt.wantOK {
				if err != nil {
					t.Fatalf("AtomicWriteFile: %v", err)
				}
				content, err := os.ReadFile(target)
				if err != nil {
					t.Fatal(err)
				}
				if string(content) != "new" {
					t.Fatalf("target content = %q, want %q", content, "new")
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), "too many levels of symbolic links") {
				t.Fatalf("AtomicWriteFile error = %v, want symlink limit error", err)
			}
			content, err := os.ReadFile(target)
			if err != nil {
				t.Fatal(err)
			}
			if string(content) != "old" {
				t.Fatalf("target content = %q, want untouched %q", content, "old")
			}
		})
	}
}

func TestAtomicWriteFileLongBasename(t *testing.T) {
	// 250 bytes is legal on filesystems with a 255-byte component limit; the
	// temp name must not push past it.
	base := strings.Repeat("n", 250)
	path := filepath.Join(t.TempDir(), base)
	if err := AtomicWriteFile(path, 0o600, func(w io.Writer) error {
		_, err := io.WriteString(w, "long")
		return err
	}); err != nil {
		t.Fatalf("AtomicWriteFile: %v", err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "long" {
		t.Fatalf("content = %q, want %q", content, "long")
	}
	requireNoAtomicTemps(t, path)
}

func TestTempFilePrefix(t *testing.T) {
	tests := []struct {
		name string
		base string
		want string
	}{
		{name: "short", base: "router.ts", want: ".router.ts.tmp-"},
		{name: "exact limit", base: strings.Repeat("a", maxTempBaseLen), want: "." + strings.Repeat("a", maxTempBaseLen) + ".tmp-"},
		{name: "truncated", base: strings.Repeat("a", maxTempBaseLen+10), want: "." + strings.Repeat("a", maxTempBaseLen) + ".tmp-"},
		{
			name: "truncates on rune boundary",
			// 63 ASCII bytes then a 3-byte rune: cutting at 64 would split it.
			base: strings.Repeat("a", maxTempBaseLen-1) + "\u65e5" + strings.Repeat("b", 10),
			want: "." + strings.Repeat("a", maxTempBaseLen-1) + ".tmp-",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tempFilePrefix(tt.base)
			if got != tt.want {
				t.Fatalf("tempFilePrefix(%q) = %q, want %q", tt.base, got, tt.want)
			}
			if !utf8.ValidString(got) {
				t.Fatalf("tempFilePrefix(%q) = %q is not valid UTF-8", tt.base, got)
			}
		})
	}
}

func TestAtomicWriteFileRejectsSymlinkLoop(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.ts")
	b := filepath.Join(dir, "b.ts")
	if err := os.Symlink(b, a); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := os.Symlink(a, b); err != nil {
		t.Fatal(err)
	}

	err := AtomicWriteFile(a, 0o600, func(io.Writer) error {
		t.Fatal("write callback should not run")
		return nil
	})
	if err == nil || !strings.Contains(err.Error(), "too many levels of symbolic links") {
		t.Fatalf("AtomicWriteFile error = %v, want symlink loop error", err)
	}
}

func TestAtomicWriteFilePayloadSizes(t *testing.T) {
	// Exercise both sides of the bufio boundary: nothing buffered at all, and
	// far more than one buffer so writes pass through mid-callback.
	sizes := []int{0, 1, 4096, 4097, 1 << 20}
	for _, size := range sizes {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "generated.ts")
			payload := bytes.Repeat([]byte("x"), size)
			if err := AtomicWriteFile(path, 0o600, func(w io.Writer) error {
				_, err := w.Write(payload)
				return err
			}); err != nil {
				t.Fatalf("AtomicWriteFile: %v", err)
			}
			content, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(content, payload) {
				t.Fatalf("content length = %d, want %d", len(content), len(payload))
			}
			requireNoAtomicTemps(t, path)
		})
	}
}

func TestAtomicWriteFileReadersNeverSeePartialContent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "generated.ts")
	// Large enough that a non-atomic write would be observable mid-way.
	a := bytes.Repeat([]byte("a"), 256<<10)
	b := bytes.Repeat([]byte("b"), 256<<10)
	if err := os.WriteFile(path, a, 0o600); err != nil {
		t.Fatal(err)
	}

	const rounds = 50
	stop := make(chan struct{})
	var wg sync.WaitGroup
	var bad atomic.Int32
	wg.Go(func() {
		for {
			select {
			case <-stop:
				return
			default:
			}
			content, err := os.ReadFile(path)
			if err != nil {
				// The rename window can race a reader on some platforms; only
				// content is under test here.
				continue
			}
			if !bytes.Equal(content, a) && !bytes.Equal(content, b) {
				bad.Add(1)
			}
		}
	})

	for i := range rounds {
		payload := a
		if i%2 == 0 {
			payload = b
		}
		if err := AtomicWriteFile(path, 0o600, func(w io.Writer) error {
			// Write in small chunks so a non-atomic implementation would
			// expose a torn file.
			for chunk := range slices.Chunk(payload, 4096) {
				if _, err := w.Write(chunk); err != nil {
					return err
				}
			}
			return nil
		}); err != nil {
			t.Fatalf("AtomicWriteFile round %d: %v", i, err)
		}
	}
	close(stop)
	wg.Wait()

	if n := bad.Load(); n != 0 {
		t.Fatalf("reader observed %d partially written file(s)", n)
	}
	requireNoAtomicTemps(t, path)
}

func TestAtomicWriteFileConcurrentWritersSamePath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "generated.ts")
	const writers = 16
	var wg sync.WaitGroup
	errs := make([]error, writers)
	for i := range writers {
		wg.Go(func() {
			errs[i] = AtomicWriteFile(path, 0o600, func(w io.Writer) error {
				_, err := fmt.Fprintf(w, "writer-%d", i)
				return err
			})
		})
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("writer %d: %v", i, err)
		}
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(content), "writer-") {
		t.Fatalf("content = %q, want one complete writer's output", content)
	}
	requireNoAtomicTemps(t, path)
}

func TestAtomicWriteFileCallbackPanicCleansUp(t *testing.T) {
	path := filepath.Join(t.TempDir(), "generated.ts")
	if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}

	func() {
		defer func() {
			if r := recover(); r != "boom" {
				t.Fatalf("recovered %v, want the callback's panic to propagate", r)
			}
		}()
		_ = AtomicWriteFile(path, 0o600, func(w io.Writer) error {
			_, _ = io.WriteString(w, "partial")
			panic("boom")
		})
	}()

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "old" {
		t.Fatalf("content = %q, want preserved %q", content, "old")
	}
	requireNoAtomicTemps(t, path)
}

func TestAtomicWriteFileUnresolvableDestinations(t *testing.T) {
	tests := []struct {
		name  string
		setup func(t *testing.T, dir string) string
	}{
		{
			name: "parent is a regular file",
			setup: func(t *testing.T, dir string) string {
				parent := filepath.Join(dir, "file")
				if err := os.WriteFile(parent, []byte("x"), 0o600); err != nil {
					t.Fatal(err)
				}
				return filepath.Join(parent, "generated.ts")
			},
		},
		{
			name: "parent directory missing",
			setup: func(t *testing.T, dir string) string {
				return filepath.Join(dir, "missing", "generated.ts")
			},
		},
		{
			name: "symlink into missing directory",
			setup: func(t *testing.T, dir string) string {
				link := filepath.Join(dir, "generated.ts")
				if err := os.Symlink(filepath.Join(dir, "missing", "target.ts"), link); err != nil {
					t.Skipf("symlinks unavailable: %v", err)
				}
				return link
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			path := tt.setup(t, dir)
			ran := false
			err := AtomicWriteFile(path, 0o600, func(io.Writer) error {
				ran = true
				return nil
			})
			if err == nil {
				t.Fatal("AtomicWriteFile succeeded, want error")
			}
			if ran {
				t.Fatal("write callback ran although the destination is unusable")
			}
			entries, readErr := os.ReadDir(dir)
			if readErr != nil {
				t.Fatal(readErr)
			}
			for _, entry := range entries {
				if strings.Contains(entry.Name(), ".tmp-") {
					t.Errorf("stray temporary file: %s", entry.Name())
				}
			}
		})
	}
}

func TestWriteAtomicTempPropagatesAndJoinsFailures(t *testing.T) {
	callbackErr := errors.New("callback failed")
	writeErr := errors.New("write failed")
	syncErr := errors.New("sync failed")
	closeErr := errors.New("close failed")

	tests := []struct {
		name      string
		file      injectedAtomicTemp
		write     func(io.Writer) error
		wantErrs  []error
		wantCalls string
	}{
		{
			name:      "success",
			write:     func(w io.Writer) error { _, err := io.WriteString(w, "data"); return err },
			wantCalls: "write,sync,close",
		},
		{
			name:      "callback and close",
			file:      injectedAtomicTemp{closeErr: closeErr},
			write:     func(io.Writer) error { return callbackErr },
			wantErrs:  []error{callbackErr, closeErr},
			wantCalls: "close",
		},
		{
			name:      "writer and close",
			file:      injectedAtomicTemp{writeErr: writeErr, closeErr: closeErr},
			write:     func(w io.Writer) error { _, err := io.WriteString(w, "data"); return err },
			wantErrs:  []error{writeErr, closeErr},
			wantCalls: "write,close",
		},
		{
			name:      "sync and close",
			file:      injectedAtomicTemp{syncErr: syncErr, closeErr: closeErr},
			write:     func(w io.Writer) error { _, err := io.WriteString(w, "data"); return err },
			wantErrs:  []error{syncErr, closeErr},
			wantCalls: "write,sync,close",
		},
		{
			name:      "close",
			file:      injectedAtomicTemp{closeErr: closeErr},
			write:     func(w io.Writer) error { _, err := io.WriteString(w, "data"); return err },
			wantErrs:  []error{closeErr},
			wantCalls: "write,sync,close",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			file := tt.file
			err := writeAtomicTemp(&file, "generated.ts", tt.write)
			if len(tt.wantErrs) == 0 && err != nil {
				t.Fatalf("writeAtomicTemp error = %v, want nil", err)
			}
			for _, wantErr := range tt.wantErrs {
				if !errors.Is(err, wantErr) {
					t.Errorf("writeAtomicTemp error = %v, want wrapping %v", err, wantErr)
				}
			}
			if got := strings.Join(file.calls, ","); got != tt.wantCalls {
				t.Errorf("calls = %q, want %q", got, tt.wantCalls)
			}
		})
	}
}

func fileMode(t *testing.T, path string) fs.FileMode {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info.Mode().Perm()
}

func requireSymlink(t *testing.T, path string) {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&fs.ModeSymlink == 0 {
		t.Fatalf("%s is no longer a symlink (mode %v)", path, info.Mode())
	}
}

func requireNoAtomicTemps(t *testing.T, destination string) {
	t.Helper()
	entries, err := os.ReadDir(filepath.Dir(destination))
	if err != nil {
		t.Fatal(err)
	}
	prefix := tempFilePrefix(filepath.Base(destination))
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), prefix) {
			t.Errorf("temporary file was not removed: %s", entry.Name())
		}
	}
}

func TestIsDirSyncUnsupported(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "nil", err: nil, want: false},
		{name: "einval", err: &fs.PathError{Op: "sync", Path: "dir", Err: syscall.EINVAL}, want: true},
		{name: "unsupported", err: fmt.Errorf("sync: %w", errors.ErrUnsupported), want: true},
		{name: "other", err: errors.New("disk on fire"), want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isDirSyncUnsupported(tt.err); got != tt.want {
				t.Fatalf("isDirSyncUnsupported(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}
