package android

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gabriel-aplok/kine-build-java/internal/core"
	"github.com/gabriel-aplok/kine-build-java/internal/toolchain"
)

const (
	resourcesAPKName = "resources.apk"
	genDirName       = "gen"
)

func (p *Provider) resourceTask(workspaceRoot string, project *core.Project, options Options, sdk *SDK, jdk *toolchain.JDK) (*core.Task, error) {
	manifests := append(append([]string(nil), options.AARManifests...), options.Manifests...)
	if len(manifests) == 0 {
		manifests = []string{options.Manifest}
	}
	resDirs := append(append([]string(nil), options.AARResDirs...), options.ResDirs...)
	inputs := make([]core.Input, 0, len(manifests)+len(resDirs))
	for _, manifest := range manifests {
		path, err := relativePath(workspaceRoot, manifest)
		if err != nil {
			return nil, err
		}
		inputs = append(inputs, core.Input{Path: path, Mode: core.ModeFile})
	}
	for _, dir := range resDirs {
		path, err := relativePath(workspaceRoot, dir)
		if err != nil {
			return nil, err
		}
		inputs = append(inputs, core.Input{Path: path, Mode: core.ModeTree})
	}

	resourcesAPK, err := relativePath(workspaceRoot, options.androidPath(project.Dir, resourcesAPKName))
	if err != nil {
		return nil, err
	}
	genDir, err := relativePath(workspaceRoot, options.androidPath(project.Dir, genDirName))
	if err != nil {
		return nil, err
	}
	outputs := []core.Output{
		{Path: resourcesAPK, Mode: core.ModeFile},
		{Path: genDir, Mode: core.ModeTree},
		{Path: mustRelative(workspaceRoot, options.androidPath(project.Dir, "AndroidManifest.xml")), Mode: core.ModeFile},
	}
	if options.Variant.BuildTypeOptions.ShrinkResources {
		outputs = append(outputs,
			core.Output{Path: mustRelative(workspaceRoot, options.androidPath(project.Dir, "res-shrunk")), Mode: core.ModeTree},
			core.Output{Path: mustRelative(workspaceRoot, options.resourcesProtoAPKPath(project.Dir)), Mode: core.ModeFile},
		)
		for _, sourceDir := range append(append([]string(nil), options.JavaDirs...), options.KotlinDirs...) {
			if path, err := relativePath(workspaceRoot, sourceDir); err == nil {
				inputs = append(inputs, core.Input{Path: path, Mode: core.ModeTree})
			}
		}
	}

	return &core.Task{
		ID:      options.taskID("android-resources"),
		Inputs:  inputs,
		Outputs: outputs,
		Key: fmt.Sprintf(
			"aapt2 %s v=%d merger=%s:%s:%d min=%d target=%d code=%d name=%s extra=%s shrink=%t variant=%s",
			sdk.BuildTools,
			androidTaskVersion,
			options.ManifestMerger,
			options.ManifestMergerVersion,
			manifestMergerImplVersion,
			options.MinSDK,
			options.TargetSDK,
			options.effectiveVersionCode(),
			options.effectiveVersionName(),
			strings.Join(options.ExtraPackages, ","),
			options.Variant.BuildTypeOptions.ShrinkResources,
			options.variantLabel(),
		),
		Run: func(ctx context.Context) error {
			return p.linkResources(ctx, project, options, sdk, jdk)
		},
	}, nil
}

func (p *Provider) linkResources(ctx context.Context, project *core.Project, options Options, sdk *SDK, jdk *toolchain.JDK) error {
	buildDir := options.androidPath(project.Dir)
	if err := os.MkdirAll(buildDir, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", buildDir, err)
	}
	manifest, err := p.mergeManifests(ctx, p.workspaceRoot(project), project, options, buildDir, jdk)
	if err != nil {
		return err
	}
	linkOptions := options
	linkOptions.Manifest = manifest
	genDir := filepath.Join(buildDir, genDirName)
	if err := os.RemoveAll(genDir); err != nil {
		return fmt.Errorf("clean %s: %w", genDir, err)
	}
	if err := os.MkdirAll(genDir, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", genDir, err)
	}
	resourcesAPK := filepath.Join(buildDir, resourcesAPKName)
	if err := os.Remove(resourcesAPK); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("clean %s: %w", resourcesAPK, err)
	}
	tmpDir := filepath.Join(project.Dir, ".kine", "tmp")
	if err := os.MkdirAll(tmpDir, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", tmpDir, err)
	}

	resDirs := append(append([]string(nil), options.AARResDirs...), options.ResDirs...)
	if options.Variant.BuildTypeOptions.ShrinkResources {
		shrunk := filepath.Join(buildDir, "res-shrunk")
		sourceDirs := append(append([]string(nil), options.JavaDirs...), options.KotlinDirs...)
		// AAR resources stay intact because library code is not part of the
		// source graph used by the shrinker.
		filtered, err := shrinkResources(options.ResDirs, sourceDirs, manifest, shrunk)
		if err != nil {
			return err
		}
		resDirs = append(append([]string(nil), options.AARResDirs...), filtered...)
	}
	flats := make([]string, 0, len(resDirs))
	for index, dir := range resDirs {
		flat := filepath.Join(buildDir, fmt.Sprintf("res-%d.zip", index))
		if err := os.Remove(flat); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("clean %s: %w", flat, err)
		}
		result, err := p.run(ctx, toolchain.Command{
			Path: sdk.Aapt2,
			Args: []string{"compile", "--dir", dir, "-o", flat},
			Dir:  project.Dir,
			Env:  toolchain.BuildEnv(jdk, tmpDir, nil),
		})
		if err != nil {
			return toolError("aapt2 compile", result, err)
		}
		flats = append(flats, flat)
	}

	result, err := p.run(ctx, toolchain.Command{
		Path: sdk.Aapt2,
		Args: linkArgs(resourcesAPK, genDir, linkOptions, sdk, flats, ""),
		Dir:  project.Dir,
		Env:  toolchain.BuildEnv(jdk, tmpDir, nil),
	})
	if err != nil {
		return toolError("aapt2 link", result, err)
	}
	if options.Variant.BuildTypeOptions.ShrinkResources {
		proto := options.resourcesProtoAPKPath(project.Dir)
		if err := os.Remove(proto); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("clean %s: %w", proto, err)
		}
		result, err = p.run(ctx, toolchain.Command{
			Path: sdk.Aapt2,
			Args: protoLinkArgs(proto, linkOptions, sdk, flats),
			Dir:  project.Dir,
			Env:  toolchain.BuildEnv(jdk, tmpDir, nil),
		})
		if err != nil {
			return toolError("aapt2 link --proto-format", result, err)
		}
	}
	return nil
}

func protoLinkArgs(output string, options Options, sdk *SDK, flats []string) []string {
	args := []string{
		"link",
		"-o", output,
		"-I", sdk.AndroidJar,
		"--manifest", options.Manifest,
		"--proto-format",
		"--min-sdk-version", strconv.Itoa(options.MinSDK),
		"--version-code", strconv.Itoa(options.effectiveVersionCode()),
		"--version-name", options.effectiveVersionName(),
	}
	if options.TargetSDK > 0 {
		args = append(args, "--target-sdk-version", strconv.Itoa(options.TargetSDK))
	}
	if len(flats) > 1 {
		args = append(args, "--auto-add-overlay")
	}
	if len(flats) > 0 {
		args = append(args, flats[0])
		for _, flat := range flats[1:] {
			args = append(args, "-R", flat)
		}
	}
	return args
}

func linkArgs(resourcesAPK, genDir string, options Options, sdk *SDK, flats []string, proguard string) []string {
	args := []string{
		"link",
		"-o", resourcesAPK,
		"-I", sdk.AndroidJar,
		"--manifest", options.Manifest,
		"--java", genDir,
		"--min-sdk-version", strconv.Itoa(options.MinSDK),
		"--version-code", strconv.Itoa(options.effectiveVersionCode()),
		"--version-name", options.effectiveVersionName(),
	}
	if options.TargetSDK > 0 {
		args = append(args, "--target-sdk-version", strconv.Itoa(options.TargetSDK))
	}
	if options.ApplicationID != "" && options.ApplicationID != options.PackageName {
		args = append(args,
			"--rename-manifest-package", options.ApplicationID,
			"--custom-package", options.PackageName,
		)
	}
	if proguard != "" {
		args = append(args, "--proguard", proguard)
	}
	if len(flats) > 1 {
		args = append(args, "--auto-add-overlay")
	}
	for _, name := range options.ExtraPackages {
		args = append(args, "--extra-packages", name)
	}
	if len(flats) > 0 {
		args = append(args, flats[0])
		for _, flat := range flats[1:] {
			args = append(args, "-R", flat)
		}
	}
	return args
}

func toolError(name string, result toolchain.Result, err error) error {
	output := strings.TrimSpace(string(result.Stdout) + "\n" + string(result.Stderr))
	if output == "" {
		return fmt.Errorf("%s: %w", name, err)
	}
	return fmt.Errorf("%s: %w\n%s", name, err, output)
}
