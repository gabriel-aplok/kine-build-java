package main

import (
	"bytes"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/gabriel-aplok/kine-build-java/internal/store"
)

func TestCacheInfoWithoutConfig(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	cacheDir := t.TempDir()

	var stdout, stderr bytes.Buffer
	code := run([]string{"cache", "info", "--cache-dir", cacheDir, "--cache-size", "10MiB"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run() = %d, stderr = %s", code, stderr.String())
	}
	output := stdout.String()
	if !strings.Contains(output, "entries=0") || !strings.Contains(output, "blobs=0") {
		t.Errorf("stdout %q does not report an empty cache", output)
	}
	if !strings.Contains(output, "cap="+strconv.FormatInt(10<<20, 10)) {
		t.Errorf("stdout %q does not report the cap", output)
	}
}

func TestCacheInfoCountsBlobs(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	cacheDir := t.TempDir()

	cache, err := store.Open(dir, cacheDir, store.Options{Logger: discardTestLogger()})
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}
	source := filepath.Join(dir, "blob.txt")
	if err := os.WriteFile(source, []byte("content"), 0o600); err != nil {
		t.Fatal(err)
	}
	blob, err := cache.PutBlob(source)
	if err != nil {
		t.Fatalf("PutBlob() error = %v", err)
	}
	if err := cache.MaterializeBlob(blob.Hash, filepath.Join(dir, "pinned.txt"), blob.Perm); err != nil {
		t.Fatalf("MaterializeBlob() error = %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := run([]string{"cache", "info", "--cache-dir", cacheDir}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run() = %d, stderr = %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "blobs=1") {
		t.Errorf("stdout %q does not count the blob", stdout.String())
	}
}

func TestCachePrune(t *testing.T) {
	chdir(t, t.TempDir())
	cacheDir := t.TempDir()

	var stdout, stderr bytes.Buffer
	code := run([]string{"cache", "prune", "--cache-dir", cacheDir}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run() = %d, stderr = %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "before:") || !strings.Contains(stdout.String(), "after:") {
		t.Errorf("stdout %q does not report before and after", stdout.String())
	}
}

func TestCachePruneError(t *testing.T) {
	chdir(t, t.TempDir())
	cacheDir := t.TempDir()
	if err := os.Mkdir(filepath.Join(cacheDir, "lock"), 0o755); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := run([]string{"cache", "prune", "--cache-dir", cacheDir}, &stdout, &stderr)
	if code != exitBuildFailure {
		t.Fatalf("run() = %d, want %d", code, exitBuildFailure)
	}
	if !strings.Contains(stderr.String(), "cache prune") {
		t.Errorf("stderr %q does not mention the prune failure", stderr.String())
	}
}

func TestCacheMissingSubcommand(t *testing.T) {
	chdir(t, t.TempDir())
	var stdout, stderr bytes.Buffer

	code := run([]string{"cache"}, &stdout, &stderr)
	if code != exitUsage {
		t.Fatalf("run() = %d, want %d", code, exitUsage)
	}
	if !strings.Contains(stderr.String(), "cache requires") {
		t.Errorf("stderr %q does not explain the subcommand", stderr.String())
	}
}

func TestCacheUnknownSubcommand(t *testing.T) {
	chdir(t, t.TempDir())
	var stdout, stderr bytes.Buffer

	code := run([]string{"cache", "nope"}, &stdout, &stderr)
	if code != exitUsage {
		t.Fatalf("run() = %d, want %d", code, exitUsage)
	}
	if !strings.Contains(stderr.String(), "unknown cache command") {
		t.Errorf("stderr %q does not name the unknown subcommand", stderr.String())
	}
}

func discardTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
