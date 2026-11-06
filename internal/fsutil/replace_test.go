package fsutil

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReplaceOverExisting(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	dst := filepath.Join(dir, "dst")
	if err := os.WriteFile(src, []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := Replace(src, dst); err != nil {
		t.Fatalf("Replace() error = %v", err)
	}
	data, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "new" {
		t.Errorf("dst = %q, want new", data)
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Errorf("src still exists: %v", err)
	}
}

func TestReplaceDirOverExisting(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	dst := filepath.Join(dir, "dst")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "new.txt"), []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dst, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dst, "old.txt"), []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := ReplaceDir(src, dst); err != nil {
		t.Fatalf("ReplaceDir() error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(dst, "new.txt")); err != nil {
		t.Errorf("new.txt missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dst, "old.txt")); !os.IsNotExist(err) {
		t.Errorf("old.txt still exists: %v", err)
	}
}
