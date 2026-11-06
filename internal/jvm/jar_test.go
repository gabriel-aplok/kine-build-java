package jvm

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/gabriel-aplok/kine-build-java/internal/core"
	"github.com/gabriel-aplok/kine-build-java/internal/toolchain"
)

func TestJarDeterministic(t *testing.T) {
	root := t.TempDir()
	classes := filepath.Join(root, "classes")
	resources := filepath.Join(root, "resources")
	writeFile(t, filepath.Join(classes, "demo", "App.class"), "class bytes")
	writeFile(t, filepath.Join(resources, "a.txt"), "a")

	first := filepath.Join(t.TempDir(), "a.jar")
	second := filepath.Join(t.TempDir(), "b.jar")
	if err := writeJar(first, []string{classes, resources}, "demo.App"); err != nil {
		t.Fatalf("writeJar(first) error = %v", err)
	}
	if err := writeJar(second, []string{classes, resources}, "demo.App"); err != nil {
		t.Fatalf("writeJar(second) error = %v", err)
	}

	firstData, err := os.ReadFile(first)
	if err != nil {
		t.Fatal(err)
	}
	secondData, err := os.ReadFile(second)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(firstData, secondData) {
		t.Error("jars are not byte-identical")
	}
}

func TestJarManifestAndEntries(t *testing.T) {
	root := t.TempDir()
	classes := filepath.Join(root, "classes")
	resources := filepath.Join(root, "resources")
	writeFile(t, filepath.Join(classes, "demo", "App.class"), "class bytes")
	writeFile(t, filepath.Join(resources, "a.txt"), "a")

	path := filepath.Join(t.TempDir(), "app.jar")
	if err := writeJar(path, []string{classes, resources}, "demo.App"); err != nil {
		t.Fatalf("writeJar() error = %v", err)
	}

	reader, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()

	var names []string
	for _, file := range reader.File {
		names = append(names, file.Name)
	}
	sort.Strings(names)
	want := []string{"META-INF/MANIFEST.MF", "a.txt", "demo/App.class"}
	if !reflect.DeepEqual(names, want) {
		t.Errorf("entries = %v, want %v", names, want)
	}

	var manifestContent string
	var classContent string
	for _, file := range reader.File {
		switch file.Name {
		case "META-INF/MANIFEST.MF":
			manifestContent = readZipFile(t, file)
		case "demo/App.class":
			classContent = readZipFile(t, file)
		}
	}
	if !strings.Contains(manifestContent, "Main-Class: demo.App") {
		t.Errorf("manifest = %q", manifestContent)
	}
	if classContent != "class bytes" {
		t.Errorf("class = %q", classContent)
	}
}

func TestTasksIncludeJar(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "src/main/java", "App.java"), "class App {}")
	writeFile(t, filepath.Join(root, "src/main/resources", "a.txt"), "a")

	provider := newTestProvider(t, root)
	project := &core.Project{ID: "app", Dir: root, Provider: "jvm"}
	tasks, err := provider.Tasks(context.Background(), project)
	if err != nil {
		t.Fatalf("Tasks() error = %v", err)
	}

	jar := findTask(tasks, "jar")
	if jar == nil {
		t.Fatalf("jar task missing: %v", taskIDs(tasks))
	}
	if !contains(jar.Deps, "app:compile-java") || !contains(jar.Deps, "app:process-resources") {
		t.Errorf("Deps = %v, want compile and resources", jar.Deps)
	}
	if len(jar.Outputs) != 1 || jar.Outputs[0].Path != "build/libs/app.jar" {
		t.Errorf("Outputs = %+v", jar.Outputs)
	}
}

func TestJarTaskRuns(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "build", "classes", "java", "main", "demo", "App.class"), "class")
	writeFile(t, filepath.Join(root, "build", "resources", "main", "a.txt"), "a")

	provider := newTestProvider(t, root)
	build := &build{
		project: &core.Project{ID: "app", Dir: root, Provider: "jvm"},
		options: mustOptions(t, nil),
		sources: sourceDirs{
			Java:      []string{filepath.Join(root, "src/main/java")},
			Resources: []string{filepath.Join(root, "src/main/resources")},
		},
		jdk: &toolchain.JDK{Home: "/fake/jdk", Version: "21.0.1", Major: 21},
	}

	if err := provider.createJar(build); err != nil {
		t.Fatalf("createJar() error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "build", "libs", "app.jar")); err != nil {
		t.Fatalf("jar: %v", err)
	}
}

func readZipFile(t *testing.T, file *zip.File) string {
	t.Helper()

	reader, err := file.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()

	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
