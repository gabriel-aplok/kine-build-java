package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/gabriel-aplok/kine-build-java/internal/core"
)

func TestTasksListsWithoutRunning(t *testing.T) {
	dir := t.TempDir()
	writeRecordingConfig(t, dir)
	chdir(t, dir)
	provider := newRecordingProvider()

	var stdout, stderr bytes.Buffer
	code := runWithProviders([]string{"tasks", "--cache-dir", t.TempDir()}, &stdout, &stderr, []core.Provider{provider})
	if code != 0 {
		t.Fatalf("run() = %d, stderr = %s", code, stderr.String())
	}

	want := "app:compile\n" +
		"app:jar <- app:compile\n" +
		"app:run <- app:compile\n" +
		"app:test <- app:compile\n"
	if stdout.String() != want {
		t.Errorf("stdout = %q, want %q", stdout.String(), want)
	}
	if runs := provider.ranIDs(); len(runs) != 0 {
		t.Errorf("ran %v, want nothing", runs)
	}
}

func TestTasksMissingConfig(t *testing.T) {
	chdir(t, t.TempDir())
	var stdout, stderr bytes.Buffer

	code := run([]string{"tasks"}, &stdout, &stderr)
	if code != exitUsage {
		t.Fatalf("run() = %d, want %d", code, exitUsage)
	}
	if !strings.Contains(stderr.String(), "kine.toml") {
		t.Errorf("stderr %q does not name kine.toml", stderr.String())
	}
}
