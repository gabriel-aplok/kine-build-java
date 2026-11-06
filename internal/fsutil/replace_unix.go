//go:build !windows

package fsutil

import "os"

// Replace moves src to dst and replaces dst when it already exists.
func Replace(src, dst string) error {
	return os.Rename(src, dst)
}
