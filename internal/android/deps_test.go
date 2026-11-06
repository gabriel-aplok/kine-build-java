package android

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gabriel-aplok/kine-build-java/internal/core"
	"github.com/gabriel-aplok/kine-build-java/internal/deps"
	"github.com/gabriel-aplok/kine-build-java/internal/store"
)

func TestResolveDependenciesSkipped(t *testing.T) {
	provider := NewProvider(Config{})
	artifacts, err := provider.resolveDependencies(context.Background(), &core.Project{ID: "app"}, Options{})
	if err != nil {
		t.Fatalf("resolveDependencies() error = %v", err)
	}
	if len(artifacts) != 0 {
		t.Errorf("artifacts = %d, want 0", len(artifacts))
	}
}

func TestResolveDependenciesNoStore(t *testing.T) {
	provider := NewProvider(Config{})
	options := Options{Dependencies: []deps.Dependency{{
		Coordinate: deps.Coordinate{Group: "com.example", Artifact: "lib", Version: "1.0"},
		Scope:      deps.ScopeCompile,
	}}}

	_, err := provider.resolveDependencies(context.Background(), &core.Project{ID: "app"}, options)
	if err == nil {
		t.Fatal("resolveDependencies() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "store") {
		t.Errorf("error %q does not mention the store", err)
	}
}

func TestResolveDependencies(t *testing.T) {
	workspaceRoot := t.TempDir()
	projectDir := filepath.Join(workspaceRoot, "app")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}

	repoRoot := t.TempDir()
	writeFileRepo(t, repoRoot, map[string]string{
		"com/example/lib/1.0/lib-1.0.pom": `<project>
  <modelVersion>4.0.0</modelVersion>
  <groupId>com.example</groupId>
  <artifactId>lib</artifactId>
  <version>1.0</version>
</project>`,
		"com/example/lib/1.0/lib-1.0.jar": "jar",
	})
	writeRepositories(t, workspaceRoot, repoRoot)

	cache := openTestStore(t, workspaceRoot)
	provider := NewProvider(Config{
		Workspace: &core.Workspace{Root: workspaceRoot},
		Cache:     cache,
		Logger:    discardTestLogger(),
	})
	options := Options{Dependencies: []deps.Dependency{{
		Coordinate: deps.Coordinate{Group: "com.example", Artifact: "lib", Version: "1.0"},
		Scope:      deps.ScopeCompile,
	}}}

	artifacts, err := provider.resolveDependencies(context.Background(), &core.Project{ID: "app", Dir: projectDir}, options)
	if err != nil {
		t.Fatalf("resolveDependencies() error = %v", err)
	}
	if len(artifacts) != 1 {
		t.Fatalf("artifacts = %d, want 1", len(artifacts))
	}
	artifact := artifacts[0]
	if artifact.Artifact != "lib" || artifact.Scope != deps.ScopeCompile {
		t.Errorf("artifact = %+v", artifact)
	}
	if _, err := os.Stat(artifact.Path); err != nil {
		t.Fatalf("artifact path: %v", err)
	}

	jars, err := classpathJars(artifacts, t.TempDir())
	if err != nil {
		t.Fatalf("classpathJars() error = %v", err)
	}
	if len(jars) != 1 || jars[0] != artifact.Path {
		t.Errorf("jars = %v, want [%s]", jars, artifact.Path)
	}
}

func openTestStore(t *testing.T, root string) *store.Store {
	t.Helper()

	cache, err := store.Open(root, filepath.Join(t.TempDir(), "cache"), store.Options{Logger: discardTestLogger()})
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}
	return cache
}

func writeFileRepo(t *testing.T, root string, files map[string]string) {
	t.Helper()

	for name, content := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func writeRepositories(t *testing.T, workspaceRoot, repoRoot string) {
	t.Helper()

	content := "[[repository]]\nid = \"test\"\nurl = \"file://" + filepath.ToSlash(repoRoot) + "\"\n"
	if err := os.WriteFile(filepath.Join(workspaceRoot, "repositories.toml"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func discardTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
