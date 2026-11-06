// Package jvm builds Java and Kotlin projects.
package jvm

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/gabriel-aplok/kine-build-java/internal/core"
	"github.com/gabriel-aplok/kine-build-java/internal/deps"
	"github.com/gabriel-aplok/kine-build-java/internal/store"
	"github.com/gabriel-aplok/kine-build-java/internal/toolchain"
)

// Config configures a jvm provider.
type Config struct {
	Workspace *core.Workspace
	Cache     *store.Store
	Logger    *slog.Logger
	Update    bool
	Offline   bool
	RunArgs   []string
}

// Provider builds jvm tasks.
type Provider struct {
	workspace *core.Workspace
	cache     *store.Store
	logger    *slog.Logger
	update    bool
	offline   bool
	runArgs   []string
	findJDK   func(toolchain.Requirement, ...string) (*toolchain.JDK, error)
	run       func(context.Context, toolchain.Command) (toolchain.Result, error)
	tools     map[string]*toolSet
}

// NewProvider returns a jvm provider.
func NewProvider(config Config) *Provider {
	logger := config.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &Provider{
		workspace: config.Workspace,
		cache:     config.Cache,
		logger:    logger,
		update:    config.Update,
		offline:   config.Offline,
		runArgs:   append([]string(nil), config.RunArgs...),
		findJDK:   toolchain.Find,
		run:       toolchain.Run,
		tools:     make(map[string]*toolSet),
	}
}

func (p *Provider) Name() string {
	return "jvm"
}

func (p *Provider) Tasks(ctx context.Context, project *core.Project) ([]*core.Task, error) {
	options, err := decodeOptions(project.Options, project.ID)
	if err != nil {
		return nil, fmt.Errorf("project %s: %w", project.ID, err)
	}
	build, err := p.prepare(ctx, project, options)
	if err != nil {
		return nil, err
	}
	return build.tasks, nil
}

type build struct {
	project     *core.Project
	options     Options
	sources     sourceDirs
	jdk         *toolchain.JDK
	artifacts   []deps.Artifact
	kotlin      *toolSet
	junit       *toolSet
	mainCompile []string
	mainRuntime []string
	testCompile []string
	testRuntime []string
	tasks       []*core.Task
}

type sourceDirs struct {
	Java          []string
	Kotlin        []string
	Resources     []string
	TestJava      []string
	TestKotlin    []string
	TestResources []string
}

func (p *Provider) prepare(ctx context.Context, project *core.Project, options Options) (*build, error) {
	sources, err := discoverSources(project.Dir, options.Sources)
	if err != nil {
		return nil, fmt.Errorf("project %s: %w", project.ID, err)
	}
	if (len(sources.Kotlin) > 0 || len(sources.TestKotlin) > 0) && options.Kotlin == "" {
		return nil, fmt.Errorf("project %s: kotlin sources require the kotlin option", project.ID)
	}

	jdk, err := p.findJDK(toolchain.Requirement{Major: options.Java})
	if err != nil {
		return nil, fmt.Errorf("project %s: %w", project.ID, err)
	}

	artifacts, err := p.resolveDependencies(ctx, project, options, sources)
	if err != nil {
		return nil, err
	}
	hasKotlin := len(sources.Kotlin) > 0
	hasTestKotlin := len(sources.TestKotlin) > 0
	paths, err := p.computeClasspaths(project, artifacts, hasKotlin, hasTestKotlin)
	if err != nil {
		return nil, fmt.Errorf("project %s: %w", project.ID, err)
	}

	build := &build{
		project:     project,
		options:     options,
		sources:     sources,
		jdk:         jdk,
		artifacts:   artifacts,
		mainCompile: paths.mainCompile,
		mainRuntime: paths.mainRuntime,
		testCompile: paths.testCompile,
		testRuntime: paths.testRuntime,
	}
	if len(sources.Kotlin) > 0 || len(sources.TestKotlin) > 0 {
		build.kotlin, err = p.resolveKotlinTools(ctx, options.Kotlin, options.Compose)
		if err != nil {
			return nil, fmt.Errorf("project %s: %w", project.ID, err)
		}
	}
	if err := p.addJavaTask(build); err != nil {
		return nil, fmt.Errorf("project %s: %w", project.ID, err)
	}
	if err := p.addKotlinTask(build); err != nil {
		return nil, fmt.Errorf("project %s: %w", project.ID, err)
	}
	if err := p.addResourceTasks(build); err != nil {
		return nil, fmt.Errorf("project %s: %w", project.ID, err)
	}
	if err := p.addJarTask(build); err != nil {
		return nil, fmt.Errorf("project %s: %w", project.ID, err)
	}
	if err := p.addTestTasks(ctx, build); err != nil {
		return nil, fmt.Errorf("project %s: %w", project.ID, err)
	}
	if err := p.addRunTask(build); err != nil {
		return nil, fmt.Errorf("project %s: %w", project.ID, err)
	}
	return build, nil
}

func (p *Provider) resolveDependencies(ctx context.Context, project *core.Project, options Options, sources sourceDirs) ([]deps.Artifact, error) {
	roots, err := options.dependencyList()
	if err != nil {
		return nil, fmt.Errorf("project %s: %w", project.ID, err)
	}

	if len(sources.Kotlin) > 0 || len(sources.TestKotlin) > 0 {
		if options.Kotlin == "" {
			return nil, fmt.Errorf("project %s: kotlin sources require the kotlin option", project.ID)
		}
		coordinate, err := deps.ParseCoordinate("org.jetbrains.kotlin:kotlin-stdlib:" + options.Kotlin)
		if err != nil {
			return nil, fmt.Errorf("project %s: %w", project.ID, err)
		}
		roots = append(roots, deps.Dependency{Coordinate: coordinate, Scope: deps.ScopeCompile})
	}

	if len(roots) == 0 {
		return nil, nil
	}
	if p.cache == nil {
		return nil, fmt.Errorf("project %s: dependency resolution requires a store", project.ID)
	}

	repositories, err := deps.LoadRepositories(p.workspaceRoot(project))
	if err != nil {
		return nil, fmt.Errorf("project %s: %w", project.ID, err)
	}
	result, err := deps.Resolve(ctx, project.Dir, p.cache, roots, deps.Options{
		Repositories: repositories,
		Update:       p.update,
		Offline:      p.offline,
		Logger:       p.logger,
	})
	if err != nil {
		return nil, fmt.Errorf("project %s: resolve dependencies: %w", project.ID, err)
	}
	return result.Artifacts, nil
}

func (p *Provider) workspaceRoot(project *core.Project) string {
	if p.workspace != nil && p.workspace.Root != "" {
		return p.workspace.Root
	}
	return project.Dir
}

type classpaths struct {
	mainCompile []string
	mainRuntime []string
	testCompile []string
	testRuntime []string
}

func (p *Provider) computeClasspaths(project *core.Project, artifacts []deps.Artifact, includeKotlin, includeTestKotlin bool) (classpaths, error) {
	var siblings []string
	if p.workspace != nil {
		for _, dependencyID := range project.DependsOn {
			dependency, ok := p.workspace.Projects[dependencyID]
			if !ok {
				return classpaths{}, fmt.Errorf("dependency project %q not found", dependencyID)
			}
			siblings = append(siblings,
				filepath.Join(dependency.Dir, "build", "classes", "java", "main"),
				filepath.Join(dependency.Dir, "build", "classes", "kotlin", "main"),
				filepath.Join(dependency.Dir, "build", "resources", "main"),
			)
		}
	}

	byScope := make(map[deps.Scope][]string)
	for _, artifact := range artifacts {
		byScope[artifact.Scope] = append(byScope[artifact.Scope], artifact.Path)
	}
	compileArtifacts := nonNativePaths(artifacts, deps.ScopeCompile)
	providedArtifacts := nonNativePaths(artifacts, deps.ScopeProvided)
	testArtifacts := nonNativePaths(artifacts, deps.ScopeTest)

	mainJava := filepath.Join(project.Dir, "build", "classes", "java", "main")
	mainKotlin := filepath.Join(project.Dir, "build", "classes", "kotlin", "main")
	mainResources := filepath.Join(project.Dir, "build", "resources", "main")
	testJava := filepath.Join(project.Dir, "build", "classes", "java", "test")
	testKotlin := filepath.Join(project.Dir, "build", "classes", "kotlin", "test")
	testResources := filepath.Join(project.Dir, "build", "resources", "test")

	mainCompileGroups := [][]string{siblings}
	if includeKotlin {
		mainCompileGroups = append(mainCompileGroups, []string{mainKotlin})
	}
	mainCompileGroups = append(mainCompileGroups, compileArtifacts, providedArtifacts)
	mainCompile := joinClasspath(mainCompileGroups...)
	mainRuntime := joinClasspath(
		siblings,
		[]string{mainJava, mainKotlin, mainResources},
		byScope[deps.ScopeCompile],
		byScope[deps.ScopeRuntime],
	)
	testCompileGroups := [][]string{
		siblings,
		{mainJava, mainKotlin, mainResources},
		compileArtifacts,
		providedArtifacts,
	}
	if includeTestKotlin {
		testCompileGroups = append(testCompileGroups, []string{testKotlin})
	}
	testCompileGroups = append(testCompileGroups, testArtifacts)
	testCompile := joinClasspath(testCompileGroups...)
	testRuntime := joinClasspath(
		mainRuntime,
		[]string{testJava, testKotlin, testResources},
		byScope[deps.ScopeTest],
	)

	return classpaths{
		mainCompile: mainCompile,
		mainRuntime: mainRuntime,
		testCompile: testCompile,
		testRuntime: testRuntime,
	}, nil
}

func nonNativePaths(artifacts []deps.Artifact, scope deps.Scope) []string {
	var result []string
	for _, artifact := range artifacts {
		if artifact.Scope != scope || isNativeArtifact(artifact) {
			continue
		}
		result = append(result, artifact.Path)
	}
	return result
}

func isNativeArtifact(artifact deps.Artifact) bool {
	return strings.HasPrefix(artifact.Classifier, "natives-")
}

func joinClasspath(groups ...[]string) []string {
	seen := make(map[string]struct{})
	var result []string
	for _, group := range groups {
		for _, value := range group {
			if value == "" {
				continue
			}
			if _, ok := seen[value]; ok {
				continue
			}
			seen[value] = struct{}{}
			result = append(result, value)
		}
	}
	return result
}

func discoverSources(projectDir string, sources Sources) (sourceDirs, error) {
	result := sourceDirs{}

	groups := []struct {
		values []string
		dest   *[]string
	}{
		{sources.Java, &result.Java},
		{sources.Kotlin, &result.Kotlin},
		{sources.Resources, &result.Resources},
		{sources.TestJava, &result.TestJava},
		{sources.TestKotlin, &result.TestKotlin},
		{sources.TestResources, &result.TestResources},
	}

	for _, group := range groups {
		for _, value := range group.values {
			path, err := projectPath(projectDir, value)
			if err != nil {
				return sourceDirs{}, err
			}
			info, err := os.Stat(path)
			if err != nil {
				if os.IsNotExist(err) {
					continue
				}
				return sourceDirs{}, fmt.Errorf("stat %s: %w", path, err)
			}
			if !info.IsDir() {
				return sourceDirs{}, fmt.Errorf("%s: not a directory", path)
			}
			*group.dest = append(*group.dest, path)
		}
	}
	return result, nil
}

func projectPath(projectDir, value string) (string, error) {
	if value == "" {
		return "", fmt.Errorf("source path is empty")
	}
	if filepath.IsAbs(value) {
		return "", fmt.Errorf("source path %q must be relative", value)
	}
	clean := filepath.Clean(filepath.FromSlash(value))
	if clean == "." {
		return "", fmt.Errorf("source path %q is the project root", value)
	}
	full := filepath.Join(projectDir, clean)
	if !core.Within(projectDir, full) {
		return "", fmt.Errorf("source path %q escapes the project", value)
	}
	return full, nil
}
