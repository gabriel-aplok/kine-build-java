package desktop

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/gabriel-aplok/kine-build-java/internal/core"
	"github.com/gabriel-aplok/kine-build-java/internal/toolchain"
)

func TestProviderName(t *testing.T) {
	provider := NewProvider(Config{})
	if provider.Name() != "desktop" {
		t.Errorf("Name() = %q, want desktop", provider.Name())
	}
}

func TestTasks(t *testing.T) {
	root := t.TempDir()
	projectDir := filepath.Join(root, "app")
	writeDesktopIcon(t, projectDir)
	jdk := fakeJDK(t)
	collected := &core.Project{}

	provider := &Provider{
		config: Config{Workspace: &core.Workspace{Root: root}},
		collectJVM: func(ctx context.Context, project *core.Project) ([]*core.Task, error) {
			*collected = *project
			return []*core.Task{{ID: "jar", Deps: []string{"app:compile-java"}}}, nil
		},
		findJDK: func(requirement toolchain.Requirement, roots ...string) (*toolchain.JDK, error) {
			if requirement.Major != 21 {
				t.Errorf("major = %d, want 21", requirement.Major)
			}
			return jdk, nil
		},
		run: func(ctx context.Context, command toolchain.Command) (toolchain.Result, error) {
			return toolchain.Result{}, nil
		},
	}

	project := &core.Project{
		ID:  "app",
		Dir: projectDir,
		Options: map[string]any{
			"java":       21,
			"main_class": "demo.App",
			"package": map[string]any{
				"name":         "Demo",
				"icon":         "assets/icon.png",
				"java_options": []any{"-Xmx128m"},
			},
		},
	}

	tasks, err := provider.Tasks(context.Background(), project)
	if err != nil {
		t.Fatalf("Tasks() error = %v", err)
	}
	if len(tasks) != 2 {
		t.Fatalf("tasks = %d, want 2", len(tasks))
	}
	if collected.Provider != "jvm" {
		t.Errorf("jvm project provider = %q", collected.Provider)
	}
	if _, ok := collected.Options["package"]; ok {
		t.Error("jvm options still contain the package table")
	}

	task := tasks[1]
	if task.ID != "package" {
		t.Errorf("ID = %q, want package", task.ID)
	}
	if !reflect.DeepEqual(task.Deps, []string{"app:jar"}) {
		t.Errorf("Deps = %v", task.Deps)
	}
	if len(task.Inputs) != 2 {
		t.Fatalf("Inputs = %+v", task.Inputs)
	}
	if task.Inputs[0].Path != "app/build/libs/app.jar" || task.Inputs[0].Mode != core.ModeFile {
		t.Errorf("jar input = %+v", task.Inputs[0])
	}
	if task.Inputs[1].Path != "app/assets/icon.png" || task.Inputs[1].Mode != core.ModeFile {
		t.Errorf("icon input = %+v", task.Inputs[1])
	}
	if len(task.Outputs) != 1 || task.Outputs[0].Path != "app/build/dist/Demo" || task.Outputs[0].Mode != core.ModeTree {
		t.Errorf("Outputs = %+v", task.Outputs)
	}
	if !strings.Contains(task.Key, "jpackage 21.0.1") || !strings.Contains(task.Key, "name=Demo") {
		t.Errorf("Key = %q", task.Key)
	}
	if task.Run == nil {
		t.Error("Run is nil")
	}
}

func TestTasksSingleProject(t *testing.T) {
	root := t.TempDir()
	jdk := fakeJDK(t)
	provider := &Provider{
		config: Config{Workspace: &core.Workspace{Root: root}},
		collectJVM: func(ctx context.Context, project *core.Project) ([]*core.Task, error) {
			return []*core.Task{{ID: "jar"}}, nil
		},
		findJDK: func(toolchain.Requirement, ...string) (*toolchain.JDK, error) {
			return jdk, nil
		},
	}
	project := &core.Project{
		ID:      "app",
		Dir:     root,
		Options: map[string]any{"main_class": "demo.App"},
	}

	tasks, err := provider.Tasks(context.Background(), project)
	if err != nil {
		t.Fatalf("Tasks() error = %v", err)
	}
	task := tasks[1]
	if task.Inputs[0].Path != "build/libs/app.jar" {
		t.Errorf("input = %q", task.Inputs[0].Path)
	}
	if task.Outputs[0].Path != "build/dist/app" {
		t.Errorf("output = %q", task.Outputs[0].Path)
	}
}

func TestTasksMissingMainClass(t *testing.T) {
	root := t.TempDir()
	provider := &Provider{
		config: Config{Workspace: &core.Workspace{Root: root}},
		collectJVM: func(ctx context.Context, project *core.Project) ([]*core.Task, error) {
			return nil, nil
		},
		findJDK: func(toolchain.Requirement, ...string) (*toolchain.JDK, error) {
			return fakeJDK(t), nil
		},
	}
	project := &core.Project{ID: "app", Dir: filepath.Join(root, "app")}

	_, err := provider.Tasks(context.Background(), project)
	if err == nil {
		t.Fatal("Tasks() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "main_class") {
		t.Errorf("error %q does not mention main_class", err)
	}
}

func TestTasksMissingJpackage(t *testing.T) {
	root := t.TempDir()
	jdk := &toolchain.JDK{Home: t.TempDir(), Version: "21.0.1", Major: 21}
	provider := &Provider{
		config: Config{Workspace: &core.Workspace{Root: root}},
		collectJVM: func(ctx context.Context, project *core.Project) ([]*core.Task, error) {
			return nil, nil
		},
		findJDK: func(toolchain.Requirement, ...string) (*toolchain.JDK, error) {
			return jdk, nil
		},
	}
	project := &core.Project{
		ID:      "app",
		Dir:     filepath.Join(root, "app"),
		Options: map[string]any{"main_class": "demo.App"},
	}

	_, err := provider.Tasks(context.Background(), project)
	if err == nil {
		t.Fatal("Tasks() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "jpackage") {
		t.Errorf("error %q does not mention jpackage", err)
	}
}

func TestTasksJVMError(t *testing.T) {
	root := t.TempDir()
	provider := &Provider{
		config: Config{Workspace: &core.Workspace{Root: root}},
		collectJVM: func(ctx context.Context, project *core.Project) ([]*core.Task, error) {
			return nil, errors.New("jvm failed")
		},
	}
	project := &core.Project{
		ID:      "app",
		Dir:     filepath.Join(root, "app"),
		Options: map[string]any{"main_class": "demo.App"},
	}

	_, err := provider.Tasks(context.Background(), project)
	if err == nil {
		t.Fatal("Tasks() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "jvm failed") {
		t.Errorf("error %q does not carry the jvm failure", err)
	}
}

func TestPackageRun(t *testing.T) {
	root := t.TempDir()
	projectDir := filepath.Join(root, "app")
	writeDesktopIcon(t, projectDir)
	jdk := fakeJDK(t)
	var recorded toolchain.Command

	provider := &Provider{
		config: Config{Workspace: &core.Workspace{Root: root}},
		collectJVM: func(ctx context.Context, project *core.Project) ([]*core.Task, error) {
			return []*core.Task{{ID: "jar"}}, nil
		},
		findJDK: func(toolchain.Requirement, ...string) (*toolchain.JDK, error) {
			return jdk, nil
		},
		run: func(ctx context.Context, command toolchain.Command) (toolchain.Result, error) {
			recorded = command
			return toolchain.Result{}, nil
		},
	}
	project := &core.Project{
		ID:  "app",
		Dir: projectDir,
		Options: map[string]any{
			"main_class": "demo.App",
			"package": map[string]any{
				"name":         "Demo",
				"vendor":       "Example",
				"version":      "2.0.0",
				"icon":         "assets/icon.png",
				"java_options": []any{"-Xmx128m"},
			},
		},
	}

	tasks, err := provider.Tasks(context.Background(), project)
	if err != nil {
		t.Fatalf("Tasks() error = %v", err)
	}
	if err := tasks[1].Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	wantPath := filepath.Join(jdk.Home, "bin", "jpackage")
	if recorded.Path != wantPath {
		t.Errorf("Path = %q, want %q", recorded.Path, wantPath)
	}
	if recorded.Dir != projectDir {
		t.Errorf("Dir = %q, want %q", recorded.Dir, projectDir)
	}

	want := []string{
		toolchain.HeapArg("-J-Xmx", desktopHeap),
		"--type", "app-image",
		"--name", "Demo",
		"--app-version", "2.0.0",
		"--vendor", "Example",
		"--input", filepath.Join(projectDir, "build", "libs"),
		"--main-jar", "app.jar",
		"--main-class", "demo.App",
		"--dest", filepath.Join(projectDir, "build", "dist"),
		"--temp", filepath.Join(projectDir, ".kine", "tmp", "jpackage"),
		"--icon", filepath.Join(projectDir, "assets", "icon.png"),
		"--java-options", "-Xmx128m",
	}
	if !reflect.DeepEqual(recorded.Args, want) {
		t.Errorf("Args = %v, want %v", recorded.Args, want)
	}
}

func TestPackageRunError(t *testing.T) {
	root := t.TempDir()
	projectDir := filepath.Join(root, "app")
	jdk := fakeJDK(t)
	provider := &Provider{
		config: Config{Workspace: &core.Workspace{Root: root}},
		collectJVM: func(ctx context.Context, project *core.Project) ([]*core.Task, error) {
			return []*core.Task{{ID: "jar"}}, nil
		},
		findJDK: func(toolchain.Requirement, ...string) (*toolchain.JDK, error) {
			return jdk, nil
		},
		run: func(ctx context.Context, command toolchain.Command) (toolchain.Result, error) {
			return toolchain.Result{Stderr: []byte("boom")}, &toolchain.ExitError{Code: 1}
		},
	}
	project := &core.Project{
		ID:      "app",
		Dir:     projectDir,
		Options: map[string]any{"main_class": "demo.App"},
	}

	tasks, err := provider.Tasks(context.Background(), project)
	if err != nil {
		t.Fatalf("Tasks() error = %v", err)
	}
	err = tasks[1].Run(context.Background())
	if err == nil {
		t.Fatal("Run() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "boom") || !strings.Contains(err.Error(), "jpackage") {
		t.Errorf("error %q does not carry the jpackage output", err)
	}
}

func fakeJDK(t *testing.T) *toolchain.JDK {
	t.Helper()

	home := t.TempDir()
	bin := filepath.Join(home, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "jpackage"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return &toolchain.JDK{
		Home:    home,
		Version: "21.0.1",
		Major:   21,
		Java:    filepath.Join(bin, "java"),
		Javac:   filepath.Join(bin, "javac"),
	}
}

func writeDesktopIcon(t *testing.T, projectDir string) {
	t.Helper()

	dir := filepath.Join(projectDir, "assets")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "icon.png"), []byte("png"), 0o600); err != nil {
		t.Fatal(err)
	}
}
