package android

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDecodeOptionsVariantDefaults(t *testing.T) {
	projectDir := newProject(t)

	options, err := decodeOptions(nil, projectDir)
	if err != nil {
		t.Fatalf("decodeOptions() error = %v", err)
	}
	if len(options.Variants) != 1 {
		t.Fatalf("Variants = %d, want 1", len(options.Variants))
	}
	variant := options.Variants[0]
	if variant.Flavor != "" || variant.BuildType != "debug" || variant.Name != "debug" || variant.Suffix != "debug" {
		t.Errorf("variant = %+v", variant)
	}
	if !options.Legacy {
		t.Error("Legacy = false, want true")
	}
}

func TestDecodeOptionsVariantMatrix(t *testing.T) {
	projectDir := newProject(t)
	if err := os.WriteFile(filepath.Join(projectDir, "proguard-rules.pro"), []byte("-keep class *\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	values := map[string]any{
		"build_types": []any{"debug", "release"},
		"flavors":     []any{"free", "paid"},
		"build_type": map[string]any{
			"release": map[string]any{
				"minify":   true,
				"proguard": []any{"proguard-rules.pro"},
				"debug":    false,
			},
		},
		"flavor": map[string]any{
			"free": map[string]any{
				"application_id_suffix": ".free",
				"version_name_suffix":   "-free",
				"version_code_offset":   10,
			},
		},
	}

	options, err := decodeOptions(values, projectDir)
	if err != nil {
		t.Fatalf("decodeOptions() error = %v", err)
	}
	if len(options.Variants) != 4 {
		t.Fatalf("Variants = %d, want 4", len(options.Variants))
	}
	byName := make(map[string]Variant)
	for _, variant := range options.Variants {
		byName[variant.Name] = variant
	}
	freeRelease, ok := byName["freeRelease"]
	if !ok {
		t.Fatalf("freeRelease is missing: %+v", byName)
	}
	if freeRelease.Suffix != "free-release" {
		t.Errorf("Suffix = %q", freeRelease.Suffix)
	}
	if !freeRelease.BuildTypeOptions.Minify || freeRelease.BuildTypeOptions.Debug {
		t.Errorf("release options = %+v", freeRelease.BuildTypeOptions)
	}
	if freeRelease.FlavorOptions.ApplicationIDSuffix != ".free" ||
		freeRelease.FlavorOptions.VersionNameSuffix != "-free" ||
		freeRelease.FlavorOptions.VersionCodeOffset != 10 {
		t.Errorf("flavor options = %+v", freeRelease.FlavorOptions)
	}
	if options.Legacy {
		t.Error("Legacy = true, want false")
	}
}

func TestDecodeOptionsVariantSourceSets(t *testing.T) {
	projectDir := newProject(t)
	for _, dir := range []string{
		"src/main/kotlin",
		"src/debug/java",
		"src/free/java",
		"src/freeDebug/java",
		"src/debug/res",
		"src/free/res",
		"src/freeDebug/res",
		"src/free/assets",
		"src/freeDebug/assets",
		"src/debug/kotlin",
	} {
		if err := os.MkdirAll(filepath.Join(projectDir, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{
		"src/debug/AndroidManifest.xml",
		"src/free/AndroidManifest.xml",
		"src/freeDebug/AndroidManifest.xml",
	} {
		if err := os.WriteFile(filepath.Join(projectDir, name), []byte("<manifest/>\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	options, err := decodeOptions(map[string]any{
		"build_types": []any{"debug"},
		"flavors":     []any{"free"},
		"kotlin":      "2.4.21",
	}, projectDir)
	if err != nil {
		t.Fatalf("decodeOptions() error = %v", err)
	}
	variant := options.Variants[0]
	wantJava := []string{
		filepath.Join(projectDir, "src/main/java"),
		filepath.Join(projectDir, "src/debug/java"),
		filepath.Join(projectDir, "src/free/java"),
		filepath.Join(projectDir, "src/freeDebug/java"),
	}
	if !sameStrings(variant.Sources.JavaDirs, wantJava) {
		t.Errorf("JavaDirs = %v, want %v", variant.Sources.JavaDirs, wantJava)
	}
	wantRes := []string{
		filepath.Join(projectDir, "src/main/res"),
		filepath.Join(projectDir, "src/debug/res"),
		filepath.Join(projectDir, "src/free/res"),
		filepath.Join(projectDir, "src/freeDebug/res"),
	}
	if !sameStrings(variant.Sources.ResDirs, wantRes) {
		t.Errorf("ResDirs = %v, want %v", variant.Sources.ResDirs, wantRes)
	}
	wantAssets := []string{
		filepath.Join(projectDir, "src/free/assets"),
		filepath.Join(projectDir, "src/freeDebug/assets"),
	}
	if !sameStrings(variant.Sources.AssetDirs, wantAssets) {
		t.Errorf("AssetDirs = %v, want %v", variant.Sources.AssetDirs, wantAssets)
	}
	wantManifests := []string{
		filepath.Join(projectDir, "src/main/AndroidManifest.xml"),
		filepath.Join(projectDir, "src/debug/AndroidManifest.xml"),
		filepath.Join(projectDir, "src/free/AndroidManifest.xml"),
		filepath.Join(projectDir, "src/freeDebug/AndroidManifest.xml"),
	}
	if !sameStrings(variant.Sources.Manifests, wantManifests) {
		t.Errorf("Manifests = %v, want %v", variant.Sources.Manifests, wantManifests)
	}
	if !sameStrings(variant.Sources.KotlinDirs, []string{
		filepath.Join(projectDir, "src/main/kotlin"),
		filepath.Join(projectDir, "src/debug/kotlin"),
	}) {
		t.Errorf("KotlinDirs = %v", variant.Sources.KotlinDirs)
	}
}

func TestDecodeOptionsVariantValidation(t *testing.T) {
	tests := []struct {
		name   string
		values map[string]any
		want   string
	}{
		{name: "duplicate build type", values: map[string]any{"build_types": []any{"debug", "debug"}}, want: "duplicate build type"},
		{name: "duplicate flavor", values: map[string]any{"flavors": []any{"free", "free"}}, want: "duplicate flavor"},
		{name: "bad build type", values: map[string]any{"build_types": []any{"Debug"}}, want: "build_types"},
		{name: "bad flavor", values: map[string]any{"flavors": []any{"free paid"}}, want: "flavors"},
		{name: "unknown build type config", values: map[string]any{"build_type": map[string]any{"release": map[string]any{}}}, want: "build_type \"release\""},
		{name: "unknown flavor config", values: map[string]any{"flavor": map[string]any{"free": map[string]any{}}}, want: "flavor \"free\""},
		{name: "shrink without minify", values: map[string]any{"build_type": map[string]any{"debug": map[string]any{"shrink_resources": true}}}, want: "shrink_resources"},
		{name: "absolute proguard", values: map[string]any{"build_type": map[string]any{"debug": map[string]any{"proguard": []any{"/tmp/rules.pro"}}}}, want: "relative"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			projectDir := newProject(t)
			_, err := decodeOptions(tt.values, projectDir)
			if err == nil {
				t.Fatal("decodeOptions() error = nil, want error")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error %q does not contain %q", err, tt.want)
			}
		})
	}
}

func TestVariantPaths(t *testing.T) {
	legacy := Options{Legacy: true, Variant: Variant{Suffix: "debug", BuildType: "debug"}}
	if legacy.taskID("apk") != "apk" {
		t.Errorf("legacy task id = %q", legacy.taskID("apk"))
	}
	if legacy.androidBuildDir() != filepath.Join("build", "android") {
		t.Errorf("legacy android dir = %q", legacy.androidBuildDir())
	}
	if legacy.apkOutputDir() != filepath.Join("build", "outputs", "apk", "debug") {
		t.Errorf("legacy apk dir = %q", legacy.apkOutputDir())
	}
	if legacy.apkFileName() != "app-debug.apk" {
		t.Errorf("legacy apk name = %q", legacy.apkFileName())
	}

	variant := Options{Variant: Variant{Flavor: "free", BuildType: "release", Suffix: "free-release"}}
	if variant.taskID("apk") != "apk-free-release" {
		t.Errorf("task id = %q", variant.taskID("apk"))
	}
	if variant.androidBuildDir() != filepath.Join("build", "android", "free-release") {
		t.Errorf("android dir = %q", variant.androidBuildDir())
	}
	if variant.apkOutputDir() != filepath.Join("build", "outputs", "apk", "free", "release") {
		t.Errorf("apk dir = %q", variant.apkOutputDir())
	}
	if variant.apkFileName() != "app-free-release.apk" {
		t.Errorf("apk name = %q", variant.apkFileName())
	}
}

func sameStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}
