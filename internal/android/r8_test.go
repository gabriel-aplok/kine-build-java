package android

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gabriel-aplok/kine-build-java/internal/core"
	"github.com/gabriel-aplok/kine-build-java/internal/toolchain"
)

func TestRunR8(t *testing.T) {
	workspace := t.TempDir()
	projectDir := filepath.Join(workspace, "app")
	buildDir := filepath.Join(projectDir, "build", "android")
	if err := os.MkdirAll(buildDir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := `<manifest xmlns:android="http://schemas.android.com/apk/res/android" package="com.example.app">
    <application android:label="App">
        <activity android:name=".MainActivity"/>
    </application>
</manifest>
`
	if err := os.WriteFile(filepath.Join(buildDir, "AndroidManifest.xml"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	classesJar := filepath.Join(buildDir, "classes.jar")
	if err := os.WriteFile(classesJar, []byte("classes"), 0o600); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(buildDir, "dex")
	if err := os.MkdirAll(output, 0o755); err != nil {
		t.Fatal(err)
	}

	options := Options{
		MinSDK: 24,
		Variant: Variant{
			BuildType:        "release",
			Name:             "release",
			BuildTypeOptions: BuildTypeOptions{Minify: true},
		},
	}
	sdk := &SDK{
		BuildTools: "37.0.0",
		AndroidJar: "/sdk/android.jar",
		R8Jar:      "/sdk/build-tools/37.0.0/lib/d8.jar",
	}
	jdk := &toolchain.JDK{Home: "/jdk", Version: "21.0.1", Major: 21, Java: "/jdk/bin/java"}

	var commands []toolchain.Command
	provider := &Provider{run: func(ctx context.Context, command toolchain.Command) (toolchain.Result, error) {
		commands = append(commands, command)
		return toolchain.Result{}, nil
	}}
	if err := provider.runR8(context.Background(), &core.Project{ID: "app", Dir: projectDir}, options, sdk, jdk, nil, classesJar, output); err != nil {
		t.Fatalf("runR8() error = %v", err)
	}
	if len(commands) != 1 {
		t.Fatalf("commands = %d, want 1", len(commands))
	}
	command := commands[0]
	if command.Path != jdk.Java {
		t.Errorf("Path = %q", command.Path)
	}
	for _, want := range []string{
		"-cp", sdk.R8Jar,
		"com.android.tools.r8.R8",
		"--release",
		"--min-api", "24",
		"--lib", sdk.AndroidJar,
		"--output", output,
		"--pg-map-output", filepath.Join(projectDir, "build", "outputs", "mapping", "release", "mapping.txt"),
		classesJar,
	} {
		if !contains(command.Args, want) {
			t.Errorf("args %v do not contain %q", command.Args, want)
		}
	}

	rules := filepath.Join(buildDir, "r8", "rules.pro")
	data, err := os.ReadFile(rules)
	if err != nil {
		t.Fatalf("rules: %v", err)
	}
	for _, want := range []string{
		"native <methods>",
		"-keep class com.example.app.MainActivity",
	} {
		if !strings.Contains(string(data), want) {
			t.Errorf("rules do not contain %q:\n%s", want, data)
		}
	}
}

func TestRunR8ShrinkResources(t *testing.T) {
	workspace := t.TempDir()
	projectDir := filepath.Join(workspace, "app")
	buildDir := filepath.Join(projectDir, "build", "android")
	if err := os.MkdirAll(buildDir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := `<manifest xmlns:android="http://schemas.android.com/apk/res/android" package="com.example.app"><application/></manifest>`
	if err := os.WriteFile(filepath.Join(buildDir, "AndroidManifest.xml"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(buildDir, "resources-proto.apk"), []byte("apk"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(buildDir, "aapt-rules.pro"), []byte("-keep class R\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	classesJar := filepath.Join(buildDir, "classes.jar")
	if err := os.WriteFile(classesJar, []byte("classes"), 0o600); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(buildDir, "dex")
	if err := os.MkdirAll(output, 0o755); err != nil {
		t.Fatal(err)
	}

	options := Options{
		MinSDK: 24,
		Variant: Variant{
			BuildType: "release",
			Name:      "release",
			BuildTypeOptions: BuildTypeOptions{
				Minify:          true,
				ShrinkResources: true,
			},
		},
	}
	sdk := &SDK{BuildTools: "37.0.0", AndroidJar: "/sdk/android.jar", R8Jar: "/sdk/r8.jar", Aapt2: "/sdk/aapt2"}
	var commands []toolchain.Command
	provider := &Provider{run: func(ctx context.Context, command toolchain.Command) (toolchain.Result, error) {
		commands = append(commands, command)
		return toolchain.Result{}, nil
	}}
	if err := provider.runR8(context.Background(), &core.Project{ID: "app", Dir: projectDir}, options, sdk, testJDK(), nil, classesJar, output); err != nil {
		t.Fatalf("runR8() error = %v", err)
	}
	if len(commands) != 1 {
		t.Fatalf("commands = %d, want 1", len(commands))
	}
	for _, want := range []string{"--pg-conf", filepath.Join(buildDir, "r8", "rules.pro")} {
		if !contains(commands[0].Args, want) {
			t.Errorf("r8 args %v do not contain %q", commands[0].Args, want)
		}
	}
	if contains(commands[0].Args, "--android-resources") {
		t.Errorf("r8 args still use --android-resources: %v", commands[0].Args)
	}
}
