package jvm

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/gabriel-aplok/kine-build-java/internal/core"
)

func (p *Provider) addResourceTasks(b *build) error {
	if len(b.sources.Resources) > 0 {
		if err := p.addResourceTask(b, "process-resources", b.sources.Resources, "build/resources/main"); err != nil {
			return err
		}
	}
	if len(b.sources.TestResources) > 0 {
		if err := p.addResourceTask(b, "process-test-resources", b.sources.TestResources, "build/resources/test"); err != nil {
			return err
		}
	}
	return nil
}

func (p *Provider) addResourceTask(b *build, id string, sourceDirs []string, outputPath string) error {
	inputs := make([]core.Input, 0, len(sourceDirs))
	for _, dir := range sourceDirs {
		relative, err := relativePath(p.workspaceRoot(b.project), dir)
		if err != nil {
			return err
		}
		inputs = append(inputs, core.Input{Path: relative, Mode: core.ModeTree})
	}

	output := filepath.Join(b.project.Dir, filepath.FromSlash(outputPath))
	relativeOutput, err := relativePath(p.workspaceRoot(b.project), output)
	if err != nil {
		return err
	}

	b.tasks = append(b.tasks, &core.Task{
		ID:      id,
		Inputs:  inputs,
		Outputs: []core.Output{{Path: relativeOutput, Mode: core.ModeTree}},
		Key:     "resources " + outputPath,
		Run: func(ctx context.Context) error {
			return copyResources(sourceDirs, output)
		},
	})
	return nil
}

func copyResources(sourceDirs []string, output string) error {
	if err := os.RemoveAll(output); err != nil {
		return fmt.Errorf("clean %s: %w", output, err)
	}
	if err := os.MkdirAll(output, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", output, err)
	}

	for _, source := range sourceDirs {
		err := filepath.WalkDir(source, func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() {
				return nil
			}

			relative, err := filepath.Rel(source, path)
			if err != nil {
				return err
			}
			dest := filepath.Join(output, relative)
			if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
				return err
			}

			if entry.Type()&os.ModeSymlink != 0 {
				target, err := os.Readlink(path)
				if err != nil {
					return err
				}
				return createResourceLink(path, dest, target)
			}

			info, err := entry.Info()
			if err != nil {
				return err
			}
			return copyFile(path, dest, info.Mode().Perm())
		})
		if err != nil {
			return fmt.Errorf("copy resources from %s: %w", source, err)
		}
	}
	return nil
}

func copyFile(source, dest string, perm os.FileMode) error {
	in, err := os.Open(source)
	if err != nil {
		return fmt.Errorf("open %s: %w", source, err)
	}
	defer in.Close()

	out, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm)
	if err != nil {
		return fmt.Errorf("create %s: %w", dest, err)
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return fmt.Errorf("copy %s: %w", dest, err)
	}
	if err := out.Close(); err != nil {
		return fmt.Errorf("close %s: %w", dest, err)
	}
	return os.Chmod(dest, perm)
}
