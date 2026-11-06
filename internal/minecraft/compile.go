package minecraft

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/gabriel-aplok/kine-build-java/internal/core"
	"github.com/gabriel-aplok/kine-build-java/internal/toolchain"
)

func (p *Provider) compileTask(workspaceRoot string, project *core.Project, options Options, jdk *toolchain.JDK) (*core.Task, error) {
	var inputs []core.Input
	for _, dir := range options.JavaDirs {
		path, err := relativePath(workspaceRoot, dir)
		if err != nil {
			return nil, err
		}
		inputs = append(inputs, core.Input{Path: path, Mode: core.ModeTree})
	}

	output, err := relativePath(workspaceRoot, filepath.Join(project.Dir, "build", "classes"))
	if err != nil {
		return nil, err
	}

	return &core.Task{
		ID:      "compile-java",
		Inputs:  inputs,
		Outputs: []core.Output{{Path: output, Mode: core.ModeTree}},
		Key: fmt.Sprintf(
			"javac mc=%s mod=%s mcjar=%s classpath=%s source=17 target=17",
			options.Minecraft,
			options.ModVersion,
			options.MinecraftHash,
			options.ClasspathHash,
		),
		Run: func(ctx context.Context) error {
			return p.compileJava(ctx, project, options, jdk)
		},
	}, nil
}

func (p *Provider) compileJava(ctx context.Context, project *core.Project, options Options, jdk *toolchain.JDK) error {
	output := filepath.Join(project.Dir, "build", "classes")
	if err := os.RemoveAll(output); err != nil {
		return fmt.Errorf("clean %s: %w", output, err)
	}
	if err := os.MkdirAll(output, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", output, err)
	}

	sources, err := sourceFiles(options.JavaDirs, ".java")
	if err != nil {
		return err
	}
	if len(sources) == 0 {
		return nil
	}

	tmpDir := filepath.Join(project.Dir, ".kine", "tmp")
	if err := os.MkdirAll(tmpDir, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", tmpDir, err)
	}

	result, err := p.run(ctx, toolchain.Command{
		Path: jdk.Javac,
		Args: javacArgs(output, modClasspath(options), sources),
		Dir:  project.Dir,
		Env:  toolchain.BuildEnv(jdk, tmpDir, nil),
	})
	if err != nil {
		return toolError("javac", result, err)
	}
	return nil
}

func modClasspath(options Options) []string {
	result := make([]string, 0, len(options.Classpath)+1)
	result = append(result, options.MinecraftJar)
	return append(result, options.Classpath...)
}

func javacArgs(output string, classpath, sources []string) []string {
	args := []string{
		toolchain.HeapArg("-J-Xmx", toolchain.DefaultCompilerHeap),
		"-source", "17",
		"-target", "17",
		"-encoding", "UTF-8",
		"-d", output,
	}
	if len(classpath) > 0 {
		args = append(args, "-classpath", strings.Join(classpath, string(os.PathListSeparator)))
	}
	return append(args, sources...)
}

func sourceFiles(dirs []string, extension string) ([]string, error) {
	var files []string
	for _, dir := range dirs {
		err := filepath.WalkDir(dir, func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() {
				return nil
			}
			if strings.HasSuffix(entry.Name(), extension) {
				files = append(files, path)
			}
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("walk %s: %w", dir, err)
		}
	}
	sort.Strings(files)
	return files, nil
}

func toolError(name string, result toolchain.Result, err error) error {
	output := strings.TrimSpace(string(result.Stdout) + "\n" + string(result.Stderr))
	if output == "" {
		return fmt.Errorf("%s: %w", name, err)
	}
	return fmt.Errorf("%s: %w\n%s", name, err, output)
}
