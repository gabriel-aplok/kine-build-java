package main

import (
	"archive/zip"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gabriel-aplok/kine-build-java/internal/core"
)

func TestTasksRejectsBadPluginConfig(t *testing.T) {
	dir := t.TempDir()
	writeRecordingConfig(t, dir)
	if err := os.WriteFile(filepath.Join(dir, "plugins.toml"), []byte("jars = []\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	chdir(t, dir)

	var stdout, stderr bytes.Buffer
	code := runWithProviders([]string{"tasks", "--cache-dir", t.TempDir()}, &stdout, &stderr, []core.Provider{newRecordingProvider()})
	if code != exitUsage {
		t.Fatalf("run() = %d, want %d", code, exitUsage)
	}
	if !strings.Contains(stderr.String(), "plugins.toml") {
		t.Errorf("stderr %q does not name the plugin config", stderr.String())
	}
}

func TestTasksRejectsBadPluginManifest(t *testing.T) {
	dir := t.TempDir()
	writeRecordingConfig(t, dir)
	jar := filepath.Join(dir, "plugins", "bad.jar")
	writeJarWithManifest(t, jar, `{"api":2,"id":"bad","version":"1.0","main":"bad.Plugin","providers":["bad"]}`)
	if err := os.WriteFile(filepath.Join(dir, "plugins.toml"), []byte("jars = [\"plugins/bad.jar\"]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	chdir(t, dir)

	var stdout, stderr bytes.Buffer
	code := runWithProviders([]string{"tasks", "--cache-dir", t.TempDir()}, &stdout, &stderr, []core.Provider{newRecordingProvider()})
	if code != exitUsage {
		t.Fatalf("run() = %d, want %d", code, exitUsage)
	}
	if !strings.Contains(stderr.String(), "api") {
		t.Errorf("stderr %q does not mention the api version", stderr.String())
	}
}

func TestDoctorFailsOnPlugins(t *testing.T) {
	root := writeDoctorWorkspace(t, `[project]
name = "app"
provider = "jvm"
`)
	if err := os.WriteFile(filepath.Join(root, "plugins.toml"), []byte("jars = []\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	chdir(t, root)

	var stdout, stderr bytes.Buffer
	code := run([]string{"doctor", "--cache-dir", t.TempDir()}, &stdout, &stderr)
	if code != exitBuildFailure {
		t.Fatalf("run() = %d, want %d", code, exitBuildFailure)
	}
	if !strings.Contains(stdout.String(), "plugins") || !strings.Contains(stdout.String(), "fail") {
		t.Errorf("stdout %q does not report the plugin failure", stdout.String())
	}
}

func TestOpenPluginsWithoutConfig(t *testing.T) {
	providers, closePlugins, err := openPlugins(
		&core.Workspace{Root: t.TempDir()},
		discardTestLogger(),
		io.Discard,
	)
	if err != nil {
		t.Fatalf("openPlugins() error = %v", err)
	}
	if len(providers) != 0 {
		t.Errorf("providers = %d, want 0", len(providers))
	}
	if err := closePlugins(); err != nil {
		t.Errorf("close() error = %v", err)
	}
}

func writeJarWithManifest(t *testing.T, path, manifest string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	entry, err := writer.Create("META-INF/kine-plugin.json")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := entry.Write([]byte(manifest)); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}
