package android

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
)

// assetFile is one project asset and its path inside the APK.
type assetFile struct {
	name string
	path string
}

func assetFiles(dirs []string) ([]assetFile, error) {
	files := make(map[string]assetFile)
	for _, dir := range dirs {
		err := filepath.WalkDir(dir, func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() {
				return nil
			}
			info, err := entry.Info()
			if err != nil {
				return err
			}
			if !info.Mode().IsRegular() {
				return nil
			}
			relative, err := filepath.Rel(dir, path)
			if err != nil {
				return err
			}
			name := "assets/" + filepath.ToSlash(relative)
			files[name] = assetFile{name: name, path: path}
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("scan assets %s: %w", dir, err)
		}
	}

	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)

	result := make([]assetFile, 0, len(names))
	for _, name := range names {
		result = append(result, files[name])
	}
	return result, nil
}

func writeAsset(writer *zip.Writer, asset assetFile) error {
	source, err := os.Open(asset.path)
	if err != nil {
		return fmt.Errorf("open %s: %w", asset.path, err)
	}
	defer source.Close()

	header := &zip.FileHeader{Name: asset.name, Method: zip.Deflate, Modified: apkFixedTime}
	header.SetMode(0o644)
	target, err := writer.CreateHeader(header)
	if err != nil {
		return fmt.Errorf("create %s: %w", asset.name, err)
	}
	if _, err := io.Copy(target, source); err != nil {
		return fmt.Errorf("write %s: %w", asset.name, err)
	}
	return nil
}
