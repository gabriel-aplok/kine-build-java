package jvm

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/gabriel-aplok/kine-build-java/internal/core"
	"github.com/gabriel-aplok/kine-build-java/internal/deps"
	"github.com/gabriel-aplok/kine-build-java/internal/toolchain"
)

func (p *Provider) addJavaTask(b *build) error {
	if len(b.sources.Java) == 0 {
		return nil
	}

	inputs, err := p.compileInputs(b, b.sources.Java, b.artifacts, deps.ScopeCompile, deps.ScopeProvided)
	if err != nil {
		return err
	}
	output, err := relativePath(p.workspaceRoot(b.project), filepath.Join(b.project.Dir, "build", "classes", "java", "main"))
	if err != nil {
		return err
	}

	taskDeps := projectTaskDependencies(b.project)
	if len(b.sources.Kotlin) > 0 {
		taskDeps = append(taskDeps, b.project.ID+":compile-kotlin")
	}

	b.tasks = append(b.tasks, &core.Task{
		ID:     "compile-java",
		Deps:   taskDeps,
		Inputs: inputs,
		Outputs: []core.Output{
			{Path: output, Mode: core.ModeTree},
			{Path: output + ".javac-state.json", Mode: core.ModeFile},
		},
		Key: fmt.Sprintf("javac jdk=%s release=%d", b.jdk.Version, b.options.Java),
		Run: func(ctx context.Context) error {
			return p.compileJava(ctx, b)
		},
	})
	return nil
}

func (p *Provider) compileJava(ctx context.Context, b *build) error {
	return p.compileJavaSourceSet(
		ctx,
		b,
		b.sources.Java,
		filepath.Join(b.project.Dir, "build", "classes", "java", "main"),
		b.mainCompile,
	)
}

func (p *Provider) compileJavaSourceSet(ctx context.Context, b *build, sourceDirs []string, output string, classpath []string) error {
	if err := os.MkdirAll(output, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", output, err)
	}

	sources, err := sourceFiles(sourceDirs, ".java")
	if err != nil {
		return err
	}
	if len(sources) == 0 {
		classpathHash, err := hashClasspath(classpath)
		if err != nil {
			return err
		}
		return writeJavacState(output, compilePlan{
			Classpath:    classpathHash,
			SourceHashes: map[string]string{},
		})
	}

	plan, err := planJavac(b.project.Dir, output, sources, classpath)
	if err != nil {
		return err
	}
	if !plan.Full && len(plan.Sources) == 0 {
		return nil
	}

	compileClasspath := classpath
	compileSources := plan.Sources
	if plan.Full {
		if err := os.RemoveAll(output); err != nil {
			return fmt.Errorf("clean %s: %w", output, err)
		}
		if err := os.MkdirAll(output, 0o755); err != nil {
			return fmt.Errorf("create %s: %w", output, err)
		}
	} else {
		compileClasspath = append(append([]string(nil), classpath...), output)
	}

	tmpDir := filepath.Join(b.project.Dir, ".kine", "tmp")
	if err := os.MkdirAll(tmpDir, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", tmpDir, err)
	}

	result, err := p.run(ctx, toolchain.Command{
		Path: b.jdk.Javac,
		Args: javacArgs(b.options.Java, output, compileClasspath, compileSources),
		Dir:  b.project.Dir,
		Env:  toolchain.BuildEnv(b.jdk, tmpDir, nil),
	})
	if err != nil {
		return compilerError("javac", result, err)
	}
	if err := writeJavacState(output, plan); err != nil {
		return err
	}
	return nil
}

func javacArgs(release int, output string, classpath, sources []string) []string {
	args := []string{
		toolchain.HeapArg("-J-Xmx", toolchain.DefaultCompilerHeap),
		"-d", output,
	}
	if release > 0 {
		args = append(args, "--release", strconv.Itoa(release))
	}
	args = append(args, "-encoding", "UTF-8", "-g")
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

func (p *Provider) compileInputs(b *build, sourceDirs []string, artifacts []deps.Artifact, scopes ...deps.Scope) ([]core.Input, error) {
	allowed := make(map[deps.Scope]struct{}, len(scopes))
	for _, scope := range scopes {
		allowed[scope] = struct{}{}
	}

	var inputs []core.Input
	for _, dir := range sourceDirs {
		path, err := relativePath(p.workspaceRoot(b.project), dir)
		if err != nil {
			return nil, err
		}
		inputs = append(inputs, core.Input{Path: path, Mode: core.ModeTree})
	}
	for _, artifact := range artifacts {
		if _, ok := allowed[artifact.Scope]; !ok || isNativeArtifact(artifact) {
			continue
		}
		path, err := relativePath(p.workspaceRoot(b.project), artifact.Path)
		if err != nil {
			return nil, err
		}
		inputs = append(inputs, core.Input{Path: path, Mode: core.ModeFile})
	}
	return inputs, nil
}

func projectTaskDependencies(project *core.Project) []string {
	dependencies := make([]string, 0, len(project.DependsOn))
	for _, dependency := range project.DependsOn {
		dependencies = append(dependencies, dependency+":jar")
	}
	return dependencies
}

func relativePath(root, path string) (string, error) {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return "", fmt.Errorf("relative path %s: %w", path, err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path %q escapes the workspace", path)
	}
	return filepath.ToSlash(rel), nil
}

func compilerError(name string, result toolchain.Result, err error) error {
	output := strings.TrimSpace(string(result.Stdout) + "\n" + string(result.Stderr))
	if output == "" {
		return fmt.Errorf("%s: %w", name, err)
	}
	return fmt.Errorf("%s: %w\n%s", name, err, output)
}
