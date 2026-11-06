package deps

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/gabriel-aplok/kine-build-java/internal/store"
)

// Artifact is one resolved and materialized dependency.
type Artifact struct {
	Coordinate
	Scope  Scope
	Path   string
	SHA256 string
	Size   int64
}

func materializeArtifacts(workspaceRoot string, cache *store.Store, artifacts []downloadedArtifact) ([]Artifact, error) {
	depsDir := filepath.Join(workspaceRoot, ".kine", "deps")
	if err := os.RemoveAll(depsDir); err != nil {
		return nil, fmt.Errorf("clean deps dir: %w", err)
	}

	result := make([]Artifact, 0, len(artifacts))
	for _, artifact := range artifacts {
		dest := filepath.Join(
			depsDir,
			string(artifact.Scope),
			filepath.FromSlash(strings.ReplaceAll(artifact.Group, ".", "/")),
			artifact.Artifact,
			artifact.Version,
			artifact.Coordinate.FileName(),
		)
		if err := cache.MaterializeBlob(artifact.Blob.Hash, dest, 0o644); err != nil {
			return nil, fmt.Errorf("materialize %s: %w", artifact.Coordinate, err)
		}
		result = append(result, Artifact{
			Coordinate: artifact.Coordinate,
			Scope:      artifact.Scope,
			Path:       dest,
			SHA256:     artifact.Blob.Hash,
			Size:       artifact.Blob.Size,
		})
	}

	sort.Slice(result, func(i, j int) bool {
		if result[i].Scope != result[j].Scope {
			return scopeRank(result[i].Scope) < scopeRank(result[j].Scope)
		}
		a := result[i].Coordinate
		b := result[j].Coordinate
		if a.Group != b.Group {
			return a.Group < b.Group
		}
		if a.Artifact != b.Artifact {
			return a.Artifact < b.Artifact
		}
		if a.Classifier != b.Classifier {
			return a.Classifier < b.Classifier
		}
		return a.Extension < b.Extension
	})
	return result, nil
}
