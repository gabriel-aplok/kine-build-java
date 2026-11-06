package minecraft

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gabriel-aplok/kine-build-java/internal/core"
	"github.com/gabriel-aplok/kine-build-java/internal/toolchain"
)

func TestProviderName(t *testing.T) {
	provider := NewProvider(Config{})
	if provider.Name() != "minecraft" {
		t.Errorf("Name() = %q, want minecraft", provider.Name())
	}
}

func TestTasks(t *testing.T) {
	workspace, projectDir := mcProject(t)

	provider := testProvider(workspace)
	tasks, err := provider.Tasks(context.Background(), &core.Project{
		ID:      "app",
		Dir:     projectDir,
		Options: baseValues(),
	})
	if err != nil {
		t.Fatalf("Tasks() error = %v", err)
	}
	if len(tasks) != 2 {
		t.Fatalf("tasks = %d, want 2", len(tasks))
	}
	if tasks[0].ID != "compile-java" || tasks[1].ID != "mod-jar" {
		t.Errorf("ids = %q, %q", tasks[0].ID, tasks[1].ID)
	}
	if len(tasks[1].Deps) != 1 || tasks[1].Deps[0] != "app:compile-java" {
		t.Errorf("jar deps = %v", tasks[1].Deps)
	}
	if tasks[1].Outputs[0].Path != "app/build/libs/example-mod-1.2.3.jar" {
		t.Errorf("jar output = %q", tasks[1].Outputs[0].Path)
	}
}

func TestTasksWithRemap(t *testing.T) {
	workspace, projectDir := mcProject(t)
	writeLocalFile(t, filepath.Join(workspace, "game", "mappings.tiny"), "tiny\t2\t0\tofficial\tintermediary\tnamed\n")
	writeLocalFile(t, filepath.Join(workspace, "game", "tiny-remapper.jar"), "remapper")

	values := baseValues()
	values["mappings"] = "../game/mappings.tiny"
	values["remapper"] = "../game/tiny-remapper.jar"

	provider := testProvider(workspace)
	tasks, err := provider.Tasks(context.Background(), &core.Project{
		ID:      "app",
		Dir:     projectDir,
		Options: values,
	})
	if err != nil {
		t.Fatalf("Tasks() error = %v", err)
	}
	if len(tasks) != 3 {
		t.Fatalf("tasks = %d, want 3", len(tasks))
	}
	if tasks[2].ID != "remap" {
		t.Errorf("third task = %q", tasks[2].ID)
	}
	if len(tasks[2].Deps) != 1 || tasks[2].Deps[0] != "app:mod-jar" {
		t.Errorf("remap deps = %v", tasks[2].Deps)
	}
}

func TestTasksOldJDK(t *testing.T) {
	workspace, projectDir := mcProject(t)
	provider := testProvider(workspace)
	provider.findJDK = func(toolchain.Requirement, ...string) (*toolchain.JDK, error) {
		return &toolchain.JDK{Major: 11, Version: "11.0.1"}, nil
	}

	_, err := provider.Tasks(context.Background(), &core.Project{ID: "app", Dir: projectDir, Options: baseValues()})
	if err == nil {
		t.Fatal("Tasks() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "17") {
		t.Errorf("error %q does not mention the required JDK", err)
	}
}

func TestTasksMissingJDK(t *testing.T) {
	workspace, projectDir := mcProject(t)
	provider := testProvider(workspace)
	provider.findJDK = func(toolchain.Requirement, ...string) (*toolchain.JDK, error) {
		return nil, errors.New("no jdk")
	}

	_, err := provider.Tasks(context.Background(), &core.Project{ID: "app", Dir: projectDir, Options: baseValues()})
	if err == nil {
		t.Fatal("Tasks() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "no jdk") {
		t.Errorf("error %q does not carry the JDK failure", err)
	}
}

func TestTasksBadOptions(t *testing.T) {
	workspace := t.TempDir()
	projectDir := filepath.Join(workspace, "app")
	provider := testProvider(workspace)

	project := &core.Project{
		ID:  "app",
		Dir: projectDir,
		Options: map[string]any{
			"version":       "1.20.1",
			"minecraft_jar": "missing.jar",
		},
	}
	_, err := provider.Tasks(context.Background(), project)
	if err == nil {
		t.Fatal("Tasks() error = nil, want error")
	}
	if !strings.Contains(err.Error(), metadataName) {
		t.Errorf("error %q does not name the metadata", err)
	}
}

func testProvider(workspace string) *Provider {
	return &Provider{
		config:  Config{Workspace: &core.Workspace{Root: workspace}},
		findJDK: func(toolchain.Requirement, ...string) (*toolchain.JDK, error) { return testJDK(), nil },
		run: func(ctx context.Context, command toolchain.Command) (toolchain.Result, error) {
			return toolchain.Result{}, nil
		},
	}
}
