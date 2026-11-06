package core

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func TestCollectTasksPrefixesIDs(t *testing.T) {
	workspace, graph := taskWorkspace(t.TempDir(), "app")
	registry := NewRegistry()
	provider := &fakeProvider{
		name: "fake",
		tasks: []*Task{{
			ID:      "compile",
			Inputs:  []Input{{Path: "app/src", Mode: ModeTree}},
			Outputs: []Output{{Path: "app/out", Mode: ModeTree}},
		}},
	}
	if err := registry.Register(provider); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	tasks, err := CollectTasks(context.Background(), workspace, graph, registry)
	if err != nil {
		t.Fatalf("CollectTasks() error = %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("len(tasks) = %d, want 1", len(tasks))
	}

	task := tasks[0]
	if task.ID != "app:compile" {
		t.Errorf("ID = %q, want app:compile", task.ID)
	}
	if task.Project != "app" {
		t.Errorf("Project = %q, want app", task.Project)
	}
	if task.AlwaysRun() {
		t.Error("AlwaysRun() = true, want false")
	}
	if len(task.Inputs) != 1 || task.Inputs[0].Path != "app/src" || task.Inputs[0].Mode != ModeTree {
		t.Errorf("Inputs = %+v, want app/src tree", task.Inputs)
	}
	if len(task.Outputs) != 1 || task.Outputs[0].Path != "app/out" || task.Outputs[0].Mode != ModeTree {
		t.Errorf("Outputs = %+v, want app/out tree", task.Outputs)
	}
}

func TestCollectTasksAlwaysRun(t *testing.T) {
	workspace, graph := taskWorkspace(t.TempDir(), "app")
	registry := NewRegistry()
	if err := registry.Register(&fakeProvider{
		name:  "fake",
		tasks: []*Task{{ID: "clean"}},
	}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	tasks, err := CollectTasks(context.Background(), workspace, graph, registry)
	if err != nil {
		t.Fatalf("CollectTasks() error = %v", err)
	}
	if !tasks[0].AlwaysRun() {
		t.Error("AlwaysRun() = false, want true")
	}
}

func TestCollectTasksRejectsDuplicateIDs(t *testing.T) {
	workspace, graph := taskWorkspace(t.TempDir(), "app")
	registry := NewRegistry()
	if err := registry.Register(&fakeProvider{
		name:  "fake",
		tasks: []*Task{{ID: "compile"}, {ID: "compile"}},
	}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	_, err := CollectTasks(context.Background(), workspace, graph, registry)
	if err == nil {
		t.Fatal("CollectTasks() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "duplicate") {
		t.Errorf("error %q does not mention duplicate", err)
	}
}

func TestCollectTasksRejectsEmptyID(t *testing.T) {
	workspace, graph := taskWorkspace(t.TempDir(), "app")
	registry := NewRegistry()
	if err := registry.Register(&fakeProvider{
		name:  "fake",
		tasks: []*Task{{ID: ""}},
	}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	_, err := CollectTasks(context.Background(), workspace, graph, registry)
	if err == nil {
		t.Fatal("CollectTasks() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "empty") {
		t.Errorf("error %q does not mention empty", err)
	}
}

func TestCollectTasksRejectsColonInID(t *testing.T) {
	workspace, graph := taskWorkspace(t.TempDir(), "app")
	registry := NewRegistry()
	if err := registry.Register(&fakeProvider{
		name:  "fake",
		tasks: []*Task{{ID: "a:b"}},
	}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	_, err := CollectTasks(context.Background(), workspace, graph, registry)
	if err == nil {
		t.Fatal("CollectTasks() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "must not contain") {
		t.Errorf("error %q does not explain the colon", err)
	}
}

func TestCollectTasksRejectsBadPaths(t *testing.T) {
	tests := []struct {
		name string
		task *Task
		want string
	}{
		{
			name: "input escape",
			task: &Task{ID: "t", Inputs: []Input{{Path: "../outside"}}},
			want: "escape",
		},
		{
			name: "output escape",
			task: &Task{ID: "t", Outputs: []Output{{Path: "../outside"}}},
			want: "escape",
		},
		{
			name: "absolute input",
			task: &Task{ID: "t", Inputs: []Input{{Path: "/tmp/in"}}},
			want: "relative",
		},
		{
			name: "empty output",
			task: &Task{ID: "t", Outputs: []Output{{Path: ""}}},
			want: "empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			workspace, graph := taskWorkspace(t.TempDir(), "app")
			registry := NewRegistry()
			if err := registry.Register(&fakeProvider{
				name:  "fake",
				tasks: []*Task{tt.task},
			}); err != nil {
				t.Fatalf("Register() error = %v", err)
			}

			_, err := CollectTasks(context.Background(), workspace, graph, registry)
			if err == nil {
				t.Fatal("CollectTasks() error = nil, want error")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error %q does not contain %q", err, tt.want)
			}
		})
	}
}

func TestCollectTasksCleansPaths(t *testing.T) {
	workspace, graph := taskWorkspace(t.TempDir(), "app")
	registry := NewRegistry()
	if err := registry.Register(&fakeProvider{
		name: "fake",
		tasks: []*Task{{
			ID:     "t",
			Inputs: []Input{{Path: "app/src/../src"}},
		}},
	}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	tasks, err := CollectTasks(context.Background(), workspace, graph, registry)
	if err != nil {
		t.Fatalf("CollectTasks() error = %v", err)
	}
	if tasks[0].Inputs[0].Path != "app/src" {
		t.Errorf("Inputs[0].Path = %q, want app/src", tasks[0].Inputs[0].Path)
	}
}

func TestCollectTasksUnknownProvider(t *testing.T) {
	workspace, graph := taskWorkspace(t.TempDir(), "app")
	workspace.Projects["app"].Provider = "missing"

	_, err := CollectTasks(context.Background(), workspace, graph, NewRegistry())
	if !errors.Is(err, ErrProviderNotFound) {
		t.Errorf("CollectTasks() error = %v, want ErrProviderNotFound", err)
	}
}

func TestCollectTasksProviderError(t *testing.T) {
	workspace, graph := taskWorkspace(t.TempDir(), "app")
	registry := NewRegistry()
	if err := registry.Register(&fakeProvider{
		name: "fake",
		err:  errors.New("boom"),
	}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	_, err := CollectTasks(context.Background(), workspace, graph, registry)
	if err == nil {
		t.Fatal("CollectTasks() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "app") || !strings.Contains(err.Error(), "boom") {
		t.Errorf("error %q does not include the project and cause", err)
	}
}

func TestCollectTasksMultipleProjects(t *testing.T) {
	workspace, graph := taskWorkspace(t.TempDir(), "app", "lib")
	registry := NewRegistry()
	if err := registry.Register(&fakeProvider{
		name:  "fake",
		tasks: []*Task{{ID: "compile"}},
	}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	tasks, err := CollectTasks(context.Background(), workspace, graph, registry)
	if err != nil {
		t.Fatalf("CollectTasks() error = %v", err)
	}
	if len(tasks) != 2 {
		t.Fatalf("len(tasks) = %d, want 2", len(tasks))
	}
	if tasks[0].ID != "app:compile" {
		t.Errorf("tasks[0].ID = %q, want app:compile", tasks[0].ID)
	}
	if tasks[1].ID != "lib:compile" {
		t.Errorf("tasks[1].ID = %q, want lib:compile", tasks[1].ID)
	}
}

func taskWorkspace(root string, ids ...string) (*Workspace, *ProjectGraph) {
	workspace := &Workspace{
		Root:     root,
		Projects: make(map[string]*Project, len(ids)),
		Order:    ids,
	}
	deps := make(map[string][]string, len(ids))
	for _, id := range ids {
		workspace.Projects[id] = &Project{
			ID:       id,
			Dir:      filepath.Join(root, id),
			Provider: "fake",
		}
		deps[id] = nil
	}
	return workspace, &ProjectGraph{Order: ids, Deps: deps}
}
