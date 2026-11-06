package jvm

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/gabriel-aplok/kine-build-java/internal/deps"
)

const (
	kotlinGroup    = "org.jetbrains.kotlin"
	junitGroup     = "org.junit.platform"
	kotlinCompiler = "kotlin-compiler-embeddable"
	kotlinCompose  = "kotlin-compose-compiler-plugin-embeddable"
	junitConsole   = "junit-platform-console-standalone"
)

type toolSet struct {
	kotlinClasspath []string
	composePlugin   string
	junitConsole    string
}

func (p *Provider) resolveKotlinTools(ctx context.Context, version string, compose bool) (*toolSet, error) {
	if version == "" {
		return nil, fmt.Errorf("kotlin version is empty")
	}
	key := "kotlin:" + version + fmt.Sprintf(":%t", compose)
	if cached, ok := p.tools[key]; ok {
		return cached, nil
	}

	root, err := p.toolRoot("kotlin", version)
	if err != nil {
		return nil, err
	}
	roots := []deps.Dependency{{
		Coordinate: deps.Coordinate{
			Group:     kotlinGroup,
			Artifact:  kotlinCompiler,
			Version:   version,
			Extension: "jar",
		},
		Scope: deps.ScopeRuntime,
	}}
	if compose {
		roots = append(roots, deps.Dependency{
			Coordinate: deps.Coordinate{
				Group:     kotlinGroup,
				Artifact:  kotlinCompose,
				Version:   version,
				Extension: "jar",
			},
			Scope: deps.ScopeRuntime,
		})
	}

	artifacts, err := p.resolveTool(ctx, root, roots)
	if err != nil {
		return nil, err
	}
	tools := &toolSet{}
	for _, artifact := range artifacts {
		tools.kotlinClasspath = append(tools.kotlinClasspath, artifact.Path)
		if artifact.Artifact == kotlinCompose {
			tools.composePlugin = artifact.Path
		}
	}
	if compose && tools.composePlugin == "" {
		return nil, fmt.Errorf("compose compiler plugin %s:%s not resolved", kotlinGroup, version)
	}

	p.tools[key] = tools
	return tools, nil
}

func (p *Provider) resolveJUnit(ctx context.Context, version string) (*toolSet, error) {
	if version == "" {
		return nil, fmt.Errorf("junit version is empty")
	}
	key := "junit:" + version
	if cached, ok := p.tools[key]; ok {
		return cached, nil
	}

	root, err := p.toolRoot("junit", version)
	if err != nil {
		return nil, err
	}
	roots := []deps.Dependency{{
		Coordinate: deps.Coordinate{
			Group:     junitGroup,
			Artifact:  junitConsole,
			Version:   version,
			Extension: "jar",
		},
		Scope: deps.ScopeRuntime,
	}}

	artifacts, err := p.resolveTool(ctx, root, roots)
	if err != nil {
		return nil, err
	}
	tools := &toolSet{}
	for _, artifact := range artifacts {
		if artifact.Artifact == junitConsole {
			tools.junitConsole = artifact.Path
		}
	}
	if tools.junitConsole == "" {
		return nil, fmt.Errorf("junit console %s:%s not resolved", junitGroup, version)
	}

	p.tools[key] = tools
	return tools, nil
}

func (p *Provider) toolRoot(name, version string) (string, error) {
	workspace := p.workspaceRootValue()
	if workspace == "" {
		return "", fmt.Errorf("workspace root is empty")
	}
	return filepath.Join(workspace, ".kine", "tools", name, version), nil
}

func (p *Provider) resolveTool(ctx context.Context, root string, roots []deps.Dependency) ([]deps.Artifact, error) {
	if p.cache == nil {
		return nil, fmt.Errorf("tool resolution requires a store")
	}
	workspace := p.workspaceRootValue()
	if workspace == "" {
		return nil, fmt.Errorf("workspace root is empty")
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, fmt.Errorf("create tool root %s: %w", root, err)
	}

	repositories, err := deps.LoadRepositories(workspace)
	if err != nil {
		return nil, err
	}
	result, err := deps.Resolve(ctx, root, p.cache, roots, deps.Options{
		Repositories: repositories,
		Update:       p.update,
		Offline:      p.offline,
		Logger:       p.logger,
	})
	if err != nil {
		return nil, fmt.Errorf("resolve tool %s: %w", root, err)
	}
	return result.Artifacts, nil
}

func (p *Provider) workspaceRootValue() string {
	if p.workspace != nil {
		return p.workspace.Root
	}
	return ""
}
