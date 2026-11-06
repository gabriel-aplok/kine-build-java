//go:build windows

package store

import (
	"errors"
	"fmt"
	"os"
	"syscall"
	"time"
)

// errSharingViolation is ERROR_SHARING_VIOLATION from the Windows API.
const errSharingViolation syscall.Errno = 32

const lockRetryInterval = 50 * time.Millisecond

func lockFile(path string, nonBlocking bool) (*Lock, error) {
	if err := ensureLockDir(path); err != nil {
		return nil, err
	}
	name, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return nil, fmt.Errorf("lock path %s: %w", path, err)
	}

	for {
		handle, err := syscall.CreateFile(
			name,
			syscall.GENERIC_READ|syscall.GENERIC_WRITE,
			0,
			nil,
			syscall.OPEN_ALWAYS,
			syscall.FILE_ATTRIBUTE_NORMAL,
			0,
		)
		if err == nil {
			return &Lock{file: os.NewFile(uintptr(handle), path)}, nil
		}
		if errors.Is(err, errSharingViolation) {
			if nonBlocking {
				return nil, ErrLocked
			}
			time.Sleep(lockRetryInterval)
			continue
		}
		return nil, fmt.Errorf("lock %s: %w", path, err)
	}
}

func releaseLock(file *os.File) error {
	if err := file.Close(); err != nil {
		return fmt.Errorf("close lock: %w", err)
	}
	return nil
}
