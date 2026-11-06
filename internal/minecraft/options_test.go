package minecraft

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDecodeOptions(t *testing.T) {
	workspace, projectDir := mcProject(t)

	options, err := decodeOptions(baseValues(), "app", projectDir)
	if err != nil {
		t.Fatalf("decodeOptions() error = %v", err)
	}
	if options.Loader != fabricLoader {
		t.Errorf("Loader = %q", options.Loader)
	}
	if options.Minecraft != "1.20.1" {
		t.Errorf("Minecraft = %q", options.Minecraft)
	}
	if options.ModID != "example-mod" || options.ModVersion != "1.2.3" {
		t.Errorf("mod = %q/%q", options.ModID, options.ModVersion)
	}
	if options.MinecraftJar != filepath.Join(workspace, "game", "minecraft.jar") {
		t.Errorf("MinecraftJar = %q", options.MinecraftJar)
	}
	if len(options.Classpath) != 1 || options.Classpath[0] != filepath.Join(workspace, "game", "fabric-loader.jar") {
		t.Errorf("Classpath = %v", options.Classpath)
	}
	if len(options.JavaDirs) != 1 || options.JavaDirs[0] != filepath.Join(projectDir, "src", "main", "java") {
		t.Errorf("JavaDirs = %v", options.JavaDirs)
	}
	if len(options.ResourceDirs) != 1 || options.ResourceDirs[0] != filepath.Join(projectDir, "src", "main", "resources") {
		t.Errorf("ResourceDirs = %v", options.ResourceDirs)
	}
	if options.MetadataPath != filepath.Join(projectDir, "src", "main", "resources", metadataName) {
		t.Errorf("MetadataPath = %q", options.MetadataPath)
	}
	if options.Metadata.ID != "example-mod" {
		t.Errorf("Metadata = %+v", options.Metadata)
	}
	if options.MinecraftHash == "" || options.ClasspathHash == "" {
		t.Errorf("hashes are empty: %q %q", options.MinecraftHash, options.ClasspathHash)
	}
}

func TestDecodeOptionsExplicitModValues(t *testing.T) {
	_, projectDir := mcProject(t)
	values := baseValues()
	values["mod_id"] = "example-mod"
	values["mod_version"] = "1.2.3"

	if _, err := decodeOptions(values, "app", projectDir); err != nil {
		t.Fatalf("decodeOptions() error = %v", err)
	}
}

func TestDecodeOptionsModMismatch(t *testing.T) {
	_, projectDir := mcProject(t)

	values := baseValues()
	values["mod_id"] = "other"
	if _, err := decodeOptions(values, "app", projectDir); err == nil {
		t.Error("decodeOptions() error = nil for a mod_id mismatch")
	}

	values = baseValues()
	values["mod_version"] = "9.9.9"
	if _, err := decodeOptions(values, "app", projectDir); err == nil {
		t.Error("decodeOptions() error = nil for a mod_version mismatch")
	}
}

func TestDecodeOptionsMissingVersion(t *testing.T) {
	_, projectDir := mcProject(t)
	values := baseValues()
	delete(values, "version")

	_, err := decodeOptions(values, "app", projectDir)
	if err == nil {
		t.Fatal("decodeOptions() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "version") {
		t.Errorf("error %q does not mention the version", err)
	}
}

func TestDecodeOptionsBadLoader(t *testing.T) {
	_, projectDir := mcProject(t)
	values := baseValues()
	values["loader"] = "forge"

	_, err := decodeOptions(values, "app", projectDir)
	if err == nil {
		t.Fatal("decodeOptions() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "loader") {
		t.Errorf("error %q does not mention the loader", err)
	}
}

func TestDecodeOptionsMissingMinecraftJar(t *testing.T) {
	_, projectDir := mcProject(t)
	values := baseValues()
	values["minecraft_jar"] = "../game/missing.jar"

	_, err := decodeOptions(values, "app", projectDir)
	if err == nil {
		t.Fatal("decodeOptions() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "minecraft_jar") {
		t.Errorf("error %q does not mention the jar", err)
	}
}

func TestDecodeOptionsMissingMetadata(t *testing.T) {
	_, projectDir := mcProject(t)
	if err := os.Remove(filepath.Join(projectDir, "src", "main", "resources", metadataName)); err != nil {
		t.Fatal(err)
	}

	_, err := decodeOptions(baseValues(), "app", projectDir)
	if err == nil {
		t.Fatal("decodeOptions() error = nil, want error")
	}
	if !strings.Contains(err.Error(), metadataName) {
		t.Errorf("error %q does not name the metadata", err)
	}
}

func TestDecodeOptionsBadMetadata(t *testing.T) {
	_, projectDir := mcProject(t)
	path := filepath.Join(projectDir, "src", "main", "resources", metadataName)
	if err := os.WriteFile(path, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := decodeOptions(baseValues(), "app", projectDir); err == nil {
		t.Fatal("decodeOptions() error = nil, want error")
	}
}

func TestDecodeOptionsExplicitMetadata(t *testing.T) {
	_, projectDir := mcProject(t)
	path := filepath.Join(projectDir, "conf", "mod.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	metadata := `{"schemaVersion":1,"id":"custom-mod","version":"2.0.0"}`
	if err := os.WriteFile(path, []byte(metadata), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(projectDir, "src", "main", "resources", metadataName)); err != nil {
		t.Fatal(err)
	}

	values := baseValues()
	values["metadata"] = "conf/mod.json"
	options, err := decodeOptions(values, "app", projectDir)
	if err != nil {
		t.Fatalf("decodeOptions() error = %v", err)
	}
	if options.ModID != "custom-mod" || options.ModVersion != "2.0.0" {
		t.Errorf("mod = %q/%q", options.ModID, options.ModVersion)
	}
}

func TestDecodeOptionsRemapPair(t *testing.T) {
	workspace, projectDir := mcProject(t)
	writeLocalFile(t, filepath.Join(workspace, "game", "mappings.tiny"), "tiny\t2\t0\ta\tb\tc\n")
	writeLocalFile(t, filepath.Join(workspace, "game", "remapper.jar"), "remapper")

	values := baseValues()
	values["mappings"] = "../game/mappings.tiny"
	if _, err := decodeOptions(values, "app", projectDir); err == nil {
		t.Error("decodeOptions() error = nil for mappings without a remapper")
	}

	values["remapper"] = "../game/remapper.jar"
	options, err := decodeOptions(values, "app", projectDir)
	if err != nil {
		t.Fatalf("decodeOptions() error = %v", err)
	}
	if options.Mappings == "" || options.Remapper == "" {
		t.Errorf("remap options = %q/%q", options.Mappings, options.Remapper)
	}
}

func TestDecodeOptionsSourceEscape(t *testing.T) {
	_, projectDir := mcProject(t)
	values := baseValues()
	values["sources"] = map[string]any{"java": []any{"../java"}}

	_, err := decodeOptions(values, "app", projectDir)
	if err == nil {
		t.Fatal("decodeOptions() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "escapes") {
		t.Errorf("error %q does not mention the escape", err)
	}
}

func TestDecodeOptionsUnknownKey(t *testing.T) {
	_, projectDir := mcProject(t)
	values := baseValues()
	values["extra"] = true

	_, err := decodeOptions(values, "app", projectDir)
	if err == nil {
		t.Fatal("decodeOptions() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "strict mode") {
		t.Errorf("error %q does not report the unknown key", err)
	}
}

func TestDecodeOptionsMissingClasspath(t *testing.T) {
	_, projectDir := mcProject(t)
	values := baseValues()
	values["classpath"] = []any{"../game/missing.jar"}

	_, err := decodeOptions(values, "app", projectDir)
	if err == nil {
		t.Fatal("decodeOptions() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "classpath") {
		t.Errorf("error %q does not mention the classpath", err)
	}
}

func baseValues() map[string]any {
	return map[string]any{
		"version":       "1.20.1",
		"minecraft_jar": "../game/minecraft.jar",
		"classpath":     []any{"../game/fabric-loader.jar"},
	}
}

func mcProject(t *testing.T) (string, string) {
	t.Helper()

	workspace := t.TempDir()
	projectDir := filepath.Join(workspace, "app")
	javaDir := filepath.Join(projectDir, "src", "main", "java", "com", "example")
	if err := os.MkdirAll(javaDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(javaDir, "ExampleMod.java"), []byte("package com.example; class ExampleMod {}"), 0o600); err != nil {
		t.Fatal(err)
	}
	resources := filepath.Join(projectDir, "src", "main", "resources")
	if err := os.MkdirAll(resources, 0o755); err != nil {
		t.Fatal(err)
	}
	metadata := `{"schemaVersion":1,"id":"example-mod","version":"1.2.3","entrypoints":{"main":["com.example.ExampleMod"]},"depends":{"minecraft":"1.20.1"}}`
	if err := os.WriteFile(filepath.Join(resources, metadataName), []byte(metadata), 0o600); err != nil {
		t.Fatal(err)
	}
	writeLocalFile(t, filepath.Join(workspace, "game", "minecraft.jar"), "minecraft")
	writeLocalFile(t, filepath.Join(workspace, "game", "fabric-loader.jar"), "loader")
	return workspace, projectDir
}

func writeLocalFile(t *testing.T, path, content string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
