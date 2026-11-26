// Package atomicfile replaces a file with a temporary file written beside it.
package atomicfile

import (
	"os"
	"runtime"
)

// Replace renames tmp over dst. Windows cannot rename over an existing file,
// so it removes the old one first; the temporary file is still written in
// the same directory, which keeps the write on one filesystem.
func Replace(tmp, dst string) error {
	err := os.Rename(tmp, dst)
	if err == nil || runtime.GOOS != "windows" {
		return err
	}
	if removeErr := os.Remove(dst); removeErr != nil && !os.IsNotExist(removeErr) {
		return err
	}
	return os.Rename(tmp, dst)
}
