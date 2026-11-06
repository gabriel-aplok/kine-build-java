package jvm

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
	"github.com/gabriel-aplok/kine-build-java/internal/fsutil"
)

var fixedJarTime = time.Date(1980, 1, 1, 0, 0, 0, 0, time.UTC)

func (p *Provider) addJarTask(b *build) error {
	inputs, deps, err := p.jarInputs(b)
	if err != nil {
		return err
	}
	output, err := relativePath(p.workspaceRoot(b.project), filepath.Join(b.project.Dir, "build", "libs", b.options.Jar))
	if err != nil {
		return err
	}

	b.tasks = append(b.tasks, &core.Task{
		ID:      "jar",
		Deps:    deps,
		Inputs:  inputs,
		Outputs: []core.Output{{Path: output, Mode: core.ModeFile}},
		Key:     fmt.Sprintf("jar %s main=%s", b.options.Jar, b.options.MainClass),
		Run: func(ctx context.Context) error {
			return p.createJar(b)
		},
	})
	return nil
}

func (p *Provider) jarInputs(b *build) ([]core.Input, []string, error) {
	var inputs []core.Input
	var deps []string

	add := func(path, task string) error {
		relative, err := relativePath(p.workspaceRoot(b.project), path)
		if err != nil {
			return err
		}
		inputs = append(inputs, core.Input{Path: relative, Mode: core.ModeTree})
		deps = append(deps, b.project.ID+":"+task)
		return nil
	}

	if len(b.sources.Java) > 0 {
		if err := add(filepath.Join(b.project.Dir, "build", "classes", "java", "main"), "compile-java"); err != nil {
			return nil, nil, err
		}
	}
	if len(b.sources.Kotlin) > 0 {
		if err := add(filepath.Join(b.project.Dir, "build", "classes", "kotlin", "main"), "compile-kotlin"); err != nil {
			return nil, nil, err
		}
	}
	if len(b.sources.Resources) > 0 {
		if err := add(filepath.Join(b.project.Dir, "build", "resources", "main"), "process-resources"); err != nil {
			return nil, nil, err
		}
	}
	return inputs, deps, nil
}

func (p *Provider) createJar(b *build) error {
	var dirs []string
	if len(b.sources.Java) > 0 {
		dirs = append(dirs, filepath.Join(b.project.Dir, "build", "classes", "java", "main"))
	}
	if len(b.sources.Kotlin) > 0 {
		dirs = append(dirs, filepath.Join(b.project.Dir, "build", "classes", "kotlin", "main"))
	}
	if len(b.sources.Resources) > 0 {
		dirs = append(dirs, filepath.Join(b.project.Dir, "build", "resources", "main"))
	}

	output := filepath.Join(b.project.Dir, "build", "libs", b.options.Jar)
	return writeJar(output, dirs, b.options.MainClass)
}

func writeJar(path string, dirs []string, mainClass string) error {
	entries, err := collectJarEntries(dirs)
	if err != nil {
		return err
	}
	entries = append(entries, jarEntry{
		name:    "META-INF/MANIFEST.MF",
		content: manifest(mainClass),
	})
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].name < entries[j].name
	})

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create jar dir: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".jar-*")
	if err != nil {
		return fmt.Errorf("create jar temp: %w", err)
	}
	name := tmp.Name()

	if err := writeJarFile(tmp, entries); err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return fmt.Errorf("close jar: %w", err)
	}
	if err := fsutil.Replace(name, path); err != nil {
		os.Remove(name)
		return fmt.Errorf("rename jar: %w", err)
	}
	return nil
}

type jarEntry struct {
	name    string
	path    string
	dir     bool
	content []byte
}

func collectJarEntries(dirs []string) ([]jarEntry, error) {
	entries := make(map[string]jarEntry)

	for _, dir := range dirs {
		err := filepath.WalkDir(dir, func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			relative, err := filepath.Rel(dir, path)
			if err != nil {
				return err
			}
			if relative == "." {
				return nil
			}
			name := filepath.ToSlash(relative)

			if entry.IsDir() {
				children, err := os.ReadDir(path)
				if err != nil {
					return err
				}
				if len(children) == 0 {
					entries[name+"/"] = jarEntry{name: name + "/", dir: true}
				}
				return nil
			}
			entries[name] = jarEntry{name: name, path: path}
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("walk %s: %w", dir, err)
		}
	}

	result := make([]jarEntry, 0, len(entries))
	for _, entry := range entries {
		result = append(result, entry)
	}
	return result, nil
}

func writeJarFile(file *os.File, entries []jarEntry) error {
	writer := zip.NewWriter(file)
	for _, entry := range entries {
		header := &zip.FileHeader{
			Name:     entry.name,
			Method:   zip.Deflate,
			Modified: fixedJarTime,
		}
		if entry.dir {
			header.Method = zip.Store
			header.SetMode(os.ModeDir | 0o755)
		} else {
			header.SetMode(0o644)
		}

		entryWriter, err := writer.CreateHeader(header)
		if err != nil {
			writer.Close()
			return fmt.Errorf("create jar entry %s: %w", entry.name, err)
		}
		if entry.dir {
			continue
		}
		if entry.content != nil {
			if _, err := entryWriter.Write(entry.content); err != nil {
				writer.Close()
				return fmt.Errorf("write jar entry %s: %w", entry.name, err)
			}
			continue
		}

		source, err := os.Open(entry.path)
		if err != nil {
			writer.Close()
			return fmt.Errorf("open %s: %w", entry.path, err)
		}
		_, err = io.Copy(entryWriter, source)
		source.Close()
		if err != nil {
			writer.Close()
			return fmt.Errorf("write jar entry %s: %w", entry.name, err)
		}
	}
	return writer.Close()
}

func manifest(mainClass string) []byte {
	content := "Manifest-Version: 1.0\r\n"
	if mainClass != "" {
		content += "Main-Class: " + mainClass + "\r\n"
	}
	content += "\r\n"
	return []byte(content)
}
