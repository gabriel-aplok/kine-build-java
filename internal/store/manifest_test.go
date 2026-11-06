package store

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPutTreeDeterministic(t *testing.T) {
	store := newTestStore(t)
	root := t.TempDir()
	writeTestFile(t, root, "a.txt", "a")
	writeTestFile(t, root, "sub/b.txt", "bb")

	first, err := store.putTree(root)
	if err != nil {
		t.Fatalf("putTree() error = %v", err)
	}
	second, err := store.putTree(root)
	if err != nil {
		t.Fatalf("putTree() error = %v", err)
	}
	if first.Manifest != second.Manifest {
		t.Errorf("manifest hashes differ: %s != %s", first.Manifest, second.Manifest)
	}
}

func TestPutTreeManifest(t *testing.T) {
	store := newTestStore(t)
	root := t.TempDir()
	writeTestFile(t, root, "a.txt", "a")
	writeTestFile(t, root, "sub/b.txt", "bb")
	if err := os.Chmod(filepath.Join(root, "a.txt"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "empty"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("a.txt", filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}

	ref, err := store.putTree(root)
	if err != nil {
		t.Fatalf("putTree() error = %v", err)
	}
	data, err := store.readBlob(ref.Manifest)
	if err != nil {
		t.Fatalf("readBlob() error = %v", err)
	}
	var m manifest
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	if m.Version != manifestVersion {
		t.Errorf("version = %d, want %d", m.Version, manifestVersion)
	}

	entries := make(map[string]manifestEntry, len(m.Entries))
	for _, entry := range m.Entries {
		entries[entry.Path] = entry
	}
	if entries["a.txt"].Type != "file" {
		t.Errorf("a.txt type = %q, want file", entries["a.txt"].Type)
	}
	if entries["a.txt"].Perm != 0o644 {
		t.Errorf("a.txt perm = %o, want 644", entries["a.txt"].Perm)
	}
	if entries["sub"].Type != "dir" {
		t.Errorf("sub type = %q, want dir", entries["sub"].Type)
	}
	if entries["sub/b.txt"].Size != 2 {
		t.Errorf("sub/b.txt size = %d, want 2", entries["sub/b.txt"].Size)
	}
	if entries["empty"].Type != "dir" {
		t.Errorf("empty type = %q, want dir", entries["empty"].Type)
	}
	if entries["link"].Type != "symlink" {
		t.Errorf("link type = %q, want symlink", entries["link"].Type)
	}
	if entries["link"].Target != "a.txt" {
		t.Errorf("link target = %q, want a.txt", entries["link"].Target)
	}
	if ref.Size != 3 {
		t.Errorf("tree size = %d, want 3", ref.Size)
	}
}

func TestMaterializeTree(t *testing.T) {
	store := newTestStore(t)
	root := t.TempDir()
	writeTestFile(t, root, "a.txt", "alpha")
	writeTestFile(t, root, "sub/b.txt", "beta")
	if err := os.Chmod(filepath.Join(root, "a.txt"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "empty"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("a.txt", filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}

	ref, err := store.putTree(root)
	if err != nil {
		t.Fatalf("putTree() error = %v", err)
	}
	dest := filepath.Join(t.TempDir(), "restored")
	if err := store.materializeTree(ref.Manifest, dest); err != nil {
		t.Fatalf("materializeTree() error = %v", err)
	}

	for name, want := range map[string]string{
		"a.txt":     "alpha",
		"sub/b.txt": "beta",
	} {
		data, err := os.ReadFile(filepath.Join(dest, filepath.FromSlash(name)))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if string(data) != want {
			t.Errorf("%s = %q, want %q", name, data, want)
		}
	}

	info, err := os.Lstat(filepath.Join(dest, "link"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Error("link is not a symlink")
	}
	target, err := os.Readlink(filepath.Join(dest, "link"))
	if err != nil {
		t.Fatal(err)
	}
	if target != "a.txt" {
		t.Errorf("link target = %q, want a.txt", target)
	}
	if _, err := os.Stat(filepath.Join(dest, "empty")); err != nil {
		t.Errorf("empty dir missing: %v", err)
	}
	info, err = os.Stat(filepath.Join(dest, "a.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o644 {
		t.Errorf("a.txt mode = %o, want 644", info.Mode().Perm())
	}
}

func TestMaterializeTreeReplacesExisting(t *testing.T) {
	store := newTestStore(t)
	root := t.TempDir()
	writeTestFile(t, root, "a.txt", "a")
	ref, err := store.putTree(root)
	if err != nil {
		t.Fatalf("putTree() error = %v", err)
	}

	dest := filepath.Join(t.TempDir(), "restored")
	writeTestFile(t, dest, "stale.txt", "stale")
	if err := store.materializeTree(ref.Manifest, dest); err != nil {
		t.Fatalf("materializeTree() error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, "stale.txt")); !os.IsNotExist(err) {
		t.Errorf("stale file still exists: %v", err)
	}
}

func TestMaterializeTreeMissingManifest(t *testing.T) {
	store := newTestStore(t)

	err := store.materializeTree(strings.Repeat("0", 64), filepath.Join(t.TempDir(), "restored"))
	if !errors.Is(err, ErrBlobMissing) {
		t.Errorf("materializeTree() error = %v, want ErrBlobMissing", err)
	}
}

func TestPutTreeRejectsFile(t *testing.T) {
	store := newTestStore(t)
	path := writeTestFile(t, t.TempDir(), "file.txt", "x")

	if _, err := store.putTree(path); err == nil {
		t.Fatal("putTree() error = nil, want error")
	}
}

func TestMaterializeTreeRejectsEscape(t *testing.T) {
	store := newTestStore(t)
	data, err := json.Marshal(manifest{
		Version: manifestVersion,
		Entries: []manifestEntry{{
			Path: "../escape.txt",
			Type: "file",
			Hash: strings.Repeat("0", 64),
			Perm: 0o644,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	hash, _, err := store.putBytes(data)
	if err != nil {
		t.Fatal(err)
	}

	err = store.materializeTree(hash, filepath.Join(t.TempDir(), "restored"))
	if err == nil {
		t.Fatal("materializeTree() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "escape") {
		t.Errorf("error %q does not mention escape", err)
	}
}

func TestMaterializeTreeRejectsSymlinkParent(t *testing.T) {
	store := newTestStore(t)
	data, err := json.Marshal(manifest{
		Version: manifestVersion,
		Entries: []manifestEntry{
			{Path: "link", Type: "symlink", Target: "/tmp"},
			{Path: "link/escape.txt", Type: "file", Hash: strings.Repeat("0", 64), Perm: 0o644},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	hash, _, err := store.putBytes(data)
	if err != nil {
		t.Fatal(err)
	}

	err = store.materializeTree(hash, filepath.Join(t.TempDir(), "restored"))
	if err == nil {
		t.Fatal("materializeTree() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "symlink") {
		t.Errorf("error %q does not mention symlink", err)
	}
}
