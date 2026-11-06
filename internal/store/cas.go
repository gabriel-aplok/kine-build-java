package store

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/gabriel-aplok/kine-build-java/internal/fsutil"
	"github.com/gabriel-aplok/kine-build-java/internal/hash"
)

// ErrBlobMissing is returned when a blob is not in the store.
var ErrBlobMissing = errors.New("blob missing")

type blobRef struct {
	Hash string
	Size int64
	Perm os.FileMode
}

func (s *Store) blobPath(hash string) string {
	return filepath.Join(s.storeRoot, "cas", hash[:2], hash)
}

func (s *Store) putFile(path string) (blobRef, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return blobRef{}, fmt.Errorf("stat %s: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return blobRef{}, fmt.Errorf("%s: not a regular file", path)
	}

	sum, err := hash.File(path)
	if err != nil {
		return blobRef{}, err
	}
	ref := blobRef{Hash: sum, Size: info.Size(), Perm: info.Mode().Perm()}
	dest := s.blobPath(sum)

	if _, err := os.Stat(dest); err == nil {
		return ref, nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return blobRef{}, fmt.Errorf("stat %s: %w", dest, err)
	}

	if err := s.writeBlob(dest, func(w io.Writer) error {
		src, err := os.Open(path)
		if err != nil {
			return err
		}
		defer src.Close()
		_, err = io.Copy(w, src)
		return err
	}); err != nil {
		return blobRef{}, err
	}
	if err := os.Chmod(dest, ref.Perm); err != nil {
		return blobRef{}, fmt.Errorf("chmod %s: %w", dest, err)
	}
	return ref, nil
}

func (s *Store) putBytes(data []byte) (string, int64, error) {
	sum := sha256.Sum256(data)
	name := hex.EncodeToString(sum[:])
	dest := s.blobPath(name)

	if _, err := os.Stat(dest); err == nil {
		return name, int64(len(data)), nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return "", 0, fmt.Errorf("stat %s: %w", dest, err)
	}

	if err := s.writeBlob(dest, func(w io.Writer) error {
		_, err := w.Write(data)
		return err
	}); err != nil {
		return "", 0, err
	}
	return name, int64(len(data)), nil
}

func (s *Store) writeBlob(dest string, write func(io.Writer) error) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return fmt.Errorf("create blob dir: %w", err)
	}

	tmp, err := os.CreateTemp(filepath.Join(s.storeRoot, "tmp"), "blob-*")
	if err != nil {
		return fmt.Errorf("create temp blob: %w", err)
	}
	name := tmp.Name()

	if err := write(tmp); err != nil {
		tmp.Close()
		os.Remove(name)
		return fmt.Errorf("write blob: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return fmt.Errorf("close blob: %w", err)
	}
	if err := fsutil.Replace(name, dest); err != nil {
		os.Remove(name)
		return fmt.Errorf("rename blob: %w", err)
	}
	return nil
}

func (s *Store) materializeFile(hash, dest string, perm os.FileMode) error {
	blob := s.blobPath(hash)
	blobInfo, err := os.Stat(blob)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("%s: %w", hash, ErrBlobMissing)
		}
		return fmt.Errorf("stat blob %s: %w", blob, err)
	}

	if destInfo, err := os.Stat(dest); err == nil {
		if os.SameFile(blobInfo, destInfo) && destInfo.Mode().Perm() == perm {
			return nil
		}
		if err := os.Remove(dest); err != nil {
			return fmt.Errorf("remove %s: %w", dest, err)
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("stat %s: %w", dest, err)
	}

	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return fmt.Errorf("create output dir: %w", err)
	}

	if blobInfo.Mode().Perm() == perm {
		if err := os.Link(blob, dest); err == nil {
			return nil
		}
	}

	src, err := os.Open(blob)
	if err != nil {
		return fmt.Errorf("open blob %s: %w", blob, err)
	}
	defer src.Close()

	out, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm)
	if err != nil {
		return fmt.Errorf("create %s: %w", dest, err)
	}
	if _, err := io.Copy(out, src); err != nil {
		out.Close()
		os.Remove(dest)
		return fmt.Errorf("copy %s: %w", dest, err)
	}
	if err := out.Close(); err != nil {
		os.Remove(dest)
		return fmt.Errorf("close %s: %w", dest, err)
	}
	if err := os.Chmod(dest, perm); err != nil {
		return fmt.Errorf("chmod %s: %w", dest, err)
	}
	return nil
}

func (s *Store) readBlob(hash string) ([]byte, error) {
	if err := validateHash(hash); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(s.blobPath(hash))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("%s: %w", hash, ErrBlobMissing)
		}
		return nil, fmt.Errorf("read blob %s: %w", hash, err)
	}
	return data, nil
}

func (s *Store) blobExists(hash string) bool {
	if validateHash(hash) != nil {
		return false
	}
	info, err := os.Stat(s.blobPath(hash))
	return err == nil && info.Mode().IsRegular()
}

func validateHash(value string) error {
	if len(value) != sha256.Size*2 {
		return fmt.Errorf("invalid hash %q", value)
	}
	if _, err := hex.DecodeString(value); err != nil {
		return fmt.Errorf("invalid hash %q: %w", value, err)
	}
	return nil
}

// Blob describes a content-addressed file.
type Blob struct {
	Hash string
	Size int64
	Perm os.FileMode
}

// PutBlob hashes a file and stores it in the CAS.
func (s *Store) PutBlob(path string) (Blob, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	ref, err := s.putFile(path)
	if err != nil {
		return Blob{}, err
	}
	return Blob{Hash: ref.Hash, Size: ref.Size, Perm: ref.Perm}, nil
}

// PutBytes stores an in-memory blob.
func (s *Store) PutBytes(data []byte) (Blob, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	hash, size, err := s.putBytes(data)
	if err != nil {
		return Blob{}, err
	}
	return Blob{Hash: hash, Size: size, Perm: 0o644}, nil
}

// BlobPath returns the CAS path for a hash.
func (s *Store) BlobPath(hash string) (string, error) {
	if err := validateHash(hash); err != nil {
		return "", err
	}
	return s.blobPath(hash), nil
}

// MaterializeBlob writes a blob to a destination inside the workspace.
func (s *Store) MaterializeBlob(hash, dest string, perm os.FileMode) error {
	if !isWithin(s.workspaceRoot, dest) {
		return fmt.Errorf("destination %q escapes the workspace", dest)
	}
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.materializeFile(hash, dest, perm)
}

// HasBlob reports whether a blob exists.
func (s *Store) HasBlob(hash string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.blobExists(hash)
}
