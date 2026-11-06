package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const importPOM = `<project>
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

func TestImportCommand(t *testing.T) {
	dir := t.TempDir()
	writeImportPOM(t, dir)

	var stdout, stderr bytes.Buffer
	code := run([]string{"import", dir}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run() = %d, stderr = %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "wrote kine.toml") {
		t.Errorf("stdout %q does not report the write", stdout.String())
	}
	data, err := os.ReadFile(filepath.Join(dir, "kine.toml"))
	if err != nil {
		t.Fatalf("kine.toml: %v", err)
	}
	if !strings.Contains(string(data), "provider") || !strings.Contains(string(data), "jvm") {
		t.Errorf("kine.toml is unexpected:\n%s", data)
	}
}

func TestImportCommandDryRun(t *testing.T) {
	dir := t.TempDir()
	writeImportPOM(t, dir)

	var stdout, stderr bytes.Buffer
	code := run([]string{"import", "--dry-run", dir}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run() = %d, stderr = %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "[project]") {
		t.Errorf("stdout %q does not contain the config", stdout.String())
	}
	if _, err := os.Stat(filepath.Join(dir, "kine.toml")); !os.IsNotExist(err) {
		t.Errorf("kine.toml exists after a dry run: %v", err)
	}
}

func TestImportCommandExisting(t *testing.T) {
	dir := t.TempDir()
	writeImportPOM(t, dir)
	if err := os.WriteFile(filepath.Join(dir, "kine.toml"), []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := run([]string{"import", dir}, &stdout, &stderr)
	if code != exitUsage {
		t.Fatalf("run() = %d, want %d", code, exitUsage)
	}
	if !strings.Contains(stderr.String(), "already exists") {
		t.Errorf("stderr %q does not explain the existing file", stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	code = run([]string{"import", "--force", dir}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run(--force) = %d, stderr = %s", code, stderr.String())
	}
}

func TestImportCommandMissingBuildFile(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"import", t.TempDir()}, &stdout, &stderr)
	if code != exitUsage {
		t.Fatalf("run() = %d, want %d", code, exitUsage)
	}
	if !strings.Contains(stderr.String(), "import failed") {
		t.Errorf("stderr %q does not report the failure", stderr.String())
	}
}

func TestImportCommandUnknownSource(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"import", "--from", "bazel", t.TempDir()}, &stdout, &stderr)
	if code != exitUsage {
		t.Fatalf("run() = %d, want %d", code, exitUsage)
	}
	if !strings.Contains(stderr.String(), "unknown import source") {
		t.Errorf("stderr %q does not name the source", stderr.String())
	}
}

func TestImportCommandTooManyDirectories(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"import", t.TempDir(), t.TempDir()}, &stdout, &stderr)
	if code != exitUsage {
		t.Fatalf("run() = %d, want %d", code, exitUsage)
	}
	if !strings.Contains(stderr.String(), "at most one directory") {
		t.Errorf("stderr %q does not explain the argument", stderr.String())
	}
}

func writeImportPOM(t *testing.T, dir string) {
	t.Helper()

	if err := os.WriteFile(filepath.Join(dir, "pom.xml"), []byte(importPOM), 0o600); err != nil {
		t.Fatal(err)
	}
}
