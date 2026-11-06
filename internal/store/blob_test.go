package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPutBlob(t *testing.T) {
	store, _ := newTestStoreWithWorkspace(t)
	source := writeTestFile(t, t.TempDir(), "input.txt", "content")

	blob, err := store.PutBlob(source)
	if err != nil {
		t.Fatalf("PutBlob() error = %v", err)
	}
	if blob.Size != int64(len("content")) {
		t.Errorf("Size = %d, want %d", blob.Size, len("content"))
	}
	if blob.Perm != 0o600 {
		t.Errorf("Perm = %o, want 600", blob.Perm)
	}
	if !store.HasBlob(blob.Hash) {
		t.Error("HasBlob() = false, want true")
	}
	if store.HasBlob(strings.Repeat("0", 64)) {
		t.Error("HasBlob() = true for a missing blob")
	}

	path, err := store.BlobPath(blob.Hash)
	if err != nil {
		t.Fatalf("BlobPath() error = %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "content" {
		t.Errorf("blob = %q, want content", data)
	}
}

func TestPutBlobDeduplicates(t *testing.T) {
	store, _ := newTestStoreWithWorkspace(t)
	first := writeTestFile(t, t.TempDir(), "a.txt", "same")
	second := writeTestFile(t, t.TempDir(), "b.txt", "same")

	firstBlob, err := store.PutBlob(first)
	if err != nil {
		t.Fatalf("PutBlob() error = %v", err)
	}
	secondBlob, err := store.PutBlob(second)
	if err != nil {
		t.Fatalf("PutBlob() error = %v", err)
	}

	if firstBlob.Hash != secondBlob.Hash {
		t.Errorf("hashes differ: %s != %s", firstBlob.Hash, secondBlob.Hash)
	}
	if got := countFiles(t, filepath.Join(store.storeRoot, "cas")); got != 1 {
		t.Errorf("blob count = %d, want 1", got)
	}
}

func TestBlobPathRejectsBadHash(t *testing.T) {
	store, _ := newTestStoreWithWorkspace(t)

	if _, err := store.BlobPath("abc"); err == nil {
		t.Fatal("BlobPath() error = nil, want error")
	}
}

func TestMaterializeBlob(t *testing.T) {
	store, workspace := newTestStoreWithWorkspace(t)
	source := writeTestFile(t, t.TempDir(), "input.txt", "data")
	if err := os.Chmod(source, 0o644); err != nil {
		t.Fatal(err)
	}

	blob, err := store.PutBlob(source)
	if err != nil {
		t.Fatalf("PutBlob() error = %v", err)
	}
	dest := filepath.Join(workspace, "lib", "dep.jar")
	if err := store.MaterializeBlob(blob.Hash, dest, 0o644); err != nil {
		t.Fatalf("MaterializeBlob() error = %v", err)
	}

	path, err := store.BlobPath(blob.Hash)
	if err != nil {
		t.Fatal(err)
	}
	blobInfo, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	destInfo, err := os.Stat(dest)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(blobInfo, destInfo) {
		t.Error("materialized file is not a hardlink to the blob")
	}
	data, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "data" {
		t.Errorf("content = %q, want data", data)
	}
}

func TestMaterializeBlobRejectsEscape(t *testing.T) {
	store, _ := newTestStoreWithWorkspace(t)
	source := writeTestFile(t, t.TempDir(), "input.txt", "x")
	blob, err := store.PutBlob(source)
	if err != nil {
		t.Fatalf("PutBlob() error = %v", err)
	}

	outside := filepath.Join(t.TempDir(), "out.txt")
	if err := store.MaterializeBlob(blob.Hash, outside, blob.Perm); err == nil {
		t.Fatal("MaterializeBlob() error = nil, want error")
	}
}
