package deps

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"

	"github.com/gabriel-aplok/kine-build-java/internal/store"
)

// Options configures dependency resolution.
type Options struct {
	Repositories []Repository
	Update       bool
	Offline      bool
	Workers      int
	Logger       *slog.Logger
	// Attributes selects Gradle Module Metadata variants, for example
	// org.gradle.usage=java-runtime.
	Attributes map[string]string
}

// Result is a resolved dependency set.
type Result struct {
	Artifacts []Artifact
	LockPath  string
	Resolved  bool
}

// Resolve resolves dependencies, materializes them, and maintains kine.lock.
func Resolve(ctx context.Context, workspaceRoot string, cache *store.Store, deps []Dependency, options Options) (*Result, error) {
	if cache == nil {
		return nil, fmt.Errorf("cache is nil")
	}

	root, err := filepath.Abs(workspaceRoot)
	if err != nil {
		return nil, fmt.Errorf("resolve workspace root %s: %w", workspaceRoot, err)
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return nil, fmt.Errorf("resolve workspace root %s: %w", workspaceRoot, err)
	}

	repos := options.Repositories
	if repos == nil {
		repos, err = LoadRepositories(root)
		if err != nil {
			return nil, err
		}
	} else if err := validateRepositories(repos); err != nil {
		return nil, err
	}

	logger := options.Logger
	if logger == nil {
		logger = slog.Default()
	}
	workers := options.Workers
	if workers < 1 {
		workers = 4
	}

	roots := normalizeRoots(deps)
	path := lockPath(root)

	if !options.Update {
		lock, err := readLock(path)
		switch {
		case err == nil && lockReusable(lock, repos, roots):
			return materializeLock(ctx, root, cache, repos, lock, options.Offline, logger)
		case err != nil && !errors.Is(err, fs.ErrNotExist):
			return nil, err
		case options.Offline:
			return nil, fmt.Errorf("offline: no usable lock file")
		}
	} else if options.Offline {
		return nil, fmt.Errorf("offline: update is not allowed")
	}

	tempDir := filepath.Join(root, ".kine", "tmp")
	if err := os.MkdirAll(tempDir, 0o755); err != nil {
		return nil, fmt.Errorf("create temp dir: %w", err)
	}

	resolver := newResolver(repos, cache, logger)
	resolver.tempDir = tempDir
	resolver.attributes = options.Attributes

	resolved, err := resolver.resolveGraph(ctx, roots)
	if err != nil {
		return nil, err
	}
	downloaded, err := resolver.downloadArtifacts(ctx, resolved, workers)
	if err != nil {
		return nil, err
	}
	artifacts, err := materializeArtifacts(root, cache, downloaded)
	if err != nil {
		return nil, err
	}

	lock := buildLock(repos, roots, downloaded)
	if err := writeLock(path, lock); err != nil {
		return nil, err
	}
	return &Result{Artifacts: artifacts, LockPath: path, Resolved: true}, nil
}

func materializeLock(ctx context.Context, root string, cache *store.Store, repos []Repository, lock *lockFile, offline bool, logger *slog.Logger) (*Result, error) {
	tempDir := filepath.Join(root, ".kine", "tmp")
	if err := os.MkdirAll(tempDir, 0o755); err != nil {
		return nil, fmt.Errorf("create temp dir: %w", err)
	}

	downloaded := make([]downloadedArtifact, 0, len(lock.Artifacts))
	for _, locked := range lock.Artifacts {
		coord := Coordinate{
			Group:      locked.Group,
			Artifact:   locked.Artifact,
			Version:    locked.Version,
			Classifier: locked.Classifier,
			Extension:  locked.Extension,
			File:       locked.File,
		}
		blob := store.Blob{Hash: locked.SHA256, Size: locked.Size, Perm: 0o644}
		if !cache.HasBlob(blob.Hash) {
			if offline {
				return nil, fmt.Errorf("offline: artifact %s is not cached", coord)
			}
			resolver := newResolver(repos, cache, logger)
			resolver.tempDir = tempDir
			fetched, err := resolver.downloadArtifact(ctx, resolvedArtifact{
				Coordinate: coord,
				Scope:      Scope(locked.Scope),
			})
			if err != nil {
				return nil, err
			}
			if fetched.Blob.Hash != locked.SHA256 {
				return nil, fmt.Errorf("artifact %s: lock hash mismatch", coord)
			}
			blob = fetched.Blob
		}
		downloaded = append(downloaded, downloadedArtifact{
			resolvedArtifact: resolvedArtifact{Coordinate: coord, Scope: Scope(locked.Scope)},
			Blob:             blob,
			Repository:       locked.Repository,
		})
	}

	artifacts, err := materializeArtifacts(root, cache, downloaded)
	if err != nil {
		return nil, err
	}
	return &Result{Artifacts: artifacts, LockPath: lockPath(root), Resolved: false}, nil
}

func buildLock(repos []Repository, roots []Dependency, downloaded []downloadedArtifact) *lockFile {
	artifacts := make([]lockArtifact, 0, len(downloaded))
	for _, artifact := range downloaded {
		artifacts = append(artifacts, lockArtifact{
			Group:      artifact.Group,
			Artifact:   artifact.Artifact,
			Version:    artifact.Version,
			Classifier: artifact.Classifier,
			Extension:  artifact.Extension,
			File:       artifact.File,
			Scope:      string(artifact.Scope),
			SHA256:     artifact.Blob.Hash,
			Size:       artifact.Blob.Size,
			Repository: artifact.Repository,
		})
	}
	return &lockFile{
		Version:      lockVersion,
		Repositories: repos,
		Roots:        lockRoots(roots),
		Artifacts:    artifacts,
	}
}

func normalizeRoots(deps []Dependency) []Dependency {
	roots := make([]Dependency, len(deps))
	for i, dependency := range deps {
		if dependency.Extension == "" {
			dependency.Extension = "jar"
		}
		if dependency.Scope == "" {
			dependency.Scope = ScopeCompile
		}
		sort.Slice(dependency.Exclusions, func(a, b int) bool {
			left := dependency.Exclusions[a]
			right := dependency.Exclusions[b]
			if left.Group != right.Group {
				return left.Group < right.Group
			}
			return left.Artifact < right.Artifact
		})
		roots[i] = dependency
	}
	return roots
}
