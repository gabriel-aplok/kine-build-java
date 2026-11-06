package minecraft

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"unicode"
)

// FabricMetadata is the part of fabric.mod.json the build uses.
type FabricMetadata struct {
	SchemaVersion int                 `json:"schemaVersion"`
	ID            string              `json:"id"`
	Version       string              `json:"version"`
	Entrypoints   map[string][]string `json:"entrypoints"`
	Depends       map[string]string   `json:"depends"`
}

var modIDPattern = regexp.MustCompile(`^[a-z][a-z0-9_-]*$`)

// ParseFabricMetadata reads and validates fabric.mod.json.
func ParseFabricMetadata(data []byte) (FabricMetadata, error) {
	var metadata FabricMetadata
	if err := json.Unmarshal(data, &metadata); err != nil {
		return FabricMetadata{}, fmt.Errorf("decode fabric.mod.json: %w", err)
	}
	if metadata.SchemaVersion != 0 && metadata.SchemaVersion != 1 {
		return FabricMetadata{}, fmt.Errorf("schemaVersion %d is not supported", metadata.SchemaVersion)
	}
	if !modIDPattern.MatchString(metadata.ID) {
		return FabricMetadata{}, fmt.Errorf("id %q must match %s", metadata.ID, modIDPattern)
	}
	if metadata.Version == "" || strings.ContainsFunc(metadata.Version, unicode.IsSpace) {
		return FabricMetadata{}, fmt.Errorf("version is empty or has whitespace")
	}

	for name, entries := range metadata.Entrypoints {
		if name == "" {
			return FabricMetadata{}, fmt.Errorf("entrypoint name is empty")
		}
		if len(entries) == 0 {
			return FabricMetadata{}, fmt.Errorf("entrypoint %q has no values", name)
		}
		for _, entry := range entries {
			if strings.TrimSpace(entry) == "" {
				return FabricMetadata{}, fmt.Errorf("entrypoint %q has an empty value", name)
			}
		}
	}
	for key, value := range metadata.Depends {
		if strings.TrimSpace(key) == "" || strings.TrimSpace(value) == "" {
			return FabricMetadata{}, fmt.Errorf("dependency %q is empty", key)
		}
	}
	return metadata, nil
}
