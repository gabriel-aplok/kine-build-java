package minecraft

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"

	"github.com/gabriel-aplok/kine-build-java/internal/core"
	"github.com/gabriel-aplok/kine-build-java/internal/toolchain"
)

// MinBuildJDK is the oldest JDK that can compile mod sources.
const MinBuildJDK = 17

// Config configures a minecraft provider.
type Config struct {
	Workspace *core.Workspace
	Logger    *slog.Logger
}

// Provider builds Fabric mods.
type Provider struct {
	config  Config
	findJDK func(toolchain.Requirement, ...string) (*toolchain.JDK, error)
	run     func(context.Context, toolchain.Command) (toolchain.Result, error)
}

// NewProvider returns a minecraft provider.
func NewProvider(config Config) *Provider {
	return &Provider{
		config:  config,
		findJDK: toolchain.Find,
		run:     toolchain.Run,
	}
}

func (p *Provider) Name() string {
	return "minecraft"
}

func (p *Provider) Tasks(ctx context.Context, project *core.Project) ([]*core.Task, error) {
	if project == nil {
		return nil, fmt.Errorf("project is nil")
	}

	options, err := decodeOptions(project.Options, project.ID, project.Dir)
	if err != nil {
		return nil, fmt.Errorf("project %s: %w", project.ID, err)
	}

	jdk, err := p.findJDK(toolchain.Requirement{})
	if err != nil {
		return nil, fmt.Errorf("project %s: %w", project.ID, err)
	}
	if jdk.Major < MinBuildJDK {
		return nil, fmt.Errorf("project %s: minecraft requires JDK %d or later, found %d", project.ID, MinBuildJDK, jdk.Major)
	}

	root := p.workspaceRoot(project)
	compile, err := p.compileTask(root, project, options, jdk)
	if err != nil {
		return nil, fmt.Errorf("project %s: %w", project.ID, err)
	}
	jar, err := p.jarTask(root, project, options)
	if err != nil {
		return nil, fmt.Errorf("project %s: %w", project.ID, err)
	}
	tasks := []*core.Task{compile, jar}

	if options.Mappings != "" {
		remap, err := p.remapTask(root, project, options, jdk)
		if err != nil {
			return nil, fmt.Errorf("project %s: %w", project.ID, err)
		}
		tasks = append(tasks, remap)
	}
	return tasks, nil
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
