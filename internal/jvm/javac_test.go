package jvm

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/gabriel-aplok/kine-build-java/internal/core"
	"github.com/gabriel-aplok/kine-build-java/internal/toolchain"
)

func TestJavacArgs(t *testing.T) {
	args := javacArgs(21, "/out", []string{"/deps/a.jar", "/deps/b.jar"}, []string{"/src/A.java", "/src/B.java"})
	want := []string{
		"-J-Xmx" + strconv.FormatInt(toolchain.DefaultCompilerHeap, 10),
		"-d", "/out",
		"--release", "21",
		"-encoding", "UTF-8",
		"-g",
		"-classpath", "/deps/a.jar" + string(os.PathListSeparator) + "/deps/b.jar",
		"/src/A.java", "/src/B.java",
	}
	if !reflect.DeepEqual(args, want) {
		t.Errorf("javacArgs() = %v, want %v", args, want)
	}
}

func TestJavacArgsNoRelease(t *testing.T) {
	args := javacArgs(0, "/out", nil, []string{"/src/A.java"})
	if contains(args, "--release") {
		t.Errorf("args = %v, want no --release", args)
	}
	if contains(args, "-classpath") {
		t.Errorf("args = %v, want no -classpath", args)
	}
}

func TestSourceFiles(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "B.java"), "")
	writeFile(t, filepath.Join(dir, "A.java"), "")
	writeFile(t, filepath.Join(dir, "sub", "C.java"), "")
	writeFile(t, filepath.Join(dir, "notes.txt"), "")

	files, err := sourceFiles([]string{dir}, ".java")
	if err != nil {
		t.Fatalf("sourceFiles() error = %v", err)
	}
	want := []string{
		filepath.Join(dir, "A.java"),
		filepath.Join(dir, "B.java"),
		filepath.Join(dir, "sub", "C.java"),
	}
	if !reflect.DeepEqual(files, want) {
		t.Errorf("sourceFiles() = %v, want %v", files, want)
	}
}

func TestCompileJava(t *testing.T) {
	root := t.TempDir()
	srcDir := filepath.Join(root, "src/main/java")
	writeFile(t, filepath.Join(srcDir, "App.java"), "class App {}")

	provider := newTestProvider(t, root)
	var recorded toolchain.Command
	provider.run = func(ctx context.Context, command toolchain.Command) (toolchain.Result, error) {
		recorded = command
		return toolchain.Result{}, nil
	}

	jdk := &toolchain.JDK{
		Home:    "/fake/jdk",
		Version: "21.0.1",
		Major:   21,
		Java:    "/fake/jdk/bin/java",
		Javac:   "/fake/jdk/bin/javac",
	}
	build := &build{
		project: &core.Project{ID: "app", Dir: root, Provider: "jvm"},
		options: mustOptions(t, nil),
		sources: sourceDirs{Java: []string{srcDir}},
		jdk:     jdk,
	}

	if err := provider.compileJava(context.Background(), build); err != nil {
		t.Fatalf("compileJava() error = %v", err)
	}
	if recorded.Path != jdk.Javac {
		t.Errorf("Path = %q, want %q", recorded.Path, jdk.Javac)
	}
	if recorded.Dir != root {
		t.Errorf("Dir = %q, want %q", recorded.Dir, root)
	}
	output := filepath.Join(root, "build", "classes", "java", "main")
	if !contains(recorded.Args, output) {
		t.Errorf("args = %v, want output dir", recorded.Args)
	}
	if !contains(recorded.Args, filepath.Join(srcDir, "App.java")) {
		t.Errorf("args = %v, want source file", recorded.Args)
	}

	env := envMap(recorded.Env)
	if env["JAVA_HOME"] != "/fake/jdk" {
		t.Errorf("JAVA_HOME = %q", env["JAVA_HOME"])
	}
	if env["TMPDIR"] != filepath.Join(root, ".kine", "tmp") {
		t.Errorf("TMPDIR = %q", env["TMPDIR"])
	}
}

func TestCompileIncremental(t *testing.T) {
	root := t.TempDir()
	srcDir := filepath.Join(root, "src/main/java")
	a := filepath.Join(srcDir, "A.java")
	writeFile(t, a, "class A {}")
	writeFile(t, filepath.Join(srcDir, "B.java"), "class B { A value; }")
	writeFile(t, filepath.Join(srcDir, "C.java"), "class C {}")

	provider := newTestProvider(t, root)
	var commands []toolchain.Command
	provider.run = func(ctx context.Context, command toolchain.Command) (toolchain.Result, error) {
		commands = append(commands, command)
		return toolchain.Result{}, nil
	}
	build := &build{
		project: &core.Project{ID: "app", Dir: root, Provider: "jvm"},
		options: mustOptions(t, nil),
		sources: sourceDirs{Java: []string{srcDir}},
		jdk:     &toolchain.JDK{Version: "21.0.1", Major: 21, Javac: "/fake/javac"},
	}

	if err := provider.compileJava(context.Background(), build); err != nil {
		t.Fatalf("first compile error = %v", err)
	}
	if len(commands) != 1 {
		t.Fatalf("commands = %d, want 1", len(commands))
	}

	writeFile(t, a, "class A { int value; }")
	if err := provider.compileJava(context.Background(), build); err != nil {
		t.Fatalf("second compile error = %v", err)
	}
	if len(commands) != 2 {
		t.Fatalf("commands = %d, want 2", len(commands))
	}

	sources := javaSources(commands[1].Args)
	names := make([]string, 0, len(sources))
	for _, source := range sources {
		names = append(names, filepath.Base(source))
	}
	if !contains(names, "A.java") || !contains(names, "B.java") {
		t.Errorf("sources = %v, want A and B", names)
	}
	if contains(names, "C.java") {
		t.Errorf("sources = %v, do not want C", names)
	}
}

func javaSources(args []string) []string {
	var sources []string
	for _, arg := range args {
		if strings.HasSuffix(arg, ".java") {
			sources = append(sources, arg)
		}
	}
	return sources
}

func TestCompileJavaError(t *testing.T) {
	root := t.TempDir()
	srcDir := filepath.Join(root, "src/main/java")
	writeFile(t, filepath.Join(srcDir, "App.java"), "class App {")

	provider := newTestProvider(t, root)
	provider.run = func(ctx context.Context, command toolchain.Command) (toolchain.Result, error) {
		return toolchain.Result{Stderr: []byte("bad source")}, &toolchain.ExitError{Code: 1}
	}
	build := &build{
		project: &core.Project{ID: "app", Dir: root, Provider: "jvm"},
		options: mustOptions(t, nil),
		sources: sourceDirs{Java: []string{srcDir}},
		jdk:     &toolchain.JDK{Home: "/fake/jdk", Version: "21.0.1", Major: 21, Javac: "/fake/jdk/bin/javac"},
	}

	err := provider.compileJava(context.Background(), build)
	if err == nil {
		t.Fatal("compileJava() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "bad source") {
		t.Errorf("error %q does not contain compiler output", err)
	}
	var exitErr *toolchain.ExitError
	if !errors.As(err, &exitErr) {
		t.Errorf("error %v is not an ExitError", err)
	}
}

func TestTasksIncludeCompileJava(t *testing.T) {
	root := t.TempDir()
	srcDir := filepath.Join(root, "src/main/java")
	writeFile(t, filepath.Join(srcDir, "App.java"), "class App {}")

	provider := newTestProvider(t, root)
	project := &core.Project{ID: "app", Dir: root, Provider: "jvm"}
	tasks, err := provider.Tasks(context.Background(), project)
	if err != nil {
		t.Fatalf("Tasks() error = %v", err)
	}
	task := findTask(tasks, "compile-java")
	if task == nil {
		t.Fatalf("compile-java task missing: %v", taskIDs(tasks))
	}
	if task.ID != "compile-java" {
		t.Errorf("ID = %q, want compile-java", task.ID)
	}
	if len(task.Outputs) != 2 || task.Outputs[0].Path != "build/classes/java/main" ||
		task.Outputs[1].Path != "build/classes/java/main.javac-state.json" {
		t.Errorf("Outputs = %+v", task.Outputs)
	}
	if len(task.Inputs) == 0 || task.Inputs[0].Path != "src/main/java" {
		t.Errorf("Inputs = %+v", task.Inputs)
	}
	if task.Key == "" {
		t.Error("Key is empty")
	}
	if task.Run == nil {
		t.Error("Run is nil")
	}
}

func TestTasksMemberOutputPath(t *testing.T) {
	workspaceRoot := t.TempDir()
	projectDir := filepath.Join(workspaceRoot, "app")
	writeFile(t, filepath.Join(projectDir, "src/main/java", "App.java"), "class App {}")

	provider := newTestProvider(t, workspaceRoot)
	project := &core.Project{ID: "app", Dir: projectDir, Provider: "jvm"}
	tasks, err := provider.Tasks(context.Background(), project)
	if err != nil {
		t.Fatalf("Tasks() error = %v", err)
	}
	task := findTask(tasks, "compile-java")
	if task == nil {
		t.Fatalf("compile-java task missing: %v", taskIDs(tasks))
	}
	if task.Outputs[0].Path != "app/build/classes/java/main" {
		t.Errorf("Output = %q, want app/build/classes/java/main", task.Outputs[0].Path)
	}
	if task.Inputs[0].Path != "app/src/main/java" {
		t.Errorf("Input = %q, want app/src/main/java", task.Inputs[0].Path)
	}
}

func TestTasksProjectDependency(t *testing.T) {
	workspaceRoot := t.TempDir()
	libDir := filepath.Join(workspaceRoot, "lib")
	appDir := filepath.Join(workspaceRoot, "app")
	writeFile(t, filepath.Join(appDir, "src/main/java", "App.java"), "class App {}")

	lib := &core.Project{ID: "lib", Dir: libDir}
	app := &core.Project{ID: "app", Dir: appDir, Provider: "jvm", DependsOn: []string{"lib"}}
	provider := newTestProvider(t, workspaceRoot)
	provider.workspace.Projects["lib"] = lib
	provider.workspace.Projects["app"] = app

	tasks, err := provider.Tasks(context.Background(), app)
	if err != nil {
		t.Fatalf("Tasks() error = %v", err)
	}
	task := findTask(tasks, "compile-java")
	if task == nil {
		t.Fatalf("compile-java task missing: %v", taskIDs(tasks))
	}
	if len(task.Deps) != 1 || task.Deps[0] != "lib:jar" {
		t.Errorf("Deps = %v, want [lib:jar]", task.Deps)
	}
}

func TestCompileJavaIntegration(t *testing.T) {
	jdk, err := toolchain.Find(toolchain.Requirement{})
	if err != nil {
		if errors.Is(err, toolchain.ErrJDKNotFound) {
			t.Skip("no JDK installed")
		}
		t.Fatalf("Find() error = %v", err)
	}

	root := t.TempDir()
	srcDir := filepath.Join(root, "src/main/java")
	writeFile(t, filepath.Join(srcDir, "demo", "App.java"),
		"package demo; public class App { public static void main(String[] args) {} }")

	provider := NewProvider(Config{
		Workspace: &core.Workspace{Root: root, Projects: map[string]*core.Project{}},
		Logger:    discardLogger(),
	})
	build := &build{
		project: &core.Project{ID: "app", Dir: root, Provider: "jvm"},
		options: mustOptions(t, nil),
		sources: sourceDirs{Java: []string{srcDir}},
		jdk:     jdk,
	}

	if err := provider.compileJava(context.Background(), build); err != nil {
		t.Fatalf("compileJava() error = %v", err)
	}
	classFile := filepath.Join(root, "build", "classes", "java", "main", "demo", "App.class")
	if _, err := os.Stat(classFile); err != nil {
		t.Fatalf("class file: %v", err)
	}
}

func findTask(tasks []*core.Task, id string) *core.Task {
	for _, task := range tasks {
		if task.ID == id {
			return task
		}
	}
	return nil
}

func taskIDs(tasks []*core.Task) []string {
	ids := make([]string, 0, len(tasks))
	for _, task := range tasks {
		ids = append(ids, task.ID)
	}
	return ids
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func envMap(env []string) map[string]string {
	values := make(map[string]string, len(env))
	for _, entry := range env {
		key, value, ok := strings.Cut(entry, "=")
		if ok {
			values[key] = value
		}
	}
	return values
}
