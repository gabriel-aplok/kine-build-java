//go:build windows

package jvm

import (
	"fmt"
	"os"
	"path/filepath"
)

// createResourceLink copies the target because Windows symlinks need extra privileges.
func createResourceLink(source, dest, target string) error {
	resolved := target
	if !filepath.IsAbs(resolved) {
		resolved = filepath.Join(filepath.Dir(source), resolved)
	}
	return copyResourcePath(resolved, dest)
}

func copyResourcePath(source, dest string) error {
	info, err := os.Stat(source)
	if err != nil {
		return fmt.Errorf("stat %s: %w", source, err)
	}
	if !info.IsDir() {
		return copyFile(source, dest, info.Mode().Perm())
	}

	return filepath.WalkDir(source, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dest, relative)
		entryInfo, err := entry.Info()
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if err := os.MkdirAll(target, entryInfo.Mode().Perm()); err != nil {
				return fmt.Errorf("create %s: %w", target, err)
			}
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			link, err := os.Readlink(path)
			if err != nil {
				return err
			}
			return createResourceLink(path, target, link)
		}
		return copyFile(path, target, entryInfo.Mode().Perm())
	})
}
