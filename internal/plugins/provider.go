package plugins

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/gabriel-aplok/kine-build-java/internal/core"
)

// Caller sends one plugin RPC.
type Caller interface {
	Call(ctx context.Context, method string, params, result any) error
}

// Provider adapts one plugin provider to core.Provider.
type Provider struct {
	plugin        Plugin
	provider      string
	caller        Caller
	workspaceRoot string
}

// NewProvider returns a core provider for one plugin provider id.
func NewProvider(plugin Plugin, providerID string, caller Caller, workspaceRoot string) (*Provider, error) {
	if caller == nil {
		return nil, fmt.Errorf("plugin %s: caller is nil", plugin.Manifest.ID)
	}
	if workspaceRoot == "" {
		return nil, fmt.Errorf("plugin %s: workspace root is empty", plugin.Manifest.ID)
	}
	if err := plugin.Manifest.Validate(); err != nil {
		return nil, err
	}

	found := false
	for _, id := range plugin.Manifest.Providers {
		if id == providerID {
			found = true
			break
		}
	}
	if !found {
		return nil, fmt.Errorf("plugin %s does not provide %q", plugin.Manifest.ID, providerID)
	}
	return &Provider{plugin: plugin, provider: providerID, caller: caller, workspaceRoot: workspaceRoot}, nil
}

func (p *Provider) Name() string {
	return p.provider
}

func (p *Provider) Tasks(ctx context.Context, project *core.Project) ([]*core.Task, error) {
	if project == nil {
		return nil, fmt.Errorf("plugin %s: project is nil", p.plugin.Manifest.ID)
	}

	options := project.Options
	if options == nil {
		options = map[string]any{}
	}
	var response tasksResponse
	err := p.caller.Call(ctx, "tasks", tasksRequest{
		Provider: p.provider,
		Project: wireProject{
			ID:      project.ID,
			Dir:     project.Dir,
			Options: options,
		},
	}, &response)
	if err != nil {
		return nil, fmt.Errorf("plugin %s: tasks for %s: %w", p.plugin.Manifest.ID, project.ID, err)
	}

	tasks := make([]*core.Task, 0, len(response.Tasks))
	seen := make(map[string]struct{}, len(response.Tasks))
	for _, definition := range response.Tasks {
		task, err := p.convert(project, definition)
		if err != nil {
			return nil, fmt.Errorf("plugin %s: %w", p.plugin.Manifest.ID, err)
		}
		if _, ok := seen[task.ID]; ok {
			return nil, fmt.Errorf("plugin %s: duplicate task id %q", p.plugin.Manifest.ID, task.ID)
		}
		seen[task.ID] = struct{}{}
		tasks = append(tasks, task)
	}
	return tasks, nil
}

func (p *Provider) convert(project *core.Project, definition wireTask) (*core.Task, error) {
	if definition.ID == "" {
		return nil, fmt.Errorf("task id is empty")
	}
	if strings.Contains(definition.ID, ":") {
		return nil, fmt.Errorf("task id %q must not contain ':'", definition.ID)
	}

	inputs, err := convertInputs(p.workspaceRoot, project.Dir, definition.Inputs)
	if err != nil {
		return nil, fmt.Errorf("task %q inputs: %w", definition.ID, err)
	}
	outputs, err := convertOutputs(p.workspaceRoot, project.Dir, definition.Outputs)
	if err != nil {
		return nil, fmt.Errorf("task %q outputs: %w", definition.ID, err)
	}

	deps := make([]string, 0, len(definition.Deps))
	for _, dep := range definition.Deps {
		if dep == "" {
			return nil, fmt.Errorf("task %q: dependency is empty", definition.ID)
		}
		if strings.Contains(dep, ":") {
			deps = append(deps, dep)
		} else {
			deps = append(deps, project.ID+":"+dep)
		}
	}

	taskID := definition.ID
	key := fmt.Sprintf("plugin %s %s %s", p.plugin.Manifest.ID, p.plugin.Manifest.Version, definition.Key)
	return &core.Task{
		ID:      taskID,
		Deps:    deps,
		Inputs:  inputs,
		Outputs: outputs,
		Key:     key,
		Run: func(ctx context.Context) error {
			err := p.caller.Call(ctx, "run", runRequest{
				Provider: p.provider,
				Project:  project.ID,
				Task:     taskID,
			}, nil)
			if err != nil {
				return fmt.Errorf("plugin %s: task %s: %w", p.plugin.Manifest.ID, taskID, err)
			}
			return nil
		},
	}, nil
}

func convertInputs(workspaceRoot, projectDir string, specs []wirePath) ([]core.Input, error) {
	result := make([]core.Input, 0, len(specs))
	for _, spec := range specs {
		path, mode, err := convertPath(workspaceRoot, projectDir, spec)
		if err != nil {
			return nil, err
		}
		result = append(result, core.Input{Path: path, Mode: mode})
	}
	return result, nil
}

func convertOutputs(workspaceRoot, projectDir string, specs []wirePath) ([]core.Output, error) {
	result := make([]core.Output, 0, len(specs))
	for _, spec := range specs {
		path, mode, err := convertPath(workspaceRoot, projectDir, spec)
		if err != nil {
			return nil, err
		}
		result = append(result, core.Output{Path: path, Mode: mode})
	}
	return result, nil
}

func convertPath(workspaceRoot, projectDir string, spec wirePath) (string, core.PathMode, error) {
	if spec.Path == "" {
		return "", core.ModeFile, fmt.Errorf("path is empty")
	}
	if filepath.IsAbs(spec.Path) {
		return "", core.ModeFile, fmt.Errorf("path %q must be relative", spec.Path)
	}

	clean := filepath.Clean(filepath.FromSlash(spec.Path))
	if clean == "." {
		return "", core.ModeFile, fmt.Errorf("path %q is the project root", spec.Path)
	}
	full := filepath.Join(projectDir, clean)
	if !core.Within(workspaceRoot, full) {
		return "", core.ModeFile, fmt.Errorf("path %q escapes the workspace", spec.Path)
	}
	relative, err := filepath.Rel(workspaceRoot, full)
	if err != nil {
		return "", core.ModeFile, fmt.Errorf("path %q: %w", spec.Path, err)
	}

	switch spec.Mode {
	case "file":
		return filepath.ToSlash(relative), core.ModeFile, nil
	case "tree":
		return filepath.ToSlash(relative), core.ModeTree, nil
	default:
		return "", core.ModeFile, fmt.Errorf("path %q: mode %q must be file or tree", spec.Path, spec.Mode)
	}
}

type tasksRequest struct {
	Provider string      `json:"provider"`
	Project  wireProject `json:"project"`
}

type wireProject struct {
	ID      string         `json:"id"`
	Dir     string         `json:"dir"`
	Options map[string]any `json:"options"`
}

type tasksResponse struct {
	Tasks []wireTask `json:"tasks"`
}

type wireTask struct {
	ID      string     `json:"id"`
	Deps    []string   `json:"deps"`
	Inputs  []wirePath `json:"inputs"`
	Outputs []wirePath `json:"outputs"`
	Key     string     `json:"key"`
}

type wirePath struct {
	Path string `json:"path"`
	Mode string `json:"mode"`
}

type runRequest struct {
	Provider string `json:"provider"`
	Project  string `json:"project"`
	Task     string `json:"task"`
}
