package fsutil

import (
	"bufio"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"unicode/utf8"
)

const (
	maxTempFileAttempts = 100
	// maxSymlinkFollows bounds how many symlinks resolveDestination follows,
	// matching the Linux MAXSYMLINKS limit: a chain of exactly 40 links is
	// accepted, the 41st fails.
	maxSymlinkFollows = 40
	// maxTempBaseLen caps how much of the destination basename is embedded in
	// the temporary filename, so the temp name stays under common 255-byte
	// component limits even for very long destination names.
	maxTempBaseLen = 64
)

type atomicTemp interface {
	io.Writer
	io.Closer
	Sync() error
}

type atomicWriteOps struct {
	replace       func(string, string) error
	syncDirectory func(string) error
}

// AtomicWriteFile writes a regular file by creating and syncing a temporary
// file in the destination directory, then replacing the destination with a
// single rename. Readers never observe a partially written file: the content
// is complete and flushed to disk before the destination is touched.
//
// On Unix the rename is atomic, so readers see either the old file or the new
// one. On Windows Go documents os.Rename as non-atomic; content is still never
// partial, but a concurrent reader may briefly find the destination missing.
//
// Destination handling mirrors [os.WriteFile]: a new file is created with perm
// (subject to the process umask), an existing regular file keeps its
// permission bits, and symlinks are followed so the link stays in place and its
// final target is replaced (a dangling link's target is created). Directories
// and other non-regular destinations are rejected.
//
// If write returns an error, the destination is left unchanged. The writer
// passed to write is buffered and is only valid during the call; the callback
// must not retain or close it.
//
// Because the destination is replaced rather than rewritten in place, it gets a
// new inode: hard links to the old file are not updated, and only permission
// bits (not ownership, ACLs, or extended attributes) carry over. A crash
// between creating and renaming the temporary file can leave a stray
// ".<name>.tmp-<random>" entry in the destination directory, where <name> is
// the destination basename truncated to 64 bytes.
func AtomicWriteFile(path string, perm fs.FileMode, write func(io.Writer) error) error {
	return atomicWriteFile(path, perm, write, atomicWriteOps{
		replace:       os.Rename,
		syncDirectory: syncDirectory,
	})
}

// isDirSyncUnsupported reports whether a directory Sync failed only because the
// filesystem cannot fsync directories (EINVAL or ENOTSUP, as seen on some FUSE,
// 9p, and network mounts). The rename has already completed by then, so such
// failures are tolerated rather than reported as a failed write.
func isDirSyncUnsupported(err error) bool {
	return errors.Is(err, syscall.EINVAL) || errors.Is(err, errors.ErrUnsupported)
}

func atomicWriteFile(path string, perm fs.FileMode, write func(io.Writer) error, ops atomicWriteOps) (err error) {
	if write == nil {
		return errors.New("atomic write: nil write callback")
	}

	dest, err := resolveDestination(path, perm)
	if err != nil {
		return err
	}

	dir := filepath.Dir(dest.path)
	temp, err := createAtomicTempFile(dir, filepath.Base(dest.path), dest.perm)
	if err != nil {
		return fmt.Errorf("creating temporary file for %q: %w", dest.path, err)
	}
	tempPath := temp.Name()
	removeTemp := true
	defer func() {
		if !removeTemp {
			return
		}
		if removeErr := os.Remove(tempPath); removeErr != nil && !errors.Is(removeErr, fs.ErrNotExist) {
			err = errors.Join(err, fmt.Errorf("removing temporary file %q: %w", tempPath, removeErr))
		}
	}()

	// OpenFile applies the process umask, which is what we want for a new
	// destination. When replacing an existing file, restore its exact bits.
	if dest.exists {
		if chmodErr := temp.Chmod(dest.perm); chmodErr != nil {
			return errors.Join(
				fmt.Errorf("setting permissions on temporary file for %q: %w", dest.path, chmodErr),
				closeAtomicTemp(temp, dest.path),
			)
		}
	}

	if err := writeAtomicTemp(temp, dest.path, write); err != nil {
		return err
	}

	if replaceErr := ops.replace(tempPath, dest.path); replaceErr != nil {
		return fmt.Errorf("replacing %q: %w", dest.path, replaceErr)
	}
	removeTemp = false

	if syncErr := ops.syncDirectory(dir); syncErr != nil {
		// The rename already happened; report that the durability step failed
		// rather than implying the file was not written.
		return fmt.Errorf("replaced %q but syncing its directory failed: %w", dest.path, syncErr)
	}
	return nil
}

// destination describes where an atomic write lands after following symlinks.
type destination struct {
	path   string      // final path to replace; never a symlink
	perm   fs.FileMode // permission bits for the new file
	exists bool        // whether path is an existing regular file
}

// resolveDestination follows symlinks from path to the regular file (or
// not-yet-existing file) an atomic write should replace.
func resolveDestination(path string, fallback fs.FileMode) (destination, error) {
	current := path
	followed := 0
	for {
		info, err := os.Lstat(current)
		switch {
		case errors.Is(err, fs.ErrNotExist):
			return destination{path: current, perm: fallback.Perm()}, nil
		case err != nil:
			return destination{}, fmt.Errorf("inspecting destination %q: %w", current, err)
		case info.Mode()&fs.ModeSymlink != 0:
			if followed == maxSymlinkFollows {
				return destination{}, fmt.Errorf("resolving destination %q: too many levels of symbolic links", path)
			}
			followed++
			target, err := os.Readlink(current)
			if err != nil {
				return destination{}, fmt.Errorf("reading symlink %q: %w", current, err)
			}
			if !filepath.IsAbs(target) {
				target = filepath.Join(filepath.Dir(current), target)
			}
			current = target
		case info.Mode().IsRegular():
			return destination{path: current, perm: info.Mode().Perm(), exists: true}, nil
		default:
			return destination{}, fmt.Errorf("atomic write destination %q is not a regular file", current)
		}
	}
}

// createAtomicTempFile creates a uniquely named temporary file next to the
// destination. It deliberately uses O_EXCL with the caller's perm instead of
// os.CreateTemp so a brand-new destination honors the process umask exactly
// like os.WriteFile would.
func createAtomicTempFile(dir, base string, perm fs.FileMode) (*os.File, error) {
	prefix := tempFilePrefix(base)
	for range maxTempFileAttempts {
		name := filepath.Join(dir, prefix+rand.Text())
		file, err := os.OpenFile(name, os.O_RDWR|os.O_CREATE|os.O_EXCL, perm)
		if errors.Is(err, fs.ErrExist) {
			continue
		}
		return file, err
	}
	return nil, fmt.Errorf("could not create a unique temporary file after %d attempts", maxTempFileAttempts)
}

// tempFilePrefix returns the temporary filename prefix for a destination
// basename. The basename is kept so a stray temp file left by a crash is
// recognizable, but truncated on a rune boundary so the full temp name fits
// the 255-byte component limit of common filesystems.
func tempFilePrefix(base string) string {
	if len(base) > maxTempBaseLen {
		cut := maxTempBaseLen
		for cut > 0 && !utf8.RuneStart(base[cut]) {
			cut--
		}
		base = base[:cut]
	}
	return "." + base + ".tmp-"
}

// writeAtomicTemp runs write against a buffered view of file, then flushes,
// syncs, and closes it. On any failure the file is closed and the joined error
// is returned; the caller removes the temporary file.
func writeAtomicTemp(file atomicTemp, path string, write func(io.Writer) error) error {
	// Every return path below closes the file explicitly. If write panics,
	// nothing does, so close here before the panic unwinds past the caller's
	// temp-file removal (Windows cannot remove an open file).
	finished := false
	defer func() {
		if !finished {
			_ = file.Close()
		}
	}()

	buffered := bufio.NewWriter(file)
	err := write(buffered)
	finished = true
	if err != nil {
		return errors.Join(
			fmt.Errorf("writing temporary file for %q: %w", path, err),
			closeAtomicTemp(file, path),
		)
	}
	if err := buffered.Flush(); err != nil {
		return errors.Join(
			fmt.Errorf("writing temporary file for %q: %w", path, err),
			closeAtomicTemp(file, path),
		)
	}
	if err := file.Sync(); err != nil {
		return errors.Join(
			fmt.Errorf("syncing temporary file for %q: %w", path, err),
			closeAtomicTemp(file, path),
		)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("closing temporary file for %q: %w", path, err)
	}
	return nil
}

func closeAtomicTemp(file io.Closer, path string) error {
	if err := file.Close(); err != nil {
		return fmt.Errorf("closing temporary file for %q: %w", path, err)
	}
	return nil
}
