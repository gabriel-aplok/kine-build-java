package android

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/gabriel-aplok/kine-build-java/internal/core"
	"github.com/gabriel-aplok/kine-build-java/internal/toolchain"
)

func TestResourceTask(t *testing.T) {
	workspace, projectDir, manifest, resDir := resourceProject(t)
	sdk := &SDK{Aapt2: "/sdk/aapt2", AndroidJar: "/sdk/android.jar", BuildTools: "36.0.0"}

	var commands []toolchain.Command
	provider := &Provider{run: func(ctx context.Context, command toolchain.Command) (toolchain.Result, error) {
		commands = append(commands, command)
		return toolchain.Result{}, nil
	}}
	options := Options{
		MinSDK:        24,
		TargetSDK:     35,
		VersionCode:   1,
		VersionName:   "1.0",
		Manifest:      manifest,
		ResDirs:       []string{resDir},
		ExtraPackages: []string{"com.example.lib", "com.example.ui"},
	}

	task, err := provider.resourceTask(workspace, &core.Project{ID: "app", Dir: projectDir}, options, sdk, &toolchain.JDK{})
	if err != nil {
		t.Fatalf("resourceTask() error = %v", err)
	}
	if task.ID != "android-resources" {
		t.Errorf("ID = %q", task.ID)
	}
	if len(task.Inputs) != 2 || task.Inputs[0].Path != "app/src/main/AndroidManifest.xml" || task.Inputs[1].Path != "app/src/main/res" {
		t.Errorf("Inputs = %+v", task.Inputs)
	}
	if len(task.Outputs) != 3 {
		t.Fatalf("Outputs = %+v", task.Outputs)
	}
	if task.Outputs[0].Path != "app/build/android/resources.apk" || task.Outputs[0].Mode != core.ModeFile {
		t.Errorf("resources output = %+v", task.Outputs[0])
	}
	if task.Outputs[1].Path != "app/build/android/gen" || task.Outputs[1].Mode != core.ModeTree {
		t.Errorf("gen output = %+v", task.Outputs[1])
	}
	if task.Outputs[2].Path != "app/build/android/AndroidManifest.xml" || task.Outputs[2].Mode != core.ModeFile {
		t.Errorf("manifest output = %+v", task.Outputs[2])
	}
	if !strings.Contains(task.Key, "aapt2 36.0.0") || !strings.Contains(task.Key, "min=24") {
		t.Errorf("Key = %q", task.Key)
	}

	if err := task.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(commands) != 2 {
		t.Fatalf("commands = %d, want 2", len(commands))
	}

	flat := filepath.Join(projectDir, "build", "android", "res-0.zip")
	compile := commands[0]
	wantCompile := []string{"compile", "--dir", resDir, "-o", flat}
	if compile.Path != sdk.Aapt2 || !reflect.DeepEqual(compile.Args, wantCompile) {
		t.Errorf("compile = %q %v", compile.Path, compile.Args)
	}

	link := commands[1]
	if link.Path != sdk.Aapt2 {
		t.Errorf("link path = %q", link.Path)
	}
	for _, want := range []string{
		"link",
		filepath.Join(projectDir, "build", "android", "resources.apk"),
		sdk.AndroidJar,
		filepath.Join(projectDir, "build", "android", "AndroidManifest.xml"),
		filepath.Join(projectDir, "build", "android", "gen"),
		"--min-sdk-version", "24",
		"--target-sdk-version", "35",
		"--version-code", "1",
		"--version-name", "1.0",
		"--extra-packages", "com.example.lib",
		"--extra-packages", "com.example.ui",
		flat,
	} {
		if !contains(link.Args, want) {
			t.Errorf("link args %v do not contain %q", link.Args, want)
		}
	}
}

func TestResourceShrinkArgs(t *testing.T) {
	options := Options{
		MinSDK:      24,
		VersionCode: 1,
		VersionName: "1.0",
		Variant:     Variant{BuildTypeOptions: BuildTypeOptions{ShrinkResources: true}},
	}
	args := linkArgs("/out/resources.apk", "/out/gen", options, &SDK{AndroidJar: "/sdk/android.jar"}, []string{"/out/res-0.zip"}, "/out/aapt-rules.pro")
	for _, want := range []string{"--proguard", "/out/aapt-rules.pro"} {
		if !contains(args, want) {
			t.Errorf("args %v do not contain %q", args, want)
		}
	}
}

func TestResourceTaskShrinkOutput(t *testing.T) {
	workspace, projectDir, manifest, resDir := resourceProject(t)
	provider := &Provider{}
	options := Options{
		MinSDK:      24,
		VersionCode: 1,
		VersionName: "1.0",
		Manifest:    manifest,
		ResDirs:     []string{resDir},
		Variant:     Variant{BuildTypeOptions: BuildTypeOptions{ShrinkResources: true}},
	}
	task, err := provider.resourceTask(workspace, &core.Project{ID: "app", Dir: projectDir}, options, &SDK{Aapt2: "/sdk/aapt2", AndroidJar: "/sdk/android.jar"}, &toolchain.JDK{})
	if err != nil {
		t.Fatal(err)
	}
	if len(task.Outputs) != 5 {
		t.Fatalf("Outputs = %+v", task.Outputs)
	}
	if task.Outputs[3].Path != "app/build/android/res-shrunk" {
		t.Errorf("shrunk resources output = %+v", task.Outputs[3])
	}
	if task.Outputs[4].Path != "app/build/android/resources-proto.apk" {
		t.Errorf("proto output = %+v", task.Outputs[4])
	}
}

func TestResourceTaskMultipleResDirs(t *testing.T) {
	workspace, projectDir, manifest, resDir := resourceProject(t)
	second := filepath.Join(projectDir, "src", "extra", "res")
	if err := os.MkdirAll(second, 0o755); err != nil {
		t.Fatal(err)
	}

	var commands []toolchain.Command
	provider := &Provider{run: func(ctx context.Context, command toolchain.Command) (toolchain.Result, error) {
		commands = append(commands, command)
		return toolchain.Result{}, nil
	}}
	options := Options{
		MinSDK:      24,
		VersionCode: 1,
		VersionName: "1.0",
		Manifest:    manifest,
		ResDirs:     []string{resDir, second},
	}

	task, err := provider.resourceTask(workspace, &core.Project{ID: "app", Dir: projectDir}, options, &SDK{Aapt2: "/sdk/aapt2", AndroidJar: "/sdk/android.jar"}, &toolchain.JDK{})
	if err != nil {
		t.Fatal(err)
	}
	if err := task.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(commands) != 3 {
		t.Fatalf("commands = %d, want 3", len(commands))
	}
	if !contains(commands[1].Args, filepath.Join(projectDir, "build", "android", "res-1.zip")) {
		t.Errorf("second compile args = %v", commands[1].Args)
	}
	if !contains(commands[2].Args, filepath.Join(projectDir, "build", "android", "res-0.zip")) ||
		!contains(commands[2].Args, filepath.Join(projectDir, "build", "android", "res-1.zip")) {
		t.Errorf("link args = %v", commands[2].Args)
	}
}

func TestResourceTaskError(t *testing.T) {
	workspace, projectDir, manifest, resDir := resourceProject(t)
	provider := &Provider{run: func(ctx context.Context, command toolchain.Command) (toolchain.Result, error) {
		return toolchain.Result{Stderr: []byte("bad resource")}, &toolchain.ExitError{Code: 1}
	}}
	options := Options{
		MinSDK:      24,
		VersionCode: 1,
		VersionName: "1.0",
		Manifest:    manifest,
		ResDirs:     []string{resDir},
	}

	task, err := provider.resourceTask(workspace, &core.Project{ID: "app", Dir: projectDir}, options, &SDK{Aapt2: "/sdk/aapt2", AndroidJar: "/sdk/android.jar"}, &toolchain.JDK{})
	if err != nil {
		t.Fatal(err)
	}
	err = task.Run(context.Background())
	if err == nil {
		t.Fatal("Run() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "aapt2 compile") || !strings.Contains(err.Error(), "bad resource") {
		t.Errorf("error %q does not carry the aapt2 output", err)
	}
}

func resourceProject(t *testing.T) (string, string, string, string) {
	t.Helper()

	workspace := t.TempDir()
	projectDir := filepath.Join(workspace, "app")
	resDir := filepath.Join(projectDir, "src", "main", "res")
	if err := os.MkdirAll(resDir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := filepath.Join(projectDir, "src", "main", "AndroidManifest.xml")
	if err := os.WriteFile(manifest, []byte("<manifest package=\"com.example.app\"/>\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return workspace, projectDir, manifest, resDir
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
