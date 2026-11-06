package desktop

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/gabriel-aplok/kine-build-java/internal/core"
	"github.com/gabriel-aplok/kine-build-java/internal/jvm"
	"github.com/gabriel-aplok/kine-build-java/internal/store"
	"github.com/gabriel-aplok/kine-build-java/internal/toolchain"
)

// Config configures a desktop provider.
type Config struct {
	Workspace *core.Workspace
	Cache     *store.Store
	Logger    *slog.Logger
	Update    bool
	Offline   bool
	RunArgs   []string
}

// Provider packages jvm applications with jpackage.
type Provider struct {
	config     Config
	jvm        *jvm.Provider
	collectJVM func(context.Context, *core.Project) ([]*core.Task, error)
	findJDK    func(toolchain.Requirement, ...string) (*toolchain.JDK, error)
	run        func(context.Context, toolchain.Command) (toolchain.Result, error)
}

// NewProvider returns a desktop provider.
func NewProvider(config Config) *Provider {
	jvmProvider := jvm.NewProvider(jvm.Config{
		Workspace: config.Workspace,
		Cache:     config.Cache,
		Logger:    config.Logger,
		Update:    config.Update,
		Offline:   config.Offline,
		RunArgs:   config.RunArgs,
	})
	return &Provider{
		config:     config,
		jvm:        jvmProvider,
		collectJVM: jvmProvider.Tasks,
		findJDK:    toolchain.Find,
		run:        toolchain.Run,
	}
}

func (p *Provider) Name() string {
	return "desktop"
}

func (p *Provider) Tasks(ctx context.Context, project *core.Project) ([]*core.Task, error) {
	if project == nil {
		return nil, fmt.Errorf("project is nil")
	}

	root := p.workspaceRoot(project)
	options, err := decodeOptions(project, root)
	if err != nil {
		return nil, fmt.Errorf("project %s: %w", project.ID, err)
	}

	jvmProject := &core.Project{
		ID:        project.ID,
		Dir:       project.Dir,
		Provider:  "jvm",
		DependsOn: append([]string(nil), project.DependsOn...),
		Options:   options.JVM,
	}
	tasks, err := p.collectJVM(ctx, jvmProject)
	if err != nil {
		return nil, err
	}

	jvmOptions, err := jvm.DecodeOptions(options.JVM, project.ID)
	if err != nil {
		return nil, fmt.Errorf("project %s: %w", project.ID, err)
	}
	if jvmOptions.MainClass == "" {
		return nil, fmt.Errorf("project %s: desktop requires main_class", project.ID)
	}

	jdk, err := p.findJDK(toolchain.Requirement{Major: jvmOptions.Java})
	if err != nil {
		return nil, fmt.Errorf("project %s: %w", project.ID, err)
	}
	jpackage, err := findJpackage(jdk)
	if err != nil {
		return nil, fmt.Errorf("project %s: %w", project.ID, err)
	}

	packageTask, err := p.packageTask(root, project, options, jvmOptions, jdk, jpackage)
	if err != nil {
		return nil, fmt.Errorf("project %s: %w", project.ID, err)
	}
	return append(tasks, packageTask), nil
}

func (p *Provider) workspaceRoot(project *core.Project) string {
	if p.config.Workspace != nil && p.config.Workspace.Root != "" {
		return p.config.Workspace.Root
	}
	return project.Dir
}
