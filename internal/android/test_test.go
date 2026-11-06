package android

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gabriel-aplok/kine-build-java/internal/core"
)

func TestWriteAndroidTestManifestGenerated(t *testing.T) {
	project := &core.Project{}
	options := Options{
		PackageName:   "com.example.app",
		ApplicationID: "com.example.app.free",
	}
	path, testPackage, runner, err := writeAndroidTestManifest(project, options, t.TempDir())
	if err != nil {
		t.Fatalf("writeAndroidTestManifest() error = %v", err)
	}
	if testPackage != "com.example.app.free.test" {
		t.Errorf("testPackage = %q", testPackage)
	}
	if runner != defaultInstrumentation {
		t.Errorf("runner = %q", runner)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, want := range []string{"package=\"com.example.app.free.test\"", "android:targetPackage=\"com.example.app.free\"", defaultInstrumentation} {
		if !strings.Contains(text, want) {
			t.Errorf("manifest does not contain %q:\n%s", want, text)
		}
	}
}

func TestWriteLintManifest(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "AndroidManifest.xml")
	if err := os.WriteFile(source, []byte(
		`<manifest xmlns:android="http://schemas.android.com/apk/res/android" package="com.example.app"><application android:debuggable="true"/></manifest>`,
	), 0o644); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "lint", "AndroidManifest.xml")
	if err := writeLintManifest(source, target, Options{VersionCode: 7, VersionName: "1.2"}); err != nil {
		t.Fatalf("writeLintManifest() error = %v", err)
	}
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if strings.Contains(text, "debuggable") {
		t.Errorf("lint manifest keeps android:debuggable:\n%s", text)
	}
	for _, want := range []string{"android:versionCode=\"7\"", "android:versionName=\"1.2\""} {
		if !strings.Contains(text, want) {
			t.Errorf("manifest does not contain %q:\n%s", want, text)
		}
	}
}

func TestDependencyAttributesPreferRelease(t *testing.T) {
	attributes := dependencyAttributes(Options{Variants: []Variant{
		{BuildType: "debug"},
		{BuildType: "release"},
	}})
	if attributes["com.android.build.api.attributes.BuildTypeAttr"] != "release" {
		t.Errorf("attributes = %v", attributes)
	}
	if attributes["org.gradle.usage"] != "java-runtime" {
		t.Errorf("usage = %q", attributes["org.gradle.usage"])
	}
}
