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

const androidManifestMergerMain = "com.android.manifmerger.Merger"

// manifestMergerImplVersion is part of the resource task key. Bump it when the
// merger implementation or fallback behavior changes.
const manifestMergerImplVersion = 2

type manifestMergerTool struct {
	Version   string
	Main      string
	Classpath []string
}

func (p *Provider) androidManifestMerger(ctx context.Context, workspaceRoot string, options Options) (*manifestMergerTool, error) {
	if p.config.Cache == nil {
		return nil, fmt.Errorf("manifest merger requires the workspace cache")
	}
	version := options.ManifestMergerVersion
	if version == "" {
		version = defaultManifestMergerVersion
	}

	p.mergerMu.Lock()
	defer p.mergerMu.Unlock()
	if p.mergers == nil {
		p.mergers = make(map[string]*manifestMergerTool)
	}
	if tool, ok := p.mergers[version]; ok {
		return tool, nil
	}

	toolRoot := filepath.Join(workspaceRoot, ".kine", "tools", "manifest-merger", version)
	if err := os.MkdirAll(toolRoot, 0o755); err != nil {
		return nil, fmt.Errorf("create %s: %w", toolRoot, err)
	}
	repositories, err := deps.ToolRepositories(workspaceRoot)
	if err != nil {
		return nil, err
	}
	result, err := deps.Resolve(ctx, toolRoot, p.config.Cache, []deps.Dependency{{
		Coordinate: deps.Coordinate{
			Group:     "com.android.tools.build",
			Artifact:  "manifest-merger",
			Version:   version,
			Extension: "jar",
		},
		Scope: deps.ScopeRuntime,
	}}, deps.Options{
		Repositories: repositories,
		Update:       p.config.Update,
		Offline:      p.config.Offline,
		Logger:       p.config.Logger,
	})
	if err != nil {
		return nil, fmt.Errorf("resolve manifest-merger: %w", err)
	}

	classpath := make([]string, 0, len(result.Artifacts))
	for _, artifact := range result.Artifacts {
		if artifact.Extension == "jar" {
			classpath = append(classpath, artifact.Path)
		}
	}
	if len(classpath) == 0 {
		return nil, fmt.Errorf("manifest-merger classpath is empty")
	}
	tool := &manifestMergerTool{
		Version:   version,
		Main:      androidManifestMergerMain,
		Classpath: classpath,
	}
	p.mergers[version] = tool
	return tool, nil
}

func (p *Provider) mergeManifests(ctx context.Context, workspaceRoot string, project *core.Project, options Options, buildDir string, jdk *toolchain.JDK) (string, error) {
	useAndroid := options.ManifestMerger == "android"
	if options.ManifestMerger == "auto" {
		useAndroid = len(options.AARManifests) > 0 || len(options.Manifests) > 1
	}
	if useAndroid {
		tool, err := p.androidManifestMerger(ctx, workspaceRoot, options)
		if err == nil {
			return p.runAndroidManifestMerger(ctx, project, options, buildDir, jdk, tool)
		}
		if options.ManifestMerger == "android" {
			return "", err
		}
		if p.config.Logger != nil {
			p.config.Logger.Warn("manifest merger fallback", "error", err)
		}
	}
	return mergeVariantManifest(buildDir, options)
}

func (p *Provider) runAndroidManifestMerger(ctx context.Context, project *core.Project, options Options, buildDir string, jdk *toolchain.JDK, tool *manifestMergerTool) (string, error) {
	base := options.BaseManifest
	if base == "" && len(options.Manifests) > 0 {
		base = options.Manifests[0]
	}
	if base == "" {
		base = options.Manifest
	}
	values, err := manifestValues(base, options)
	if err != nil {
		return "", err
	}

	target := filepath.Join(buildDir, "AndroidManifest.xml")
	if err := os.Remove(target); err != nil && !os.IsNotExist(err) {
		return "", fmt.Errorf("clean %s: %w", target, err)
	}

	args := []string{
		"-cp", strings.Join(tool.Classpath, string(os.PathListSeparator)),
		tool.Main,
		"--main", base,
		"--out", target,
		"--log", "WARNING",
		"--remove-tools-declarations",
		"--property", "VERSION_CODE=" + strconv.Itoa(options.effectiveVersionCode()),
		"--property", "VERSION_NAME=" + options.effectiveVersionName(),
		"--property", "MIN_SDK_VERSION=" + strconv.Itoa(options.MinSDK),
	}
	if options.TargetSDK > 0 {
		args = append(args, "--property", "TARGET_SDK_VERSION="+strconv.Itoa(options.TargetSDK))
	}
	if len(options.AARManifests) > 0 {
		args = append(args, "--libs", strings.Join(options.AARManifests, string(os.PathListSeparator)))
	}
	var overlays []string
	for _, manifest := range options.Manifests {
		if manifest == base {
			continue
		}
		overlays = append(overlays, manifest)
	}
	if len(overlays) > 0 {
		args = append(args, "--overlays", strings.Join(overlays, string(os.PathListSeparator)))
	}
	for key, value := range values {
		args = append(args, "--placeholder", key+"="+value)
	}

	result, err := p.run(ctx, toolchain.Command{
		Path: jdk.Java,
		Args: args,
		Dir:  project.Dir,
		Env:  toolchain.BuildEnv(jdk, filepath.Join(project.Dir, ".kine", "tmp"), nil),
	})
	if err != nil {
		return "", toolError("manifest merger", result, err)
	}
	if !isRegular(target) {
		return "", fmt.Errorf("manifest merger produced no %s", target)
	}

	merged, err := parseManifest(target)
	if err != nil {
		return "", err
	}
	if values["applicationId"] != values["packageName"] {
		qualifyRelativeComponentNames(merged, values["packageName"])
	}
	setManifestAttribute(merged, "", "package", values["packageName"])
	if options.Variant.Name != "" {
		if application := findManifestChild(merged, "application"); application != nil {
			setManifestAttribute(application, "android", "debuggable", strconv.FormatBool(options.Variant.BuildTypeOptions.Debug))
		}
	}
	if err := writeManifestFile(target, merged); err != nil {
		return "", err
	}
	return target, nil
}
