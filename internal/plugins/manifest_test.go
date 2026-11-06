package plugins

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const validManifest = `{"api":1,"id":"example","version":"0.1.0","main":"dev.kine.example.ExamplePlugin","providers":["example"]}`

func TestReadManifest(t *testing.T) {
	jar := writeManifestJar(t, t.TempDir(), validManifest)

	manifest, err := ReadManifest(jar)
	if err != nil {
		t.Fatalf("ReadManifest() error = %v", err)
	}
	if manifest.API != APIVersion || manifest.ID != "example" || manifest.Version != "0.1.0" {
		t.Errorf("manifest = %+v", manifest)
	}
	if manifest.Main != "dev.kine.example.ExamplePlugin" {
		t.Errorf("Main = %q", manifest.Main)
	}
	if len(manifest.Providers) != 1 || manifest.Providers[0] != "example" {
		t.Errorf("Providers = %v", manifest.Providers)
	}
}

func TestReadManifestMissingEntry(t *testing.T) {
	jar := writeManifestJar(t, t.TempDir(), "")

	_, err := ReadManifest(jar)
	if err == nil {
		t.Fatal("ReadManifest() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "missing") {
		t.Errorf("error %q does not mention the missing manifest", err)
	}
}

func TestReadManifestInvalidJSON(t *testing.T) {
	jar := writeManifestJar(t, t.TempDir(), "{")

	_, err := ReadManifest(jar)
	if err == nil {
		t.Fatal("ReadManifest() error = nil, want error")
	}
}

func TestReadManifestUnknownField(t *testing.T) {
	jar := writeManifestJar(t, t.TempDir(),
		`{"api":1,"id":"example","version":"0.1.0","main":"a.B","providers":["example"],"extra":true}`)

	_, err := ReadManifest(jar)
	if err == nil {
		t.Fatal("ReadManifest() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "extra") {
		t.Errorf("error %q does not name the unknown field", err)
	}
}

func TestReadManifestWrongAPI(t *testing.T) {
	jar := writeManifestJar(t, t.TempDir(), strings.Replace(validManifest, `"api":1`, `"api":2`, 1))

	_, err := ReadManifest(jar)
	if err == nil {
		t.Fatal("ReadManifest() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "api") {
		t.Errorf("error %q does not mention the api", err)
	}
}

func TestReadManifestBadID(t *testing.T) {
	jar := writeManifestJar(t, t.TempDir(), strings.Replace(validManifest, `"example"`, `"Example"`, 1))

	_, err := ReadManifest(jar)
	if err == nil {
		t.Fatal("ReadManifest() error = nil, want error")
	}
}

func TestReadManifestEmptyVersion(t *testing.T) {
	jar := writeManifestJar(t, t.TempDir(), strings.Replace(validManifest, `"0.1.0"`, `""`, 1))

	_, err := ReadManifest(jar)
	if err == nil {
		t.Fatal("ReadManifest() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "version") {
		t.Errorf("error %q does not mention the version", err)
	}
}

func TestReadManifestVersionWhitespace(t *testing.T) {
	jar := writeManifestJar(t, t.TempDir(), strings.Replace(validManifest, `"0.1.0"`, `"0.1 0"`, 1))

	_, err := ReadManifest(jar)
	if err == nil {
		t.Fatal("ReadManifest() error = nil, want error")
	}
}

func TestReadManifestBadMain(t *testing.T) {
	jar := writeManifestJar(t, t.TempDir(),
		strings.Replace(validManifest, `"dev.kine.example.ExamplePlugin"`, `"not a class"`, 1))

	_, err := ReadManifest(jar)
	if err == nil {
		t.Fatal("ReadManifest() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "main") {
		t.Errorf("error %q does not mention the main class", err)
	}
}

func TestReadManifestNoProviders(t *testing.T) {
	jar := writeManifestJar(t, t.TempDir(),
		strings.Replace(validManifest, `["example"]`, `[]`, 1))

	_, err := ReadManifest(jar)
	if err == nil {
		t.Fatal("ReadManifest() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "providers") {
		t.Errorf("error %q does not mention providers", err)
	}
}

func TestReadManifestDuplicateProviders(t *testing.T) {
	jar := writeManifestJar(t, t.TempDir(),
		strings.Replace(validManifest, `["example"]`, `["one","one"]`, 1))

	_, err := ReadManifest(jar)
	if err == nil {
		t.Fatal("ReadManifest() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "duplicate") {
		t.Errorf("error %q does not mention the duplicate", err)
	}
}

func TestReadManifestBuiltinProvider(t *testing.T) {
	jar := writeManifestJar(t, t.TempDir(),
		strings.Replace(validManifest, `["example"]`, `["jvm"]`, 1))

	_, err := ReadManifest(jar)
	if err == nil {
		t.Fatal("ReadManifest() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "built in") {
		t.Errorf("error %q does not mention the built-in provider", err)
	}
}

func TestReadManifestTooLarge(t *testing.T) {
	jar := writeManifestJar(t, t.TempDir(), strings.Repeat(" ", maxManifestSize+1))

	_, err := ReadManifest(jar)
	if err == nil {
		t.Fatal("ReadManifest() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "exceeds") {
		t.Errorf("error %q does not mention the size", err)
	}
}

func TestReadManifestNotAJar(t *testing.T) {
	path := filepath.Join(t.TempDir(), "plugin.jar")
	writeTestFile(t, path, "not a zip")

	_, err := ReadManifest(path)
	if err == nil {
		t.Fatal("ReadManifest() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "open jar") {
		t.Errorf("error %q does not mention the jar", err)
	}
}

func writeManifestJar(t *testing.T, dir, manifest string) string {
	t.Helper()

	path := filepath.Join(dir, "plugin.jar")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	if manifest != "" {
		entry, err := writer.Create(manifestPath)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write([]byte(manifest)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}
