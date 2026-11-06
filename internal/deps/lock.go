package deps

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"

	"github.com/gabriel-aplok/kine-build-java/internal/fsutil"
)

const lockVersion = 1

type lockFile struct {
	Version      int            `json:"version"`
	Repositories []Repository   `json:"repositories"`
	Roots        []lockRoot     `json:"roots"`
	Artifacts    []lockArtifact `json:"artifacts"`
}

type lockRoot struct {
	Group      string      `json:"group"`
	Artifact   string      `json:"artifact"`
	Version    string      `json:"version"`
	Classifier string      `json:"classifier"`
	Extension  string      `json:"extension"`
	Scope      string      `json:"scope"`
	Exclusions []Exclusion `json:"exclusions,omitempty"`
}

type lockArtifact struct {
	Group      string `json:"group"`
	Artifact   string `json:"artifact"`
	Version    string `json:"version"`
	Classifier string `json:"classifier"`
	Extension  string `json:"extension"`
	File       string `json:"file,omitempty"`
	Scope      string `json:"scope"`
	SHA256     string `json:"sha256"`
	Size       int64  `json:"size"`
	Repository string `json:"repository"`
}

func lockPath(workspaceRoot string) string {
	return filepath.Join(workspaceRoot, "kine.lock")
}

func readLock(path string) (*lockFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var lock lockFile
	if err := json.Unmarshal(data, &lock); err != nil {
		return nil, fmt.Errorf("decode %s: %w", path, err)
	}
	if lock.Version != lockVersion {
		return nil, fmt.Errorf("%s: unsupported lock version %d", path, lock.Version)
	}
	return &lock, nil
}

func writeLock(path string, lock *lockFile) error {
	lock.Roots = sortedLockRoots(lock.Roots)
	sort.Slice(lock.Artifacts, func(i, j int) bool {
		return lockArtifactKey(lock.Artifacts[i]) < lockArtifactKey(lock.Artifacts[j])
	})

	data, err := json.MarshalIndent(lock, "", "  ")
	if err != nil {
		return fmt.Errorf("encode lock: %w", err)
	}
	data = append(data, '\n')

	tmp, err := os.CreateTemp(filepath.Dir(path), ".kine-lock-*")
	if err != nil {
		return fmt.Errorf("create lock temp: %w", err)
	}
	name := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(name)
		return fmt.Errorf("write lock: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return fmt.Errorf("close lock: %w", err)
	}
	if err := fsutil.Replace(name, path); err != nil {
		os.Remove(name)
		return fmt.Errorf("rename lock: %w", err)
	}
	return nil
}

func lockReusable(lock *lockFile, repos []Repository, roots []Dependency) bool {
	if lock.Version != lockVersion {
		return false
	}
	if !reflect.DeepEqual(lock.Repositories, repos) {
		return false
	}
	return reflect.DeepEqual(lock.Roots, lockRoots(roots))
}

func lockRoots(deps []Dependency) []lockRoot {
	roots := make([]lockRoot, 0, len(deps))
	for _, dependency := range deps {
		roots = append(roots, lockRoot{
			Group:      dependency.Group,
			Artifact:   dependency.Artifact,
			Version:    dependency.Version,
			Classifier: dependency.Classifier,
			Extension:  dependency.Extension,
			Scope:      string(dependency.Scope),
			Exclusions: dependency.Exclusions,
		})
	}
	return sortedLockRoots(roots)
}

func sortedLockRoots(roots []lockRoot) []lockRoot {
	sort.Slice(roots, func(i, j int) bool {
		return lockRootKey(roots[i]) < lockRootKey(roots[j])
	})
	for i := range roots {
		sort.Slice(roots[i].Exclusions, func(a, b int) bool {
			left := roots[i].Exclusions[a]
			right := roots[i].Exclusions[b]
			if left.Group != right.Group {
				return left.Group < right.Group
			}
			return left.Artifact < right.Artifact
		})
	}
	return roots
}

func lockRootKey(root lockRoot) string {
	return root.Group + ":" + root.Artifact + ":" + root.Version + ":" + root.Classifier + ":" + root.Extension + ":" + root.Scope
}

func lockArtifactKey(artifact lockArtifact) string {
	return artifact.Group + ":" + artifact.Artifact + ":" + artifact.Version + ":" + artifact.Classifier + ":" + artifact.Extension + ":" + artifact.Scope
}
