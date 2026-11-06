package android

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gabriel-aplok/kine-build-java/internal/core"
	"github.com/gabriel-aplok/kine-build-java/internal/deps"
	"github.com/gabriel-aplok/kine-build-java/internal/toolchain"
)

func (p *Provider) lintTask(workspaceRoot string, project *core.Project, options Options, sdk *SDK, jdk *toolchain.JDK, artifacts []deps.Artifact) (*core.Task, error) {
	manifest := options.androidPath(project.Dir, "AndroidManifest.xml")
	classes := options.androidPath(project.Dir, "classes")
	reportDir := filepath.Join(project.Dir, "build", "reports", "lint", options.variantLabel())

	inputs := []core.Input{
		{Path: mustRelative(workspaceRoot, manifest), Mode: core.ModeFile},
		{Path: mustRelative(workspaceRoot, classes), Mode: core.ModeTree},
	}
	depsIDs := []string{
		project.ID + ":" + options.taskID("android-resources"),
		project.ID + ":" + options.taskID("compile-java"),
	}
	for _, dir := range append(append([]string(nil), options.JavaDirs...), options.KotlinDirs...) {
		inputs = append(inputs, core.Input{Path: mustRelative(workspaceRoot, dir), Mode: core.ModeTree})
	}
	for _, dir := range append(append([]string(nil), options.AARResDirs...), options.ResDirs...) {
		inputs = append(inputs, core.Input{Path: mustRelative(workspaceRoot, dir), Mode: core.ModeTree})
	}
	for _, artifact := range artifacts {
		if artifact.Extension == "aar" || strings.HasPrefix(artifact.Classifier, "natives-") {
			continue
		}
		inputs = append(inputs, core.Input{Path: mustRelative(workspaceRoot, artifact.Path), Mode: core.ModeFile})
	}

	return &core.Task{
		ID:      options.taskID("lint"),
		Deps:    depsIDs,
		Inputs:  inputs,
		Outputs: []core.Output{{Path: mustRelative(workspaceRoot, reportDir), Mode: core.ModeTree}},
		Key: fmt.Sprintf(
			"android-lint v=%d sdk=%d java=%d variant=%s",
			androidTaskVersion,
			options.CompileSDK,
			javaVersion(options),
			options.variantLabel(),
		),
		Run: func(ctx context.Context) error {
			return p.runLint(ctx, project, options, sdk, jdk, artifacts)
		},
	}, nil
}

func (p *Provider) runLint(ctx context.Context, project *core.Project, options Options, sdk *SDK, jdk *toolchain.JDK, artifacts []deps.Artifact) error {
	if sdk.Lint == "" {
		return fmt.Errorf("Android lint was not found; install cmdline-tools")
	}
	lintDir := options.androidPath(project.Dir, "lint")
	if err := os.RemoveAll(lintDir); err != nil {
		return fmt.Errorf("clean %s: %w", lintDir, err)
	}
	if err := os.MkdirAll(lintDir, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", lintDir, err)
	}

	lintManifest := filepath.Join(lintDir, "AndroidManifest.xml")
	if err := writeLintManifest(options.androidPath(project.Dir, "AndroidManifest.xml"), lintManifest, options); err != nil {
		return err
	}

	reportDir := filepath.Join(project.Dir, "build", "reports", "lint", options.variantLabel())
	if err := os.RemoveAll(reportDir); err != nil {
		return fmt.Errorf("clean %s: %w", reportDir, err)
	}
	if err := os.MkdirAll(reportDir, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", reportDir, err)
	}

	args := []string{
		"--exitcode",
		"--quiet",
		"--sdk-home", sdk.Root,
		"--classpath", sdk.AndroidJar,
		"--classpath", options.androidPath(project.Dir, "classes"),
		"--text", filepath.Join(reportDir, "lint-results.txt"),
		"--html", filepath.Join(reportDir, "lint-results.html"),
	}
	jars, err := classpathJars(artifacts, filepath.Join(project.Dir, "build", "android", "aar"))
	if err != nil {
		return err
	}
	for _, jar := range jars {
		args = append(args, "--libraries", jar)
	}
	for _, dir := range append(append([]string(nil), options.JavaDirs...), options.KotlinDirs...) {
		args = append(args, "--sources", dir)
	}
	for _, dir := range append(append([]string(nil), options.AARResDirs...), options.ResDirs...) {
		args = append(args, "--resources", dir)
	}
	args = append(args, lintDir)

	result, err := p.run(ctx, toolchain.Command{
		Path: sdk.Lint,
		Args: args,
		Dir:  project.Dir,
		Env:  toolchain.BuildEnv(jdk, filepath.Join(project.Dir, ".kine", "tmp"), nil),
	})
	if err != nil {
		return toolError("android lint", result, err)
	}
	return nil
}

func writeLintManifest(source, target string, options Options) error {
	root, err := parseManifest(source)
	if err != nil {
		return err
	}
	removeManifestAttribute(root, "android", "debuggable")
	if application := findManifestChild(root, "application"); application != nil {
		removeManifestAttribute(application, "android", "debuggable")
	}
	setManifestAttribute(root, "android", "versionCode", strconv.Itoa(options.effectiveVersionCode()))
	setManifestAttribute(root, "android", "versionName", options.effectiveVersionName())
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Dir(target), err)
	}
	return writeManifestFile(target, root)
}
