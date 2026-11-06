package android

import (
	"archive/zip"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/gabriel-aplok/kine-build-java/internal/core"
	"github.com/gabriel-aplok/kine-build-java/internal/deps"
	"github.com/gabriel-aplok/kine-build-java/internal/toolchain"
)

func (p *Provider) dexLibTask(workspaceRoot string, project *core.Project, options Options, sdk *SDK, jdk *toolchain.JDK, artifacts []deps.Artifact) (*core.Task, error) {
	if len(artifacts) == 0 && options.KotlinStdlib == "" {
		return nil, nil
	}

	inputs := make([]core.Input, 0, len(artifacts)+1)
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
	if options.KotlinStdlib != "" {
		path, err := relativePath(workspaceRoot, options.KotlinStdlib)
		if err != nil {
			return nil, err
		}
		inputs = append(inputs, core.Input{Path: path, Mode: core.ModeFile})
	}

	output, err := relativePath(workspaceRoot, filepath.Join(project.Dir, "build", "android", "dex-libs"))
	if err != nil {
		return nil, err
	}

	return &core.Task{
		ID:      "dex-libs",
		Inputs:  inputs,
		Outputs: []core.Output{{Path: output, Mode: core.ModeTree}},
		Key: fmt.Sprintf(
			"d8-libs %s v=%d platform=%s min=%d kotlin=%s",
			sdk.BuildTools,
			androidTaskVersion,
			filepath.Base(sdk.Platform),
			options.MinSDK,
			options.KotlinVersion,
		),
		Run: func(ctx context.Context) error {
			return p.dexLibraries(ctx, project, options, sdk, jdk, artifacts)
		},
	}, nil
}

func (p *Provider) dexLibraries(ctx context.Context, project *core.Project, options Options, sdk *SDK, jdk *toolchain.JDK, artifacts []deps.Artifact) error {
	output := filepath.Join(project.Dir, "build", "android", "dex-libs")
	if err := os.RemoveAll(output); err != nil {
		return fmt.Errorf("clean %s: %w", output, err)
	}
	if err := os.MkdirAll(output, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", output, err)
	}

	jars, err := classpathJars(artifacts, filepath.Join(project.Dir, "build", "android", "aar"))
	if err != nil {
		return err
	}
	if options.KotlinStdlib != "" {
		jars = append(jars, options.KotlinStdlib)
	}
	if len(jars) == 0 {
		return nil
	}

	tmpDir := filepath.Join(project.Dir, ".kine", "tmp")
	if err := os.MkdirAll(tmpDir, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", tmpDir, err)
	}

	args := []string{
		"--lib", sdk.AndroidJar,
		"--min-api", strconv.Itoa(options.MinSDK),
		"--output", output,
	}
	args = append(args, jars...)

	result, err := p.run(ctx, toolchain.Command{
		Path: sdk.D8,
		Args: args,
		Dir:  project.Dir,
		Env:  toolchain.BuildEnv(jdk, tmpDir, nil),
	})
	if err != nil {
		return toolError("d8-libs", result, err)
	}
	return nil
}

func (p *Provider) dexTask(workspaceRoot string, project *core.Project, options Options, sdk *SDK, jdk *toolchain.JDK, artifacts []deps.Artifact, siblingDirs, siblingTasks []string) (*core.Task, error) {
	classes, err := relativePath(workspaceRoot, options.androidPath(project.Dir, "classes"))
	if err != nil {
		return nil, err
	}
	inputs := []core.Input{{Path: classes, Mode: core.ModeTree}}
	if len(options.KotlinDirs) > 0 {
		kotlinClasses, err := relativePath(workspaceRoot, options.androidPath(project.Dir, "classes-kotlin"))
		if err != nil {
			return nil, err
		}
		inputs = append(inputs, core.Input{Path: kotlinClasses, Mode: core.ModeTree})
	}
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
	if options.Variant.BuildTypeOptions.Minify {
		inputs = append(inputs, r8Inputs(workspaceRoot, project, options)...)
	}

	output, err := relativePath(workspaceRoot, options.androidPath(project.Dir, "dex"))
	if err != nil {
		return nil, err
	}

	outputs := []core.Output{
		{Path: output, Mode: core.ModeTree},
		{Path: mustRelative(workspaceRoot, options.androidPath(project.Dir, "classes.jar")), Mode: core.ModeFile},
	}

	return &core.Task{
		ID:      options.taskID("dex"),
		Deps:    append([]string{project.ID + ":" + options.taskID("compile-java")}, siblingTasks...),
		Inputs:  inputs,
		Outputs: outputs,
		Key: fmt.Sprintf(
			"dex-app %s v=%d platform=%s min=%d kotlin=%s minify=%t shrink=%t r8=%s rules=%d variant=%s",
			sdk.BuildTools,
			androidTaskVersion,
			filepath.Base(sdk.Platform),
			options.MinSDK,
			options.KotlinVersion,
			options.Variant.BuildTypeOptions.Minify,
			options.Variant.BuildTypeOptions.ShrinkResources,
			sdk.R8Jar,
			r8RulesVersion,
			options.variantLabel(),
		),
		Run: func(ctx context.Context) error {
			return p.dex(ctx, project, options, sdk, jdk, artifacts, siblingDirs)
		},
	}, nil
}

func (p *Provider) dex(ctx context.Context, project *core.Project, options Options, sdk *SDK, jdk *toolchain.JDK, artifacts []deps.Artifact, siblingDirs []string) error {
	classes := options.androidPath(project.Dir, "classes")
	output := options.androidPath(project.Dir, "dex")
	if err := os.RemoveAll(output); err != nil {
		return fmt.Errorf("clean %s: %w", output, err)
	}
	if err := os.MkdirAll(output, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", output, err)
	}

	classesJar := options.androidPath(project.Dir, "classes.jar")
	classesDirs := []string{classes}
	if len(options.KotlinDirs) > 0 {
		classesDirs = append(classesDirs, options.androidPath(project.Dir, "classes-kotlin"))
	}
	classesDirs = append(classesDirs, siblingDirs...)
	if err := jarClasses(classesDirs, classesJar); err != nil {
		return err
	}
	jars, err := classpathJars(artifacts, filepath.Join(project.Dir, "build", "android", "aar"))
	if err != nil {
		return err
	}
	if options.Variant.BuildTypeOptions.Minify {
		return p.runR8(ctx, project, options, sdk, jdk, artifacts, classesJar, output)
	}

	tmpDir := filepath.Join(project.Dir, ".kine", "tmp")
	if err := os.MkdirAll(tmpDir, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", tmpDir, err)
	}

	result, err := p.run(ctx, toolchain.Command{
		Path: sdk.D8,
		Args: dexArgs(classesJar, output, options, sdk, jars),
		Dir:  project.Dir,
		Env:  toolchain.BuildEnv(jdk, tmpDir, nil),
	})
	if err != nil {
		return toolError("d8", result, err)
	}
	return nil
}

func dexArgs(classesJar, output string, options Options, sdk *SDK, classpath []string) []string {
	args := []string{
		"--lib", sdk.AndroidJar,
		"--min-api", strconv.Itoa(options.MinSDK),
		"--output", output,
	}
	for _, jar := range classpath {
		args = append(args, "--classpath", jar)
	}
	return append(args, classesJar)
}

func jarClasses(classesDirs []string, jarPath string) error {
	entries := make(map[string]string)
	for _, classesDir := range classesDirs {
		err := filepath.WalkDir(classesDir, func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				if os.IsNotExist(walkErr) {
					return nil
				}
				return walkErr
			}
			if entry.IsDir() || filepath.Ext(path) != ".class" {
				return nil
			}
			relative, err := filepath.Rel(classesDir, path)
			if err != nil {
				return err
			}
			name := filepath.ToSlash(relative)
			if _, ok := entries[name]; !ok {
				entries[name] = path
			}
			return nil
		})
		if err != nil {
			return fmt.Errorf("walk %s: %w", classesDir, err)
		}
	}

	names := make([]string, 0, len(entries))
	for name := range entries {
		names = append(names, name)
	}
	sort.Strings(names)

	file, err := os.Create(jarPath)
	if err != nil {
		return fmt.Errorf("create %s: %w", jarPath, err)
	}
	writer := zip.NewWriter(file)
	for _, name := range names {
		header := &zip.FileHeader{Name: name, Method: zip.Deflate, Modified: apkFixedTime}
		target, err := writer.CreateHeader(header)
		if err != nil {
			writer.Close()
			file.Close()
			return fmt.Errorf("create %s: %w", name, err)
		}
		source, err := os.Open(entries[name])
		if err != nil {
			writer.Close()
			file.Close()
			return fmt.Errorf("open %s: %w", entries[name], err)
		}
		_, copyErr := io.Copy(target, source)
		source.Close()
		if copyErr != nil {
			writer.Close()
			file.Close()
			return fmt.Errorf("write %s: %w", name, copyErr)
		}
	}
	if err := writer.Close(); err != nil {
		file.Close()
		return fmt.Errorf("close %s: %w", jarPath, err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close %s: %w", jarPath, err)
	}
	return nil
}
