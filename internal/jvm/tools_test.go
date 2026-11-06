package jvm

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gabriel-aplok/kine-build-java/internal/store"
)

func TestResolveKotlinTools(t *testing.T) {
	provider, workspaceRoot := newToolTestProvider(t)

	tools, err := provider.resolveKotlinTools(context.Background(), "2.0.21", false)
	if err != nil {
		t.Fatalf("resolveKotlinTools() error = %v", err)
	}
	if len(tools.kotlinClasspath) != 1 {
		t.Fatalf("classpath = %v, want one compiler jar", tools.kotlinClasspath)
	}
	if tools.composePlugin != "" {
		t.Errorf("composePlugin = %q, want empty", tools.composePlugin)
	}
	if _, err := os.Stat(tools.kotlinClasspath[0]); err != nil {
		t.Fatalf("compiler jar: %v", err)
	}

	lock := filepath.Join(workspaceRoot, ".kine", "tools", "kotlin", "2.0.21", "kine.lock")
	if _, err := os.Stat(lock); err != nil {
		t.Fatalf("tool lock: %v", err)
	}
}

func TestResolveKotlinToolsCompose(t *testing.T) {
	provider, _ := newToolTestProvider(t)

	tools, err := provider.resolveKotlinTools(context.Background(), "2.0.21", true)
	if err != nil {
		t.Fatalf("resolveKotlinTools() error = %v", err)
	}
	if len(tools.kotlinClasspath) != 2 {
		t.Fatalf("classpath = %v, want compiler and compose", tools.kotlinClasspath)
	}
	if tools.composePlugin == "" {
		t.Fatal("composePlugin is empty")
	}
	if _, err := os.Stat(tools.composePlugin); err != nil {
		t.Fatalf("compose plugin: %v", err)
	}
}

func TestResolveJUnitConsole(t *testing.T) {
	provider, workspaceRoot := newToolTestProvider(t)

	tools, err := provider.resolveJUnit(context.Background(), "1.11.4")
	if err != nil {
		t.Fatalf("resolveJUnit() error = %v", err)
	}
	if tools.junitConsole == "" {
		t.Fatal("junitConsole is empty")
	}
	if _, err := os.Stat(tools.junitConsole); err != nil {
		t.Fatalf("console jar: %v", err)
	}

	lock := filepath.Join(workspaceRoot, ".kine", "tools", "junit", "1.11.4", "kine.lock")
	if _, err := os.Stat(lock); err != nil {
		t.Fatalf("tool lock: %v", err)
	}
}

func TestResolveToolCache(t *testing.T) {
	provider, _ := newToolTestProvider(t)

	first, err := provider.resolveKotlinTools(context.Background(), "2.0.21", false)
	if err != nil {
		t.Fatal(err)
	}
	second, err := provider.resolveKotlinTools(context.Background(), "2.0.21", false)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Error("tool resolution was not cached")
	}
}

func TestResolveToolNoCache(t *testing.T) {
	provider, _ := newToolTestProvider(t)
	provider.cache = nil

	_, err := provider.resolveKotlinTools(context.Background(), "2.0.21", false)
	if err == nil {
		t.Fatal("resolveKotlinTools() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "store") {
		t.Errorf("error %q does not mention the store", err)
	}
}

func TestResolveToolEmptyWorkspace(t *testing.T) {
	provider, _ := newToolTestProvider(t)
	provider.workspace = nil

	_, err := provider.resolveKotlinTools(context.Background(), "2.0.21", false)
	if err == nil {
		t.Fatal("resolveKotlinTools() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "workspace") {
		t.Errorf("error %q does not mention the workspace", err)
	}
}

func newToolTestProvider(t *testing.T) (*Provider, string) {
	t.Helper()

	workspaceRoot := t.TempDir()
	repoRoot := t.TempDir()
	writeFileRepo(t, repoRoot, map[string]string{
		pomFile("org.jetbrains.kotlin", "kotlin-compiler-embeddable", "2.0.21"):                toolPOM("org.jetbrains.kotlin", "kotlin-compiler-embeddable", "2.0.21"),
		jarFile("org.jetbrains.kotlin", "kotlin-compiler-embeddable", "2.0.21"):                "compiler",
		pomFile("org.jetbrains.kotlin", "kotlin-compose-compiler-plugin-embeddable", "2.0.21"): toolPOM("org.jetbrains.kotlin", "kotlin-compose-compiler-plugin-embeddable", "2.0.21"),
		jarFile("org.jetbrains.kotlin", "kotlin-compose-compiler-plugin-embeddable", "2.0.21"): "compose",
		pomFile("org.junit.platform", "junit-platform-console-standalone", "1.11.4"):           toolPOM("org.junit.platform", "junit-platform-console-standalone", "1.11.4"),
		jarFile("org.junit.platform", "junit-platform-console-standalone", "1.11.4"):           "junit",
		pomFile("org.jetbrains.kotlin", "kotlin-stdlib", "2.0.21"):                             toolPOM("org.jetbrains.kotlin", "kotlin-stdlib", "2.0.21"),
		jarFile("org.jetbrains.kotlin", "kotlin-stdlib", "2.0.21"):                             "stdlib",
	})
	writeRepositories(t, workspaceRoot, repoRoot)

	cache, err := store.Open(workspaceRoot, filepath.Join(t.TempDir(), "cache"), store.Options{Logger: discardLogger()})
	if err != nil {
		t.Fatal(err)
	}
	provider := newTestProvider(t, workspaceRoot)
	provider.cache = cache
	return provider, workspaceRoot
}

func toolPOM(group, artifact, version string) string {
	return `<project>
  <modelVersion>4.0.0</modelVersion>
  <groupId>` + group + `</groupId>
  <artifactId>` + artifact + `</artifactId>
  <version>` + version + `</version>
</project>`
}

func pomFile(group, artifact, version string) string {
	return strings.ReplaceAll(group, ".", "/") + "/" + artifact + "/" + version + "/" + artifact + "-" + version + ".pom"
}

func jarFile(group, artifact, version string) string {
	return strings.ReplaceAll(group, ".", "/") + "/" + artifact + "/" + version + "/" + artifact + "-" + version + ".jar"
}
