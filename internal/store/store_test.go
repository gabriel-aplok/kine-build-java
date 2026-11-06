package store

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gabriel-aplok/kine-build-java/internal/core"
)

func TestDefaultRoot(t *testing.T) {
	cacheHome := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cacheHome)

	got, err := DefaultRoot()
	if err != nil {
		t.Fatalf("DefaultRoot() error = %v", err)
	}
	want := filepath.Join(cacheHome, "kine")
	if got != want {
		t.Errorf("DefaultRoot() = %q, want %q", got, want)
	}
}

func TestOpenCreatesLayout(t *testing.T) {
	workspace := t.TempDir()
	storeRoot := filepath.Join(t.TempDir(), "cache")

	store, err := Open(workspace, storeRoot, Options{})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	for _, name := range []string{"cas", "tasks", "tmp"} {
		info, err := os.Stat(filepath.Join(storeRoot, name))
		if err != nil {
			t.Fatalf("stat %s: %v", name, err)
		}
		if !info.IsDir() {
			t.Errorf("%s is not a directory", name)
		}
	}
	if store.maxSize != DefaultMaxSize {
		t.Errorf("maxSize = %d, want %d", store.maxSize, DefaultMaxSize)
	}
	if store.logger == nil {
		t.Error("logger is nil")
	}
	if store.now == nil {
		t.Error("now is nil")
	}
}

func TestOpenRejectsFileStoreRoot(t *testing.T) {
	workspace := t.TempDir()
	storeRoot := filepath.Join(t.TempDir(), "cache")
	if err := os.WriteFile(storeRoot, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := Open(workspace, storeRoot, Options{}); err == nil {
		t.Fatal("Open() error = nil, want error")
	}
}

func TestOpenRejectsMissingWorkspace(t *testing.T) {
	workspace := filepath.Join(t.TempDir(), "missing")

	if _, err := Open(workspace, t.TempDir(), Options{}); err == nil {
		t.Fatal("Open() error = nil, want error")
	}
}

func TestOpenResolvesRoots(t *testing.T) {
	workspace := t.TempDir()
	storeRoot := filepath.Join(t.TempDir(), "cache")

	store, err := Open(workspace, storeRoot, Options{MaxSize: 1234})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	wantWorkspace, err := filepath.EvalSymlinks(workspace)
	if err != nil {
		t.Fatal(err)
	}
	wantStore, err := filepath.EvalSymlinks(storeRoot)
	if err != nil {
		t.Fatal(err)
	}
	if store.workspaceRoot != wantWorkspace {
		t.Errorf("workspaceRoot = %q, want %q", store.workspaceRoot, wantWorkspace)
	}
	if store.storeRoot != wantStore {
		t.Errorf("storeRoot = %q, want %q", store.storeRoot, wantStore)
	}
	if store.maxSize != 1234 {
		t.Errorf("maxSize = %d, want 1234", store.maxSize)
	}
}

func TestOpenUsesDefaultRoot(t *testing.T) {
	cacheHome := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cacheHome)

	store, err := Open(t.TempDir(), "", Options{})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	want, err := filepath.EvalSymlinks(filepath.Join(cacheHome, "kine"))
	if err != nil {
		t.Fatal(err)
	}
	if store.storeRoot != want {
		t.Errorf("storeRoot = %q, want %q", store.storeRoot, want)
	}
}

func TestStoreFileOutput(t *testing.T) {
	store, workspace := newTestStoreWithWorkspace(t)
	writeTestFile(t, workspace, "app/out.txt", "content")
	fingerprint := strings.Repeat("a", 64)

	err := store.Store(context.Background(), fingerprint, []core.Output{{
		Path: "app/out.txt",
		Mode: core.ModeFile,
	}})
	if err != nil {
		t.Fatalf("Store() error = %v", err)
	}

	e, err := store.readEntry(fingerprint)
	if err != nil {
		t.Fatalf("readEntry() error = %v", err)
	}
	if len(e.Outputs) != 1 {
		t.Fatalf("len(Outputs) = %d, want 1", len(e.Outputs))
	}
	output := e.Outputs[0]
	if output.Path != "app/out.txt" || output.Mode != "file" {
		t.Errorf("output = %+v, want app/out.txt file", output)
	}
	if output.Size != int64(len("content")) {
		t.Errorf("size = %d, want %d", output.Size, len("content"))
	}
	if output.Perm != 0o600 {
		t.Errorf("perm = %o, want 600", output.Perm)
	}
	if !store.blobExists(output.Hash) {
		t.Error("blob missing")
	}
	if e.Size != int64(len("content")) {
		t.Errorf("entry size = %d, want %d", e.Size, len("content"))
	}
}

func TestStoreTreeOutput(t *testing.T) {
	store, workspace := newTestStoreWithWorkspace(t)
	writeTestFile(t, workspace, "app/classes/App.class", "bytes")
	fingerprint := strings.Repeat("a", 64)

	err := store.Store(context.Background(), fingerprint, []core.Output{{
		Path: "app/classes",
		Mode: core.ModeTree,
	}})
	if err != nil {
		t.Fatalf("Store() error = %v", err)
	}

	e, err := store.readEntry(fingerprint)
	if err != nil {
		t.Fatalf("readEntry() error = %v", err)
	}
	if len(e.Outputs) != 1 {
		t.Fatalf("len(Outputs) = %d, want 1", len(e.Outputs))
	}
	output := e.Outputs[0]
	if output.Path != "app/classes" || output.Mode != "tree" {
		t.Errorf("output = %+v, want app/classes tree", output)
	}
	if !store.blobExists(output.Manifest) {
		t.Error("manifest blob missing")
	}
	if e.Size != int64(len("bytes")) {
		t.Errorf("entry size = %d, want %d", e.Size, len("bytes"))
	}
}

func TestStoreMissingOutput(t *testing.T) {
	store, _ := newTestStoreWithWorkspace(t)

	err := store.Store(context.Background(), strings.Repeat("a", 64), []core.Output{{
		Path: "app/missing.txt",
		Mode: core.ModeFile,
	}})
	if err == nil {
		t.Fatal("Store() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "missing.txt") {
		t.Errorf("error %q does not name the output", err)
	}
}

func TestStoreMissingTree(t *testing.T) {
	store, _ := newTestStoreWithWorkspace(t)

	err := store.Store(context.Background(), strings.Repeat("a", 64), []core.Output{{
		Path: "app/missing",
		Mode: core.ModeTree,
	}})
	if err == nil {
		t.Fatal("Store() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "missing") {
		t.Errorf("error %q does not name the output", err)
	}
}

func TestStoreRejectsBadPaths(t *testing.T) {
	tests := []struct {
		name   string
		output core.Output
	}{
		{name: "empty", output: core.Output{Path: "", Mode: core.ModeFile}},
		{name: "absolute", output: core.Output{Path: "/tmp/out", Mode: core.ModeFile}},
		{name: "escape", output: core.Output{Path: "../outside", Mode: core.ModeFile}},
		{name: "root", output: core.Output{Path: ".", Mode: core.ModeTree}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store, _ := newTestStoreWithWorkspace(t)
			err := store.Store(context.Background(), strings.Repeat("a", 64), []core.Output{tt.output})
			if err == nil {
				t.Fatal("Store() error = nil, want error")
			}
		})
	}
}

func TestStoreRejectsUnknownMode(t *testing.T) {
	store, _ := newTestStoreWithWorkspace(t)

	err := store.Store(context.Background(), strings.Repeat("a", 64), []core.Output{{
		Path: "app/out.txt",
		Mode: core.PathMode(99),
	}})
	if err == nil {
		t.Fatal("Store() error = nil, want error")
	}
}

func TestStoreRejectsBadFingerprint(t *testing.T) {
	store, _ := newTestStoreWithWorkspace(t)

	if err := store.Store(context.Background(), "abc", nil); err == nil {
		t.Fatal("Store() error = nil, want error")
	}
}

func TestStoreCleansOutputPath(t *testing.T) {
	store, workspace := newTestStoreWithWorkspace(t)
	writeTestFile(t, workspace, "app/out.txt", "x")
	fingerprint := strings.Repeat("a", 64)

	err := store.Store(context.Background(), fingerprint, []core.Output{{
		Path: "app/../app/out.txt",
		Mode: core.ModeFile,
	}})
	if err != nil {
		t.Fatalf("Store() error = %v", err)
	}
	e, err := store.readEntry(fingerprint)
	if err != nil {
		t.Fatalf("readEntry() error = %v", err)
	}
	if e.Outputs[0].Path != "app/out.txt" {
		t.Errorf("path = %q, want app/out.txt", e.Outputs[0].Path)
	}
}

func TestStoreRejectsSymlinkOutput(t *testing.T) {
	store, workspace := newTestStoreWithWorkspace(t)
	outside := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(outside, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(workspace, "app", "link.txt")
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink not supported: %v", err)
	}

	err := store.Store(context.Background(), strings.Repeat("a", 64), []core.Output{{
		Path: "app/link.txt",
		Mode: core.ModeFile,
	}})
	if err == nil {
		t.Fatal("Store() error = nil, want error")
	}
}

func TestStoreNoTempFiles(t *testing.T) {
	store, workspace := newTestStoreWithWorkspace(t)
	writeTestFile(t, workspace, "app/out.txt", "content")

	if err := store.Store(context.Background(), strings.Repeat("a", 64), []core.Output{{
		Path: "app/out.txt",
		Mode: core.ModeFile,
	}}); err != nil {
		t.Fatalf("Store() error = %v", err)
	}

	entries, err := os.ReadDir(filepath.Join(store.storeRoot, "tmp"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("temp dir has %d entries, want 0", len(entries))
	}
}

func TestLookupEmpty(t *testing.T) {
	store := newTestStore(t)
	fingerprint := strings.Repeat("a", 64)

	hit, err := store.Lookup(context.Background(), fingerprint, []core.Output{{
		Path: "app/out.txt",
		Mode: core.ModeFile,
	}})
	if err != nil {
		t.Fatalf("Lookup() error = %v", err)
	}
	if hit {
		t.Error("hit = true, want false")
	}
}

func TestLookupFileRestoresDeletedOutput(t *testing.T) {
	store, workspace := newTestStoreWithWorkspace(t)
	path := writeTestFile(t, workspace, "app/out.txt", "content")
	fingerprint := strings.Repeat("a", 64)
	output := core.Output{Path: "app/out.txt", Mode: core.ModeFile}
	mustStore(t, store, fingerprint, output)

	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if !mustLookup(t, store, fingerprint, output) {
		t.Fatal("hit = false, want true")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "content" {
		t.Errorf("content = %q, want content", data)
	}
}

func TestLookupTreeRestoresDeletedOutput(t *testing.T) {
	store, workspace := newTestStoreWithWorkspace(t)
	dir := filepath.Join(workspace, "app/classes")
	writeTestFile(t, workspace, "app/classes/App.class", "bytes")
	fingerprint := strings.Repeat("a", 64)
	output := core.Output{Path: "app/classes", Mode: core.ModeTree}
	mustStore(t, store, fingerprint, output)

	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	if !mustLookup(t, store, fingerprint, output) {
		t.Fatal("hit = false, want true")
	}
	data, err := os.ReadFile(filepath.Join(dir, "App.class"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "bytes" {
		t.Errorf("content = %q, want bytes", data)
	}
}

func TestLookupReplacesModifiedOutput(t *testing.T) {
	store, workspace := newTestStoreWithWorkspace(t)
	path := writeTestFile(t, workspace, "app/out.txt", "content")
	fingerprint := strings.Repeat("a", 64)
	output := core.Output{Path: "app/out.txt", Mode: core.ModeFile}
	mustStore(t, store, fingerprint, output)

	if err := os.WriteFile(path, []byte("other"), 0o600); err != nil {
		t.Fatal(err)
	}
	if !mustLookup(t, store, fingerprint, output) {
		t.Fatal("hit = false, want true")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "content" {
		t.Errorf("content = %q, want content", data)
	}
}

func TestLookupMissingBlob(t *testing.T) {
	store, workspace := newTestStoreWithWorkspace(t)
	writeTestFile(t, workspace, "app/out.txt", "content")
	fingerprint := strings.Repeat("a", 64)
	output := core.Output{Path: "app/out.txt", Mode: core.ModeFile}
	mustStore(t, store, fingerprint, output)

	e, err := store.readEntry(fingerprint)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(store.blobPath(e.Outputs[0].Hash)); err != nil {
		t.Fatal(err)
	}
	if mustLookup(t, store, fingerprint, output) {
		t.Error("hit = true, want false")
	}
}

func TestLookupMissingManifestBlob(t *testing.T) {
	store, workspace := newTestStoreWithWorkspace(t)
	writeTestFile(t, workspace, "app/classes/App.class", "bytes")
	fingerprint := strings.Repeat("a", 64)
	output := core.Output{Path: "app/classes", Mode: core.ModeTree}
	mustStore(t, store, fingerprint, output)

	e, err := store.readEntry(fingerprint)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(store.blobPath(e.Outputs[0].Manifest)); err != nil {
		t.Fatal(err)
	}
	if mustLookup(t, store, fingerprint, output) {
		t.Error("hit = true, want false")
	}
}

func TestLookupMissingTreeFileBlob(t *testing.T) {
	store, workspace := newTestStoreWithWorkspace(t)
	writeTestFile(t, workspace, "app/classes/App.class", "bytes")
	fingerprint := strings.Repeat("a", 64)
	output := core.Output{Path: "app/classes", Mode: core.ModeTree}
	mustStore(t, store, fingerprint, output)

	e, err := store.readEntry(fingerprint)
	if err != nil {
		t.Fatal(err)
	}
	data, err := store.readBlob(e.Outputs[0].Manifest)
	if err != nil {
		t.Fatal(err)
	}
	var m manifest
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	for _, entry := range m.Entries {
		if entry.Type == "file" {
			if err := os.Remove(store.blobPath(entry.Hash)); err != nil {
				t.Fatal(err)
			}
			break
		}
	}
	if mustLookup(t, store, fingerprint, output) {
		t.Error("hit = true, want false")
	}
}

func TestLookupChangedOutputList(t *testing.T) {
	store, workspace := newTestStoreWithWorkspace(t)
	writeTestFile(t, workspace, "app/out.txt", "x")
	fingerprint := strings.Repeat("a", 64)
	mustStore(t, store, fingerprint, core.Output{Path: "app/out.txt", Mode: core.ModeFile})

	hit := mustLookup(t, store, fingerprint, core.Output{Path: "app/other.txt", Mode: core.ModeFile})
	if hit {
		t.Error("hit = true, want false")
	}
}

func TestLookupChangedOutputMode(t *testing.T) {
	store, workspace := newTestStoreWithWorkspace(t)
	writeTestFile(t, workspace, "app/out.txt", "x")
	fingerprint := strings.Repeat("a", 64)
	mustStore(t, store, fingerprint, core.Output{Path: "app/out.txt", Mode: core.ModeFile})

	hit := mustLookup(t, store, fingerprint, core.Output{Path: "app/out.txt", Mode: core.ModeTree})
	if hit {
		t.Error("hit = true, want false")
	}
}

func TestLookupInvalidEntry(t *testing.T) {
	store, _ := newTestStoreWithWorkspace(t)
	fingerprint := strings.Repeat("a", 64)
	path := store.entryPath(fingerprint)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}

	hit, err := store.Lookup(context.Background(), fingerprint, []core.Output{{
		Path: "app/out.txt",
		Mode: core.ModeFile,
	}})
	if err != nil {
		t.Fatalf("Lookup() error = %v", err)
	}
	if hit {
		t.Error("hit = true, want false")
	}
}

func TestLookupUpdatesAccessTime(t *testing.T) {
	store, workspace := newTestStoreWithWorkspace(t)
	writeTestFile(t, workspace, "app/out.txt", "content")
	fingerprint := strings.Repeat("a", 64)
	output := core.Output{Path: "app/out.txt", Mode: core.ModeFile}

	created := time.Date(2026, 11, 3, 10, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return created }
	mustStore(t, store, fingerprint, output)

	accessed := created.Add(time.Hour)
	store.now = func() time.Time { return accessed }
	if !mustLookup(t, store, fingerprint, output) {
		t.Fatal("hit = false, want true")
	}

	e, err := store.readEntry(fingerprint)
	if err != nil {
		t.Fatal(err)
	}
	if !e.Created.Equal(created) {
		t.Errorf("Created = %v, want %v", e.Created, created)
	}
	if !e.Accessed.Equal(accessed) {
		t.Errorf("Accessed = %v, want %v", e.Accessed, accessed)
	}
}

func TestLookupRejectsBadFingerprint(t *testing.T) {
	store := newTestStore(t)

	hit, err := store.Lookup(context.Background(), "abc", nil)
	if err == nil {
		t.Fatal("Lookup() error = nil, want error")
	}
	if hit {
		t.Error("hit = true, want false")
	}
}

func TestLookupContextCanceled(t *testing.T) {
	store := newTestStore(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	hit, err := store.Lookup(ctx, strings.Repeat("a", 64), nil)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("Lookup() error = %v, want context.Canceled", err)
	}
	if hit {
		t.Error("hit = true, want false")
	}
}

func TestStatsEmpty(t *testing.T) {
	store := newTestStore(t)

	stats, err := store.Stats(context.Background())
	if err != nil {
		t.Fatalf("Stats() error = %v", err)
	}
	if stats.Entries != 0 || stats.Blobs != 0 || stats.Size != 0 {
		t.Errorf("stats = %+v, want empty", stats)
	}
	if stats.MaxSize != store.maxSize {
		t.Errorf("MaxSize = %d, want %d", stats.MaxSize, store.maxSize)
	}
}

func TestStatsCountsFileAndTree(t *testing.T) {
	store, workspace := newTestStoreWithWorkspace(t)
	writeTestFile(t, workspace, "app/out.txt", "abc")
	writeTestFile(t, workspace, "app/classes/App.class", "bytes")
	mustStore(t, store, strings.Repeat("a", 64),
		core.Output{Path: "app/out.txt", Mode: core.ModeFile},
		core.Output{Path: "app/classes", Mode: core.ModeTree},
	)

	stats, err := store.Stats(context.Background())
	if err != nil {
		t.Fatalf("Stats() error = %v", err)
	}
	if stats.Entries != 1 {
		t.Errorf("Entries = %d, want 1", stats.Entries)
	}
	if stats.Blobs != 3 {
		t.Errorf("Blobs = %d, want 3", stats.Blobs)
	}
	if stats.Size != casSize(t, store) {
		t.Errorf("Size = %d, want %d", stats.Size, casSize(t, store))
	}
}

func TestStatsDeduplicatesBlobs(t *testing.T) {
	store, workspace := newTestStoreWithWorkspace(t)
	writeTestFile(t, workspace, "app/out.txt", "same")
	output := core.Output{Path: "app/out.txt", Mode: core.ModeFile}
	mustStore(t, store, strings.Repeat("a", 64), output)
	mustStore(t, store, strings.Repeat("b", 64), output)

	stats, err := store.Stats(context.Background())
	if err != nil {
		t.Fatalf("Stats() error = %v", err)
	}
	if stats.Entries != 2 {
		t.Errorf("Entries = %d, want 2", stats.Entries)
	}
	if stats.Blobs != 1 {
		t.Errorf("Blobs = %d, want 1", stats.Blobs)
	}
	if stats.Size != int64(len("same")) {
		t.Errorf("Size = %d, want %d", stats.Size, len("same"))
	}
}

func TestStatsSkipsInvalidEntry(t *testing.T) {
	store, workspace := newTestStoreWithWorkspace(t)
	writeTestFile(t, workspace, "app/out.txt", "x")
	mustStore(t, store, strings.Repeat("a", 64), core.Output{Path: "app/out.txt", Mode: core.ModeFile})

	path := store.entryPath(strings.Repeat("b", 64))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}

	stats, err := store.Stats(context.Background())
	if err != nil {
		t.Fatalf("Stats() error = %v", err)
	}
	if stats.Entries != 1 {
		t.Errorf("Entries = %d, want 1", stats.Entries)
	}
	if stats.Blobs != 1 {
		t.Errorf("Blobs = %d, want 1", stats.Blobs)
	}
}

func TestStatsSkipsMissingBlob(t *testing.T) {
	store, workspace := newTestStoreWithWorkspace(t)
	writeTestFile(t, workspace, "app/out.txt", "x")
	fingerprint := strings.Repeat("a", 64)
	mustStore(t, store, fingerprint, core.Output{Path: "app/out.txt", Mode: core.ModeFile})

	e, err := store.readEntry(fingerprint)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(store.blobPath(e.Outputs[0].Hash)); err != nil {
		t.Fatal(err)
	}

	stats, err := store.Stats(context.Background())
	if err != nil {
		t.Fatalf("Stats() error = %v", err)
	}
	if stats.Entries != 0 || stats.Blobs != 0 || stats.Size != 0 {
		t.Errorf("stats = %+v, want empty", stats)
	}
}

func TestStatsContextCanceled(t *testing.T) {
	store := newTestStore(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := store.Stats(ctx); !errors.Is(err, context.Canceled) {
		t.Errorf("Stats() error = %v, want context.Canceled", err)
	}
}
