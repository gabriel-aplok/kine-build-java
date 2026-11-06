package fsutil

import "os"

// ReplaceDir moves src to dst and replaces an existing destination directory.
func ReplaceDir(src, dst string) error {
	if err := os.RemoveAll(dst); err != nil {
		return err
	}
	return os.Rename(src, dst)
}
