package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/gabriel-aplok/kine-build-java/internal/fsutil"
)

const entryVersion = 1

// ErrEntryMissing is returned when a cache entry is not on disk.
var ErrEntryMissing = errors.New("entry missing")

type entry struct {
	Version     int           `json:"version"`
	Fingerprint string        `json:"fingerprint"`
	Outputs     []entryOutput `json:"outputs"`
	Size        int64         `json:"size"`
	Created     time.Time     `json:"created"`
	Accessed    time.Time     `json:"accessed"`
}

type entryOutput struct {
	Path     string `json:"path"`
	Mode     string `json:"mode"`
	Hash     string `json:"hash,omitempty"`
	Manifest string `json:"manifest,omitempty"`
	Size     int64  `json:"size,omitempty"`
	Perm     uint32 `json:"perm,omitempty"`
}

func (s *Store) entryPath(fingerprint string) string {
	return filepath.Join(s.storeRoot, "tasks", fingerprint[:2], fingerprint+".json")
}

func (s *Store) writeEntry(e entry) error {
	if err := validateEntry(e); err != nil {
		return err
	}
	data, err := json.Marshal(e)
	if err != nil {
		return fmt.Errorf("encode entry: %w", err)
	}

	path := s.entryPath(e.Fingerprint)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create entry dir: %w", err)
	}

	tmp, err := os.CreateTemp(filepath.Join(s.storeRoot, "tmp"), "entry-*")
	if err != nil {
		return fmt.Errorf("create temp entry: %w", err)
	}
	name := tmp.Name()

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(name)
		return fmt.Errorf("write entry: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return fmt.Errorf("close entry: %w", err)
	}
	if err := fsutil.Replace(name, path); err != nil {
		os.Remove(name)
		return fmt.Errorf("rename entry: %w", err)
	}
	return nil
}

func (s *Store) readEntry(fingerprint string) (*entry, error) {
	if err := validateHash(fingerprint); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(s.entryPath(fingerprint))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, ErrEntryMissing
		}
		return nil, fmt.Errorf("read entry %s: %w", fingerprint, err)
	}

	e, err := decodeEntry(data)
	if err != nil {
		return nil, err
	}
	if e.Fingerprint != fingerprint {
		return nil, fmt.Errorf("entry %s: fingerprint mismatch %s", fingerprint, e.Fingerprint)
	}
	return e, nil
}

func decodeEntry(data []byte) (*entry, error) {
	var e entry
	if err := json.Unmarshal(data, &e); err != nil {
		return nil, fmt.Errorf("decode entry: %w", err)
	}
	if err := validateEntry(e); err != nil {
		return nil, err
	}
	return &e, nil
}

func validateEntry(e entry) error {
	if e.Version != entryVersion {
		return fmt.Errorf("entry version %d: want %d", e.Version, entryVersion)
	}
	if err := validateHash(e.Fingerprint); err != nil {
		return err
	}
	for _, output := range e.Outputs {
		if output.Path == "" {
			return fmt.Errorf("output path is empty")
		}
		switch output.Mode {
		case "file":
			if err := validateHash(output.Hash); err != nil {
				return fmt.Errorf("output %q: %w", output.Path, err)
			}
		case "tree":
			if err := validateHash(output.Manifest); err != nil {
				return fmt.Errorf("output %q: %w", output.Path, err)
			}
		default:
			return fmt.Errorf("output %q: unknown mode %q", output.Path, output.Mode)
		}
	}
	return nil
}
