package plugins

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"unicode"
)

const (
	// APIVersion is the plugin API version this module hosts.
	APIVersion      = 1
	manifestPath    = "META-INF/kine-plugin.json"
	maxManifestSize = 64 << 10
	builtinProvider = "jvm"
)

// Manifest describes one plugin jar.
type Manifest struct {
	API       int      `json:"api"`
	ID        string   `json:"id"`
	Version   string   `json:"version"`
	Main      string   `json:"main"`
	Providers []string `json:"providers"`
}

var (
	idPattern            = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)
	javaClassNamePattern = regexp.MustCompile(`^[A-Za-z_$][A-Za-z0-9_$]*(\.[A-Za-z_$][A-Za-z0-9_$]*)*$`)
)

// ReadManifest reads and validates META-INF/kine-plugin.json from a jar.
func ReadManifest(jarPath string) (Manifest, error) {
	reader, err := zip.OpenReader(jarPath)
	if err != nil {
		return Manifest{}, fmt.Errorf("open jar %s: %w", jarPath, err)
	}
	defer reader.Close()

	for _, file := range reader.File {
		if file.Name != manifestPath {
			continue
		}
		if file.UncompressedSize64 > maxManifestSize {
			return Manifest{}, fmt.Errorf("%s: %s exceeds %d bytes", jarPath, manifestPath, maxManifestSize)
		}

		entry, err := file.Open()
		if err != nil {
			return Manifest{}, fmt.Errorf("%s: open %s: %w", jarPath, manifestPath, err)
		}
		data, err := io.ReadAll(io.LimitReader(entry, maxManifestSize+1))
		entry.Close()
		if err != nil {
			return Manifest{}, fmt.Errorf("%s: read %s: %w", jarPath, manifestPath, err)
		}
		if len(data) > maxManifestSize {
			return Manifest{}, fmt.Errorf("%s: %s exceeds %d bytes", jarPath, manifestPath, maxManifestSize)
		}

		var manifest Manifest
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&manifest); err != nil {
			return Manifest{}, fmt.Errorf("%s: decode %s: %w", jarPath, manifestPath, err)
		}
		if err := manifest.Validate(); err != nil {
			return Manifest{}, fmt.Errorf("%s: %w", jarPath, err)
		}
		return manifest, nil
	}
	return Manifest{}, fmt.Errorf("%s: missing %s", jarPath, manifestPath)
}

// Validate checks the manifest rules.
func (m Manifest) Validate() error {
	if m.API != APIVersion {
		return fmt.Errorf("%s: api %d, want %d", manifestPath, m.API, APIVersion)
	}
	if err := validateID(m.ID); err != nil {
		return fmt.Errorf("%s: plugin id: %w", manifestPath, err)
	}
	if m.Version == "" || strings.ContainsFunc(m.Version, unicode.IsSpace) {
		return fmt.Errorf("%s: version is empty or has whitespace", manifestPath)
	}
	if !javaClassNamePattern.MatchString(m.Main) {
		return fmt.Errorf("%s: main %q is not a Java class name", manifestPath, m.Main)
	}
	if len(m.Providers) == 0 {
		return fmt.Errorf("%s: providers is empty", manifestPath)
	}

	seen := make(map[string]struct{}, len(m.Providers))
	for _, provider := range m.Providers {
		if err := validateID(provider); err != nil {
			return fmt.Errorf("%s: provider: %w", manifestPath, err)
		}
		if provider == builtinProvider {
			return fmt.Errorf("%s: provider %q is built in", manifestPath, provider)
		}
		if _, ok := seen[provider]; ok {
			return fmt.Errorf("%s: duplicate provider %q", manifestPath, provider)
		}
		seen[provider] = struct{}{}
	}
	return nil
}

func validateID(id string) error {
	if id == "" {
		return errors.New("id is empty")
	}
	if !idPattern.MatchString(id) {
		return fmt.Errorf("id %q must match %s", id, idPattern)
	}
	return nil
}
