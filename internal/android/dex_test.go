package android

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/gabriel-aplok/kine-build-java/internal/core"
	"github.com/gabriel-aplok/kine-build-java/internal/deps"
	"github.com/gabriel-aplok/kine-build-java/internal/toolchain"
)

func TestDexTask(t *testing.T) {
	workspace, projectDir, _, _ := resourceProject(t)
	writeClasses(t, projectDir)
	artifactPath := filepath.Join(projectDir, ".kine", "deps", "lib.jar")
	if err := os.MkdirAll(filepath.Dir(artifactPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(artifactPath, []byte("jar"), 0o600); err != nil {
		t.Fatal(err)
	}

	var commands []toolchain.Command
	provider := &Provider{run: func(ctx context.Context, command toolchain.Command) (toolchain.Result, error) {
		commands = append(commands, command)
		return toolchain.Result{}, nil
	}}
	sdk := &SDK{D8: "/sdk/d8", AndroidJar: "/sdk/android.jar", BuildTools: "36.0.0", Platform: "/sdk/platforms/android-36"}
	jdk := &toolchain.JDK{Home: "/jdk", Version: "21.0.1", Java: "/jdk/bin/java"}
	options := Options{MinSDK: 24}
	artifacts := []deps.Artifact{{Path: artifactPath}}

	task, err := provider.dexTask(workspace, &core.Project{ID: "app", Dir: projectDir}, options, sdk, jdk, artifacts, nil, nil)
	if err != nil {
		t.Fatalf("dexTask() error = %v", err)
	}
	if task.ID != "dex" {
		t.Errorf("ID = %q", task.ID)
	}
	if len(task.Deps) != 1 || task.Deps[0] != "app:compile-java" {
		t.Errorf("Deps = %v", task.Deps)
	}
	if len(task.Outputs) != 2 || task.Outputs[0].Path != "app/build/android/dex" || task.Outputs[0].Mode != core.ModeTree {
		t.Errorf("Outputs = %+v", task.Outputs)
	}
	if task.Outputs[1].Path != "app/build/android/classes.jar" || task.Outputs[1].Mode != core.ModeFile {
		t.Errorf("classes.jar output = %+v", task.Outputs[1])
	}
	if !strings.Contains(task.Key, "dex-app 36.0.0") || !strings.Contains(task.Key, "min=24") {
		t.Errorf("Key = %q", task.Key)
	}

	if err := task.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(commands) != 1 {
		t.Fatalf("commands = %d, want 1", len(commands))
	}
	command := commands[0]
	want := []string{
		"--lib", "/sdk/android.jar",
		"--min-api", "24",
		"--output", filepath.Join(projectDir, "build", "android", "dex"),
		"--classpath", artifactPath,
		filepath.Join(projectDir, "build", "android", "classes.jar"),
	}
	if command.Path != sdk.D8 || !reflect.DeepEqual(command.Args, want) {
		t.Errorf("command = %q %v, want %v", command.Path, command.Args, want)
	}
}

func TestDexTaskError(t *testing.T) {
	workspace, projectDir, _, _ := resourceProject(t)
	writeClasses(t, projectDir)
	provider := &Provider{run: func(ctx context.Context, command toolchain.Command) (toolchain.Result, error) {
		return toolchain.Result{Stderr: []byte("bad dex")}, &toolchain.ExitError{Code: 1}
	}}
	sdk := &SDK{D8: "/sdk/d8", AndroidJar: "/sdk/android.jar"}
	jdk := &toolchain.JDK{Home: "/jdk", Version: "21.0.1", Java: "/jdk/bin/java"}

	task, err := provider.dexTask(workspace, &core.Project{ID: "app", Dir: projectDir}, Options{MinSDK: 24}, sdk, jdk, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	err = task.Run(context.Background())
	if err == nil {
		t.Fatal("Run() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "d8") || !strings.Contains(err.Error(), "bad dex") {
		t.Errorf("error %q does not carry the d8 output", err)
	}
}

func writeClasses(t *testing.T, projectDir string) {
	t.Helper()

	classes := filepath.Join(projectDir, "build", "android", "classes")
	if err := os.MkdirAll(classes, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(classes, "Main.class"), []byte("class"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestDexLibTask(t *testing.T) {
	workspace, projectDir, _, _ := resourceProject(t)
	artifactPath := filepath.Join(projectDir, ".kine", "deps", "lib.jar")
	if err := os.MkdirAll(filepath.Dir(artifactPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(artifactPath, []byte("jar"), 0o600); err != nil {
		t.Fatal(err)
	}

	var commands []toolchain.Command
	provider := &Provider{run: func(ctx context.Context, command toolchain.Command) (toolchain.Result, error) {
		commands = append(commands, command)
		return toolchain.Result{}, nil
	}}
	sdk := &SDK{D8: "/sdk/d8", AndroidJar: "/sdk/android.jar", BuildTools: "36.0.0", Platform: "/sdk/platforms/android-36"}
	jdk := &toolchain.JDK{Home: "/jdk", Version: "21.0.1", Major: 21, Java: "/jdk/bin/java"}
	artifacts := []deps.Artifact{{Path: artifactPath}}

	task, err := provider.dexLibTask(workspace, &core.Project{ID: "app", Dir: projectDir}, Options{MinSDK: 24}, sdk, jdk, artifacts)
	if err != nil {
		t.Fatalf("dexLibTask() error = %v", err)
	}
	if task.ID != "dex-libs" {
		t.Errorf("ID = %q", task.ID)
	}
	if !strings.Contains(task.Key, "d8-libs") {
		t.Errorf("Key = %q", task.Key)
	}
	if err := task.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(commands) != 1 {
		t.Fatalf("commands = %d, want 1", len(commands))
	}
	want := []string{
		"--lib", "/sdk/android.jar",
		"--min-api", "24",
		"--output", filepath.Join(projectDir, "build", "android", "dex-libs"),
		artifactPath,
	}
	if commands[0].Path != sdk.D8 || !reflect.DeepEqual(commands[0].Args, want) {
		t.Errorf("command = %q %v, want %v", commands[0].Path, commands[0].Args, want)
	}
}

func TestDexLibTaskNoArtifacts(t *testing.T) {
	task, err := (&Provider{}).dexLibTask("/tmp", &core.Project{ID: "app", Dir: "/tmp/app"}, Options{}, &SDK{}, &toolchain.JDK{}, nil)
	if err != nil {
		t.Fatalf("dexLibTask() error = %v", err)
	}
	if task != nil {
		t.Errorf("task = %+v, want nil", task)
	}
}
