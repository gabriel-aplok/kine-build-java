// Package store implements Kine's content-addressed build cache.
package store

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gabriel-aplok/kine-build-java/internal/core"
)

// DefaultMaxSize is the default store cap in bytes.
const DefaultMaxSize int64 = 10 << 30

const tempMaxAge = time.Hour

// Options configures a store.
type Options struct {
	MaxSize int64
	Logger  *slog.Logger
}

// Store is a content-addressed task cache bound to one workspace.
type Store struct {
	workspaceRoot string
	storeRoot     string
	maxSize       int64
	logger        *slog.Logger
	now           func() time.Time
	mu            sync.RWMutex
}

// Stats reports cache entries and unique blob usage.
type Stats struct {
	Entries int
	Blobs   int
	Size    int64
	MaxSize int64
}

// DefaultRoot returns the default global store root.
func DefaultRoot() (string, error) {
	root, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("resolve user cache dir: %w", err)
	}
	return filepath.Join(root, "kine"), nil
}

// Open prepares a store for one workspace.
func Open(workspaceRoot, storeRoot string, options Options) (*Store, error) {
	workspace, err := filepath.Abs(workspaceRoot)
	if err != nil {
		return nil, fmt.Errorf("resolve workspace root %s: %w", workspaceRoot, err)
	}
	workspace, err = filepath.EvalSymlinks(workspace)
	if err != nil {
		return nil, fmt.Errorf("resolve workspace root %s: %w", workspaceRoot, err)
	}
	info, err := os.Stat(workspace)
	if err != nil {
		return nil, fmt.Errorf("stat workspace root %s: %w", workspace, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("workspace root %s: not a directory", workspace)
	}

	if storeRoot == "" {
		storeRoot, err = DefaultRoot()
		if err != nil {
			return nil, err
		}
	}
	storeRoot, err = filepath.Abs(storeRoot)
	if err != nil {
		return nil, fmt.Errorf("resolve store root %s: %w", storeRoot, err)
	}
	if err := os.MkdirAll(storeRoot, 0o755); err != nil {
		return nil, fmt.Errorf("create store root %s: %w", storeRoot, err)
	}
	storeRoot, err = filepath.EvalSymlinks(storeRoot)
	if err != nil {
		return nil, fmt.Errorf("resolve store root %s: %w", storeRoot, err)
	}

	for _, name := range []string{"cas", "tasks", "tmp"} {
		path := filepath.Join(storeRoot, name)
		if err := os.MkdirAll(path, 0o755); err != nil {
			return nil, fmt.Errorf("create %s: %w", path, err)
		}
	}

	if options.MaxSize <= 0 {
		options.MaxSize = DefaultMaxSize
	}
	logger := options.Logger
	if logger == nil {
		logger = slog.Default()
	}

	return &Store{
		workspaceRoot: workspace,
		storeRoot:     storeRoot,
		maxSize:       options.MaxSize,
		logger:        logger,
		now:           time.Now,
	}, nil
}

// Store records the outputs of a task under its fingerprint.
func (s *Store) Store(ctx context.Context, fingerprint string, outputs []core.Output) error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if err := validateHash(fingerprint); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	now := s.now()
	e := entry{
		Version:     entryVersion,
		Fingerprint: fingerprint,
		Outputs:     make([]entryOutput, 0, len(outputs)),
		Created:     now,
		Accessed:    now,
	}

	for _, output := range outputs {
		if err := ctx.Err(); err != nil {
			return err
		}
		resolved, err := s.resolveOutput(output)
		if err != nil {
			return err
		}

		switch resolved.mode {
		case "file":
			ref, err := s.putFile(resolved.full)
			if err != nil {
				return fmt.Errorf("store output %q: %w", output.Path, err)
			}
			e.Outputs = append(e.Outputs, entryOutput{
				Path: resolved.path,
				Mode: "file",
				Hash: ref.Hash,
				Size: ref.Size,
				Perm: uint32(ref.Perm),
			})
			e.Size += ref.Size
		case "tree":
			ref, err := s.putTree(resolved.full)
			if err != nil {
				return fmt.Errorf("store output %q: %w", output.Path, err)
			}
			e.Outputs = append(e.Outputs, entryOutput{
				Path:     resolved.path,
				Mode:     "tree",
				Manifest: ref.Manifest,
				Size:     ref.Size,
			})
			e.Size += ref.Size
		}
	}

	return s.writeEntry(e)
}

// Lookup restores the outputs for a fingerprint when a valid entry exists.
func (s *Store) Lookup(ctx context.Context, fingerprint string, outputs []core.Output) (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if err := validateHash(fingerprint); err != nil {
		return false, err
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}

	requested := make([]resolvedOutput, 0, len(outputs))
	for _, output := range outputs {
		resolved, err := s.resolveOutput(output)
		if err != nil {
			return false, err
		}
		requested = append(requested, resolved)
	}
	sort.Slice(requested, func(i, j int) bool {
		if requested[i].path != requested[j].path {
			return requested[i].path < requested[j].path
		}
		return requested[i].mode < requested[j].mode
	})

	e, err := s.readEntry(fingerprint)
	if err != nil {
		if errors.Is(err, ErrEntryMissing) {
			return false, nil
		}
		s.logger.Warn("read cache entry", "fingerprint", fingerprint, "error", err)
		return false, nil
	}

	cached := append([]entryOutput(nil), e.Outputs...)
	sort.Slice(cached, func(i, j int) bool {
		if cached[i].Path != cached[j].Path {
			return cached[i].Path < cached[j].Path
		}
		return cached[i].Mode < cached[j].Mode
	})
	if len(cached) != len(requested) {
		return false, nil
	}
	for i := range cached {
		if cached[i].Path != requested[i].path || cached[i].Mode != requested[i].mode {
			return false, nil
		}
	}

	for _, output := range cached {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		switch output.Mode {
		case "file":
			if !s.blobExists(output.Hash) {
				return false, nil
			}
		case "tree":
			ok, err := s.treeBlobsExist(output.Manifest)
			if err != nil {
				s.logger.Warn("read cache manifest", "fingerprint", fingerprint, "error", err)
				return false, nil
			}
			if !ok {
				return false, nil
			}
		}
	}

	for i, output := range cached {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		switch output.Mode {
		case "file":
			if err := s.materializeFile(output.Hash, requested[i].full, os.FileMode(output.Perm)); err != nil {
				return false, err
			}
		case "tree":
			if err := s.materializeTree(output.Manifest, requested[i].full); err != nil {
				return false, err
			}
		}
	}

	e.Accessed = s.now()
	if err := s.writeEntry(*e); err != nil {
		s.logger.Warn("update cache access time", "fingerprint", fingerprint, "error", err)
	}
	return true, nil
}

// Stats scans valid cache entries and reports unique blob usage.
func (s *Store) Stats(ctx context.Context) (Stats, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if err := ctx.Err(); err != nil {
		return Stats{}, err
	}

	stats := Stats{MaxSize: s.maxSize}
	blobs := make(map[string]int64)
	tasksDir := filepath.Join(s.storeRoot, "tasks")

	err := filepath.WalkDir(tasksDir, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || filepath.Ext(path) != ".json" {
			return nil
		}
		if err := ctx.Err(); err != nil {
			return err
		}

		data, err := os.ReadFile(path)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return fmt.Errorf("read entry %s: %w", path, err)
		}
		e, err := decodeEntry(data)
		if err != nil {
			return nil
		}
		entryBlobs, err := s.entryBlobs(e)
		if err != nil {
			return nil
		}

		stats.Entries++
		for hash, size := range entryBlobs {
			blobs[hash] = size
		}
		return nil
	})
	if err != nil {
		return Stats{}, err
	}

	if err := s.walkPinned(func(hash string, size int64) error {
		if _, ok := blobs[hash]; !ok {
			blobs[hash] = size
		}
		return nil
	}); err != nil {
		return Stats{}, err
	}

	stats.Blobs = len(blobs)
	for _, size := range blobs {
		stats.Size += size
	}
	return stats, nil
}

func (s *Store) walkPinned(fn func(hash string, size int64) error) error {
	casDir := filepath.Join(s.storeRoot, "cas")
	return filepath.WalkDir(casDir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		pinned, err := pinnedBlob(path, info)
		if err != nil {
			return err
		}
		if !pinned {
			return nil
		}
		return fn(filepath.Base(path), info.Size())
	})
}

func (s *Store) entryBlobs(e *entry) (map[string]int64, error) {
	blobs := make(map[string]int64)
	for _, output := range e.Outputs {
		switch output.Mode {
		case "file":
			if err := s.addBlobSize(blobs, output.Hash); err != nil {
				return nil, err
			}
		case "tree":
			if err := s.addBlobSize(blobs, output.Manifest); err != nil {
				return nil, err
			}
			hashes, err := s.manifestFileHashes(output.Manifest)
			if err != nil {
				return nil, err
			}
			for _, hash := range hashes {
				if err := s.addBlobSize(blobs, hash); err != nil {
					return nil, err
				}
			}
		}
	}
	return blobs, nil
}

func (s *Store) addBlobSize(blobs map[string]int64, hash string) error {
	if _, ok := blobs[hash]; ok {
		return nil
	}
	if err := validateHash(hash); err != nil {
		return err
	}
	info, err := os.Stat(s.blobPath(hash))
	if err != nil {
		return fmt.Errorf("stat blob %s: %w", hash, err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("blob %s: not a regular file", hash)
	}
	blobs[hash] = info.Size()
	return nil
}

type cachedEntry struct {
	entry *entry
	path  string
	blobs map[string]int64
}

// Prune removes the oldest entries until unique blob usage fits the cap.
func (s *Store) Prune(ctx context.Context) (Stats, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := ctx.Err(); err != nil {
		return Stats{}, err
	}

	lock, err := lockFile(filepath.Join(s.storeRoot, "lock"), false)
	if err != nil {
		return Stats{}, err
	}
	defer func() {
		if err := lock.Close(); err != nil {
			s.logger.Warn("release store lock", "error", err)
		}
	}()

	entries, refs, sizes, liveSize, err := s.scanEntries(ctx)
	if err != nil {
		return Stats{}, err
	}
	if err := s.walkPinned(func(hash string, size int64) error {
		refs[hash]++
		if _, ok := sizes[hash]; !ok {
			sizes[hash] = size
			liveSize += size
		}
		return nil
	}); err != nil {
		return Stats{}, err
	}

	sort.Slice(entries, func(i, j int) bool {
		if !entries[i].entry.Accessed.Equal(entries[j].entry.Accessed) {
			return entries[i].entry.Accessed.Before(entries[j].entry.Accessed)
		}
		return entries[i].path < entries[j].path
	})

	remaining := len(entries)
	for _, cached := range entries {
		if liveSize <= s.maxSize {
			break
		}
		if err := os.Remove(cached.path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return Stats{}, fmt.Errorf("remove entry %s: %w", cached.path, err)
		}
		remaining--
		for hash := range cached.blobs {
			refs[hash]--
			if refs[hash] == 0 {
				liveSize -= sizes[hash]
			}
		}
	}

	if err := s.removeOrphanBlobs(ctx, refs); err != nil {
		return Stats{}, err
	}
	if err := s.removeStaleTemp(ctx); err != nil {
		return Stats{}, err
	}

	stats := Stats{Entries: remaining, Size: liveSize, MaxSize: s.maxSize}
	for _, count := range refs {
		if count > 0 {
			stats.Blobs++
		}
	}
	return stats, nil
}

func (s *Store) scanEntries(ctx context.Context) ([]cachedEntry, map[string]int, map[string]int64, int64, error) {
	var entries []cachedEntry
	refs := make(map[string]int)
	sizes := make(map[string]int64)
	var liveSize int64

	tasksDir := filepath.Join(s.storeRoot, "tasks")
	err := filepath.WalkDir(tasksDir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() || filepath.Ext(path) != ".json" {
			return nil
		}
		if err := ctx.Err(); err != nil {
			return err
		}

		data, err := os.ReadFile(path)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return fmt.Errorf("read entry %s: %w", path, err)
		}
		e, err := decodeEntry(data)
		if err != nil {
			return removeInvalidEntry(path)
		}
		blobs, err := s.entryBlobs(e)
		if err != nil {
			return removeInvalidEntry(path)
		}

		entries = append(entries, cachedEntry{entry: e, path: path, blobs: blobs})
		for hash, size := range blobs {
			refs[hash]++
			if _, ok := sizes[hash]; !ok {
				sizes[hash] = size
				liveSize += size
			}
		}
		return nil
	})
	if err != nil {
		return nil, nil, nil, 0, err
	}
	return entries, refs, sizes, liveSize, nil
}

func removeInvalidEntry(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("remove invalid entry %s: %w", path, err)
	}
	return nil
}

func (s *Store) removeOrphanBlobs(ctx context.Context, refs map[string]int) error {
	casDir := filepath.Join(s.storeRoot, "cas")
	err := filepath.WalkDir(casDir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if refs[filepath.Base(path)] > 0 {
			return nil
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("remove blob %s: %w", path, err)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("clean blobs: %w", err)
	}
	return nil
}

func (s *Store) removeStaleTemp(ctx context.Context) error {
	cutoff := s.now().Add(-tempMaxAge)
	tmpDir := filepath.Join(s.storeRoot, "tmp")
	err := filepath.WalkDir(tmpDir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if info.ModTime().After(cutoff) {
			return nil
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("remove temp %s: %w", path, err)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("clean temp: %w", err)
	}
	return nil
}

type resolvedOutput struct {
	full string
	path string
	mode string
}

func (s *Store) resolveOutput(output core.Output) (resolvedOutput, error) {
	full, clean, err := s.outputPath(output)
	if err != nil {
		return resolvedOutput{}, err
	}
	switch output.Mode {
	case core.ModeFile:
		return resolvedOutput{full: full, path: clean, mode: "file"}, nil
	case core.ModeTree:
		return resolvedOutput{full: full, path: clean, mode: "tree"}, nil
	default:
		return resolvedOutput{}, fmt.Errorf("output %q: unknown mode %d", output.Path, output.Mode)
	}
}

func (s *Store) outputPath(output core.Output) (string, string, error) {
	if output.Path == "" {
		return "", "", fmt.Errorf("output path is empty")
	}
	if filepath.IsAbs(output.Path) {
		return "", "", fmt.Errorf("output %q must be relative", output.Path)
	}
	clean := filepath.Clean(filepath.FromSlash(output.Path))
	full := filepath.Join(s.workspaceRoot, clean)
	if full == s.workspaceRoot || !isWithin(s.workspaceRoot, full) {
		return "", "", fmt.Errorf("output %q escapes the workspace", output.Path)
	}
	return full, filepath.ToSlash(clean), nil
}

func isWithin(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	if rel == "." {
		return true
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

var _ core.Cache = (*Store)(nil)
