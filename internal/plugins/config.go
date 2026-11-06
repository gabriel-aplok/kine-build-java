// Package plugins loads JVM plugins and runs their tasks in a host process.
package plugins

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/pelletier/go-toml/v2"

	"github.com/gabriel-aplok/kine-build-java/internal/core"
)

const configFileName = "plugins.toml"

type configFile struct {
	Jars []string `toml:"jars"`
}

// LoadConfig returns the absolute plugin jar paths from plugins.toml.
func LoadConfig(workspaceRoot string) ([]string, error) {
	root, err := resolveRoot(workspaceRoot)
	if err != nil {
		return nil, err
	}

	path := filepath.Join(root, configFileName)
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read %s: %w", path, err)
	}

	var file configFile
	decoder := toml.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&file); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if len(file.Jars) == 0 {
		return nil, fmt.Errorf("%s: jars is empty", path)
	}

	jars := make([]string, 0, len(file.Jars))
	seen := make(map[string]struct{}, len(file.Jars))
	for _, value := range file.Jars {
		jar, err := resolveJar(root, value)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		if _, ok := seen[jar]; ok {
			return nil, fmt.Errorf("%s: duplicate jar %q", path, value)
		}
		seen[jar] = struct{}{}
		jars = append(jars, jar)
	}
	return jars, nil
}

func resolveRoot(root string) (string, error) {
	absolute, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("resolve workspace root %s: %w", root, err)
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", fmt.Errorf("resolve workspace root %s: %w", root, err)
	}
	return resolved, nil
}

func resolveJar(root, value string) (string, error) {
	if strings.TrimSpace(value) == "" {
		return "", fmt.Errorf("jar path is empty")
	}
	if filepath.IsAbs(value) {
		return "", fmt.Errorf("jar %q must be relative", value)
	}

	clean := filepath.Clean(filepath.FromSlash(value))
	if clean == "." {
		return "", fmt.Errorf("jar %q is the workspace root", value)
	}
	full := filepath.Join(root, clean)
	if !core.Within(root, full) {
		return "", fmt.Errorf("jar %q escapes the workspace", value)
	}

	resolved, err := filepath.EvalSymlinks(full)
	if err != nil {
		return "", fmt.Errorf("jar %q: %w", value, err)
	}
	if !core.Within(root, resolved) {
		return "", fmt.Errorf("jar %q escapes the workspace", value)
	}

	info, err := os.Stat(resolved)
	if err != nil {
		return "", fmt.Errorf("jar %q: %w", value, err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("jar %q is not a regular file", value)
	}
	return resolved, nil
}
