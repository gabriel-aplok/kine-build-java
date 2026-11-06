package store

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestLockWorkspace(t *testing.T) {
	root := t.TempDir()
	lock, err := LockWorkspace(root)
	if err != nil {
		t.Fatalf("LockWorkspace() error = %v", err)
	}
	defer lock.Close()

	lockPath := filepath.Join(root, ".kine", "lock")
	if _, err := os.Stat(lockPath); err != nil {
		t.Fatalf("stat lock file: %v", err)
	}

	second, err := LockWorkspace(root)
	if !errors.Is(err, ErrLocked) {
		if second != nil {
			second.Close()
		}
		t.Fatalf("second LockWorkspace() error = %v, want ErrLocked", err)
	}
	if second != nil {
		t.Error("second lock is not nil")
	}

	if err := lock.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	third, err := LockWorkspace(root)
	if err != nil {
		t.Fatalf("LockWorkspace() after close error = %v", err)
	}
	if err := third.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

func TestLockCloseTwice(t *testing.T) {
	lock, err := LockWorkspace(t.TempDir())
	if err != nil {
		t.Fatalf("LockWorkspace() error = %v", err)
	}
	if err := lock.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if err := lock.Close(); err != nil {
		t.Fatalf("second Close() error = %v", err)
	}
}

func TestLockWorkspaceMissingRoot(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing")

	if _, err := LockWorkspace(missing); err == nil {
		t.Fatal("LockWorkspace() error = nil, want error")
	}
}

func TestLockWorkspaceRootFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := LockWorkspace(path); err == nil {
		t.Fatal("LockWorkspace() error = nil, want error")
	}
}
