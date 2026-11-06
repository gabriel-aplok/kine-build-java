package store

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPutFileDeduplicates(t *testing.T) {
	store := newTestStore(t)
	dir := t.TempDir()
	first := writeTestFile(t, dir, "a.txt", "same")
	second := writeTestFile(t, dir, "b.txt", "same")

	firstRef, err := store.putFile(first)
	if err != nil {
		t.Fatalf("putFile() error = %v", err)
	}
	secondRef, err := store.putFile(second)
	if err != nil {
		t.Fatalf("putFile() error = %v", err)
	}

	if firstRef.Hash != secondRef.Hash {
		t.Errorf("hashes differ: %s != %s", firstRef.Hash, secondRef.Hash)
	}
	if got := countFiles(t, filepath.Join(store.storeRoot, "cas")); got != 1 {
		t.Errorf("blob count = %d, want 1", got)
	}
}

func TestPutFileStoresContent(t *testing.T) {
	store := newTestStore(t)
	path := writeTestFile(t, t.TempDir(), "input.txt", "content")

	ref, err := store.putFile(path)
	if err != nil {
		t.Fatalf("putFile() error = %v", err)
	}
	data, err := os.ReadFile(store.blobPath(ref.Hash))
	if err != nil {
		t.Fatalf("read blob: %v", err)
	}
	if string(data) != "content" {
		t.Errorf("blob = %q, want content", data)
	}
	if ref.Size != int64(len("content")) {
		t.Errorf("Size = %d, want %d", ref.Size, len("content"))
	}
}

func TestPutFileRejectsDirectory(t *testing.T) {
	store := newTestStore(t)

	if _, err := store.putFile(t.TempDir()); err == nil {
		t.Fatal("putFile() error = nil, want error")
	}
}

func TestPutBytes(t *testing.T) {
	store := newTestStore(t)

	hash, size, err := store.putBytes([]byte("manifest"))
	if err != nil {
		t.Fatalf("putBytes() error = %v", err)
	}
	data, err := os.ReadFile(store.blobPath(hash))
	if err != nil {
		t.Fatalf("read blob: %v", err)
	}
	if string(data) != "manifest" {
		t.Errorf("blob = %q, want manifest", data)
	}
	if size != int64(len("manifest")) {
		t.Errorf("size = %d, want %d", size, len("manifest"))
	}
}

func TestMaterializeFileHardlinks(t *testing.T) {
	store := newTestStore(t)
	source := writeTestFile(t, t.TempDir(), "in.txt", "data")

	ref, err := store.putFile(source)
	if err != nil {
		t.Fatalf("putFile() error = %v", err)
	}
	dest := filepath.Join(t.TempDir(), "out.txt")
	if err := store.materializeFile(ref.Hash, dest, ref.Perm); err != nil {
		t.Fatalf("materializeFile() error = %v", err)
	}

	blobInfo, err := os.Stat(store.blobPath(ref.Hash))
	if err != nil {
		t.Fatal(err)
	}
	destInfo, err := os.Stat(dest)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(blobInfo, destInfo) {
		t.Error("destination is not a hardlink to the blob")
	}
}

func TestMaterializeFileHardlinksNormalMode(t *testing.T) {
	store := newTestStore(t)
	source := writeTestFile(t, t.TempDir(), "in.txt", "data")
	if err := os.Chmod(source, 0o644); err != nil {
		t.Fatal(err)
	}

	ref, err := store.putFile(source)
	if err != nil {
		t.Fatalf("putFile() error = %v", err)
	}
	dest := filepath.Join(t.TempDir(), "out.txt")
	if err := store.materializeFile(ref.Hash, dest, 0o644); err != nil {
		t.Fatalf("materializeFile() error = %v", err)
	}

	blobInfo, err := os.Stat(store.blobPath(ref.Hash))
	if err != nil {
		t.Fatal(err)
	}
	destInfo, err := os.Stat(dest)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(blobInfo, destInfo) {
		t.Error("destination is not a hardlink for a normal file mode")
	}
}

func TestMaterializeFileCopiesWhenModeDiffers(t *testing.T) {
	store := newTestStore(t)
	source := writeTestFile(t, t.TempDir(), "in.txt", "data")

	ref, err := store.putFile(source)
	if err != nil {
		t.Fatalf("putFile() error = %v", err)
	}
	dest := filepath.Join(t.TempDir(), "out.txt")
	if err := store.materializeFile(ref.Hash, dest, 0o644); err != nil {
		t.Fatalf("materializeFile() error = %v", err)
	}

	blobInfo, err := os.Stat(store.blobPath(ref.Hash))
	if err != nil {
		t.Fatal(err)
	}
	destInfo, err := os.Stat(dest)
	if err != nil {
		t.Fatal(err)
	}
	if os.SameFile(blobInfo, destInfo) {
		t.Error("destination shares an inode with the blob, want a copy")
	}
	if destInfo.Mode().Perm() != 0o644 {
		t.Errorf("mode = %v, want 0644", destInfo.Mode().Perm())
	}
}

func TestMaterializeFileReplacesExisting(t *testing.T) {
	store := newTestStore(t)
	source := writeTestFile(t, t.TempDir(), "in.txt", "new")
	ref, err := store.putFile(source)
	if err != nil {
		t.Fatalf("putFile() error = %v", err)
	}

	dest := filepath.Join(t.TempDir(), "out.txt")
	if err := os.WriteFile(dest, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := store.materializeFile(ref.Hash, dest, ref.Perm); err != nil {
		t.Fatalf("materializeFile() error = %v", err)
	}
	data, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "new" {
		t.Errorf("destination = %q, want new", data)
	}
}

func TestMaterializeFileCreatesParent(t *testing.T) {
	store := newTestStore(t)
	source := writeTestFile(t, t.TempDir(), "in.txt", "data")
	ref, err := store.putFile(source)
	if err != nil {
		t.Fatalf("putFile() error = %v", err)
	}

	dest := filepath.Join(t.TempDir(), "a", "b", "out.txt")
	if err := store.materializeFile(ref.Hash, dest, ref.Perm); err != nil {
		t.Fatalf("materializeFile() error = %v", err)
	}
	if _, err := os.Stat(dest); err != nil {
		t.Fatalf("stat destination: %v", err)
	}
}

func TestMaterializeFileMissingBlob(t *testing.T) {
	store := newTestStore(t)
	missing := strings.Repeat("0", 64)

	err := store.materializeFile(missing, filepath.Join(t.TempDir(), "out.txt"), 0o600)
	if !errors.Is(err, ErrBlobMissing) {
		t.Errorf("materializeFile() error = %v, want ErrBlobMissing", err)
	}
}

func TestNoTempFiles(t *testing.T) {
	store := newTestStore(t)
	path := writeTestFile(t, t.TempDir(), "input.txt", "content")

	if _, err := store.putFile(path); err != nil {
		t.Fatalf("putFile() error = %v", err)
	}
	if _, _, err := store.putBytes([]byte("manifest")); err != nil {
		t.Fatalf("putBytes() error = %v", err)
	}

	entries, err := os.ReadDir(filepath.Join(store.storeRoot, "tmp"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("temp dir has %d entries, want 0", len(entries))
	}
}
