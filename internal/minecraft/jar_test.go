package minecraft

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/gabriel-aplok/kine-build-java/internal/core"
)

func TestJarTask(t *testing.T) {
	workspace, projectDir := mcProject(t)
	writeClassFile(t, projectDir)
	resources := filepath.Join(projectDir, "src", "main", "resources")
	writeLocalFile(t, filepath.Join(resources, "assets", "example", "icon.png"), "png")

	options := Options{
		ModID:        "example-mod",
		ModVersion:   "1.2.3",
		ResourceDirs: []string{resources},
		MetadataPath: filepath.Join(resources, metadataName),
	}
	provider := &Provider{}

	task, err := provider.jarTask(workspace, &core.Project{ID: "app", Dir: projectDir}, options)
	if err != nil {
		t.Fatalf("jarTask() error = %v", err)
	}
	if task.ID != "mod-jar" {
		t.Errorf("ID = %q", task.ID)
	}
	if len(task.Deps) != 1 || task.Deps[0] != "app:compile-java" {
		t.Errorf("Deps = %v", task.Deps)
	}
	if len(task.Outputs) != 1 || task.Outputs[0].Path != "app/build/libs/example-mod-1.2.3.jar" {
		t.Errorf("Outputs = %+v", task.Outputs)
	}
	if task.Key != "mod-jar example-mod 1.2.3" {
		t.Errorf("Key = %q", task.Key)
	}

	if err := task.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	path := filepath.Join(projectDir, "build", "libs", "example-mod-1.2.3.jar")
	entries := readJar(t, path)
	for _, name := range []string{
		"META-INF/MANIFEST.MF",
		"com/example/ExampleMod.class",
		"fabric.mod.json",
		"assets/example/icon.png",
	} {
		if _, ok := entries[name]; !ok {
			t.Errorf("jar does not contain %s: %v", name, keys(entries))
		}
	}
}

func TestBuildJarDeterministic(t *testing.T) {
	_, projectDir := mcProject(t)
	writeClassFile(t, projectDir)
	resources := filepath.Join(projectDir, "src", "main", "resources")
	options := Options{
		ModID:        "example-mod",
		ModVersion:   "1.2.3",
		ResourceDirs: []string{resources},
		MetadataPath: filepath.Join(resources, metadataName),
	}
	provider := &Provider{}
	project := &core.Project{ID: "app", Dir: projectDir}

	if err := provider.buildJar(project, options); err != nil {
		t.Fatalf("buildJar() error = %v", err)
	}
	path := filepath.Join(projectDir, "build", "libs", "example-mod-1.2.3.jar")
	first, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := provider.buildJar(project, options); err != nil {
		t.Fatalf("second buildJar() error = %v", err)
	}
	second, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Error("jars are not byte-identical")
	}
}

func TestBuildJarExplicitMetadata(t *testing.T) {
	_, projectDir := mcProject(t)
	writeClassFile(t, projectDir)
	resources := filepath.Join(projectDir, "src", "main", "resources")
	if err := os.Remove(filepath.Join(resources, metadataName)); err != nil {
		t.Fatal(err)
	}
	metadata := filepath.Join(projectDir, "conf", metadataName)
	writeLocalFile(t, metadata, `{"schemaVersion":1,"id":"example-mod","version":"1.2.3"}`)

	options := Options{
		ModID:        "example-mod",
		ModVersion:   "1.2.3",
		ResourceDirs: []string{resources},
		MetadataPath: metadata,
	}
	provider := &Provider{}
	project := &core.Project{ID: "app", Dir: projectDir}
	if err := provider.buildJar(project, options); err != nil {
		t.Fatalf("buildJar() error = %v", err)
	}

	path := filepath.Join(projectDir, "build", "libs", "example-mod-1.2.3.jar")
	entries := readJar(t, path)
	if _, ok := entries[metadataName]; !ok {
		t.Errorf("jar does not contain the explicit metadata: %v", keys(entries))
	}
}

func readJar(t *testing.T, path string) map[string]string {
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

func keys(values map[string]string) []string {
	result := make([]string, 0, len(values))
	for key := range values {
		result = append(result, key)
	}
	return result
}
