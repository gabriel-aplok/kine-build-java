package deps

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

type moduleMetadata struct {
	FormatVersion string          `json:"formatVersion"`
	Variants      []moduleVariant `json:"variants"`
}

type moduleVariant struct {
	Name                  string             `json:"name"`
	Attributes            map[string]any     `json:"attributes"`
	Dependencies          []moduleDependency `json:"dependencies"`
	DependencyConstraints []moduleDependency `json:"dependencyConstraints"`
	Files                 []moduleFile       `json:"files"`
	AvailableAt           *moduleAvailableAt `json:"available-at"`
}

type moduleDependency struct {
	Group   string        `json:"group"`
	Module  string        `json:"module"`
	Version moduleVersion `json:"version"`
}

type moduleVersion struct {
	Requires string   `json:"requires"`
	Strictly string   `json:"strictly"`
	Prefers  string   `json:"prefers"`
	Rejects  []string `json:"rejects"`
}

func (v moduleVersion) value() string {
	for _, value := range []string{v.Strictly, v.Requires, v.Prefers} {
		if value != "" {
			return value
		}
	}
	return ""
}

type moduleFile struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

type moduleAvailableAt struct {
	Group   string `json:"group"`
	Module  string `json:"module"`
	Version string `json:"version"`
}

func (r *resolver) loadModuleMetadata(ctx context.Context, coord Coordinate) (*moduleMetadata, error) {
	key := coord.Group + ":" + coord.Artifact + ":" + coord.Version
	if cached, ok := r.modules[key]; ok {
		return cached, nil
	}
	path := strings.ReplaceAll(coord.Group, ".", "/") + "/" + coord.Artifact + "/" +
		coord.Version + "/" + coord.Artifact + "-" + coord.Version + ".module"
	for _, repo := range r.repos {
		data, err := r.client.fetchBytes(ctx, repo, path)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				continue
			}
			return nil, fmt.Errorf("module metadata %s from %s: %w", coord, repo.ID, err)
		}
		var metadata moduleMetadata
		if err := json.Unmarshal(data, &metadata); err != nil {
			return nil, fmt.Errorf("module metadata %s: %w", coord, err)
		}
		if len(metadata.Variants) == 0 {
			return nil, nil
		}
		r.modules[key] = &metadata
		return &metadata, nil
	}
	r.modules[key] = nil
	return nil, nil
}

func (r *resolver) selectModuleVariant(metadata *moduleMetadata) *moduleVariant {
	var best *moduleVariant
	bestScore := 0
	for index := range metadata.Variants {
		variant := &metadata.Variants[index]
		score, ok := scoreVariant(variant, r.attributes)
		if !ok {
			continue
		}
		if best == nil || score > bestScore {
			best = variant
			bestScore = score
		}
	}
	return best
}

func scoreVariant(variant *moduleVariant, wanted map[string]string) (int, bool) {
	attributes := variant.Attributes
	if attributes == nil {
		attributes = map[string]any{}
	}
	attr := func(key string) string {
		value, ok := attributes[key]
		if !ok || value == nil {
			return ""
		}
		return fmt.Sprint(value)
	}
	category := attr("org.gradle.category")
	if category == "documentation" || category == "platform" || category == "enforced-platform" {
		return 0, false
	}
	usage := attr("org.gradle.usage")
	if usage == "kotlin-metadata" || usage == "c++-metadata" {
		return 0, false
	}
	platform := attr("org.jetbrains.kotlin.platform.type")
	switch platform {
	case "common", "js", "native", "wasm":
		return 0, false
	}

	score := 0
	for key, value := range wanted {
		if _, ok := attributes[key]; ok {
			if attr(key) == value {
				score += 20
			} else {
				score -= 10
			}
		}
	}
	switch usage {
	case "java-runtime":
		score += 8
	case "java-api":
		score += 4
	case "":
	default:
		score -= 2
	}
	switch platform {
	case "androidJvm":
		score += 6
	case "jvm":
		score += 2
	}
	switch attr("com.android.build.api.attributes.BuildTypeAttr") {
	case "release":
		score += 3
	case "debug":
		score += 1
	}
	if len(variant.Files) > 0 || variant.AvailableAt != nil {
		score += 1
	}
	return score, true
}

func moduleArtifact(variant *moduleVariant, coord Coordinate) (Coordinate, bool) {
	bestRank := -1
	artifact := coord
	for _, file := range variant.Files {
		name := file.Name
		if name == "" {
			name = filepath.Base(file.URL)
		}
		extension := strings.TrimPrefix(strings.ToLower(filepath.Ext(name)), ".")
		rank := -1
		switch extension {
		case "aar":
			rank = 3
		case "jar":
			rank = 2
		case "zip":
			rank = 1
		}
		if rank <= bestRank {
			continue
		}
		bestRank = rank
		artifact.Extension = extension
		artifact.File = name
	}
	return artifact, bestRank > 0
}

func (r *resolver) recordVariantConstraints(variant *moduleVariant) {
	for _, constraint := range variant.DependencyConstraints {
		r.recordConstraint(constraint.Group+":"+constraint.Module, constraint.Version)
	}
	for _, dependency := range variant.Dependencies {
		if dependency.Version.Strictly != "" || len(dependency.Version.Rejects) > 0 {
			r.recordConstraint(dependency.Group+":"+dependency.Module, dependency.Version)
		}
	}
}

func (r *resolver) moduleDependencies(variant *moduleVariant) ([]Dependency, error) {
	constraints := make(map[string]string, len(variant.DependencyConstraints))
	for _, constraint := range variant.DependencyConstraints {
		if value := constraint.Version.value(); value != "" {
			constraints[constraint.Group+":"+constraint.Module] = value
		}
	}
	result := make([]Dependency, 0, len(variant.Dependencies))
	for _, dependency := range variant.Dependencies {
		version := dependency.Version.value()
		if version == "" {
			version = constraints[dependency.Group+":"+dependency.Module]
		}
		if dependency.Group == "" || dependency.Module == "" || version == "" {
			continue
		}
		normalized, err := normalizeVersion(version)
		if err != nil {
			return nil, err
		}
		result = append(result, Dependency{
			Coordinate: Coordinate{
				Group:     dependency.Group,
				Artifact:  dependency.Module,
				Version:   normalized,
				Extension: "jar",
			},
			Scope: ScopeCompile,
		})
	}
	return result, nil
}
