package store

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// ErrLocked is returned when a workspace is already locked.
var ErrLocked = errors.New("workspace is locked")

// Lock holds an exclusive file lock.
type Lock struct {
	file *os.File
}

// Close releases the lock.
func (l *Lock) Close() error {
	if l == nil || l.file == nil {
		return nil
	}
	file := l.file
	l.file = nil
	return releaseLock(file)
}

// LockWorkspace locks one workspace for a build.
func LockWorkspace(workspaceRoot string) (*Lock, error) {
	root, err := filepath.Abs(workspaceRoot)
	if err != nil {
		return nil, fmt.Errorf("resolve workspace root %s: %w", workspaceRoot, err)
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return nil, fmt.Errorf("resolve workspace root %s: %w", workspaceRoot, err)
	}
	info, err := os.Stat(root)
	if err != nil {
		return nil, fmt.Errorf("stat workspace root %s: %w", root, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("workspace root %s: not a directory", root)
	}
	return lockFile(filepath.Join(root, ".kine", "lock"), true)
}

func ensureLockDir(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create lock dir: %w", err)
	}
	return nil
}
