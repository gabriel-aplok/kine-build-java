package android

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gabriel-aplok/kine-build-java/internal/core"
	"github.com/gabriel-aplok/kine-build-java/internal/deps"
)

func TestFindKotlinTool(t *testing.T) {
	home := fakeKotlinHome(t, "2.4.21")

	tool, err := findKotlinTool(home, "2.4.21")
	if err != nil {
		t.Fatalf("findKotlinTool() error = %v", err)
	}
	if tool.Home != home || tool.Version != "2.4.21" {
		t.Errorf("tool = %+v", tool)
	}
	if tool.Compiler != filepath.Join(home, "bin", "kotlinc") {
		t.Errorf("Compiler = %q", tool.Compiler)
	}
	if tool.Stdlib != filepath.Join(home, "lib", "kotlin-stdlib.jar") {
		t.Errorf("Stdlib = %q", tool.Stdlib)
	}

	_, err = findKotlinTool(home, "2.4.22")
	if err == nil {
		t.Fatal("findKotlinTool() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "2.4.21") || !strings.Contains(err.Error(), "2.4.22") {
		t.Errorf("error %q does not name both versions", err)
	}
}

func TestFindKotlinToolMissing(t *testing.T) {
	_, err := findKotlinTool(t.TempDir(), "2.4.21")
	if err == nil {
		t.Fatal("findKotlinTool() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "kotlinc") {
		t.Errorf("error %q does not mention kotlinc", err)
	}
}

func TestPrepareKotlin(t *testing.T) {
	home := fakeKotlinHome(t, "2.4.21")
	workspace := t.TempDir()
	projectDir := filepath.Join(workspace, "app")
	kotlinDir := filepath.Join(projectDir, "src", "main", "kotlin")
	if err := os.MkdirAll(kotlinDir, 0o755); err != nil {
		t.Fatal(err)
	}

	options := Options{
		Kotlin:     "2.4.21",
		KotlinHome: home,
		KotlinDirs: []string{kotlinDir},
	}
	provider := &Provider{}
	if err := provider.prepareKotlin(workspace, &options); err != nil {
		t.Fatalf("prepareKotlin() error = %v", err)
	}

	stdlib := filepath.Join(workspace, ".kine", "tools", "kotlin", "2.4.21", "kotlin-stdlib.jar")
	if options.KotlinStdlib != stdlib {
		t.Errorf("KotlinStdlib = %q, want %q", options.KotlinStdlib, stdlib)
	}
	if options.KotlinVersion != "2.4.21" {
		t.Errorf("KotlinVersion = %q", options.KotlinVersion)
	}
	if data, err := os.ReadFile(stdlib); err != nil || string(data) != "stdlib" {
		t.Fatalf("copied stdlib = %q, %v", data, err)
	}
}

func TestPrepareKotlinCompose(t *testing.T) {
	home := fakeKotlinHome(t, "2.4.21")
	plugin := filepath.Join(home, "lib", "compose-compiler-plugin.jar")
	if err := os.WriteFile(plugin, []byte("compose"), 0o644); err != nil {
		t.Fatal(err)
	}
	workspace := t.TempDir()
	projectDir := filepath.Join(workspace, "app")
	kotlinDir := filepath.Join(projectDir, "src", "main", "kotlin")
	if err := os.MkdirAll(kotlinDir, 0o755); err != nil {
		t.Fatal(err)
	}

	options := Options{Kotlin: "2.4.21", KotlinHome: home, KotlinDirs: []string{kotlinDir}, Compose: true}
	provider := &Provider{}
	if err := provider.prepareKotlin(workspace, &options); err != nil {
		t.Fatalf("prepareKotlin() error = %v", err)
	}
	want := filepath.Join(workspace, ".kine", "tools", "kotlin", "2.4.21", "compose-compiler-plugin.jar")
	if options.ComposePlugin != want {
		t.Errorf("ComposePlugin = %q, want %q", options.ComposePlugin, want)
	}
	if data, err := os.ReadFile(want); err != nil || string(data) != "compose" {
		t.Fatalf("compose plugin = %q, %v", data, err)
	}
}

func TestPrepareKotlinComposeMissingPlugin(t *testing.T) {
	home := fakeKotlinHome(t, "2.4.21")
	workspace := t.TempDir()
	projectDir := filepath.Join(workspace, "app")
	kotlinDir := filepath.Join(projectDir, "src", "main", "kotlin")
	if err := os.MkdirAll(kotlinDir, 0o755); err != nil {
		t.Fatal(err)
	}
	options := Options{Kotlin: "2.4.21", KotlinHome: home, KotlinDirs: []string{kotlinDir}, Compose: true}
	provider := &Provider{}
	err := provider.prepareKotlin(workspace, &options)
	if err == nil {
		t.Fatal("prepareKotlin() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "compose-compiler-plugin") {
		t.Errorf("error %q does not mention the compose plugin", err)
	}
}

func TestKotlinArgs(t *testing.T) {
	options := Options{Java: 8}
	args := kotlinArgs(options, "/out", []string{"/stdlib.jar", "/android.jar"}, []string{"/java", "/gen"}, []string{"/A.kt", "/B.kt"})

	for _, want := range []string{
		"-no-stdlib",
		"-no-reflect",
		"-jvm-target", "1.8",
		"-classpath", "/stdlib.jar" + string(os.PathListSeparator) + "/android.jar",
		"-d", "/out",
		"-Xjava-source-roots=/java,/gen",
		"/A.kt", "/B.kt",
	} {
		if !contains(args, want) {
			t.Errorf("args %v do not contain %q", args, want)
		}
	}
}

func TestKotlinArgsCompose(t *testing.T) {
	options := Options{Java: 8, Compose: true, ComposePlugin: "/tools/compose-compiler-plugin.jar"}
	args := kotlinArgs(options, "/out", []string{"/stdlib.jar", "/android.jar"}, []string{"/java"}, []string{"/A.kt"})
	if !contains(args, "-Xplugin=/tools/compose-compiler-plugin.jar") {
		t.Errorf("args %v do not contain the compose plugin", args)
	}
}

func TestWithoutKotlinStdlib(t *testing.T) {
	artifacts := []deps.Artifact{
		{Coordinate: deps.Coordinate{Group: "org.jetbrains.kotlin", Artifact: "kotlin-stdlib", Version: "1.8.22"}},
		{Coordinate: deps.Coordinate{Group: "org.jetbrains.kotlin", Artifact: "kotlin-stdlib-jdk8", Version: "1.8.22"}},
		{Coordinate: deps.Coordinate{Group: "org.jetbrains.kotlin", Artifact: "kotlin-reflect", Version: "1.8.22"}},
		{Coordinate: deps.Coordinate{Group: "com.example", Artifact: "lib", Version: "1.0"}},
	}
	filtered := withoutKotlinStdlib(artifacts)
	if len(filtered) != 2 {
		t.Fatalf("filtered = %+v", filtered)
	}
	if filtered[0].Artifact != "kotlin-reflect" || filtered[1].Artifact != "lib" {
		t.Errorf("filtered = %+v", filtered)
	}
}

func TestTasksKotlin(t *testing.T) {
	workspace, projectDir := providerProject(t)
	kotlinDir := filepath.Join(projectDir, "src", "main", "kotlin")
	if err := os.MkdirAll(kotlinDir, 0o755); err != nil {
		t.Fatal(err)
	}
	home := fakeKotlinHome(t, "2.4.21")

	provider := &Provider{
		config:  Config{Workspace: &core.Workspace{Root: workspace}},
		findSDK: fakeFindSDK(testSDK()),
		findJDK: fakeFindJDK(testJDK()),
	}
	project := &core.Project{
		ID:  "app",
		Dir: projectDir,
		Options: map[string]any{
			"compile_sdk": 36,
			"kotlin":      "2.4.21",
			"kotlin_home": home,
		},
	}

	tasks, err := provider.Tasks(context.Background(), project)
	if err != nil {
		t.Fatalf("Tasks() error = %v", err)
	}
	ids := make(map[string]*core.Task, len(tasks))
	for _, task := range tasks {
		ids[task.ID] = task
	}
	for _, id := range []string{"android-resources", "compile-kotlin", "compile-java", "dex-libs", "dex", "apk"} {
		if ids[id] == nil {
			t.Errorf("task %q is missing: %v", id, ids)
		}
	}
	if !contains(ids["compile-java"].Deps, "app:compile-kotlin") {
		t.Errorf("compile deps = %v", ids["compile-java"].Deps)
	}
	if !contains(ids["apk"].Deps, "app:dex-libs") {
		t.Errorf("apk deps = %v", ids["apk"].Deps)
	}
	inputPaths := make([]string, 0, len(ids["dex"].Inputs))
	for _, input := range ids["dex"].Inputs {
		inputPaths = append(inputPaths, input.Path)
	}
	if !contains(inputPaths, "app/build/android/classes-kotlin") {
		t.Errorf("dex inputs = %v", inputPaths)
	}
	if _, err := os.Stat(filepath.Join(workspace, ".kine", "tools", "kotlin", "2.4.21", "kotlin-stdlib.jar")); err != nil {
		t.Errorf("stdlib was not copied: %v", err)
	}
}

func fakeKotlinHome(t *testing.T, version string) string {
	t.Helper()

	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(home, "lib"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "bin", "kotlinc"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "lib", "kotlin-stdlib.jar"), []byte("stdlib"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "build.txt"), []byte(version+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return home
}
