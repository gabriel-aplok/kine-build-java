//go:build windows

package store

import (
	"fmt"
	"os"
	"syscall"
)

func pinnedBlob(path string, _ os.FileInfo) (bool, error) {
	file, err := os.Open(path)
	if err != nil {
		return false, fmt.Errorf("open %s: %w", path, err)
	}
	defer file.Close()

	var data syscall.ByHandleFileInformation
	if err := syscall.GetFileInformationByHandle(syscall.Handle(file.Fd()), &data); err != nil {
		return false, fmt.Errorf("stat links %s: %w", path, err)
	}
	return data.NumberOfLinks >= 2, nil
}
