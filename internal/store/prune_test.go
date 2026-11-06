package store

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gabriel-aplok/kine-build-java/internal/core"
)

func TestPruneRemovesOldest(t *testing.T) {
	store, workspace := newTestStoreWithWorkspace(t)
	store.maxSize = 4

	first := strings.Repeat("a", 64)
	second := strings.Repeat("b", 64)
	firstTime := time.Date(2026, 11, 3, 10, 0, 0, 0, time.UTC)
	secondTime := firstTime.Add(time.Hour)

	writeTestFile(t, workspace, "app/a.txt", "aaaa")
	store.now = func() time.Time { return firstTime }
	mustStore(t, store, first, core.Output{Path: "app/a.txt", Mode: core.ModeFile})

	writeTestFile(t, workspace, "app/b.txt", "bbbb")
	store.now = func() time.Time { return secondTime }
	mustStore(t, store, second, core.Output{Path: "app/b.txt", Mode: core.ModeFile})

	store.now = func() time.Time { return secondTime.Add(time.Hour) }
	stats, err := store.Prune(context.Background())
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if stats.Entries != 1 {
		t.Errorf("Entries = %d, want 1", stats.Entries)
	}
	if stats.Blobs != 1 {
		t.Errorf("Blobs = %d, want 1", stats.Blobs)
	}
	if stats.Size != 4 {
		t.Errorf("Size = %d, want 4", stats.Size)
	}
	if _, err := store.readEntry(first); !errors.Is(err, ErrEntryMissing) {
		t.Errorf("old entry error = %v, want ErrEntryMissing", err)
	}
	if _, err := store.readEntry(second); err != nil {
		t.Errorf("new entry error = %v", err)
	}
}

func TestPruneKeepsSharedBlob(t *testing.T) {
	store, workspace := newTestStoreWithWorkspace(t)
	store.maxSize = 4

	first := strings.Repeat("a", 64)
	second := strings.Repeat("b", 64)
	firstTime := time.Date(2026, 11, 3, 10, 0, 0, 0, time.UTC)
	secondTime := firstTime.Add(time.Hour)

	writeTestFile(t, workspace, "app/a.txt", "aaaa")
	writeTestFile(t, workspace, "app/b.txt", "bbbb")
	writeTestFile(t, workspace, "app/b2.txt", "bbbb")
	store.now = func() time.Time { return firstTime }
	mustStore(t, store, first,
		core.Output{Path: "app/a.txt", Mode: core.ModeFile},
		core.Output{Path: "app/b.txt", Mode: core.ModeFile},
	)
	store.now = func() time.Time { return secondTime }
	mustStore(t, store, second, core.Output{Path: "app/b2.txt", Mode: core.ModeFile})

	firstEntry, err := store.readEntry(first)
	if err != nil {
		t.Fatal(err)
	}
	secondEntry, err := store.readEntry(second)
	if err != nil {
		t.Fatal(err)
	}
	hashA := firstEntry.Outputs[0].Hash
	hashB := firstEntry.Outputs[1].Hash
	if secondEntry.Outputs[0].Hash != hashB {
		t.Fatal("entries do not share the expected blob")
	}

	stats, err := store.Prune(context.Background())
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if stats.Entries != 1 || stats.Blobs != 1 || stats.Size != 4 {
		t.Errorf("stats = %+v, want 1 entry, 1 blob, size 4", stats)
	}
	if store.blobExists(hashA) {
		t.Error("evicted blob still exists")
	}
	if !store.blobExists(hashB) {
		t.Error("shared blob was removed")
	}
}

func TestPruneRemovesOrphanBlob(t *testing.T) {
	store, workspace := newTestStoreWithWorkspace(t)
	writeTestFile(t, workspace, "app/out.txt", "x")
	mustStore(t, store, strings.Repeat("a", 64), core.Output{Path: "app/out.txt", Mode: core.ModeFile})

	orphan, _, err := store.putBytes([]byte("orphan"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Prune(context.Background()); err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if store.blobExists(orphan) {
		t.Error("orphan blob still exists")
	}

	stats, err := store.Stats(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if stats.Entries != 1 || stats.Blobs != 1 {
		t.Errorf("stats = %+v, want 1 entry and 1 blob", stats)
	}
}

func TestPruneRemovesInvalidEntry(t *testing.T) {
	store, workspace := newTestStoreWithWorkspace(t)
	writeTestFile(t, workspace, "app/out.txt", "x")
	mustStore(t, store, strings.Repeat("a", 64), core.Output{Path: "app/out.txt", Mode: core.ModeFile})

	invalid := store.entryPath(strings.Repeat("b", 64))
	if err := os.MkdirAll(filepath.Dir(invalid), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(invalid, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := store.Prune(context.Background()); err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if _, err := os.Stat(invalid); !os.IsNotExist(err) {
		t.Errorf("invalid entry still exists: %v", err)
	}
}

func TestPruneRemovesStaleTemp(t *testing.T) {
	store, _ := newTestStoreWithWorkspace(t)
	now := time.Date(2026, 11, 3, 10, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return now }

	stale := filepath.Join(store.storeRoot, "tmp", "stale")
	fresh := filepath.Join(store.storeRoot, "tmp", "fresh")
	if err := os.WriteFile(stale, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fresh, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	old := now.Add(-2 * time.Hour)
	if err := os.Chtimes(stale, old, old); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(fresh, now, now); err != nil {
		t.Fatal(err)
	}

	if _, err := store.Prune(context.Background()); err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Errorf("stale temp still exists: %v", err)
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Errorf("fresh temp was removed: %v", err)
	}
}

func TestPruneKeepsPinnedBlob(t *testing.T) {
	store, workspace := newTestStoreWithWorkspace(t)
	store.maxSize = 1

	source := writeTestFile(t, workspace, "pinned.txt", "pinned")
	blob, err := store.PutBlob(source)
	if err != nil {
		t.Fatalf("PutBlob() error = %v", err)
	}
	dest := filepath.Join(workspace, "pinned-link.txt")
	if err := store.MaterializeBlob(blob.Hash, dest, blob.Perm); err != nil {
		t.Fatalf("MaterializeBlob() error = %v", err)
	}

	stats, err := store.Prune(context.Background())
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if stats.Blobs != 1 {
		t.Errorf("Blobs = %d, want 1", stats.Blobs)
	}
	if stats.Size != blob.Size {
		t.Errorf("Size = %d, want %d", stats.Size, blob.Size)
	}
	if !store.HasBlob(blob.Hash) {
		t.Error("pinned blob was removed")
	}

	if err := os.Remove(dest); err != nil {
		t.Fatal(err)
	}
	stats, err = store.Prune(context.Background())
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if stats.Blobs != 0 || stats.Size != 0 {
		t.Errorf("stats = %+v, want empty", stats)
	}
	if store.HasBlob(blob.Hash) {
		t.Error("unpinned blob still exists")
	}
}

func TestStatsCountsPinnedBlob(t *testing.T) {
	store, workspace := newTestStoreWithWorkspace(t)
	source := writeTestFile(t, workspace, "pinned.txt", "pinned")
	blob, err := store.PutBlob(source)
	if err != nil {
		t.Fatalf("PutBlob() error = %v", err)
	}
	dest := filepath.Join(workspace, "pinned-link.txt")
	if err := store.MaterializeBlob(blob.Hash, dest, blob.Perm); err != nil {
		t.Fatalf("MaterializeBlob() error = %v", err)
	}

	stats, err := store.Stats(context.Background())
	if err != nil {
		t.Fatalf("Stats() error = %v", err)
	}
	if stats.Entries != 0 {
		t.Errorf("Entries = %d, want 0", stats.Entries)
	}
	if stats.Blobs != 1 {
		t.Errorf("Blobs = %d, want 1", stats.Blobs)
	}
	if stats.Size != blob.Size {
		t.Errorf("Size = %d, want %d", stats.Size, blob.Size)
	}
}

func TestPruneEmpty(t *testing.T) {
	store := newTestStore(t)

	stats, err := store.Prune(context.Background())
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if stats.Entries != 0 || stats.Blobs != 0 || stats.Size != 0 {
		t.Errorf("stats = %+v, want empty", stats)
	}
	if stats.MaxSize != store.maxSize {
		t.Errorf("MaxSize = %d, want %d", stats.MaxSize, store.maxSize)
	}
}

func TestPruneContextCanceled(t *testing.T) {
	store := newTestStore(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := store.Prune(ctx); !errors.Is(err, context.Canceled) {
		t.Errorf("Prune() error = %v, want context.Canceled", err)
	}
}
