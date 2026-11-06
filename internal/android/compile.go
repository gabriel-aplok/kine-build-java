package android

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

func (p *Provider) compileTask(workspaceRoot string, project *core.Project, options Options, sdk *SDK, jdk *toolchain.JDK, artifacts []deps.Artifact, siblingDirs, siblingTasks []string) (*core.Task, error) {
	var inputs []core.Input
	for _, dir := range options.JavaDirs {
		path, err := relativePath(workspaceRoot, dir)
		if err != nil {
			return nil, err
		}
		inputs = append(inputs, core.Input{Path: path, Mode: core.ModeTree})
	}

	genDir := options.androidPath(project.Dir, genDirName)
	gen, err := relativePath(workspaceRoot, genDir)
	if err != nil {
		return nil, err
	}
	inputs = append(inputs, core.Input{Path: gen, Mode: core.ModeTree})
	deps := []string{project.ID + ":" + options.taskID("android-resources")}
	if len(options.KotlinDirs) > 0 {
		kotlinClasses, err := relativePath(workspaceRoot, options.androidPath(project.Dir, "classes-kotlin"))
		if err != nil {
			return nil, err
		}
		inputs = append(inputs, core.Input{Path: kotlinClasses, Mode: core.ModeTree})
		deps = append(deps, project.ID+":"+options.taskID("compile-kotlin"))
	}
	deps = append(deps, siblingTasks...)

	for _, artifact := range artifacts {
		if strings.HasPrefix(artifact.Classifier, "natives-") {
			continue
		}
		path, err := relativePath(workspaceRoot, artifact.Path)
		if err != nil {
			return nil, err
		}
		inputs = append(inputs, core.Input{Path: path, Mode: core.ModeFile})
	}
	for _, dir := range siblingDirs {
		path, err := relativePath(workspaceRoot, dir)
		if err != nil {
			return nil, err
		}
		inputs = append(inputs, core.Input{Path: path, Mode: core.ModeTree})
	}

	classes, err := relativePath(workspaceRoot, options.androidPath(project.Dir, "classes"))
	if err != nil {
		return nil, err
	}

	return &core.Task{
		ID:      options.taskID("compile-java"),
		Deps:    deps,
		Inputs:  inputs,
		Outputs: []core.Output{{Path: classes, Mode: core.ModeTree}},
		Key:     fmt.Sprintf("javac %s v=%d android=%s platform=%s source=%d target=%d kotlin=%s variant=%s", jdk.Version, androidTaskVersion, sdk.BuildTools, filepath.Base(sdk.Platform), javaVersion(options), javaVersion(options), options.KotlinVersion, options.variantLabel()),
		Run: func(ctx context.Context) error {
			return p.compileJava(ctx, project, options, sdk, jdk, artifacts, siblingDirs)
		},
	}, nil
}

func (p *Provider) compileJava(ctx context.Context, project *core.Project, options Options, sdk *SDK, jdk *toolchain.JDK, artifacts []deps.Artifact, siblingDirs []string) error {
	classes := options.androidPath(project.Dir, "classes")
	if err := os.RemoveAll(classes); err != nil {
		return fmt.Errorf("clean %s: %w", classes, err)
	}
	if err := os.MkdirAll(classes, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", classes, err)
	}

	sources, err := sourceFiles(options.JavaDirs, ".java")
	if err != nil {
		return err
	}
	genDir := options.androidPath(project.Dir, genDirName)
	generated, err := sourceFiles([]string{genDir}, ".java")
	if err != nil {
		return err
	}
	sources = append(sources, generated...)
	sort.Strings(sources)
	if len(sources) == 0 {
		return nil
	}

	tmpDir := filepath.Join(project.Dir, ".kine", "tmp")
	if err := os.MkdirAll(tmpDir, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", tmpDir, err)
	}

	jars, err := classpathJars(artifacts, filepath.Join(project.Dir, "build", "android", "aar"))
	if err != nil {
		return err
	}
	classpath := append([]string{sdk.AndroidJar}, jars...)
	if len(options.KotlinDirs) > 0 {
		classpath = append(classpath, options.androidPath(project.Dir, "classes-kotlin"))
	}
	classpath = append(classpath, siblingDirs...)
	result, err := p.run(ctx, toolchain.Command{
		Path: jdk.Javac,
		Args: javacArgs(classes, classpath, sources, javaVersion(options)),
		Dir:  project.Dir,
		Env:  toolchain.BuildEnv(jdk, tmpDir, nil),
	})
	if err != nil {
		return toolError("javac", result, err)
	}
	return nil
}

func javaVersion(options Options) int {
	if options.Java > 0 {
		return options.Java
	}
	return defaultJava
}

func javacArgs(output string, classpath, sources []string, version int) []string {
	value := strconv.Itoa(version)
	args := []string{
		toolchain.HeapArg("-J-Xmx", toolchain.DefaultCompilerHeap),
		"-source", value,
		"-target", value,
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
