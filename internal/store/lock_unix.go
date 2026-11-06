//go:build !windows

package store

import (
	"errors"
	"fmt"
	"os"
	"syscall"
)

func lockFile(path string, nonBlocking bool) (*Lock, error) {
	if err := ensureLockDir(path); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open lock %s: %w", path, err)
	}

	how := syscall.LOCK_EX
	if nonBlocking {
		how |= syscall.LOCK_NB
	}
	for {
		err := syscall.Flock(int(file.Fd()), how)
		if err == nil {
			return &Lock{file: file}, nil
		}
		if errors.Is(err, syscall.EINTR) {
			continue
		}
		file.Close()
		if nonBlocking && errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, ErrLocked
		}
		return nil, fmt.Errorf("lock %s: %w", path, err)
	}
}

func releaseLock(file *os.File) error {
	unlockErr := syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
	closeErr := file.Close()
	if unlockErr != nil {
		return fmt.Errorf("unlock: %w", unlockErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close lock: %w", closeErr)
	}
	return nil
}
