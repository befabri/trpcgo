//go:build !windows

package fsutil

import (
	"errors"
	"fmt"
	"os"
)

// syncDirectory flushes directory metadata so a completed rename survives a
// crash. Filesystems that cannot fsync directories are tolerated; see
// isDirSyncUnsupported.
func syncDirectory(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	syncErr := dir.Sync()
	if isDirSyncUnsupported(syncErr) {
		syncErr = nil
	}
	closeErr := dir.Close()
	return errors.Join(
		wrapAtomicDirectoryError("syncing", syncErr),
		wrapAtomicDirectoryError("closing", closeErr),
	)
}

func wrapAtomicDirectoryError(action string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s directory: %w", action, err)
}
