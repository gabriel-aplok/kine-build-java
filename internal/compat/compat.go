package compat

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ErrConfigExists means the target already has a kine.toml.
var ErrConfigExists = errors.New("kine.toml already exists")

// Source selects the build system to import.
type Source string

const (
	SourceAuto   Source = "auto"
	SourceMaven  Source = "maven"
	SourceGradle Source = "gradle"
)

// Options configures one import.
type Options struct {
	Dir    string
	From   Source
	Force  bool
	DryRun bool
}

// Result is the outcome of an import.
type Result struct {
	Source       string
	File         string
	ProjectID    string
	Java         int
	Kotlin       string
	Dependencies int
	Warnings     []string
	Config       []byte
	Written      bool
}

// Summary is one line describing the import.
func (r Result) Summary() string {
	parts := []string{fmt.Sprintf("%s: %s", r.Source, r.File)}
	if r.Java > 0 {
		parts = append(parts, fmt.Sprintf("java %d", r.Java))
	}
	if r.Kotlin != "" {
		parts = append(parts, "kotlin "+r.Kotlin)
	}
	parts = append(parts,
		fmt.Sprintf("%d dependencies", r.Dependencies),
		fmt.Sprintf("%d warnings", len(r.Warnings)))
	return strings.Join(parts, ", ")
}

// Import reads a build file and optionally writes kine.toml.
func Import(options Options) (Result, error) {
	dir, err := filepath.Abs(options.Dir)
	if err != nil {
		return Result{}, fmt.Errorf("resolve directory %s: %w", options.Dir, err)
	}
	info, err := os.Stat(dir)
	if err != nil {
		return Result{}, fmt.Errorf("stat directory %s: %w", dir, err)
	}
	if !info.IsDir() {
		return Result{}, fmt.Errorf("%s is not a directory", dir)
	}

	source := options.From
	if source == "" {
		source = SourceAuto
	}
	if source == SourceAuto {
		source, err = detectSource(dir)
		if err != nil {
			return Result{}, err
		}
	}

	var build *Build
	switch source {
	case SourceMaven:
		build, err = parseMaven(dir)
	case SourceGradle:
		build, err = parseGradle(dir)
	default:
		return Result{}, fmt.Errorf("unknown import source %q", source)
	}
	if err != nil {
		return Result{}, err
	}

	id := projectID(dir)
	data, err := generateConfig(build, id)
	if err != nil {
		return Result{}, err
	}

	target := filepath.Join(dir, "kine.toml")
	written := false
	if !options.DryRun {
		if _, err := os.Stat(target); err == nil {
			if !options.Force {
				return Result{}, ErrConfigExists
			}
		} else if !os.IsNotExist(err) {
			return Result{}, fmt.Errorf("stat %s: %w", target, err)
		}
		if err := os.WriteFile(target, data, 0o644); err != nil {
			return Result{}, fmt.Errorf("write %s: %w", target, err)
		}
		written = true
	}

	return Result{
		Source:       build.Source,
		File:         build.File,
		ProjectID:    id,
		Java:         build.Java,
		Kotlin:       build.Kotlin,
		Dependencies: build.dependencyCount(),
		Warnings:     build.Warnings,
		Config:       data,
		Written:      written,
	}, nil
}

func detectSource(dir string) (Source, error) {
	candidates := []struct {
		name   string
		source Source
	}{
		{name: "pom.xml", source: SourceMaven},
		{name: "build.gradle", source: SourceGradle},
		{name: "build.gradle.kts", source: SourceGradle},
	}
	for _, candidate := range candidates {
		path := filepath.Join(dir, candidate.name)
		if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() {
			return candidate.source, nil
		}
	}
	return "", fmt.Errorf("no pom.xml, build.gradle, or build.gradle.kts in %s", dir)
}
