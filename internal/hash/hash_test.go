package hash

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestFileKnownVector(t *testing.T) {
	path := filepath.Join(t.TempDir(), "abc.txt")
	if err := os.WriteFile(path, []byte("abc"), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := File(path)
	if err != nil {
		t.Fatalf("File() error = %v", err)
	}
	const want = "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"
	if got != want {
		t.Errorf("File() = %s, want %s", got, want)
	}
}

func TestFileMissing(t *testing.T) {
	_, err := File(filepath.Join(t.TempDir(), "missing"))
	if err == nil {
		t.Fatal("File() error = nil, want error")
	}
}

func TestTreeDeterministic(t *testing.T) {
	first := writeTree(t, map[string]string{
		"b.txt":     "b",
		"a.txt":     "a",
		"sub/c.txt": "c",
	})
	second := writeTree(t, map[string]string{
		"sub/c.txt": "c",
		"a.txt":     "a",
		"b.txt":     "b",
	})

	firstHash, err := Tree(first)
	if err != nil {
		t.Fatalf("Tree(first) error = %v", err)
	}
	secondHash, err := Tree(second)
	if err != nil {
		t.Fatalf("Tree(second) error = %v", err)
	}
	if firstHash != secondHash {
		t.Errorf("Tree() differs for equal trees: %s != %s", firstHash, secondHash)
	}
}

func TestTreeChangesWithContent(t *testing.T) {
	root := writeTree(t, map[string]string{"a.txt": "one"})

	before, err := Tree(root)
	if err != nil {
		t.Fatalf("Tree() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("two"), 0o600); err != nil {
		t.Fatal(err)
	}
	after, err := Tree(root)
	if err != nil {
		t.Fatalf("Tree() error = %v", err)
	}
	if before == after {
		t.Error("Tree() did not change after content change")
	}
}

func TestTreeDoesNotFollowSymlink(t *testing.T) {
	outside := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(outside, []byte("one"), 0o600); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}

	before, err := Tree(root)
	if err != nil {
		t.Fatalf("Tree() error = %v", err)
	}
	if err := os.WriteFile(outside, []byte("two"), 0o600); err != nil {
		t.Fatal(err)
	}
	after, err := Tree(root)
	if err != nil {
		t.Fatalf("Tree() error = %v", err)
	}
	if before != after {
		t.Error("Tree() followed a symlink and changed with the target content")
	}
}

func TestTreeHashesSymlinkTarget(t *testing.T) {
	rootOne := t.TempDir()
	rootTwo := t.TempDir()
	if err := os.Symlink("/tmp/one", filepath.Join(rootOne, "link")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/tmp/two", filepath.Join(rootTwo, "link")); err != nil {
		t.Fatal(err)
	}

	one, err := Tree(rootOne)
	if err != nil {
		t.Fatalf("Tree(rootOne) error = %v", err)
	}
	two, err := Tree(rootTwo)
	if err != nil {
		t.Fatalf("Tree(rootTwo) error = %v", err)
	}
	if one == two {
		t.Error("Tree() ignored the symlink target")
	}
}

func TestTreeRootMustBeDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "file.txt")
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := Tree(path); err == nil {
		t.Fatal("Tree() error = nil, want error")
	}
}

func TestTreeEmpty(t *testing.T) {
	got, err := Tree(t.TempDir())
	if err != nil {
		t.Fatalf("Tree() error = %v", err)
	}
	sum := sha256.Sum256(nil)
	want := hex.EncodeToString(sum[:])
	if got != want {
		t.Errorf("Tree() = %s, want %s", got, want)
	}
}

func TestTreeLargeFile(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping large file test in short mode")
	}

	root := t.TempDir()
	path := filepath.Join(root, "large.bin")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(64 << 20); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	got, err := Tree(root)
	if err != nil {
		t.Fatalf("Tree() error = %v", err)
	}
	if len(got) != 64 {
		t.Errorf("Tree() length = %d, want 64", len(got))
	}
}

func TestTreeLargeTree(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < 1000; i++ {
		path := filepath.Join(root, fmt.Sprintf("dir-%02d", i%10), fmt.Sprintf("file-%04d.txt", i))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("content"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	first, err := Tree(root)
	if err != nil {
		t.Fatalf("Tree() error = %v", err)
	}
	second, err := Tree(root)
	if err != nil {
		t.Fatalf("Tree() error = %v", err)
	}
	if first != second {
		t.Error("Tree() is not deterministic for a large tree")
	}
}

func BenchmarkHashFile(b *testing.B) {
	path := filepath.Join(b.TempDir(), "input.bin")
	file, err := os.Create(path)
	if err != nil {
		b.Fatal(err)
	}
	if err := file.Truncate(1 << 20); err != nil {
		file.Close()
		b.Fatal(err)
	}
	if err := file.Close(); err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := File(path); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkHashTree(b *testing.B) {
	root := b.TempDir()
	for i := 0; i < 100; i++ {
		path := filepath.Join(root, fmt.Sprintf("file-%03d.txt", i))
		if err := os.WriteFile(path, []byte("content"), 0o600); err != nil {
			b.Fatal(err)
		}
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := Tree(root); err != nil {
			b.Fatal(err)
		}
	}
}

func writeTree(t *testing.T, files map[string]string) string {
	t.Helper()

	root := t.TempDir()
	for name, content := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return root
}
