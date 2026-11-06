package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
)

const manifestVersion = 1

type manifestEntry struct {
	Path   string `json:"path"`
	Type   string `json:"type"`
	Hash   string `json:"hash,omitempty"`
	Size   int64  `json:"size,omitempty"`
	Perm   uint32 `json:"perm,omitempty"`
	Target string `json:"target,omitempty"`
}

type manifest struct {
	Version int             `json:"version"`
	Entries []manifestEntry `json:"entries"`
}

type treeRef struct {
	Manifest string
	Size     int64
}

func (s *Store) putTree(root string) (treeRef, error) {
	info, err := os.Lstat(root)
	if err != nil {
		return treeRef{}, fmt.Errorf("stat %s: %w", root, err)
	}
	if !info.IsDir() {
		return treeRef{}, fmt.Errorf("%s: not a directory", root)
	}

	var entries []manifestEntry
	var total int64
	err = filepath.WalkDir(root, func(current string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if current == root {
			return nil
		}

		rel, err := filepath.Rel(root, current)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)

		switch {
		case entry.Type()&os.ModeSymlink != 0:
			target, err := os.Readlink(current)
			if err != nil {
				return err
			}
			entries = append(entries, manifestEntry{Path: rel, Type: "symlink", Target: target})
		case entry.IsDir():
			info, err := entry.Info()
			if err != nil {
				return err
			}
			entries = append(entries, manifestEntry{Path: rel, Type: "dir", Perm: uint32(info.Mode().Perm())})
		case entry.Type().IsRegular():
			ref, err := s.putFile(current)
			if err != nil {
				return err
			}
			total += ref.Size
			entries = append(entries, manifestEntry{
				Path: rel,
				Type: "file",
				Hash: ref.Hash,
				Size: ref.Size,
				Perm: uint32(ref.Perm),
			})
		default:
			return fmt.Errorf("%s: unsupported file type", current)
		}
		return nil
	})
	if err != nil {
		return treeRef{}, fmt.Errorf("walk %s: %w", root, err)
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Path < entries[j].Path
	})

	data, err := json.Marshal(manifest{Version: manifestVersion, Entries: entries})
	if err != nil {
		return treeRef{}, fmt.Errorf("encode manifest: %w", err)
	}
	hash, _, err := s.putBytes(data)
	if err != nil {
		return treeRef{}, err
	}
	return treeRef{Manifest: hash, Size: total}, nil
}

func (s *Store) manifestFileHashes(manifestHash string) ([]string, error) {
	data, err := s.readBlob(manifestHash)
	if err != nil {
		return nil, err
	}

	var m manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("decode manifest %s: %w", manifestHash, err)
	}
	if m.Version != manifestVersion {
		return nil, fmt.Errorf("manifest %s: unsupported version %d", manifestHash, m.Version)
	}

	hashes := make([]string, 0, len(m.Entries))
	for _, entry := range m.Entries {
		if entry.Type == "file" {
			hashes = append(hashes, entry.Hash)
		}
	}
	return hashes, nil
}

func (s *Store) treeBlobsExist(manifestHash string) (bool, error) {
	hashes, err := s.manifestFileHashes(manifestHash)
	if err != nil {
		if errors.Is(err, ErrBlobMissing) {
			return false, nil
		}
		return false, err
	}
	for _, hash := range hashes {
		if !s.blobExists(hash) {
			return false, nil
		}
	}
	return true, nil
}

func (s *Store) materializeTree(hash, dest string) error {
	data, err := s.readBlob(hash)
	if err != nil {
		return err
	}

	var m manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return fmt.Errorf("decode manifest %s: %w", hash, err)
	}
	if m.Version != manifestVersion {
		return fmt.Errorf("manifest %s: unsupported version %d", hash, m.Version)
	}
	if err := validateManifest(dest, m); err != nil {
		return fmt.Errorf("manifest %s: %w", hash, err)
	}

	if err := os.RemoveAll(dest); err != nil {
		return fmt.Errorf("remove %s: %w", dest, err)
	}
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", dest, err)
	}

	for _, entry := range m.Entries {
		if entry.Type == "symlink" {
			continue
		}
		full := filepath.Join(dest, filepath.FromSlash(entry.Path))
		switch entry.Type {
		case "dir":
			perm := os.FileMode(entry.Perm)
			if err := os.MkdirAll(full, perm); err != nil {
				return fmt.Errorf("create dir %s: %w", full, err)
			}
			if err := os.Chmod(full, perm); err != nil {
				return fmt.Errorf("chmod %s: %w", full, err)
			}
		case "file":
			if err := s.materializeFile(entry.Hash, full, os.FileMode(entry.Perm)); err != nil {
				return err
			}
		}
	}

	for _, entry := range m.Entries {
		if entry.Type != "symlink" {
			continue
		}
		full := filepath.Join(dest, filepath.FromSlash(entry.Path))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return fmt.Errorf("create symlink dir: %w", err)
		}
		if err := os.Symlink(entry.Target, full); err != nil {
			return fmt.Errorf("create symlink %s: %w", full, err)
		}
	}
	return nil
}

func validateManifest(dest string, m manifest) error {
	symlinks := make(map[string]struct{})
	for _, entry := range m.Entries {
		if entry.Path == "" {
			return fmt.Errorf("entry path is empty")
		}
		switch entry.Type {
		case "dir", "file", "symlink":
		default:
			return fmt.Errorf("unknown entry type %q", entry.Type)
		}
		if path.IsAbs(entry.Path) || filepath.IsAbs(entry.Path) {
			return fmt.Errorf("path %q is absolute", entry.Path)
		}
		full := filepath.Join(dest, filepath.FromSlash(entry.Path))
		if full == dest || !isWithin(dest, full) {
			return fmt.Errorf("path %q escapes the output tree", entry.Path)
		}
		if entry.Type == "file" {
			if err := validateHash(entry.Hash); err != nil {
				return err
			}
		}
		if entry.Type == "symlink" {
			symlinks[entry.Path] = struct{}{}
		}
	}

	for _, entry := range m.Entries {
		parent := path.Dir(entry.Path)
		for parent != "." && parent != "/" {
			if _, ok := symlinks[parent]; ok {
				return fmt.Errorf("path %q is under symlink %q", entry.Path, parent)
			}
			parent = path.Dir(parent)
		}
	}
	return nil
}
