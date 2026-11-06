// Package minecraft builds Fabric mods from local game artifacts.
package minecraft

import (
	"encoding/json"
	"fmt"

	"github.com/gabriel-aplok/kine-build-java/internal/deps"
)

// Download describes one downloadable file.
type Download struct {
	URL  string
	SHA1 string
	Size int64
}

// Version is the part of a Mojang version JSON the build uses.
type Version struct {
	ID         string
	Libraries  []deps.Coordinate
	Client     Download
	AssetIndex string
}

type versionFile struct {
	ID        string `json:"id"`
	Libraries []struct {
		Name string `json:"name"`
	} `json:"libraries"`
	Downloads struct {
		Client struct {
			URL  string `json:"url"`
			SHA1 string `json:"sha1"`
			Size int64  `json:"size"`
		} `json:"client"`
	} `json:"downloads"`
	AssetIndex struct {
		ID string `json:"id"`
	} `json:"assetIndex"`
}

// ParseVersion reads a Mojang version JSON.
func ParseVersion(data []byte) (Version, error) {
	var file versionFile
	if err := json.Unmarshal(data, &file); err != nil {
		return Version{}, fmt.Errorf("decode version json: %w", err)
	}
	if file.ID == "" {
		return Version{}, fmt.Errorf("version id is empty")
	}

	libraries := make([]deps.Coordinate, 0, len(file.Libraries))
	for index, library := range file.Libraries {
		if library.Name == "" {
			return Version{}, fmt.Errorf("library %d: name is empty", index)
		}
		coordinate, err := deps.ParseCoordinate(library.Name)
		if err != nil {
			return Version{}, fmt.Errorf("library %d %q: %w", index, library.Name, err)
		}
		libraries = append(libraries, coordinate)
	}

	return Version{
		ID:        file.ID,
		Libraries: libraries,
		Client: Download{
			URL:  file.Downloads.Client.URL,
			SHA1: file.Downloads.Client.SHA1,
			Size: file.Downloads.Client.Size,
		},
		AssetIndex: file.AssetIndex.ID,
	}, nil
}
