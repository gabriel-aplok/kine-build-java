package jvm

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/gabriel-aplok/kine-build-java/internal/core"
	"github.com/gabriel-aplok/kine-build-java/internal/toolchain"
)

func TestRunMain(t *testing.T) {
	root := t.TempDir()
	provider := newTestProvider(t, root)
	var recorded toolchain.Command
	provider.run = func(ctx context.Context, command toolchain.Command) (toolchain.Result, error) {
		recorded = command
		return toolchain.Result{}, nil
	}

	jdk := &toolchain.JDK{Home: "/fake/jdk", Version: "21.0.1", Major: 21, Java: "/fake/jdk/bin/java"}
	build := &build{
		project:     &core.Project{ID: "app", Dir: root, Provider: "jvm"},
		options:     mustOptions(t, optionsFromTOML(t, `main_class = "demo.App"`)),
		jdk:         jdk,
		mainRuntime: []string{"/cp/a.jar", "/cp/b.jar"},
	}

	if err := provider.runMain(context.Background(), build); err != nil {
		t.Fatalf("runMain() error = %v", err)
	}
	if recorded.Path != jdk.Java {
		t.Errorf("Path = %q, want %q", recorded.Path, jdk.Java)
	}
	want := []string{
		"-cp",
		"/cp/a.jar" + string(os.PathListSeparator) + "/cp/b.jar",
		"demo.App",
	}
	if !reflect.DeepEqual(recorded.Args, want) {
		t.Errorf("Args = %v, want %v", recorded.Args, want)
	}
	if recorded.Dir != root {
		t.Errorf("Dir = %q, want %q", recorded.Dir, root)
	}
	if recorded.Stdout == nil || recorded.Stderr == nil {
		t.Error("stdout or stderr writer is nil")
	}
}

func TestRunMainPassesDisplayEnv(t *testing.T) {
	t.Setenv("DISPLAY", ":99")
	t.Setenv("WAYLAND_DISPLAY", "wayland-0")

	root := t.TempDir()
	provider := newTestProvider(t, root)
	var recorded toolchain.Command
	provider.run = func(ctx context.Context, command toolchain.Command) (toolchain.Result, error) {
		recorded = command
		return toolchain.Result{}, nil
	}

	build := &build{
		project:     &core.Project{ID: "app", Dir: root, Provider: "jvm"},
		options:     mustOptions(t, optionsFromTOML(t, `main_class = "demo.App"`)),
		jdk:         &toolchain.JDK{Home: "/fake/jdk", Version: "21.0.1", Major: 21, Java: "/fake/jdk/bin/java"},
		mainRuntime: []string{"/cp/a.jar"},
	}
	if err := provider.runMain(context.Background(), build); err != nil {
		t.Fatalf("runMain() error = %v", err)
	}

	env := envMap(recorded.Env)
	if env["DISPLAY"] != ":99" {
		t.Errorf("DISPLAY = %q, want :99", env["DISPLAY"])
	}
	if env["WAYLAND_DISPLAY"] != "wayland-0" {
		t.Errorf("WAYLAND_DISPLAY = %q, want wayland-0", env["WAYLAND_DISPLAY"])
	}
}

func TestRunArgs(t *testing.T) {
	args := runArgs([]string{"/cp/a.jar"}, "demo.App", []string{"one", "two"})
	want := []string{
		"-cp", "/cp/a.jar",
		"demo.App",
		"one", "two",
	}
	if !reflect.DeepEqual(args, want) {
		t.Errorf("runArgs() = %v, want %v", args, want)
	}
}

func TestRunMainPassesRunArgs(t *testing.T) {
	root := t.TempDir()
	provider := newTestProvider(t, root)
	provider.runArgs = []string{"--flag", "value"}
	var recorded toolchain.Command
	provider.run = func(ctx context.Context, command toolchain.Command) (toolchain.Result, error) {
		recorded = command
		return toolchain.Result{}, nil
	}

	build := &build{
		project:     &core.Project{ID: "app", Dir: root, Provider: "jvm"},
		options:     mustOptions(t, optionsFromTOML(t, `main_class = "demo.App"`)),
		jdk:         &toolchain.JDK{Home: "/fake/jdk", Version: "21.0.1", Major: 21, Java: "/fake/jdk/bin/java"},
		mainRuntime: []string{"/cp/a.jar"},
	}

	if err := provider.runMain(context.Background(), build); err != nil {
		t.Fatalf("runMain() error = %v", err)
	}
	want := []string{"-cp", "/cp/a.jar", "demo.App", "--flag", "value"}
	if !reflect.DeepEqual(recorded.Args, want) {
		t.Errorf("Args = %v, want %v", recorded.Args, want)
	}
}

func TestNewProviderCopiesRunArgs(t *testing.T) {
	args := []string{"one"}
	provider := NewProvider(Config{RunArgs: args})
	args[0] = "changed"

	if !reflect.DeepEqual(provider.runArgs, []string{"one"}) {
		t.Errorf("runArgs = %v, want [one]", provider.runArgs)
	}
}

func TestRunMainError(t *testing.T) {
	root := t.TempDir()
	provider := newTestProvider(t, root)
	provider.run = func(ctx context.Context, command toolchain.Command) (toolchain.Result, error) {
		return toolchain.Result{}, &toolchain.ExitError{Code: 2}
	}
	build := &build{
		project: &core.Project{ID: "app", Dir: root, Provider: "jvm"},
		options: mustOptions(t, optionsFromTOML(t, `main_class = "demo.App"`)),
		jdk:     &toolchain.JDK{Home: "/fake/jdk", Version: "21.0.1", Major: 21, Java: "/fake/jdk/bin/java"},
	}

	err := provider.runMain(context.Background(), build)
	if err == nil {
		t.Fatal("runMain() error = nil, want error")
	}
	var exitErr *toolchain.ExitError
	if !errors.As(err, &exitErr) {
		t.Errorf("error %v is not an ExitError", err)
	}
}

func TestTasksIncludeRun(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "src/main/java", "App.java"), "class App {}")

	provider := newTestProvider(t, root)
	project := &core.Project{
		ID:       "app",
		Dir:      root,
		Provider: "jvm",
		Options:  optionsFromTOML(t, `main_class = "demo.App"`),
	}
	tasks, err := provider.Tasks(context.Background(), project)
	if err != nil {
		t.Fatalf("Tasks() error = %v", err)
	}

	run := findTask(tasks, "run")
	if run == nil {
		t.Fatalf("run task missing: %v", taskIDs(tasks))
	}
	if !contains(run.Deps, "app:compile-java") {
		t.Errorf("Deps = %v, want compile-java", run.Deps)
	}
	if len(run.Outputs) != 0 {
		t.Errorf("Outputs = %+v, want none", run.Outputs)
	}
	if run.Key != "run demo.App" {
		t.Errorf("Key = %q", run.Key)
	}
}

func TestNoRunWithoutMainClass(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "src/main/java", "App.java"), "class App {}")

	provider := newTestProvider(t, root)
	project := &core.Project{ID: "app", Dir: root, Provider: "jvm"}
	tasks, err := provider.Tasks(context.Background(), project)
	if err != nil {
		t.Fatalf("Tasks() error = %v", err)
	}
	if findTask(tasks, "run") != nil {
		t.Error("run task exists without main_class")
	}
}
