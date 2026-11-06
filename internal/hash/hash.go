// Package hash computes deterministic SHA-256 digests for files and trees.
package hash

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	stdhash "hash"
	"io"
	"os"
	"path/filepath"
	"sort"
	"sync"
)

const copyBufferSize = 32 * 1024

var bufferPool = sync.Pool{
	New: func() any {
		buf := make([]byte, copyBufferSize)
		return &buf
	},
}

// File returns the SHA-256 of path as lowercase hex.
func File(path string) (string, error) {
	sum := sha256.New()
	if err := hashFile(sum, path); err != nil {
		return "", err
	}
	return hex.EncodeToString(sum.Sum(nil)), nil
}

// Tree returns a deterministic SHA-256 over every entry under root.
// The root directory is not part of the digest.
func Tree(root string) (string, error) {
	info, err := os.Lstat(root)
	if err != nil {
		return "", fmt.Errorf("stat %s: %w", root, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("hash tree %s: not a directory", root)
	}

	var entries []treeEntry
	err = filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == root {
			return nil
		}

		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)

		switch {
		case entry.Type()&os.ModeSymlink != 0:
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			sum := sha256.Sum256([]byte(target))
			entries = append(entries, treeEntry{path: rel, kind: 'l', sum: hex.EncodeToString(sum[:])})
		case entry.IsDir():
			entries = append(entries, treeEntry{path: rel, kind: 'd'})
		case entry.Type().IsRegular():
			sum := sha256.New()
			if err := hashFile(sum, path); err != nil {
				return err
			}
			entries = append(entries, treeEntry{path: rel, kind: 'f', sum: hex.EncodeToString(sum.Sum(nil))})
		default:
			return fmt.Errorf("unsupported file type at %s", path)
		}
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("walk %s: %w", root, err)
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].path < entries[j].path
	})

	sum := sha256.New()
	for _, entry := range entries {
		sum.Write([]byte{entry.kind})
		sum.Write([]byte(entry.path))
		sum.Write([]byte{0})
		sum.Write([]byte(entry.sum))
		sum.Write([]byte{0})
	}
	return hex.EncodeToString(sum.Sum(nil)), nil
}

type treeEntry struct {
	path string
	kind byte
	sum  string
}

func hashFile(sum stdhash.Hash, path string) error {
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	defer file.Close()

	buf := bufferPool.Get().(*[]byte)
	defer bufferPool.Put(buf)

	// os.File implements WriteTo, which ignores the pooled buffer.
	if _, err := io.CopyBuffer(sum, readerOnly{file}, *buf); err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	return nil
}

type readerOnly struct {
	io.Reader
}
