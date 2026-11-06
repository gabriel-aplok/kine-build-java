package android

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gabriel-aplok/kine-build-java/internal/deps"
)

func TestClasspathJarsExtractsAAR(t *testing.T) {
	dir := t.TempDir()
	aarPath := filepath.Join(dir, "backend.aar")
	writeAAR(t, aarPath, "classes", true)
	artifact := deps.Artifact{
		Coordinate: deps.Coordinate{
			Group: "com.example", Artifact: "backend", Version: "1.0", Extension: "aar",
		},
		Path:   aarPath,
		SHA256: "abcdef",
	}

	jars, err := classpathJars([]deps.Artifact{artifact}, filepath.Join(dir, "work"))
	if err != nil {
		t.Fatalf("classpathJars() error = %v", err)
	}
	if len(jars) != 1 {
		t.Fatalf("jars = %v, want one classes.jar", jars)
	}
	data, err := os.ReadFile(jars[0])
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "classes" {
		t.Errorf("classes.jar = %q, want classes", data)
	}
}

func TestExtractAAR(t *testing.T) {
	dir := t.TempDir()
	aarPath := filepath.Join(dir, "library.aar")
	writeZip(t, aarPath, map[string]string{
		"classes.jar":                 "classes",
		"AndroidManifest.xml":         "<manifest xmlns:android=\"http://schemas.android.com/apk/res/android\" package=\"com.example.lib\"><application><activity android:name=\".LibActivity\"/></application></manifest>",
		"res/values/strings.xml":      "<resources><string name=\"lib_name\">Lib</string></resources>",
		"assets/library.txt":          "asset",
		"jni/arm64-v8a/libexample.so": "native",
		"proguard.txt":                "-keep class com.example.lib.**",
		"R.txt":                       "int string lib_name 0x7f010000",
		"public.txt":                  "string lib_name",
		"unknown/ignored.txt":         "ignored",
	})
	artifact := deps.Artifact{
		Coordinate: deps.Coordinate{Group: "com.example", Artifact: "library", Version: "1.0", Extension: "aar"},
		Path:       aarPath,
		SHA256:     "hash",
	}

	content, err := extractAAR(artifact, filepath.Join(dir, "work"))
	if err != nil {
		t.Fatalf("extractAAR() error = %v", err)
	}
	if content.ClassesJar == "" || content.ResDir == "" || content.AssetsDir == "" ||
		content.Manifest == "" || content.Proguard == "" || content.NativeDir == "" {
		t.Fatalf("content = %+v", content)
	}
	if data, err := os.ReadFile(filepath.Join(content.ResDir, "values", "strings.xml")); err != nil || !strings.Contains(string(data), "lib_name") {
		t.Errorf("resource = %q, %v", data, err)
	}
	if data, err := os.ReadFile(filepath.Join(content.AssetsDir, "library.txt")); err != nil || string(data) != "asset" {
		t.Errorf("asset = %q, %v", data, err)
	}
	if data, err := os.ReadFile(filepath.Join(content.NativeDir, "arm64-v8a", "libexample.so")); err != nil || string(data) != "native" {
		t.Errorf("native = %q, %v", data, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "work", "hash", "unknown", "ignored.txt")); !os.IsNotExist(err) {
		t.Errorf("unexpected extraction of unknown entry: %v", err)
	}

	if err := os.Remove(aarPath); err != nil {
		t.Fatal(err)
	}
	if _, err := extractAAR(artifact, filepath.Join(dir, "work")); err != nil {
		t.Errorf("second extractAAR() error = %v", err)
	}
}

func TestExtractAARRejectsUnsafePath(t *testing.T) {
	dir := t.TempDir()
	aarPath := filepath.Join(dir, "unsafe.aar")
	writeZip(t, aarPath, map[string]string{"../escape.txt": "bad"})
	artifact := deps.Artifact{
		Coordinate: deps.Coordinate{Group: "com.example", Artifact: "unsafe", Version: "1.0", Extension: "aar"},
		Path:       aarPath,
		SHA256:     "hash",
	}

	_, err := extractAAR(artifact, filepath.Join(dir, "work"))
	if err == nil {
		t.Fatal("extractAAR() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "unsafe") {
		t.Errorf("error %q does not mention the unsafe entry", err)
	}
}

func TestPrepareAARs(t *testing.T) {
	dir := t.TempDir()
	aarPath := filepath.Join(dir, "library.aar")
	writeZip(t, aarPath, map[string]string{
		"classes.jar":           "classes",
		"AndroidManifest.xml":   "<manifest package=\"com.example.lib\"/>",
		"res/values/values.xml": "<resources/>",
		"assets/a.txt":          "a",
		"jni/arm64-v8a/liba.so": "a",
		"proguard.txt":          "-keep class *",
	})
	artifact := deps.Artifact{
		Coordinate: deps.Coordinate{Group: "com.example", Artifact: "library", Version: "1.0", Extension: "aar"},
		Path:       aarPath,
		SHA256:     "hash",
	}
	options := Options{}
	provider := &Provider{}
	if err := provider.prepareAARs(dir, &options, []deps.Artifact{artifact}); err != nil {
		t.Fatalf("prepareAARs() error = %v", err)
	}
	if len(options.AARResDirs) != 1 || len(options.AARAssetDirs) != 1 ||
		len(options.AARManifests) != 1 || len(options.AARNativeDirs) != 1 ||
		len(options.AARProguard) != 1 {
		t.Errorf("options = %+v", options)
	}
}

func TestClasspathJarsKeepsJars(t *testing.T) {
	dir := t.TempDir()
	jarPath := filepath.Join(dir, "lib.jar")
	if err := os.WriteFile(jarPath, []byte("jar"), 0o600); err != nil {
		t.Fatal(err)
	}
	artifact := deps.Artifact{
		Coordinate: deps.Coordinate{Group: "com.example", Artifact: "lib", Version: "1.0", Extension: "jar"},
		Path:       jarPath,
	}

	jars, err := classpathJars([]deps.Artifact{artifact}, filepath.Join(dir, "work"))
	if err != nil {
		t.Fatalf("classpathJars() error = %v", err)
	}
	if len(jars) != 1 || jars[0] != jarPath {
		t.Errorf("jars = %v, want [%s]", jars, jarPath)
	}
}

func TestClasspathJarsAARWithoutClasses(t *testing.T) {
	dir := t.TempDir()
	aarPath := filepath.Join(dir, "empty.aar")
	writeAAR(t, aarPath, "", false)
	artifact := deps.Artifact{
		Coordinate: deps.Coordinate{Group: "com.example", Artifact: "empty", Version: "1.0", Extension: "aar"},
		Path:       aarPath,
		SHA256:     "empty",
	}

	jars, err := classpathJars([]deps.Artifact{artifact}, filepath.Join(dir, "work"))
	if err != nil {
		t.Fatalf("classpathJars() error = %v", err)
	}
	if len(jars) != 0 {
		t.Errorf("jars = %v, want none", jars)
	}
}

func writeAAR(t *testing.T, path, classes string, withClasses bool) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	if withClasses {
		entry, err := writer.Create("classes.jar")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write([]byte(classes)); err != nil {
			t.Fatal(err)
		}
	}
	manifest, err := writer.Create("AndroidManifest.xml")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manifest.Write([]byte("<manifest/>")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestClasspathJarsSkipsNatives(t *testing.T) {
	dir := t.TempDir()
	jarPath := filepath.Join(dir, "lib.jar")
	if err := os.WriteFile(jarPath, []byte("jar"), 0o600); err != nil {
		t.Fatal(err)
	}
	artifacts := []deps.Artifact{
		{Coordinate: deps.Coordinate{Group: "com.example", Artifact: "lib", Version: "1.0", Extension: "jar"}, Path: jarPath},
		{Coordinate: deps.Coordinate{Group: "org.lwjgl", Artifact: "lwjgl", Version: "3.3.3", Classifier: "natives-linux", Extension: "jar"}, Path: jarPath},
	}

	jars, err := classpathJars(artifacts, filepath.Join(dir, "work"))
	if err != nil {
		t.Fatalf("classpathJars() error = %v", err)
	}
	if len(jars) != 1 || jars[0] != jarPath {
		t.Errorf("jars = %v, want [%s]", jars, jarPath)
	}
}
