package jvm

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCopyResources(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "resources")
	writeFile(t, filepath.Join(source, "a.txt"), "a")
	writeFile(t, filepath.Join(source, "sub", "b.txt"), "b")
	output := filepath.Join(root, "build", "resources", "main")

	if err := copyResources([]string{source}, output); err != nil {
		t.Fatalf("copyResources() error = %v", err)
	}

	data, err := os.ReadFile(filepath.Join(output, "a.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "a" {
		t.Errorf("a.txt = %q, want a", data)
	}
	data, err = os.ReadFile(filepath.Join(output, "sub", "b.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "b" {
		t.Errorf("sub/b.txt = %q, want b", data)
	}
}

func TestCopyResourcesRemovesStaleFiles(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "resources")
	writeFile(t, filepath.Join(source, "a.txt"), "a")
	output := filepath.Join(root, "build", "resources", "main")
	writeFile(t, filepath.Join(output, "stale.txt"), "stale")

	if err := copyResources([]string{source}, output); err != nil {
		t.Fatalf("copyResources() error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(output, "stale.txt")); !os.IsNotExist(err) {
		t.Errorf("stale file still exists: %v", err)
	}
}
