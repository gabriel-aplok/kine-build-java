package compat

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const simplePOM = `<project>
  <modelVersion>4.0.0</modelVersion>
  <groupId>com.example</groupId>
  <artifactId>app</artifactId>
  <version>1.0.0</version>
  <properties>
    <maven.compiler.release>21</maven.compiler.release>
  </properties>
  <build>
    <plugins>
      <plugin>
        <artifactId>maven-jar-plugin</artifactId>
        <configuration><archive><manifest><mainClass>demo.App</mainClass></manifest></archive></configuration>
      </plugin>
    </plugins>
  </build>
</project>`

func TestImportMaven(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "pom.xml"), simplePOM)

	result, err := Import(Options{Dir: dir})
	if err != nil {
		t.Fatalf("Import() error = %v", err)
	}
	if result.Source != "maven" {
		t.Errorf("Source = %q", result.Source)
	}
	if result.Java != 21 || result.Dependencies != 0 {
		t.Errorf("result = %+v", result)
	}
	if !result.Written {
		t.Error("Written = false")
	}
	if _, err := os.Stat(filepath.Join(dir, "kine.toml")); err != nil {
		t.Fatalf("kine.toml: %v", err)
	}
}

func TestImportDryRun(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "pom.xml"), simplePOM)

	result, err := Import(Options{Dir: dir, DryRun: true})
	if err != nil {
		t.Fatalf("Import() error = %v", err)
	}
	if result.Written {
		t.Error("Written = true for a dry run")
	}
	if len(result.Config) == 0 {
		t.Error("Config is empty")
	}
	if _, err := os.Stat(filepath.Join(dir, "kine.toml")); !os.IsNotExist(err) {
		t.Errorf("kine.toml exists after a dry run: %v", err)
	}
}

func TestImportExists(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "pom.xml"), simplePOM)
	writeFile(t, filepath.Join(dir, "kine.toml"), "existing")

	_, err := Import(Options{Dir: dir})
	if !errors.Is(err, ErrConfigExists) {
		t.Fatalf("Import() error = %v, want ErrConfigExists", err)
	}

	if _, err := Import(Options{Dir: dir, Force: true}); err != nil {
		t.Fatalf("Import(force) error = %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "kine.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "provider") || !strings.Contains(string(data), "jvm") {
		t.Errorf("kine.toml was not overwritten:\n%s", data)
	}
}

func TestImportGradleAutoDetect(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "build.gradle"), "plugins { id 'java' }\n")

	result, err := Import(Options{Dir: dir, DryRun: true})
	if err != nil {
		t.Fatalf("Import() error = %v", err)
	}
	if result.Source != "gradle" {
		t.Errorf("Source = %q", result.Source)
	}
}

func TestImportMissingBuildFile(t *testing.T) {
	_, err := Import(Options{Dir: t.TempDir()})
	if err == nil {
		t.Fatal("Import() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "pom.xml") {
		t.Errorf("error %q does not list the expected files", err)
	}
}

func TestImportForcedSource(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "build.gradle"), "plugins { id 'java' }\n")

	_, err := Import(Options{Dir: dir, From: SourceMaven, DryRun: true})
	if err == nil {
		t.Fatal("Import() error = nil, want error")
	}

	_, err = Import(Options{Dir: dir, From: Source("bazel"), DryRun: true})
	if err == nil {
		t.Fatal("Import() error = nil for an unknown source")
	}
}

func TestImportTargetNotDirectory(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "file")
	writeFile(t, path, "x")

	if _, err := Import(Options{Dir: path}); err == nil {
		t.Fatal("Import() error = nil, want error")
	}
}
