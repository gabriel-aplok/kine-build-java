package core

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

// PathMode says whether a task path is one file or a directory tree.
type PathMode int

const (
	ModeFile PathMode = iota
	ModeTree
)

// Input is a workspace-relative file or tree read by a task.
type Input struct {
	Path string
	Mode PathMode
}

// Output is a workspace-relative file or tree written by a task.
type Output struct {
	Path string
	Mode PathMode
}

// Task is one unit of work in the build graph.
type Task struct {
	ID      string
	Project string
	Deps    []string
	Inputs  []Input
	Outputs []Output
	Key     string
	Run     func(context.Context) error
}

// AlwaysRun reports whether the task has no outputs to cache.
func (t *Task) AlwaysRun() bool {
	return len(t.Outputs) == 0
}

// Provider builds the tasks for one project.
type Provider interface {
	Name() string
	Tasks(ctx context.Context, project *Project) ([]*Task, error)
}

// CollectTasks asks every project provider for tasks and validates them.
func CollectTasks(ctx context.Context, workspace *Workspace, graph *ProjectGraph, registry *Registry) ([]*Task, error) {
	tasks := make([]*Task, 0, len(graph.Order))
	ids := make(map[string]struct{})

	for _, projectID := range graph.Order {
		project, ok := workspace.Projects[projectID]
		if !ok {
			return nil, fmt.Errorf("project %q: missing from workspace", projectID)
		}
		provider, err := registry.Provider(project.Provider)
		if err != nil {
			return nil, fmt.Errorf("project %q: %w", projectID, err)
		}

		projectTasks, err := provider.Tasks(ctx, project)
		if err != nil {
			return nil, fmt.Errorf("project %q: %w", projectID, err)
		}

		for _, task := range projectTasks {
			prepared, err := prepareTask(workspace.Root, projectID, task)
			if err != nil {
				return nil, err
			}
			if _, ok := ids[prepared.ID]; ok {
				return nil, fmt.Errorf("duplicate task id %q", prepared.ID)
			}
			ids[prepared.ID] = struct{}{}
			tasks = append(tasks, prepared)
		}
	}

	return tasks, nil
}

func prepareTask(root, projectID string, task *Task) (*Task, error) {
	if task == nil {
		return nil, fmt.Errorf("project %q: provider returned a nil task", projectID)
	}
	if task.ID == "" {
		return nil, fmt.Errorf("project %q: task id is empty", projectID)
	}
	if strings.Contains(task.ID, ":") {
		return nil, fmt.Errorf("project %q: task id %q must not contain ':'", projectID, task.ID)
	}

	copied := *task
	copied.ID = projectID + ":" + task.ID
	copied.Project = projectID
	copied.Deps = append([]string(nil), task.Deps...)
	copied.Inputs = append([]Input(nil), task.Inputs...)
	copied.Outputs = append([]Output(nil), task.Outputs...)

	for i := range copied.Inputs {
		clean, err := cleanTaskPath(root, copied.Inputs[i].Path)
		if err != nil {
			return nil, fmt.Errorf("task %q input: %w", copied.ID, err)
		}
		copied.Inputs[i].Path = clean
	}
	for i := range copied.Outputs {
		clean, err := cleanTaskPath(root, copied.Outputs[i].Path)
		if err != nil {
			return nil, fmt.Errorf("task %q output: %w", copied.ID, err)
		}
		copied.Outputs[i].Path = clean
	}

	return &copied, nil
}

func cleanTaskPath(root, path string) (string, error) {
	if path == "" {
		return "", errors.New("path is empty")
	}
	if filepath.IsAbs(path) {
		return "", fmt.Errorf("path %q must be relative", path)
	}

	clean := filepath.Clean(filepath.FromSlash(path))
	joined := filepath.Join(root, clean)
	if !isWithin(root, joined) {
		return "", fmt.Errorf("path %q escapes the workspace root", path)
	}
	return filepath.ToSlash(clean), nil
}
