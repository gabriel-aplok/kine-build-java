//go:build windows

package fsutil

import (
	"errors"
	"io/fs"
	"os"
)

// Replace moves src to dst and replaces dst when it already exists.
// Windows refuses to rename over an existing file, so dst is removed first.
func Replace(src, dst string) error {
	err := os.Rename(src, dst)
	if err == nil {
		return nil
	}
	if _, statErr := os.Stat(dst); statErr != nil {
		return err
	}
	if removeErr := os.Remove(dst); removeErr != nil && !errors.Is(removeErr, fs.ErrNotExist) {
		return err
	}
	return os.Rename(src, dst)
}
