//go:build windows

package fsutil

// syncDirectory is a no-op on Windows: directories cannot be opened for fsync
// there. Note that Go documents os.Rename as non-atomic on Windows, so
// AtomicWriteFile's guarantee is weaker on this platform; see its doc comment.
func syncDirectory(string) error {
	return nil
}
