package android

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"

	"github.com/gabriel-aplok/kine-build-java/internal/core"
	"github.com/gabriel-aplok/kine-build-java/internal/store"
	"github.com/gabriel-aplok/kine-build-java/internal/toolchain"
)

// MinBuildJDK is the oldest JDK that can compile Android sources.
const MinBuildJDK = 17

// androidTaskVersion is part of every task key. Bump it when the Android
// pipeline changes in a way that affects outputs without changing inputs.
const androidTaskVersion = 13

// Config configures an android provider.
type Config struct {
	Workspace *core.Workspace
	Cache     *store.Store
	Logger    *slog.Logger
	Update    bool
	Offline   bool
	Device    string
	Variant   string
}

// Provider builds Android APKs.
type Provider struct {
	config  Config
	device  string
	variant string
	findSDK func(Requirement, ...string) (*SDK, error)
	findJDK func(toolchain.Requirement, ...string) (*toolchain.JDK, error)
	run     func(context.Context, toolchain.Command) (toolchain.Result, error)

	mergerMu sync.Mutex
	mergers  map[string]*manifestMergerTool
}

// NewProvider returns an android provider.
func NewProvider(config Config) *Provider {
	return &Provider{
		config:  config,
		device:  config.Device,
		variant: config.Variant,
		findSDK: FindSDK,
		findJDK: toolchain.Find,
		run:     toolchain.Run,
		mergers: make(map[string]*manifestMergerTool),
	}
}

func (p *Provider) Name() string {
	return "android"
}

func (p *Provider) Tasks(ctx context.Context, project *core.Project) ([]*core.Task, error) {
	if project == nil {
		return nil, fmt.Errorf("project is nil")
	}

	options, err := decodeOptions(project.Options, project.Dir)
	if err != nil {
		return nil, fmt.Errorf("project %s: %w", project.ID, err)
	}

	root := p.workspaceRoot(project)
	if p.variant != "" {
		variant, err := selectVariant(options.Variants, p.variant)
		if err != nil {
			return nil, fmt.Errorf("project %s: %w", project.ID, err)
		}
		options.Variants = []Variant{variant}
		options.Legacy = variant.Flavor == "" && variant.BuildType == "debug"
	}
	if err := p.prepareKotlin(root, &options); err != nil {
		return nil, fmt.Errorf("project %s: %w", project.ID, err)
	}
	if err := p.prepareBundletool(ctx, root, &options); err != nil {
		return nil, fmt.Errorf("project %s: %w", project.ID, err)
	}
	hasUnitTests := len(options.TestDirs) > 0
	if hasUnitTests {
		if err := p.prepareJUnit(ctx, root, &options); err != nil {
			return nil, fmt.Errorf("project %s: %w", project.ID, err)
		}
	}

	requirement := Requirement{
		CompileSDK: options.CompileSDK,
		BuildTools: options.BuildTools,
	}
	if options.Native != nil {
		requirement.Native = true
		requirement.NDKVersion = options.Native.NDKVersion
		requirement.CMakeVersion = options.Native.CMakeVersion
	}
	for _, module := range options.Modules {
		if module.Native == nil {
			continue
		}
		requirement.Native = true
		if requirement.NDKVersion == "" {
			requirement.NDKVersion = module.Native.NDKVersion
		}
		if requirement.CMakeVersion == "" {
			requirement.CMakeVersion = module.Native.CMakeVersion
		}
	}

	sdk, err := p.findSDK(requirement)
	if err != nil {
		return nil, fmt.Errorf("project %s: %w", project.ID, err)
	}
	if options.TargetSDK == 0 {
		options.TargetSDK = platformMajor(sdk.Platform)
	}

	jdk, err := p.findJDK(toolchain.Requirement{})
	if err != nil {
		return nil, fmt.Errorf("project %s: %w", project.ID, err)
	}
	if jdk.Major < MinBuildJDK {
		return nil, fmt.Errorf("project %s: android requires JDK %d or later, found %d", project.ID, MinBuildJDK, jdk.Major)
	}

	artifacts, err := p.resolveDependencies(ctx, project, options)
	if err != nil {
		return nil, err
	}
	testArtifacts, err := p.resolveTestDependencies(ctx, project, options)
	if err != nil {
		return nil, err
	}
	if options.Kotlin != "" {
		artifacts = withoutKotlinStdlib(artifacts)
	}
	if err := p.prepareAARs(project.Dir, &options, artifacts); err != nil {
		return nil, fmt.Errorf("project %s: %w", project.ID, err)
	}

	siblings, siblingTasks, err := p.siblingClasses(project)
	if err != nil {
		return nil, fmt.Errorf("project %s: %w", project.ID, err)
	}

	native, err := p.nativeTask(root, project, options, sdk, jdk)
	if err != nil {
		return nil, fmt.Errorf("project %s: %w", project.ID, err)
	}
	dexLibs, err := p.dexLibTask(root, project, options, sdk, jdk, artifacts)
	if err != nil {
		return nil, fmt.Errorf("project %s: %w", project.ID, err)
	}

	var tasks []*core.Task
	if native != nil {
		tasks = append(tasks, native)
	}
	if dexLibs != nil {
		tasks = append(tasks, dexLibs)
	}

	variants := options.Variants
	if len(variants) == 0 {
		variants = []Variant{{BuildType: "debug", Name: "debug", Suffix: "debug", Sources: VariantSources{
			JavaDirs:   options.JavaDirs,
			KotlinDirs: options.KotlinDirs,
			ResDirs:    options.ResDirs,
			AssetDirs:  options.AssetDirs,
			Manifests:  options.Manifests,
		}}}
	}

	runOptions := (*Options)(nil)
	for _, variant := range variants {
		variantOptions := options.forVariant(variant, options.Legacy)
		resources, err := p.resourceTask(root, project, variantOptions, sdk, jdk)
		if err != nil {
			return nil, fmt.Errorf("project %s: %w", project.ID, err)
		}
		kotlin, err := p.kotlinTask(root, project, variantOptions, sdk, jdk, artifacts, siblings, siblingTasks)
		if err != nil {
			return nil, fmt.Errorf("project %s: %w", project.ID, err)
		}
		compile, err := p.compileTask(root, project, variantOptions, sdk, jdk, artifacts, siblings, siblingTasks)
		if err != nil {
			return nil, fmt.Errorf("project %s: %w", project.ID, err)
		}
		dex, err := p.dexTask(root, project, variantOptions, sdk, jdk, artifacts, siblings, siblingTasks)
		if err != nil {
			return nil, fmt.Errorf("project %s: %w", project.ID, err)
		}
		apk, err := p.apkTask(root, project, variantOptions, sdk, jdk, artifacts, dexLibs != nil)
		if err != nil {
			return nil, fmt.Errorf("project %s: %w", project.ID, err)
		}
		var moduleTasks []*core.Task
		var moduleZips []string
		var moduleTaskIDs []string
		if variantOptions.Bundle && len(variantOptions.Modules) > 0 {
			moduleTasks, moduleZips, moduleTaskIDs, err = p.moduleTasks(root, project, variantOptions, variantOptions, sdk, jdk, artifacts)
			if err != nil {
				return nil, fmt.Errorf("project %s: %w", project.ID, err)
			}
			for index, id := range moduleTaskIDs {
				moduleTaskIDs[index] = project.ID + ":" + id
			}
		}
		bundle, err := p.bundleTask(root, project, variantOptions, sdk, jdk, artifacts, dexLibs != nil, moduleZips, moduleTaskIDs)
		if err != nil {
			return nil, fmt.Errorf("project %s: %w", project.ID, err)
		}
		tasks = append(tasks, resources)
		if kotlin != nil {
			tasks = append(tasks, kotlin)
		}
		tasks = append(tasks, compile, dex, apk)
		tasks = append(tasks, moduleTasks...)
		if bundle != nil {
			tasks = append(tasks, bundle)
		}

		lint, err := p.lintTask(root, project, variantOptions, sdk, jdk, artifacts)
		if err != nil {
			return nil, fmt.Errorf("project %s: %w", project.ID, err)
		}
		tasks = append(tasks, lint)
		if hasUnitTests {
			test, err := p.testTask(root, project, variantOptions, sdk, jdk, artifacts, testArtifacts)
			if err != nil {
				return nil, fmt.Errorf("project %s: %w", project.ID, err)
			}
			tasks = append(tasks, test)
		}
		if len(variantOptions.AndroidTestDirs) > 0 {
			testAPK, err := p.androidTestAPKTask(root, project, variantOptions, sdk, jdk, artifacts, testArtifacts)
			if err != nil {
				return nil, fmt.Errorf("project %s: %w", project.ID, err)
			}
			instrument, err := p.instrumentTask(root, project, variantOptions, sdk, jdk)
			if err != nil {
				return nil, fmt.Errorf("project %s: %w", project.ID, err)
			}
			tasks = append(tasks, testAPK, instrument)
		}
		if runOptions == nil && variant.BuildType == "debug" {
			copied := variantOptions
			runOptions = &copied
		}
	}
	if runOptions == nil {
		copied := options.forVariant(variants[0], options.Legacy)
		runOptions = &copied
	}
	run, err := p.runTask(root, project, *runOptions, sdk, jdk)
	if err != nil {
		return nil, fmt.Errorf("project %s: %w", project.ID, err)
	}
	if run != nil {
		tasks = append(tasks, run)
	}
	return tasks, nil
}

func (p *Provider) siblingClasses(project *core.Project) ([]string, []string, error) {
	if len(project.DependsOn) == 0 {
		return nil, nil, nil
	}
	if p.config.Workspace == nil {
		return nil, nil, fmt.Errorf("workspace is required for depends_on")
	}

	dirs := make([]string, 0, len(project.DependsOn))
	tasks := make([]string, 0, len(project.DependsOn))
	for _, dependencyID := range project.DependsOn {
		dependency, ok := p.config.Workspace.Projects[dependencyID]
		if !ok {
			return nil, nil, fmt.Errorf("dependency project %q not found", dependencyID)
		}
		dirs = append(dirs, filepath.Join(dependency.Dir, "build", "classes", "java", "main"))
		tasks = append(tasks, dependencyID+":compile-java")
	}
	return dirs, tasks, nil
}

func (p *Provider) workspaceRoot(project *core.Project) string {
	if p.config.Workspace != nil && p.config.Workspace.Root != "" {
		return p.config.Workspace.Root
	}
	return project.Dir
}

func relativePath(root, path string) (string, error) {
	relative, err := filepath.Rel(root, path)
	if err != nil {
		return "", fmt.Errorf("relative path %s: %w", path, err)
	}
	if relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path %q escapes the workspace", path)
	}
	return filepath.ToSlash(relative), nil
}
