package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCleanRemovesBuildsAndCaches(t *testing.T) {
	root := t.TempDir()
	appDir := filepath.Join(root, "app")
	writeConfig(t, root, `[workspace]
name = "demo"
members = ["app"]
`)
	writeMemberConfig(t, appDir, "app", "jvm")

	paths := []string{
		filepath.Join(appDir, "build", "classes", "App.class"),
		filepath.Join(appDir, ".kine", "deps", "lib.jar"),
		filepath.Join(appDir, ".kine", "tools", "tool.jar"),
		filepath.Join(appDir, ".kine", "tmp", "scratch"),
		filepath.Join(root, ".kine", "tools", "tool.jar"),
		filepath.Join(root, ".kine", "tmp", "scratch"),
		filepath.Join(root, ".kine", "lock"),
		filepath.Join(appDir, "kine.lock"),
	}
	for _, path := range paths {
		writeTestFile(t, path)
	}

	cacheDir := t.TempDir()
	cacheFile := filepath.Join(cacheDir, "blob")
	writeTestFile(t, cacheFile)

	chdir(t, root)
	var stdout, stderr bytes.Buffer
	code := run([]string{"clean"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run() = %d, stderr = %s", code, stderr.String())
	}

	removed := []string{
		filepath.Join(appDir, "build"),
		filepath.Join(appDir, ".kine", "deps"),
		filepath.Join(appDir, ".kine", "tools"),
		filepath.Join(appDir, ".kine", "tmp"),
		filepath.Join(root, ".kine", "tools"),
		filepath.Join(root, ".kine", "tmp"),
	}
	for _, path := range removed {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("%s still exists: %v", path, err)
		}
	}

	kept := []string{
		filepath.Join(root, ".kine", "lock"),
		filepath.Join(appDir, "kine.lock"),
		cacheFile,
	}
	for _, path := range kept {
		if _, err := os.Stat(path); err != nil {
			t.Errorf("%s was removed: %v", path, err)
		}
	}

	if !strings.Contains(stdout.String(), "clean complete") {
		t.Errorf("stdout %q does not contain the completion message", stdout.String())
	}
	if !strings.Contains(stderr.String(), "removed build") {
		t.Errorf("stderr %q does not log the removed build", stderr.String())
	}
}

func TestCleanMissingConfig(t *testing.T) {
	chdir(t, t.TempDir())
	var stdout, stderr bytes.Buffer

	code := run([]string{"clean"}, &stdout, &stderr)
	if code != exitUsage {
		t.Fatalf("run() = %d, want %d", code, exitUsage)
	}
	if !strings.Contains(stderr.String(), "kine.toml") {
		t.Errorf("stderr %q does not name kine.toml", stderr.String())
	}
}

func writeMemberConfig(t *testing.T, dir, name, provider string) {
	t.Helper()

	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeConfig(t, dir, `[project]
name = "`+name+`"
provider = "`+provider+`"
`)
}

func writeTestFile(t *testing.T, path string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
}
