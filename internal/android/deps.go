package android

import (
	"context"
	"fmt"

	"github.com/gabriel-aplok/kine-build-java/internal/core"
	"github.com/gabriel-aplok/kine-build-java/internal/deps"
)

func (p *Provider) resolveDependencies(ctx context.Context, project *core.Project, options Options) ([]deps.Artifact, error) {
	return p.resolveDependencyList(ctx, project, options, options.Dependencies)
}

func (p *Provider) resolveTestDependencies(ctx context.Context, project *core.Project, options Options) ([]deps.Artifact, error) {
	return p.resolveDependencyList(ctx, project, options, options.TestDependencies)
}

func dependencyAttributes(options Options) map[string]string {
	buildType := "release"
	if len(options.Variants) == 1 {
		buildType = options.Variants[0].BuildType
	} else if len(options.Variants) > 0 {
		buildType = options.Variants[0].BuildType
		for _, variant := range options.Variants {
			if variant.BuildType == "release" {
				buildType = "release"
				break
			}
		}
	}
	return map[string]string{
		"org.gradle.usage":                               "java-runtime",
		"org.jetbrains.kotlin.platform.type":             "androidJvm",
		"com.android.build.api.attributes.BuildTypeAttr": buildType,
	}
}

func (p *Provider) resolveDependencyList(ctx context.Context, project *core.Project, options Options, dependencies []deps.Dependency) ([]deps.Artifact, error) {
	if len(dependencies) == 0 {
		return nil, nil
	}
	if p.config.Cache == nil {
		return nil, fmt.Errorf("project %s: dependency resolution requires a store", project.ID)
	}

	repositories, err := deps.LoadRepositories(p.workspaceRoot(project))
	if err != nil {
		return nil, fmt.Errorf("project %s: %w", project.ID, err)
	}
	result, err := deps.Resolve(ctx, project.Dir, p.config.Cache, dependencies, deps.Options{
		Repositories: repositories,
		Update:       p.config.Update,
		Offline:      p.config.Offline,
		Logger:       p.config.Logger,
		Attributes:   dependencyAttributes(options),
	})
	if err != nil {
		return nil, fmt.Errorf("project %s: resolve dependencies: %w", project.ID, err)
	}
	return result.Artifacts, nil
}
