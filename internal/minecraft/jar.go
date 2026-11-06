package minecraft

import (
	"archive/zip"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/gabriel-aplok/kine-build-java/internal/core"
)

var jarFixedTime = time.Date(1980, 1, 1, 0, 0, 0, 0, time.UTC)

func (p *Provider) jarTask(workspaceRoot string, project *core.Project, options Options) (*core.Task, error) {
	inputs := []core.Input{}
	classes, err := relativePath(workspaceRoot, filepath.Join(project.Dir, "build", "classes"))
	if err != nil {
		return nil, err
	}
	inputs = append(inputs, core.Input{Path: classes, Mode: core.ModeTree})

	for _, dir := range options.ResourceDirs {
		path, err := relativePath(workspaceRoot, dir)
		if err != nil {
			return nil, err
		}
		inputs = append(inputs, core.Input{Path: path, Mode: core.ModeTree})
	}
	metadata, err := relativePath(workspaceRoot, options.MetadataPath)
	if err != nil {
		return nil, err
	}
	inputs = append(inputs, core.Input{Path: metadata, Mode: core.ModeFile})

	output, err := relativePath(workspaceRoot, filepath.Join(project.Dir, "build", "libs", options.ModID+"-"+options.ModVersion+".jar"))
	if err != nil {
		return nil, err
	}

	return &core.Task{
		ID:      "mod-jar",
		Deps:    []string{project.ID + ":compile-java"},
		Inputs:  inputs,
		Outputs: []core.Output{{Path: output, Mode: core.ModeFile}},
		Key:     fmt.Sprintf("mod-jar %s %s", options.ModID, options.ModVersion),
		Run: func(ctx context.Context) error {
			return p.buildJar(project, options)
		},
	}, nil
}

func (p *Provider) buildJar(project *core.Project, options Options) error {
	entries := make(map[string]jarSource)

	classes := filepath.Join(project.Dir, "build", "classes")
	if err := collectEntries(classes, entries); err != nil {
		return err
	}
	for _, dir := range options.ResourceDirs {
		if err := collectEntries(dir, entries); err != nil {
			return err
		}
	}

	if _, ok := entries[metadataName]; !ok {
		entries[metadataName] = jarSource{path: options.MetadataPath}
	}
	if _, ok := entries["META-INF/MANIFEST.MF"]; !ok {
		entries["META-INF/MANIFEST.MF"] = jarSource{content: []byte("Manifest-Version: 1.0\r\n\r\n")}
	}

	outputDir := filepath.Join(project.Dir, "build", "libs")
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", outputDir, err)
	}
	path := filepath.Join(outputDir, options.ModID+"-"+options.ModVersion+".jar")
	if err := writeJar(path, entries); err != nil {
		return err
	}
	return nil
}

type jarSource struct {
	path    string
	content []byte
}

func collectEntries(root string, entries map[string]jarSource) error {
	info, err := os.Stat(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("stat %s: %w", root, err)
	}
	if !info.IsDir() {
		return nil
	}

	return filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		entries[filepath.ToSlash(relative)] = jarSource{path: path}
		return nil
	})
}

func writeJar(path string, entries map[string]jarSource) error {
	names := make([]string, 0, len(entries))
	for name := range entries {
		names = append(names, name)
	}
	sort.Strings(names)

	file, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create %s: %w", path, err)
	}
	writer := zip.NewWriter(file)
	for _, name := range names {
		header := &zip.FileHeader{Name: name, Method: zip.Deflate, Modified: jarFixedTime}
		header.SetMode(0o644)
		target, err := writer.CreateHeader(header)
		if err != nil {
			writer.Close()
			file.Close()
			return fmt.Errorf("create %s: %w", name, err)
		}
		source := entries[name]
		if source.content != nil {
			if _, err := target.Write(source.content); err != nil {
				writer.Close()
				file.Close()
				return fmt.Errorf("write %s: %w", name, err)
			}
			continue
		}
		in, err := os.Open(source.path)
		if err != nil {
			writer.Close()
			file.Close()
			return fmt.Errorf("open %s: %w", source.path, err)
		}
		_, copyErr := io.Copy(target, in)
		in.Close()
		if copyErr != nil {
			writer.Close()
			file.Close()
			return fmt.Errorf("write %s: %w", name, copyErr)
		}
	}
	if err := writer.Close(); err != nil {
		file.Close()
		return fmt.Errorf("close %s: %w", path, err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close %s: %w", path, err)
	}
	return nil
}
