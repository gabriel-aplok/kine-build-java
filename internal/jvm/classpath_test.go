package jvm

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gabriel-aplok/kine-build-java/internal/core"
	"github.com/gabriel-aplok/kine-build-java/internal/deps"
	"github.com/gabriel-aplok/kine-build-java/internal/store"
)

func TestComputeClasspaths(t *testing.T) {
	workspaceRoot := t.TempDir()
	libDir := filepath.Join(workspaceRoot, "lib")
	appDir := filepath.Join(workspaceRoot, "app")
	lib := &core.Project{ID: "lib", Dir: libDir}
	app := &core.Project{ID: "app", Dir: appDir, DependsOn: []string{"lib"}}

	provider := newTestProvider(t, workspaceRoot)
	provider.workspace.Projects["lib"] = lib
	provider.workspace.Projects["app"] = app

	artifacts := []deps.Artifact{
		{Coordinate: deps.Coordinate{Group: "g", Artifact: "compile", Version: "1"}, Scope: deps.ScopeCompile, Path: "/deps/compile.jar"},
		{Coordinate: deps.Coordinate{Group: "g", Artifact: "provided", Version: "1"}, Scope: deps.ScopeProvided, Path: "/deps/provided.jar"},
		{Coordinate: deps.Coordinate{Group: "g", Artifact: "runtime", Version: "1"}, Scope: deps.ScopeRuntime, Path: "/deps/runtime.jar"},
		{Coordinate: deps.Coordinate{Group: "g", Artifact: "test", Version: "1"}, Scope: deps.ScopeTest, Path: "/deps/test.jar"},
		{Coordinate: deps.Coordinate{Group: "org.lwjgl", Artifact: "lwjgl", Version: "3.3.3", Classifier: "natives-linux"}, Scope: deps.ScopeCompile, Path: "/deps/natives.jar"},
	}

	paths, err := provider.computeClasspaths(app, artifacts, false, false)
	if err != nil {
		t.Fatalf("computeClasspaths() error = %v", err)
	}

	libJava := filepath.Join(libDir, "build", "classes", "java", "main")
	libKotlin := filepath.Join(libDir, "build", "classes", "kotlin", "main")
	libResources := filepath.Join(libDir, "build", "resources", "main")
	assertContains(t, paths.mainCompile, libJava)
	assertContains(t, paths.mainCompile, libKotlin)
	assertContains(t, paths.mainCompile, libResources)
	assertContains(t, paths.mainCompile, "/deps/compile.jar")
	assertContains(t, paths.mainCompile, "/deps/provided.jar")
	assertNotContains(t, paths.mainCompile, "/deps/runtime.jar")
	assertNotContains(t, paths.mainCompile, "/deps/test.jar")
	assertNotContains(t, paths.mainCompile, "/deps/natives.jar")

	assertContains(t, paths.mainRuntime, "/deps/compile.jar")
	assertContains(t, paths.mainRuntime, "/deps/runtime.jar")
	assertContains(t, paths.mainRuntime, "/deps/natives.jar")
	assertNotContains(t, paths.mainRuntime, "/deps/provided.jar")
	assertContains(t, paths.mainRuntime, filepath.Join(appDir, "build", "classes", "java", "main"))
	assertContains(t, paths.mainRuntime, filepath.Join(appDir, "build", "resources", "main"))

	assertContains(t, paths.testCompile, "/deps/test.jar")
	assertNotContains(t, paths.testCompile, "/deps/natives.jar")
	assertContains(t, paths.testRuntime, "/deps/test.jar")
	assertContains(t, paths.testRuntime, "/deps/natives.jar")
	assertContains(t, paths.testRuntime, filepath.Join(appDir, "build", "classes", "java", "test"))
	assertContains(t, paths.testRuntime, filepath.Join(appDir, "build", "resources", "test"))
}

func TestComputeClasspathsKotlin(t *testing.T) {
	workspaceRoot := t.TempDir()
	appDir := filepath.Join(workspaceRoot, "app")
	app := &core.Project{ID: "app", Dir: appDir}
	provider := newTestProvider(t, workspaceRoot)
	provider.workspace.Projects["app"] = app

	paths, err := provider.computeClasspaths(app, nil, true, false)
	if err != nil {
		t.Fatalf("computeClasspaths() error = %v", err)
	}
	assertContains(t, paths.mainCompile, filepath.Join(appDir, "build", "classes", "kotlin", "main"))
}

func TestComputeClasspathsMissingProject(t *testing.T) {
	provider := newTestProvider(t, t.TempDir())
	app := &core.Project{ID: "app", Dir: t.TempDir(), DependsOn: []string{"missing"}}

	if _, err := provider.computeClasspaths(app, nil, false, false); err == nil {
		t.Fatal("computeClasspaths() error = nil, want error")
	}
}

func TestResolveDependenciesSkipped(t *testing.T) {
	root := t.TempDir()
	project := &core.Project{ID: "app", Dir: root}
	provider := newTestProvider(t, root)

	artifacts, err := provider.resolveDependencies(context.Background(), project, mustOptions(t, nil), sourceDirs{})
	if err != nil {
		t.Fatalf("resolveDependencies() error = %v", err)
	}
	if len(artifacts) != 0 {
		t.Errorf("artifacts = %d, want 0", len(artifacts))
	}
}

func TestResolveDependenciesNoCache(t *testing.T) {
	root := t.TempDir()
	project := &core.Project{ID: "app", Dir: root}
	options := mustOptions(t, optionsFromTOML(t, `
[dependencies]
compile = ["com.example:lib:1.0"]
`))
	provider := newTestProvider(t, root)

	_, err := provider.resolveDependencies(context.Background(), project, options, sourceDirs{})
	if err == nil {
		t.Fatal("resolveDependencies() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "store") {
		t.Errorf("error %q does not mention the store", err)
	}
}

func TestPrepareResolvesKotlinStdlib(t *testing.T) {
	workspaceRoot := t.TempDir()
	projectDir := filepath.Join(workspaceRoot, "app")
	if err := os.MkdirAll(filepath.Join(projectDir, "src/main/kotlin"), 0o755); err != nil {
		t.Fatal(err)
	}

	repoRoot := t.TempDir()
	writeFileRepo(t, repoRoot, map[string]string{
		"org/jetbrains/kotlin/kotlin-stdlib/2.0.21/kotlin-stdlib-2.0.21.pom": `<project>
  <modelVersion>4.0.0</modelVersion>
  <groupId>org.jetbrains.kotlin</groupId>
  <artifactId>kotlin-stdlib</artifactId>
  <version>2.0.21</version>
</project>`,
		"org/jetbrains/kotlin/kotlin-stdlib/2.0.21/kotlin-stdlib-2.0.21.jar":                           "stdlib",
		"org/jetbrains/kotlin/kotlin-compiler-embeddable/2.0.21/kotlin-compiler-embeddable-2.0.21.pom": toolPOM("org.jetbrains.kotlin", "kotlin-compiler-embeddable", "2.0.21"),
		"org/jetbrains/kotlin/kotlin-compiler-embeddable/2.0.21/kotlin-compiler-embeddable-2.0.21.jar": "compiler",
	})
	writeRepositories(t, workspaceRoot, repoRoot)

	cache, err := store.Open(workspaceRoot, filepath.Join(t.TempDir(), "cache"), store.Options{Logger: discardLogger()})
	if err != nil {
		t.Fatal(err)
	}

	provider := newTestProvider(t, workspaceRoot)
	provider.cache = cache
	project := &core.Project{ID: "app", Dir: projectDir, Provider: "jvm"}
	options := mustOptions(t, optionsFromTOML(t, `kotlin = "2.0.21"`))

	build, err := provider.prepare(context.Background(), project, options)
	if err != nil {
		t.Fatalf("prepare() error = %v", err)
	}
	if len(build.artifacts) != 1 {
		t.Fatalf("artifacts = %d, want 1", len(build.artifacts))
	}
	artifact := build.artifacts[0]
	if artifact.Artifact != "kotlin-stdlib" || artifact.Scope != deps.ScopeCompile {
		t.Errorf("artifact = %+v", artifact)
	}
	if _, err := os.Stat(artifact.Path); err != nil {
		t.Fatalf("artifact path: %v", err)
	}
	assertContains(t, build.mainCompile, artifact.Path)
	if build.kotlin == nil || len(build.kotlin.kotlinClasspath) == 0 {
		t.Error("kotlin tools were not resolved")
	}
	if _, err := os.Stat(filepath.Join(projectDir, "kine.lock")); err != nil {
		t.Fatalf("lock file: %v", err)
	}
}

func assertContains(t *testing.T, values []string, want string) {
	t.Helper()

	if !contains(values, want) {
		t.Errorf("%v does not contain %q", values, want)
	}
}

func assertNotContains(t *testing.T, values []string, want string) {
	t.Helper()

	if contains(values, want) {
		t.Errorf("%v contains %q", values, want)
	}
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func writeFileRepo(t *testing.T, root string, files map[string]string) {
	t.Helper()

	for name, content := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func writeRepositories(t *testing.T, workspaceRoot, repoRoot string) {
	t.Helper()

	content := "[[repository]]\nid = \"test\"\nurl = \"file://" + filepath.ToSlash(repoRoot) + "\"\n"
	if err := os.WriteFile(filepath.Join(workspaceRoot, "repositories.toml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
