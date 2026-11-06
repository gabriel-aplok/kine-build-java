//go:build windows

package toolchain

import (
	"path/filepath"
	"testing"
)

func TestPrepareExecutableBatch(t *testing.T) {
	path := filepath.Join(`C:\Android\Sdk`, "build-tools", "34.0.0", "d8.bat")
	target := prepareExecutable(path, []string{"--output", `C:\out`}, `C:\project`)

	if filepath.Base(target.path) != "cmd.exe" {
		t.Errorf("path = %q, want cmd.exe", target.path)
	}
	if target.dir != filepath.Dir(path) {
		t.Errorf("dir = %q, want %q", target.dir, filepath.Dir(path))
	}
	want := []string{"/d", "/c", "d8.bat", "--output", `C:\out`}
	if len(target.args) != len(want) {
		t.Fatalf("args = %v, want %v", target.args, want)
	}
	for i := range want {
		if target.args[i] != want[i] {
			t.Errorf("args[%d] = %q, want %q", i, target.args[i], want[i])
		}
	}
}

func TestPrepareExecutableBinary(t *testing.T) {
	path := filepath.Join(`C:\Android\Sdk`, "build-tools", "34.0.0", "aapt2.exe")
	target := prepareExecutable(path, []string{"version"}, `C:\project`)

	if target.path != path {
		t.Errorf("path = %q, want %q", target.path, path)
	}
	if target.dir != `C:\project` {
		t.Errorf("dir = %q", target.dir)
	}
}

func TestAvailableMemoryWindows(t *testing.T) {
	memory, err := AvailableMemory()
	if err != nil {
		t.Fatalf("AvailableMemory() error = %v", err)
	}
	if memory <= 0 {
		t.Errorf("memory = %d, want a positive value", memory)
	}
}
