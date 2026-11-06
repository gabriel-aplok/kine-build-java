package android

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gabriel-aplok/kine-build-java/internal/core"
	"github.com/gabriel-aplok/kine-build-java/internal/deps"
	"github.com/gabriel-aplok/kine-build-java/internal/toolchain"
)

type kotlinTool struct {
	Home     string
	Version  string
	Compiler string
	Stdlib   string
}

func findKotlinTool(home, version string) (*kotlinTool, error) {
	homes := kotlinHomes(home)
	var found []string
	for _, candidate := range homes {
		compiler, ok := toolPath(filepath.Join(candidate, "bin"), "kotlinc")
		if !ok {
			continue
		}
		stdlib := filepath.Join(candidate, "lib", "kotlin-stdlib.jar")
		if !isRegular(stdlib) {
			continue
		}
		installed := kotlinVersion(candidate, compiler)
		if installed == "" {
			continue
		}
		found = append(found, installed+" at "+candidate)
		if version != "" && installed != version {
			continue
		}
		return &kotlinTool{
			Home:     candidate,
			Version:  installed,
			Compiler: compiler,
			Stdlib:   stdlib,
		}, nil
	}

	if version == "" {
		return nil, fmt.Errorf("kotlinc was not found; install Kotlin or set kotlin_home")
	}
	if len(found) == 0 {
		return nil, fmt.Errorf("kotlinc was not found; install Kotlin %s or set kotlin_home", version)
	}
	return nil, fmt.Errorf("kotlin = %q does not match installed %s", version, strings.Join(found, ", "))
}

func kotlinHomes(explicit string) []string {
	var homes []string
	add := func(value string) {
		if value == "" {
			return
		}
		for _, existing := range homes {
			if existing == value {
				return
			}
		}
		homes = append(homes, value)
	}

	if explicit != "" {
		return []string{explicit}
	}
	if env := os.Getenv("KOTLIN_HOME"); env != "" {
		return []string{env}
	}
	if home, err := os.UserHomeDir(); err == nil {
		add(filepath.Join(home, ".local", "opt", "kotlinc"))
	}
	add("/opt/kotlinc")
	if path, err := exec.LookPath("kotlinc"); err == nil {
		if resolved, err := filepath.EvalSymlinks(path); err == nil {
			path = resolved
		}
		add(filepath.Dir(filepath.Dir(path)))
	}
	return homes
}

func kotlinVersion(home, compiler string) string {
	if data, err := os.ReadFile(filepath.Join(home, "build.txt")); err == nil {
		if version := strings.TrimSpace(string(data)); version != "" {
			return version
		}
	}
	output, err := exec.Command(compiler, "-version").CombinedOutput()
	if err != nil {
		return ""
	}
	fields := strings.Fields(string(output))
	for index, field := range fields {
		if field == "kotlinc-jvm" && index+1 < len(fields) {
			return fields[index+1]
		}
	}
	return ""
}

func (p *Provider) prepareKotlin(workspaceRoot string, options *Options) error {
	if options.Kotlin == "" || !hasKotlinSources(*options) {
		return nil
	}

	tool, err := findKotlinTool(options.KotlinHome, options.Kotlin)
	if err != nil {
		return err
	}
	root := filepath.Join(workspaceRoot, ".kine", "tools", "kotlin", tool.Version)
	if err := os.MkdirAll(root, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", root, err)
	}
	stdlib := filepath.Join(root, "kotlin-stdlib.jar")
	if !isRegular(stdlib) {
		if err := copyFile(tool.Stdlib, stdlib); err != nil {
			return err
		}
	}

	var composePlugin string
	if options.Compose {
		source := filepath.Join(tool.Home, "lib", "compose-compiler-plugin.jar")
		if !isRegular(source) {
			return fmt.Errorf("kotlin %s has no compose-compiler-plugin.jar", tool.Version)
		}
		composePlugin = filepath.Join(root, "compose-compiler-plugin.jar")
		if !isRegular(composePlugin) {
			if err := copyFile(source, composePlugin); err != nil {
				return err
			}
		}
	}

	options.KotlinCompiler = tool.Compiler
	options.KotlinStdlib = stdlib
	options.KotlinVersion = tool.Version
	options.ComposePlugin = composePlugin
	return nil
}

func withoutKotlinStdlib(artifacts []deps.Artifact) []deps.Artifact {
	result := artifacts[:0]
	for _, artifact := range artifacts {
		if artifact.Coordinate.Group == "org.jetbrains.kotlin" &&
			strings.HasPrefix(artifact.Coordinate.Artifact, "kotlin-stdlib") {
			continue
		}
		result = append(result, artifact)
	}
	return result
}

func hasKotlinSources(options Options) bool {
	if len(options.KotlinDirs) > 0 {
		return true
	}
	for _, variant := range options.Variants {
		if len(variant.Sources.KotlinDirs) > 0 {
			return true
		}
	}
	for _, module := range options.Modules {
		if len(module.KotlinDirs) > 0 {
			return true
		}
	}
	return false
}

func (p *Provider) kotlinTask(workspaceRoot string, project *core.Project, options Options, sdk *SDK, jdk *toolchain.JDK, artifacts []deps.Artifact, siblingDirs, siblingTasks []string) (*core.Task, error) {
	if len(options.KotlinDirs) == 0 {
		return nil, nil
	}
	if options.KotlinCompiler == "" || options.KotlinStdlib == "" {
		return nil, fmt.Errorf("kotlin tools are not resolved")
	}

	var inputs []core.Input
	for _, dir := range append(append([]string(nil), options.KotlinDirs...), options.JavaDirs...) {
		path, err := relativePath(workspaceRoot, dir)
		if err != nil {
			return nil, err
		}
		inputs = append(inputs, core.Input{Path: path, Mode: core.ModeTree})
	}
	gen, err := relativePath(workspaceRoot, options.androidPath(project.Dir, genDirName))
	if err != nil {
		return nil, err
	}
	inputs = append(inputs, core.Input{Path: gen, Mode: core.ModeTree})
	stdlib, err := relativePath(workspaceRoot, options.KotlinStdlib)
	if err != nil {
		return nil, err
	}
	inputs = append(inputs, core.Input{Path: stdlib, Mode: core.ModeFile})
	if options.ComposePlugin != "" {
		plugin, err := relativePath(workspaceRoot, options.ComposePlugin)
		if err != nil {
			return nil, err
		}
		inputs = append(inputs, core.Input{Path: plugin, Mode: core.ModeFile})
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

	output, err := relativePath(workspaceRoot, options.androidPath(project.Dir, "classes-kotlin"))
	if err != nil {
		return nil, err
	}

	return &core.Task{
		ID:      options.taskID("compile-kotlin"),
		Deps:    append([]string{project.ID + ":" + options.taskID("android-resources")}, siblingTasks...),
		Inputs:  inputs,
		Outputs: []core.Output{{Path: output, Mode: core.ModeTree}},
		Key: fmt.Sprintf(
			"kotlinc %s v=%d android=%s java=%d compose=%t variant=%s",
			options.KotlinVersion,
			androidTaskVersion,
			sdk.BuildTools,
			javaVersion(options),
			options.Compose,
			options.variantLabel(),
		),
		Run: func(ctx context.Context) error {
			return p.compileKotlin(ctx, project, options, sdk, jdk, artifacts, siblingDirs)
		},
	}, nil
}

func (p *Provider) compileKotlin(ctx context.Context, project *core.Project, options Options, sdk *SDK, jdk *toolchain.JDK, artifacts []deps.Artifact, siblingDirs []string) error {
	output := options.androidPath(project.Dir, "classes-kotlin")
	if err := os.RemoveAll(output); err != nil {
		return fmt.Errorf("clean %s: %w", output, err)
	}
	if err := os.MkdirAll(output, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", output, err)
	}

	sources, err := sourceFiles(options.KotlinDirs, ".kt")
	if err != nil {
		return err
	}
	if len(sources) == 0 {
		return nil
	}

	jars, err := classpathJars(artifacts, filepath.Join(project.Dir, "build", "android", "aar"))
	if err != nil {
		return err
	}
	classpath := []string{options.KotlinStdlib, sdk.AndroidJar}
	classpath = append(classpath, jars...)
	classpath = append(classpath, siblingDirs...)

	javaRoots := append([]string(nil), options.JavaDirs...)
	javaRoots = append(javaRoots, options.androidPath(project.Dir, genDirName))

	tmpDir := filepath.Join(project.Dir, ".kine", "tmp")
	if err := os.MkdirAll(tmpDir, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", tmpDir, err)
	}

	result, err := p.run(ctx, toolchain.Command{
		Path: options.KotlinCompiler,
		Args: kotlinArgs(options, output, classpath, javaRoots, sources),
		Dir:  project.Dir,
		Env:  toolchain.BuildEnv(jdk, tmpDir, nil),
	})
	if err != nil {
		return toolError("kotlinc", result, err)
	}
	return nil
}

func kotlinArgs(options Options, output string, classpath, javaRoots, sources []string) []string {
	args := []string{
		"-no-stdlib",
		"-no-reflect",
		"-jvm-target", kotlinJvmTarget(javaVersion(options)),
		"-classpath", strings.Join(classpath, string(os.PathListSeparator)),
		"-d", output,
	}
	if options.Compose && options.ComposePlugin != "" {
		args = append(args, "-Xplugin="+options.ComposePlugin)
	}
	if len(javaRoots) > 0 {
		args = append(args, "-Xjava-source-roots="+strings.Join(javaRoots, ","))
	}
	return append(args, sources...)
}

func kotlinJvmTarget(major int) string {
	if major == 8 {
		return "1.8"
	}
	return strconv.Itoa(major)
}
