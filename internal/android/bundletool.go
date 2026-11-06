package android

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/gabriel-aplok/kine-build-java/internal/deps"
)

const bundleToolMain = "com.android.tools.build.bundletool.BundleToolMain"

func (p *Provider) prepareBundletool(ctx context.Context, workspaceRoot string, options *Options) error {
	if !options.Bundle {
		return nil
	}
	if options.BundletoolVersion == "" {
		options.BundletoolVersion = defaultBundletoolVersion
	}
	if p.config.Cache == nil {
		return fmt.Errorf("bundle requires the workspace cache")
	}

	toolRoot := filepath.Join(workspaceRoot, ".kine", "tools", "bundletool", options.BundletoolVersion)
	if err := os.MkdirAll(toolRoot, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", toolRoot, err)
	}
	repositories, err := deps.ToolRepositories(workspaceRoot)
	if err != nil {
		return err
	}
	result, err := deps.Resolve(ctx, toolRoot, p.config.Cache, []deps.Dependency{
		{
			Coordinate: deps.Coordinate{
				Group:     "com.android.tools.build",
				Artifact:  "bundletool",
				Version:   options.BundletoolVersion,
				Extension: "jar",
			},
			Scope: deps.ScopeRuntime,
		},
		{
			// bundletool 1.18.3 uses Kotlin classes in ddmlib but does not
			// declare the stdlib in its POM.
			Coordinate: deps.Coordinate{
				Group:     "org.jetbrains.kotlin",
				Artifact:  "kotlin-stdlib",
				Version:   "2.0.21",
				Extension: "jar",
			},
			Scope: deps.ScopeRuntime,
		},
	}, deps.Options{
		Repositories: repositories,
		Update:       p.config.Update,
		Offline:      p.config.Offline,
		Logger:       p.config.Logger,
	})
	if err != nil {
		return fmt.Errorf("resolve bundletool: %w", err)
	}

	classpath := make([]string, 0, len(result.Artifacts))
	for _, artifact := range result.Artifacts {
		if artifact.Extension != "jar" {
			continue
		}
		classpath = append(classpath, artifact.Path)
	}
	if len(classpath) == 0 {
		return fmt.Errorf("bundletool classpath is empty")
	}
	options.BundletoolClasspath = classpath
	options.BundletoolMain = bundleToolMain
	return nil
}
