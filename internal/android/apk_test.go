package android

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/gabriel-aplok/kine-build-java/internal/core"
	"github.com/gabriel-aplok/kine-build-java/internal/deps"
	"github.com/gabriel-aplok/kine-build-java/internal/toolchain"
)

func TestSigningKeyUnresolved(t *testing.T) {
	options := Options{
		Variant:        Variant{BuildType: "release"},
		ReleaseSigning: &SigningOptions{Unresolved: true},
	}
	key, err := signingKey(options)
	if err != nil {
		t.Fatalf("signingKey() error = %v", err)
	}
	if key != "unresolved" {
		t.Errorf("signingKey() = %q, want unresolved", key)
	}
}

func TestZipalignArgs(t *testing.T) {
	modern := zipalignArgs(&SDK{BuildTools: "/sdk/build-tools/37.0.0"}, "in.apk", "out.apk")
	wantModern := []string{"-f", "-P", "16", "4", "in.apk", "out.apk"}
	if !reflect.DeepEqual(modern, wantModern) {
		t.Errorf("modern args = %v, want %v", modern, wantModern)
	}
	old := zipalignArgs(&SDK{BuildTools: "/sdk/build-tools/34.0.0"}, "in.apk", "out.apk")
	wantOld := []string{"-f", "-p", "4", "in.apk", "out.apk"}
	if !reflect.DeepEqual(old, wantOld) {
		t.Errorf("old args = %v, want %v", old, wantOld)
	}
}

func TestAssembleAPK(t *testing.T) {
	dir := t.TempDir()
	resources := filepath.Join(dir, "resources.apk")
	writeZip(t, resources, map[string]string{
		"AndroidManifest.xml": "manifest",
		"resources.arsc":      "arsc",
	})
	dexDir := filepath.Join(dir, "dex")
	if err := os.MkdirAll(dexDir, 0o755); err != nil {
		t.Fatal(err)
	}
	dex := filepath.Join(dexDir, "classes.dex")
	if err := os.WriteFile(dex, []byte("dex-bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(dir, "out.apk")

	if err := assembleAPK(resources, []string{dexDir}, nil, nil, nil, output); err != nil {
		t.Fatalf("assembleAPK() error = %v", err)
	}

	reader, err := zip.OpenReader(output)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()

	entries := make(map[string]string)
	for _, entry := range reader.File {
		source, err := entry.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(source)
		source.Close()
		if err != nil {
			t.Fatal(err)
		}
		entries[entry.Name] = string(data)
	}
	want := map[string]string{
		"AndroidManifest.xml": "manifest",
		"resources.arsc":      "arsc",
		"classes.dex":         "dex-bytes",
	}
	if !reflect.DeepEqual(entries, want) {
		t.Errorf("entries = %v, want %v", entries, want)
	}
}

func TestAssembleAPKMissingResources(t *testing.T) {
	dir := t.TempDir()
	dexDir := filepath.Join(dir, "dex")
	if err := os.MkdirAll(dexDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dexDir, "classes.dex"), []byte("dex"), 0o600); err != nil {
		t.Fatal(err)
	}

	err := assembleAPK(filepath.Join(dir, "missing.apk"), []string{dexDir}, nil, nil, nil, filepath.Join(dir, "out.apk"))
	if err == nil {
		t.Fatal("assembleAPK() error = nil, want error")
	}
}

func TestNativeLibraries(t *testing.T) {
	dir := t.TempDir()
	jarPath := filepath.Join(dir, "gdx-platform-natives-arm64-v8a.jar")
	writeZip(t, jarPath, map[string]string{
		"libgdx.so":            "native",
		"META-INF/MANIFEST.MF": "manifest",
	})
	artifacts := []deps.Artifact{{
		Coordinate: deps.Coordinate{
			Group: "com.badlogicgames.gdx", Artifact: "gdx-platform", Version: "1.13.1",
			Classifier: "natives-arm64-v8a", Extension: "jar",
		},
		Path: jarPath,
	}}

	natives, err := nativeLibraries(artifacts)
	if err != nil {
		t.Fatalf("nativeLibraries() error = %v", err)
	}
	if len(natives) != 1 || natives[0].abi != "arm64-v8a" || natives[0].name != "libgdx.so" {
		t.Fatalf("natives = %+v", natives)
	}

	resources := filepath.Join(dir, "resources.apk")
	writeZip(t, resources, map[string]string{"resources.arsc": "arsc"})
	dexDir := filepath.Join(dir, "dex")
	if err := os.MkdirAll(dexDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dexDir, "classes.dex"), []byte("dex"), 0o600); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(dir, "out.apk")
	if err := assembleAPK(resources, []string{dexDir}, natives, nil, nil, output); err != nil {
		t.Fatalf("assembleAPK() error = %v", err)
	}

	entries := readAPKEntries(t, output)
	if entries["lib/arm64-v8a/libgdx.so"] != "native" {
		t.Errorf("native entry = %q, entries = %v", entries["lib/arm64-v8a/libgdx.so"], entries)
	}
}

func TestAssembleAPKAssetsAndNative(t *testing.T) {
	dir := t.TempDir()
	resources := filepath.Join(dir, "resources.apk")
	writeZip(t, resources, map[string]string{"resources.arsc": "arsc"})
	dexDir := filepath.Join(dir, "dex")
	if err := os.MkdirAll(dexDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dexDir, "classes.dex"), []byte("dex"), 0o600); err != nil {
		t.Fatal(err)
	}

	assetDir := filepath.Join(dir, "assets")
	if err := os.MkdirAll(filepath.Join(assetDir, "engine", "shaders"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(assetDir, "engine", "shaders", "basic.glsl"), []byte("shader"), 0o600); err != nil {
		t.Fatal(err)
	}
	assets, err := assetFiles([]string{assetDir})
	if err != nil {
		t.Fatalf("assetFiles() error = %v", err)
	}

	nativeDir := filepath.Join(dir, "native", "arm64-v8a")
	if err := os.MkdirAll(nativeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nativeDir, "libkolpa.so"), []byte("native"), 0o755); err != nil {
		t.Fatal(err)
	}
	natives, err := localNativeLibraries(filepath.Join(dir, "native"))
	if err != nil {
		t.Fatalf("localNativeLibraries() error = %v", err)
	}

	output := filepath.Join(dir, "out.apk")
	if err := assembleAPK(resources, []string{dexDir}, natives, nil, assets, output); err != nil {
		t.Fatalf("assembleAPK() error = %v", err)
	}

	entries := readAPKEntries(t, output)
	if entries["assets/engine/shaders/basic.glsl"] != "shader" {
		t.Errorf("asset entry = %q, entries = %v", entries["assets/engine/shaders/basic.glsl"], entries)
	}
	if entries["lib/arm64-v8a/libkolpa.so"] != "native" {
		t.Errorf("native entry = %q, entries = %v", entries["lib/arm64-v8a/libkolpa.so"], entries)
	}
}

func TestAPKTaskNativeAndAssets(t *testing.T) {
	workspace, projectDir, _, _ := resourceProject(t)
	nativeDir := filepath.Join(projectDir, "build", "android", "native", "arm64-v8a")
	if err := os.MkdirAll(nativeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nativeDir, "libkolpa.so"), []byte("native"), 0o755); err != nil {
		t.Fatal(err)
	}
	assetDir := filepath.Join(projectDir, "src", "main", "assets")
	if err := os.MkdirAll(assetDir, 0o755); err != nil {
		t.Fatal(err)
	}

	provider := &Provider{run: func(ctx context.Context, command toolchain.Command) (toolchain.Result, error) {
		return toolchain.Result{}, nil
	}}
	options := Options{
		VersionCode: 1,
		VersionName: "1.0",
		AssetDirs:   []string{assetDir},
		Native:      &NativeOptions{ABIs: []string{"arm64-v8a"}},
	}
	task, err := provider.apkTask(workspace, &core.Project{ID: "app", Dir: projectDir}, options, testSDK(), testJDK(), nil, false)
	if err != nil {
		t.Fatalf("apkTask() error = %v", err)
	}
	if !contains(task.Deps, "app:android-native") {
		t.Errorf("Deps = %v, want android-native", task.Deps)
	}
	inputPaths := make([]string, 0, len(task.Inputs))
	for _, input := range task.Inputs {
		inputPaths = append(inputPaths, input.Path)
	}
	for _, want := range []string{"app/build/android/native", "app/src/main/assets"} {
		if !contains(inputPaths, want) {
			t.Errorf("Inputs %v do not contain %q", inputPaths, want)
		}
	}
}

func TestDependencyResources(t *testing.T) {
	dir := t.TempDir()
	jarPath := filepath.Join(dir, "gdx.jar")
	writeZip(t, jarPath, map[string]string{
		"com/badlogic/gdx/utils/lsans-15.fnt": "font",
		"com/badlogic/gdx/utils/lsans-15.png": "png",
		"com/badlogic/gdx/Game.class":         "class",
		"META-INF/MANIFEST.MF":                "manifest",
		"META-INF/services/example.Service":   "service",
	})
	artifacts := []deps.Artifact{{
		Coordinate: deps.Coordinate{Group: "com.badlogicgames.gdx", Artifact: "gdx", Version: "1.12.1", Extension: "jar"},
		Path:       jarPath,
	}}

	resources, err := dependencyResources(artifacts)
	if err != nil {
		t.Fatalf("dependencyResources() error = %v", err)
	}
	names := make([]string, 0, len(resources))
	for _, resource := range resources {
		names = append(names, resource.name)
	}
	for _, want := range []string{"com/badlogic/gdx/utils/lsans-15.fnt", "com/badlogic/gdx/utils/lsans-15.png", "META-INF/services/example.Service"} {
		if !contains(names, want) {
			t.Errorf("resources %v do not contain %q", names, want)
		}
	}
	for _, unwanted := range []string{"com/badlogic/gdx/Game.class", "META-INF/MANIFEST.MF"} {
		if contains(names, unwanted) {
			t.Errorf("resources %v contain %q", names, unwanted)
		}
	}
}

func TestNativeLibrariesSkipsDesktop(t *testing.T) {
	artifacts := []deps.Artifact{{
		Coordinate: deps.Coordinate{
			Group: "com.badlogicgames.gdx", Artifact: "gdx-platform", Version: "1.13.1",
			Classifier: "natives-desktop", Extension: "jar",
		},
		Path: "/tmp/desktop-natives.jar",
	}}
	natives, err := nativeLibraries(artifacts)
	if err != nil {
		t.Fatalf("nativeLibraries() error = %v", err)
	}
	if len(natives) != 0 {
		t.Errorf("natives = %+v, want none", natives)
	}
}

func readAPKEntries(t *testing.T, path string) map[string]string {
	t.Helper()

	reader, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()

	entries := make(map[string]string)
	for _, file := range reader.File {
		source, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(source)
		source.Close()
		if err != nil {
			t.Fatal(err)
		}
		entries[file.Name] = string(data)
	}
	return entries
}

func TestAPKTask(t *testing.T) {
	workspace, projectDir, _, _ := resourceProject(t)
	prepareAPKInputs(t, projectDir)
	keystore := filepath.Join(workspace, ".kine", "android", "debug.keystore")
	if err := os.MkdirAll(filepath.Dir(keystore), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keystore, []byte("ks"), 0o600); err != nil {
		t.Fatal(err)
	}

	var commands []toolchain.Command
	provider := &Provider{run: func(ctx context.Context, command toolchain.Command) (toolchain.Result, error) {
		commands = append(commands, command)
		return toolchain.Result{}, nil
	}}
	sdk := &SDK{Zipalign: "/sdk/zipalign", ApkSigner: "/sdk/apksigner", BuildTools: "36.0.0", Platform: "/sdk/platforms/android-36"}
	jdk := &toolchain.JDK{Home: "/jdk", Version: "21.0.1", Major: 21}

	task, err := provider.apkTask(workspace, &core.Project{ID: "app", Dir: projectDir}, Options{VersionCode: 1, VersionName: "1.0"}, sdk, jdk, nil, false)
	if err != nil {
		t.Fatalf("apkTask() error = %v", err)
	}
	if task.ID != "apk" {
		t.Errorf("ID = %q", task.ID)
	}
	if len(task.Deps) != 2 || task.Deps[0] != "app:android-resources" || task.Deps[1] != "app:dex" {
		t.Errorf("Deps = %v", task.Deps)
	}
	if len(task.Outputs) != 1 || task.Outputs[0].Path != "app/build/outputs/apk/debug/app-debug.apk" {
		t.Errorf("Outputs = %+v", task.Outputs)
	}
	if !strings.Contains(task.Key, "apk 36.0.0") || !strings.Contains(task.Key, "code=1") {
		t.Errorf("Key = %q", task.Key)
	}

	if err := task.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(commands) != 2 {
		t.Fatalf("commands = %d, want 2", len(commands))
	}

	outputDir := filepath.Join(projectDir, "build", "outputs", "apk", "debug")
	unsigned := filepath.Join(outputDir, "app-debug-unsigned.apk")
	aligned := filepath.Join(outputDir, "app-debug-aligned.apk")
	final := filepath.Join(outputDir, "app-debug.apk")

	wantAlign := []string{"-f", "-P", "16", "4", unsigned, aligned}
	if commands[0].Path != sdk.Zipalign || !reflect.DeepEqual(commands[0].Args, wantAlign) {
		t.Errorf("zipalign = %q %v", commands[0].Path, commands[0].Args)
	}

	wantSign := []string{
		"sign",
		"--ks", keystore,
		"--ks-key-alias", debugAlias,
		"--ks-pass", "pass:android",
		"--key-pass", "pass:android",
		"--v4-signing-enabled", "false",
		"--out", final,
		aligned,
	}
	if commands[1].Path != sdk.ApkSigner || !reflect.DeepEqual(commands[1].Args, wantSign) {
		t.Errorf("apksigner = %q %v", commands[1].Path, commands[1].Args)
	}
}

func TestAPKTaskCustomSigning(t *testing.T) {
	workspace, projectDir, _, _ := resourceProject(t)
	prepareAPKInputs(t, projectDir)
	keystore := filepath.Join(t.TempDir(), "debug.keystore")
	if err := os.WriteFile(keystore, []byte("custom-keystore"), 0o600); err != nil {
		t.Fatal(err)
	}

	var commands []toolchain.Command
	provider := &Provider{run: func(ctx context.Context, command toolchain.Command) (toolchain.Result, error) {
		commands = append(commands, command)
		return toolchain.Result{}, nil
	}}
	options := Options{
		VersionCode: 1,
		VersionName: "1.0",
		Signing: &SigningOptions{
			Keystore:      keystore,
			Alias:         "customalias",
			StorePassword: "storepass",
			KeyPassword:   "keypass",
		},
	}
	task, err := provider.apkTask(workspace, &core.Project{ID: "app", Dir: projectDir}, options, testSDK(), testJDK(), nil, false)
	if err != nil {
		t.Fatalf("apkTask() error = %v", err)
	}
	if !strings.Contains(task.Key, "signing=custom:") {
		t.Errorf("Key = %q, want a custom signing fingerprint", task.Key)
	}

	if err := task.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(commands) != 2 {
		t.Fatalf("commands = %d, want 2", len(commands))
	}
	sign := commands[1]
	for _, want := range []string{"--ks", keystore, "--ks-key-alias", "customalias", "--ks-pass", "pass:storepass", "--key-pass", "pass:keypass"} {
		if !contains(sign.Args, want) {
			t.Errorf("sign args %v do not contain %q", sign.Args, want)
		}
	}
}

func TestAPKTaskError(t *testing.T) {
	workspace, projectDir, _, _ := resourceProject(t)
	prepareAPKInputs(t, projectDir)

	provider := &Provider{run: func(ctx context.Context, command toolchain.Command) (toolchain.Result, error) {
		return toolchain.Result{Stderr: []byte("bad apk")}, &toolchain.ExitError{Code: 1}
	}}
	sdk := &SDK{Zipalign: "/sdk/zipalign", ApkSigner: "/sdk/apksigner"}
	jdk := &toolchain.JDK{Home: "/jdk", Version: "21.0.1", Major: 21}

	task, err := provider.apkTask(workspace, &core.Project{ID: "app", Dir: projectDir}, Options{VersionCode: 1, VersionName: "1.0"}, sdk, jdk, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	err = task.Run(context.Background())
	if err == nil {
		t.Fatal("Run() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "zipalign") || !strings.Contains(err.Error(), "bad apk") {
		t.Errorf("error %q does not carry the zipalign output", err)
	}
}

func TestEnsureKeystore(t *testing.T) {
	workspace := t.TempDir()
	jdk := &toolchain.JDK{Home: t.TempDir(), Version: "21.0.1", Major: 21}
	bin := filepath.Join(jdk.Home, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	keytool := filepath.Join(bin, "keytool")
	if err := os.WriteFile(keytool, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	var commands []toolchain.Command
	provider := &Provider{run: func(ctx context.Context, command toolchain.Command) (toolchain.Result, error) {
		commands = append(commands, command)
		if command.Path == keytool {
			if err := os.WriteFile(filepath.Join(workspace, ".kine", "android", "debug.keystore"), []byte("ks"), 0o600); err != nil {
				return toolchain.Result{}, err
			}
		}
		return toolchain.Result{}, nil
	}}

	path, err := provider.ensureKeystore(context.Background(), workspace, jdk, t.TempDir(), nil)
	if err != nil {
		t.Fatalf("ensureKeystore() error = %v", err)
	}
	if path != filepath.Join(workspace, ".kine", "android", "debug.keystore") {
		t.Errorf("path = %q", path)
	}
	if len(commands) != 1 || commands[0].Path != keytool {
		t.Fatalf("commands = %+v", commands)
	}
	for _, want := range []string{"-genkeypair", "-keystore", path, "-alias", debugAlias, "-storepass", debugPass} {
		if !contains(commands[0].Args, want) {
			t.Errorf("args %v do not contain %q", commands[0].Args, want)
		}
	}
}

func prepareAPKInputs(t *testing.T, projectDir string) {
	t.Helper()

	resources := filepath.Join(projectDir, "build", "android", "resources.apk")
	writeZip(t, resources, map[string]string{"resources.arsc": "arsc"})
	dex := filepath.Join(projectDir, "build", "android", "dex", "classes.dex")
	if err := os.MkdirAll(filepath.Dir(dex), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dex, []byte("dex"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func writeZip(t *testing.T, path string, entries map[string]string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	buffer := &bytes.Buffer{}
	writer := zip.NewWriter(buffer)
	for name, content := range entries {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buffer.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
}
