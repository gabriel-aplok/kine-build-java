//go:build !windows

package store

import (
	"os"
	"syscall"
)

func pinnedBlob(_ string, info os.FileInfo) (bool, error) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return false, nil
	}
	return stat.Nlink >= 2, nil
}
