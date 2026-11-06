package minecraft

import (
	"context"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/gabriel-aplok/kine-build-java/internal/core"
	"github.com/gabriel-aplok/kine-build-java/internal/toolchain"
)

func TestRemapTask(t *testing.T) {
	workspace, projectDir := mcProject(t)
	mappings := filepath.Join(workspace, "game", "mappings.tiny")
	writeLocalFile(t, mappings, "tiny\t2\t0\tofficial\tintermediary\tnamed\n")
	remapper := filepath.Join(workspace, "game", "tiny-remapper.jar")
	writeLocalFile(t, remapper, "remapper")

	var commands []toolchain.Command
	provider := &Provider{run: func(ctx context.Context, command toolchain.Command) (toolchain.Result, error) {
		commands = append(commands, command)
		return toolchain.Result{}, nil
	}}
	options := Options{
		Minecraft:    "1.20.1",
		ModID:        "example-mod",
		ModVersion:   "1.2.3",
		MinecraftJar: filepath.Join(workspace, "game", "minecraft.jar"),
		Classpath:    []string{filepath.Join(workspace, "game", "fabric-loader.jar")},
		Mappings:     mappings,
		Remapper:     remapper,
	}
	jdk := &toolchain.JDK{Home: "/jdk", Version: "21.0.1", Major: 21, Java: "/jdk/bin/java"}

	task, err := provider.remapTask(workspace, &core.Project{ID: "app", Dir: projectDir}, options, jdk)
	if err != nil {
		t.Fatalf("remapTask() error = %v", err)
	}
	if task.ID != "remap" {
		t.Errorf("ID = %q", task.ID)
	}
	if len(task.Deps) != 1 || task.Deps[0] != "app:mod-jar" {
		t.Errorf("Deps = %v", task.Deps)
	}
	if len(task.Outputs) != 1 || task.Outputs[0].Path != "app/build/libs/example-mod-1.2.3-remapped.jar" {
		t.Errorf("Outputs = %+v", task.Outputs)
	}
	if !strings.Contains(task.Key, "named=intermediary") {
		t.Errorf("Key = %q", task.Key)
	}

	if err := task.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(commands) != 1 {
		t.Fatalf("commands = %d, want 1", len(commands))
	}
	command := commands[0]
	if command.Path != jdk.Java {
		t.Errorf("Path = %q", command.Path)
	}
	input := filepath.Join(projectDir, "build", "libs", "example-mod-1.2.3.jar")
	output := filepath.Join(projectDir, "build", "libs", "example-mod-1.2.3-remapped.jar")
	want := []string{
		toolchain.HeapArg("-Xmx", toolchain.DefaultCompilerHeap),
		"-cp", remapper,
		"net.fabricmc.tinyremapper.Main",
		input,
		output,
		mappings,
		"named",
		"intermediary",
		options.MinecraftJar,
		options.Classpath[0],
	}
	if !reflect.DeepEqual(command.Args, want) {
		t.Errorf("Args = %v, want %v", command.Args, want)
	}
}

func TestRemapTaskMissingNamespace(t *testing.T) {
	workspace, projectDir := mcProject(t)
	mappings := filepath.Join(workspace, "game", "mappings.tiny")
	writeLocalFile(t, mappings, "tiny\t2\t0\tofficial\tintermediary\n")
	remapper := filepath.Join(workspace, "game", "tiny-remapper.jar")
	writeLocalFile(t, remapper, "remapper")

	provider := &Provider{}
	options := Options{
		ModID:      "example-mod",
		ModVersion: "1.2.3",
		Mappings:   mappings,
		Remapper:   remapper,
	}
	_, err := provider.remapTask(workspace, &core.Project{ID: "app", Dir: projectDir}, options, testJDK())
	if err == nil {
		t.Fatal("remapTask() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "named") {
		t.Errorf("error %q does not mention the namespace", err)
	}
}

func TestRemapTaskError(t *testing.T) {
	workspace, projectDir := mcProject(t)
	mappings := filepath.Join(workspace, "game", "mappings.tiny")
	writeLocalFile(t, mappings, "tiny\t2\t0\tofficial\tintermediary\tnamed\n")
	remapper := filepath.Join(workspace, "game", "tiny-remapper.jar")
	writeLocalFile(t, remapper, "remapper")

	provider := &Provider{run: func(ctx context.Context, command toolchain.Command) (toolchain.Result, error) {
		return toolchain.Result{Stderr: []byte("bad remap")}, &toolchain.ExitError{Code: 1}
	}}
	options := Options{
		Minecraft:  "1.20.1",
		ModID:      "example-mod",
		ModVersion: "1.2.3",
		Mappings:   mappings,
		Remapper:   remapper,
	}
	task, err := provider.remapTask(workspace, &core.Project{ID: "app", Dir: projectDir}, options, testJDK())
	if err != nil {
		t.Fatal(err)
	}
	err = task.Run(context.Background())
	if err == nil {
		t.Fatal("Run() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "tiny-remapper") || !strings.Contains(err.Error(), "bad remap") {
		t.Errorf("error %q does not carry the remapper output", err)
	}
}

func testJDK() *toolchain.JDK {
	return &toolchain.JDK{Home: "/jdk", Version: "21.0.1", Major: 21, Java: "/jdk/bin/java", Javac: "/jdk/bin/javac"}
}
