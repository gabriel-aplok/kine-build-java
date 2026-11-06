package android

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gabriel-aplok/kine-build-java/internal/core"
	"github.com/gabriel-aplok/kine-build-java/internal/deps"
	"github.com/gabriel-aplok/kine-build-java/internal/toolchain"
)

func TestCompileTask(t *testing.T) {
	workspace, projectDir, _, _ := resourceProject(t)
	javaDir := filepath.Join(projectDir, "src", "main", "java")
	if err := os.MkdirAll(javaDir, 0o755); err != nil {
		t.Fatal(err)
	}
	appSource := filepath.Join(javaDir, "App.java")
	if err := os.WriteFile(appSource, []byte("class App {}"), 0o600); err != nil {
		t.Fatal(err)
	}
	genDir := filepath.Join(projectDir, "build", "android", "gen")
	if err := os.MkdirAll(filepath.Join(genDir, "com", "example"), 0o755); err != nil {
		t.Fatal(err)
	}
	rSource := filepath.Join(genDir, "com", "example", "R.java")
	if err := os.WriteFile(rSource, []byte("package com.example; class R {}"), 0o600); err != nil {
		t.Fatal(err)
	}

	var commands []toolchain.Command
	provider := &Provider{run: func(ctx context.Context, command toolchain.Command) (toolchain.Result, error) {
		commands = append(commands, command)
		return toolchain.Result{}, nil
	}}
	sdk := &SDK{AndroidJar: "/sdk/android.jar", BuildTools: "36.0.0"}
	jdk := &toolchain.JDK{Home: "/jdk", Version: "21.0.1", Major: 21, Javac: "/jdk/bin/javac"}
	options := Options{JavaDirs: []string{javaDir}}
	artifactPath := filepath.Join(projectDir, ".kine", "deps", "lib.jar")
	if err := os.MkdirAll(filepath.Dir(artifactPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(artifactPath, []byte("jar"), 0o600); err != nil {
		t.Fatal(err)
	}
	artifacts := []deps.Artifact{{Path: artifactPath}}

	task, err := provider.compileTask(workspace, &core.Project{ID: "app", Dir: projectDir}, options, sdk, jdk, artifacts, nil, nil)
	if err != nil {
		t.Fatalf("compileTask() error = %v", err)
	}
	if task.ID != "compile-java" {
		t.Errorf("ID = %q", task.ID)
	}
	if len(task.Deps) != 1 || task.Deps[0] != "app:android-resources" {
		t.Errorf("Deps = %v", task.Deps)
	}
	if len(task.Outputs) != 1 || task.Outputs[0].Path != "app/build/android/classes" {
		t.Errorf("Outputs = %+v", task.Outputs)
	}
	if !strings.Contains(task.Key, "javac 21.0.1") || !strings.Contains(task.Key, "android=36.0.0") {
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
		"-d", filepath.Join(projectDir, "build", "android", "classes"),
		"-classpath",
		appSource,
		rSource,
	} {
		if !contains(command.Args, want) {
			t.Errorf("args %v do not contain %q", command.Args, want)
		}
	}
	classpathIndex := indexOf(command.Args, "-classpath")
	if classpathIndex < 0 || classpathIndex+1 >= len(command.Args) {
		t.Fatal("classpath is missing")
	}
	classpath := command.Args[classpathIndex+1]
	if !strings.Contains(classpath, "/sdk/android.jar") || !strings.Contains(classpath, artifactPath) {
		t.Errorf("classpath = %q", classpath)
	}
	if !strings.HasPrefix(classpath, "/sdk/android.jar") {
		t.Errorf("classpath does not start with android.jar: %q", classpath)
	}
}

func TestJavacArgsJavaVersion(t *testing.T) {
	args := javacArgs("/out", []string{"/sdk/android.jar"}, []string{"/src/App.java"}, 8)
	for i := 0; i+1 < len(args); i++ {
		if (args[i] == "-source" || args[i] == "-target") && args[i+1] != "8" {
			t.Errorf("%s = %q, want 8", args[i], args[i+1])
		}
	}
	if javaVersion(Options{}) != defaultJava {
		t.Errorf("default java = %d, want %d", javaVersion(Options{}), defaultJava)
	}
	if javaVersion(Options{Java: 11}) != 11 {
		t.Errorf("java = %d, want 11", javaVersion(Options{Java: 11}))
	}
}

func TestCompileTaskWithSiblings(t *testing.T) {
	workspace, projectDir, _, _ := resourceProject(t)
	javaDir := filepath.Join(projectDir, "src", "main", "java")
	if err := os.MkdirAll(javaDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(javaDir, "App.java"), []byte("class App {}"), 0o600); err != nil {
		t.Fatal(err)
	}
	genDir := filepath.Join(projectDir, "build", "android", "gen")
	if err := os.MkdirAll(genDir, 0o755); err != nil {
		t.Fatal(err)
	}
	siblingDir := filepath.Join(workspace, "core", "build", "classes", "java", "main")
	if err := os.MkdirAll(siblingDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(siblingDir, "Core.class"), []byte("class"), 0o600); err != nil {
		t.Fatal(err)
	}

	var recorded toolchain.Command
	provider := &Provider{run: func(ctx context.Context, command toolchain.Command) (toolchain.Result, error) {
		recorded = command
		return toolchain.Result{}, nil
	}}
	sdk := &SDK{AndroidJar: "/sdk/android.jar", BuildTools: "36.0.0"}
	jdk := &toolchain.JDK{Home: "/jdk", Version: "21.0.1", Major: 21, Javac: "/jdk/bin/javac"}
	options := Options{JavaDirs: []string{javaDir}}

	task, err := provider.compileTask(workspace, &core.Project{ID: "app", Dir: projectDir}, options, sdk, jdk, nil,
		[]string{siblingDir}, []string{"core:compile-java"})
	if err != nil {
		t.Fatalf("compileTask() error = %v", err)
	}
	if !contains(task.Deps, "core:compile-java") {
		t.Errorf("Deps = %v, want core:compile-java", task.Deps)
	}
	found := false
	for _, input := range task.Inputs {
		if input.Path == "core/build/classes/java/main" {
			found = true
		}
	}
	if !found {
		t.Errorf("Inputs = %+v, want the sibling classes", task.Inputs)
	}

	if err := task.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	index := indexOf(recorded.Args, "-classpath")
	if index < 0 || index+1 >= len(recorded.Args) {
		t.Fatal("classpath is missing")
	}
	if !strings.Contains(recorded.Args[index+1], siblingDir) {
		t.Errorf("classpath = %q, want the sibling classes", recorded.Args[index+1])
	}
}

func TestCompileTaskError(t *testing.T) {
	workspace, projectDir, _, _ := resourceProject(t)
	javaDir := filepath.Join(projectDir, "src", "main", "java")
	if err := os.MkdirAll(javaDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(javaDir, "App.java"), []byte("class App {"), 0o600); err != nil {
		t.Fatal(err)
	}
	genDir := filepath.Join(projectDir, "build", "android", "gen")
	if err := os.MkdirAll(genDir, 0o755); err != nil {
		t.Fatal(err)
	}

	provider := &Provider{run: func(ctx context.Context, command toolchain.Command) (toolchain.Result, error) {
		return toolchain.Result{Stderr: []byte("bad java")}, &toolchain.ExitError{Code: 1}
	}}
	sdk := &SDK{AndroidJar: "/sdk/android.jar", BuildTools: "36.0.0"}
	jdk := &toolchain.JDK{Home: "/jdk", Version: "21.0.1", Major: 21, Javac: "/jdk/bin/javac"}
	options := Options{JavaDirs: []string{javaDir}}

	task, err := provider.compileTask(workspace, &core.Project{ID: "app", Dir: projectDir}, options, sdk, jdk, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	err = task.Run(context.Background())
	if err == nil {
		t.Fatal("Run() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "javac") || !strings.Contains(err.Error(), "bad java") {
		t.Errorf("error %q does not carry the javac output", err)
	}
}

func indexOf(values []string, want string) int {
	for i, value := range values {
		if value == want {
			return i
		}
	}
	return -1
}
