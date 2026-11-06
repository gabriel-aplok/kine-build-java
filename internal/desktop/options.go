// Package desktop packages JVM applications with jpackage.
package desktop

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/pelletier/go-toml/v2"

	"github.com/gabriel-aplok/kine-build-java/internal/core"
)

const (
	packageTable   = "package"
	appImageType   = "app-image"
	defaultVendor  = "Kine"
	defaultVersion = "1.0.0"
	defaultDest    = "build/dist"
)

// Options holds the jvm options and the package options.
type Options struct {
	JVM     map[string]any
	Package PackageOptions
}

// PackageOptions configures jpackage.
type PackageOptions struct {
	Name        string   `toml:"name"`
	Vendor      string   `toml:"vendor"`
	Version     string   `toml:"version"`
	Icon        string   `toml:"icon"`
	Type        string   `toml:"type"`
	Dest        string   `toml:"dest"`
	JavaOptions []string `toml:"java_options"`
}

func decodeOptions(project *core.Project, workspaceRoot string) (Options, error) {
	values := make(map[string]any, len(project.Options))
	for key, value := range project.Options {
		values[key] = value
	}

	var options PackageOptions
	if raw, ok := values[packageTable]; ok {
		delete(values, packageTable)
		data, err := toml.Marshal(raw)
		if err != nil {
			return Options{}, fmt.Errorf("encode package options: %w", err)
		}
		decoder := toml.NewDecoder(bytes.NewReader(data))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&options); err != nil {
			return Options{}, fmt.Errorf("decode package options: %w", err)
		}
	}

	applyDefaults(&options, project.ID)
	if err := validateOptions(options, project.Dir, workspaceRoot); err != nil {
		return Options{}, err
	}
	icon, err := resolveIcon(project.Dir, workspaceRoot, options.Icon)
	if err != nil {
		return Options{}, err
	}
	options.Icon = icon
	return Options{JVM: values, Package: options}, nil
}

func applyDefaults(options *PackageOptions, projectID string) {
	if options.Name == "" {
		options.Name = projectID
	}
	if options.Vendor == "" {
		options.Vendor = defaultVendor
	}
	if options.Version == "" {
		options.Version = defaultVersion
	}
	if options.Type == "" {
		options.Type = appImageType
	}
	if options.Dest == "" {
		options.Dest = defaultDest
	}
}

func validateOptions(options PackageOptions, projectDir, workspaceRoot string) error {
	if strings.TrimSpace(options.Name) == "" {
		return fmt.Errorf("package name is empty")
	}
	if strings.ContainsAny(options.Name, `/\`) || options.Name == "." || options.Name == ".." {
		return fmt.Errorf("package name %q must be a plain name", options.Name)
	}
	if strings.TrimSpace(options.Vendor) == "" {
		return fmt.Errorf("package vendor is empty")
	}
	if strings.TrimSpace(options.Version) == "" {
		return fmt.Errorf("package version is empty")
	}
	if options.Version[0] < '0' || options.Version[0] > '9' {
		return fmt.Errorf("package version %q must start with a digit", options.Version)
	}
	if options.Type != appImageType {
		return fmt.Errorf("package type %q is not supported, use %q", options.Type, appImageType)
	}
	if filepath.IsAbs(options.Dest) {
		return fmt.Errorf("package dest %q must be relative", options.Dest)
	}
	dest := filepath.Join(projectDir, filepath.FromSlash(options.Dest))
	if !core.Within(projectDir, dest) || dest == projectDir {
		return fmt.Errorf("package dest %q escapes the project", options.Dest)
	}
	for _, option := range options.JavaOptions {
		if strings.TrimSpace(option) == "" {
			return fmt.Errorf("package java option is empty")
		}
	}
	return nil
}

func resolveIcon(projectDir, workspaceRoot, value string) (string, error) {
	if value == "" {
		return "", nil
	}
	if filepath.IsAbs(value) {
		return "", fmt.Errorf("package icon %q must be relative", value)
	}

	icon := filepath.Join(projectDir, filepath.FromSlash(value))
	if !core.Within(workspaceRoot, icon) {
		return "", fmt.Errorf("package icon %q escapes the workspace", value)
	}
	info, err := os.Stat(icon)
	if err != nil {
		return "", fmt.Errorf("package icon %q: %w", value, err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("package icon %q is not a regular file", value)
	}
	return icon, nil
}
