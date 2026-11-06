package store

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/gabriel-aplok/kine-build-java/internal/core"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()

	store, _ := newTestStoreWithWorkspace(t)
	return store
}

func newTestStoreWithWorkspace(t *testing.T) (*Store, string) {
	t.Helper()

	workspace := t.TempDir()
	store, err := Open(workspace, filepath.Join(t.TempDir(), "cache"), Options{Logger: testLogger()})
	if err != nil {
		t.Fatal(err)
	}
	return store, workspace
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func writeTestFile(t *testing.T, dir, name, content string) string {
	t.Helper()

	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func mustStore(t *testing.T, store *Store, fingerprint string, outputs ...core.Output) {
	t.Helper()

	if err := store.Store(context.Background(), fingerprint, outputs); err != nil {
		t.Fatalf("Store() error = %v", err)
	}
}

func mustLookup(t *testing.T, store *Store, fingerprint string, outputs ...core.Output) bool {
	t.Helper()

	hit, err := store.Lookup(context.Background(), fingerprint, outputs)
	if err != nil {
		t.Fatalf("Lookup() error = %v", err)
	}
	return hit
}

func casSize(t *testing.T, store *Store) int64 {
	t.Helper()

	var total int64
	err := filepath.WalkDir(filepath.Join(store.storeRoot, "cas"), func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		total += info.Size()
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return total
}

func countFiles(t *testing.T, root string) int {
	t.Helper()

	count := 0
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() {
			count++
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return count
}
