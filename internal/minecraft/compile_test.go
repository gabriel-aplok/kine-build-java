package minecraft

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gabriel-aplok/kine-build-java/internal/core"
	"github.com/gabriel-aplok/kine-build-java/internal/toolchain"
)

func TestCompileTask(t *testing.T) {
	workspace, projectDir := mcProject(t)
	javaDir := filepath.Join(projectDir, "src", "main", "java")

	var commands []toolchain.Command
	provider := &Provider{run: func(ctx context.Context, command toolchain.Command) (toolchain.Result, error) {
		commands = append(commands, command)
		return toolchain.Result{}, nil
	}}
	options := Options{
		Minecraft:     "1.20.1",
		ModID:         "example-mod",
		ModVersion:    "1.2.3",
		MinecraftJar:  "/game/minecraft.jar",
		Classpath:     []string{"/game/fabric-loader.jar"},
		JavaDirs:      []string{javaDir},
		MinecraftHash: "mc-hash",
		ClasspathHash: "cp-hash",
	}
	jdk := &toolchain.JDK{Home: "/jdk", Version: "21.0.1", Major: 21, Javac: "/jdk/bin/javac"}

	task, err := provider.compileTask(workspace, &core.Project{ID: "app", Dir: projectDir}, options, jdk)
	if err != nil {
		t.Fatalf("compileTask() error = %v", err)
	}
	if task.ID != "compile-java" {
		t.Errorf("ID = %q", task.ID)
	}
	if len(task.Outputs) != 1 || task.Outputs[0].Path != "app/build/classes" {
		t.Errorf("Outputs = %+v", task.Outputs)
	}
	if !strings.Contains(task.Key, "mc=1.20.1") || !strings.Contains(task.Key, "mc-hash") {
		t.Errorf("Key = %q", task.Key)
	}

	if err := task.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(commands) != 1 {
		t.Fatalf("commands = %d, want 1", len(commands))
	}
	command := commands[0]
	if command.Path != jdk.Javac {
		t.Errorf("Path = %q", command.Path)
	}
	for _, want := range []string{
		"-source", "17",
		"-target", "17",
		"-encoding", "UTF-8",
		"-d", filepath.Join(projectDir, "build", "classes"),
		"-classpath",
		filepath.Join(javaDir, "com", "example", "ExampleMod.java"),
	} {
		if !contains(command.Args, want) {
			t.Errorf("args %v do not contain %q", command.Args, want)
		}
	}
	index := indexOf(command.Args, "-classpath")
	if index < 0 || index+1 >= len(command.Args) {
		t.Fatal("classpath is missing")
	}
	classpath := command.Args[index+1]
	if !strings.HasPrefix(classpath, "/game/minecraft.jar") || !strings.Contains(classpath, "/game/fabric-loader.jar") {
		t.Errorf("classpath = %q", classpath)
	}
}

func TestCompileTaskError(t *testing.T) {
	workspace, projectDir := mcProject(t)
	provider := &Provider{run: func(ctx context.Context, command toolchain.Command) (toolchain.Result, error) {
		return toolchain.Result{Stderr: []byte("bad mod")}, &toolchain.ExitError{Code: 1}
	}}
	options := Options{
		Minecraft:    "1.20.1",
		ModVersion:   "1.2.3",
		MinecraftJar: "/game/minecraft.jar",
		JavaDirs:     []string{filepath.Join(projectDir, "src", "main", "java")},
	}
	jdk := &toolchain.JDK{Home: "/jdk", Version: "21.0.1", Major: 21, Javac: "/jdk/bin/javac"}

	task, err := provider.compileTask(workspace, &core.Project{ID: "app", Dir: projectDir}, options, jdk)
	if err != nil {
		t.Fatal(err)
	}
	err = task.Run(context.Background())
	if err == nil {
		t.Fatal("Run() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "javac") || !strings.Contains(err.Error(), "bad mod") {
		t.Errorf("error %q does not carry the javac output", err)
	}
}

func TestCompileTaskNoSources(t *testing.T) {
	workspace, projectDir := mcProject(t)
	called := false
	provider := &Provider{run: func(ctx context.Context, command toolchain.Command) (toolchain.Result, error) {
		called = true
		return toolchain.Result{}, nil
	}}
	options := Options{
		Minecraft:    "1.20.1",
		ModVersion:   "1.2.3",
		MinecraftJar: "/game/minecraft.jar",
	}
	jdk := &toolchain.JDK{Home: "/jdk", Version: "21.0.1", Major: 21, Javac: "/jdk/bin/javac"}

	task, err := provider.compileTask(workspace, &core.Project{ID: "app", Dir: projectDir}, options, jdk)
	if err != nil {
		t.Fatal(err)
	}
	if err := task.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if called {
		t.Error("javac ran without sources")
	}
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func indexOf(values []string, want string) int {
	for i, value := range values {
		if value == want {
			return i
		}
	}
	return -1
}

func writeClassFile(t *testing.T, projectDir string) {
	t.Helper()

	path := filepath.Join(projectDir, "build", "classes", "com", "example", "ExampleMod.class")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("class"), 0o600); err != nil {
		t.Fatal(err)
	}
}
