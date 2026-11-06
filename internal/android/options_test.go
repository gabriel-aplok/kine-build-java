package android

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gabriel-aplok/kine-build-java/internal/deps"
)

func TestDecodeOptionsDefaults(t *testing.T) {
	projectDir := newProject(t)

	options, err := decodeOptions(nil, projectDir)
	if err != nil {
		t.Fatalf("decodeOptions() error = %v", err)
	}
	if options.CompileSDK != 0 {
		t.Errorf("CompileSDK = %d, want 0", options.CompileSDK)
	}
	if options.MinSDK != defaultMinSDK {
		t.Errorf("MinSDK = %d, want %d", options.MinSDK, defaultMinSDK)
	}
	if options.Java != defaultJava {
		t.Errorf("Java = %d, want %d", options.Java, defaultJava)
	}
	if options.TargetSDK != 0 {
		t.Errorf("TargetSDK = %d, want 0", options.TargetSDK)
	}
	if options.VersionCode != defaultVersionCode {
		t.Errorf("VersionCode = %d", options.VersionCode)
	}
	if options.VersionName != defaultVersionName {
		t.Errorf("VersionName = %q", options.VersionName)
	}
	if len(options.JavaDirs) != 1 || options.JavaDirs[0] != filepath.Join(projectDir, "src", "main", "java") {
		t.Errorf("JavaDirs = %v", options.JavaDirs)
	}
	if len(options.ResDirs) != 1 || options.ResDirs[0] != filepath.Join(projectDir, "src", "main", "res") {
		t.Errorf("ResDirs = %v", options.ResDirs)
	}
	if options.Manifest != filepath.Join(projectDir, "src", "main", "AndroidManifest.xml") {
		t.Errorf("Manifest = %q", options.Manifest)
	}
}

func TestDecodeOptions(t *testing.T) {
	projectDir := newProject(t)
	if err := os.MkdirAll(filepath.Join(projectDir, "src", "java"), 0o755); err != nil {
		t.Fatal(err)
	}

	values := map[string]any{
		"compile_sdk":  35,
		"min_sdk":      24,
		"target_sdk":   34,
		"version_code": 7,
		"version_name": "7.0",
		"sources": map[string]any{
			"java":     []any{"src/java"},
			"res":      []any{"src/main/res"},
			"manifest": "src/main/AndroidManifest.xml",
		},
		"dependencies": map[string]any{
			"compile": []any{"com.example:lib:1.0"},
		},
	}

	options, err := decodeOptions(values, projectDir)
	if err != nil {
		t.Fatalf("decodeOptions() error = %v", err)
	}
	if options.CompileSDK != 35 || options.MinSDK != 24 || options.TargetSDK != 34 {
		t.Errorf("sdk = %d/%d/%d", options.CompileSDK, options.MinSDK, options.TargetSDK)
	}
	if options.VersionCode != 7 || options.VersionName != "7.0" {
		t.Errorf("version = %d/%q", options.VersionCode, options.VersionName)
	}
	if len(options.JavaDirs) != 1 || options.JavaDirs[0] != filepath.Join(projectDir, "src", "java") {
		t.Errorf("JavaDirs = %v", options.JavaDirs)
	}
	if len(options.Dependencies) != 1 || options.Dependencies[0].Scope != deps.ScopeCompile {
		t.Errorf("Dependencies = %+v", options.Dependencies)
	}
	if options.Dependencies[0].Artifact != "lib" || options.Dependencies[0].Version != "1.0" {
		t.Errorf("Dependency = %+v", options.Dependencies[0])
	}
}

func TestDecodeOptionsTargetDefaultsToCompile(t *testing.T) {
	projectDir := newProject(t)

	options, err := decodeOptions(map[string]any{"compile_sdk": 35}, projectDir)
	if err != nil {
		t.Fatalf("decodeOptions() error = %v", err)
	}
	if options.TargetSDK != 35 {
		t.Errorf("TargetSDK = %d, want 35", options.TargetSDK)
	}
}

func TestDecodeOptionsNativeAndAssets(t *testing.T) {
	projectDir := newProject(t)
	if err := os.MkdirAll(filepath.Join(projectDir, "src", "main", "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(projectDir, "src", "main", "cpp"), 0o755); err != nil {
		t.Fatal(err)
	}
	cmake := filepath.Join(projectDir, "src", "main", "cpp", "CMakeLists.txt")
	if err := os.WriteFile(cmake, []byte("project(demo)\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	values := map[string]any{
		"build_tools":    "33.0.0",
		"extra_packages": []any{"com.example.lib"},
		"sources": map[string]any{
			"assets": []any{"src/main/assets"},
		},
		"native": map[string]any{
			"ndk_version":   "30.0.16248370",
			"cmake_version": "3.22.1",
		},
	}

	options, err := decodeOptions(values, projectDir)
	if err != nil {
		t.Fatalf("decodeOptions() error = %v", err)
	}
	if options.BuildTools != "33.0.0" {
		t.Errorf("BuildTools = %q", options.BuildTools)
	}
	if len(options.ExtraPackages) != 1 || options.ExtraPackages[0] != "com.example.lib" {
		t.Errorf("ExtraPackages = %v", options.ExtraPackages)
	}
	if len(options.AssetDirs) != 1 || options.AssetDirs[0] != filepath.Join(projectDir, "src", "main", "assets") {
		t.Errorf("AssetDirs = %v", options.AssetDirs)
	}
	if options.Native == nil {
		t.Fatal("Native = nil")
	}
	if options.Native.CMake != cmake {
		t.Errorf("CMake = %q, want %q", options.Native.CMake, cmake)
	}
	if len(options.Native.ABIs) != 1 || options.Native.ABIs[0] != defaultNativeABI {
		t.Errorf("ABIs = %v", options.Native.ABIs)
	}
	if options.Native.BuildType != defaultNativeBuildType || options.Native.STL != defaultNativeSTL {
		t.Errorf("native defaults = %+v", options.Native)
	}
	if options.Native.NDKVersion != "30.0.16248370" || options.Native.CMakeVersion != "3.22.1" {
		t.Errorf("native versions = %+v", options.Native)
	}
}

func TestDecodeOptionsNativeCustom(t *testing.T) {
	projectDir := newProject(t)
	if err := os.MkdirAll(filepath.Join(projectDir, "cpp"), 0o755); err != nil {
		t.Fatal(err)
	}
	cmake := filepath.Join(projectDir, "cpp", "CMakeLists.txt")
	if err := os.WriteFile(cmake, []byte("project(demo)\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	values := map[string]any{
		"native": map[string]any{
			"cmake":      "cpp/CMakeLists.txt",
			"abi":        []any{"x86_64", "arm64-v8a"},
			"build_type": "Release",
			"stl":        "c++_shared",
		},
	}

	options, err := decodeOptions(values, projectDir)
	if err != nil {
		t.Fatalf("decodeOptions() error = %v", err)
	}
	if options.Native.CMake != cmake {
		t.Errorf("CMake = %q", options.Native.CMake)
	}
	if len(options.Native.ABIs) != 2 || options.Native.ABIs[0] != "x86_64" {
		t.Errorf("ABIs = %v", options.Native.ABIs)
	}
	if options.Native.BuildType != "Release" || options.Native.STL != "c++_shared" {
		t.Errorf("native = %+v", options.Native)
	}
}

func TestDecodeOptionsSigning(t *testing.T) {
	projectDir := newProject(t)
	keystore := filepath.Join(t.TempDir(), "debug.keystore")
	if err := os.WriteFile(keystore, []byte("keystore"), 0o600); err != nil {
		t.Fatal(err)
	}

	options, err := decodeOptions(map[string]any{
		"signing": map[string]any{
			"keystore": keystore,
			"alias":    "customalias",
		},
	}, projectDir)
	if err != nil {
		t.Fatalf("decodeOptions() error = %v", err)
	}
	if options.Signing == nil {
		t.Fatal("Signing = nil")
	}
	if options.Signing.Keystore != keystore || options.Signing.Alias != "customalias" {
		t.Errorf("Signing = %+v", options.Signing)
	}
	if options.Signing.StorePassword != debugPass || options.Signing.KeyPassword != debugPass {
		t.Errorf("Signing passwords = %+v", options.Signing)
	}
}

func TestDecodeOptionsSigningEnvironment(t *testing.T) {
	projectDir := newProject(t)
	keystore := filepath.Join(t.TempDir(), "release.p12")
	if err := os.WriteFile(keystore, []byte("keystore"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("KOLPA_KEYSTORE", keystore)
	t.Setenv("KOLPA_ALIAS", "release")
	t.Setenv("KOLPA_STORE_PASSWORD", "store-secret")
	t.Setenv("KOLPA_KEY_PASSWORD", "key-secret")

	options, err := decodeOptions(map[string]any{
		"release_signing": map[string]any{
			"keystore":       "${KOLPA_KEYSTORE}",
			"alias":          "${KOLPA_ALIAS}",
			"store_password": "${KOLPA_STORE_PASSWORD}",
			"key_password":   "${KOLPA_KEY_PASSWORD}",
		},
	}, projectDir)
	if err != nil {
		t.Fatalf("decodeOptions() error = %v", err)
	}
	signing := options.ReleaseSigning
	if signing == nil {
		t.Fatal("ReleaseSigning = nil")
	}
	if signing.Keystore != keystore || signing.Alias != "release" {
		t.Errorf("signing = %+v", signing)
	}
	if signing.StorePassword != "store-secret" || signing.KeyPassword != "key-secret" {
		t.Errorf("passwords = %+v", signing)
	}
}

func TestDecodeOptionsSigningMissingEnvironment(t *testing.T) {
	projectDir := newProject(t)
	options, err := decodeOptions(map[string]any{
		"release_signing": map[string]any{
			"keystore": "${KOLPA_MISSING_KEYSTORE}",
		},
	}, projectDir)
	if err != nil {
		t.Fatalf("decodeOptions() error = %v", err)
	}
	if options.ReleaseSigning == nil || !options.ReleaseSigning.Unresolved {
		t.Errorf("ReleaseSigning = %+v, want unresolved", options.ReleaseSigning)
	}
}

func TestDecodeOptionsNamedSigning(t *testing.T) {
	projectDir := newProject(t)
	keystore := filepath.Join(t.TempDir(), "release.p12")
	if err := os.WriteFile(keystore, []byte("keystore"), 0o600); err != nil {
		t.Fatal(err)
	}
	options, err := decodeOptions(map[string]any{
		"build_types": []any{"debug", "release"},
		"signing_config": map[string]any{
			"release": map[string]any{
				"keystore": keystore,
				"alias":    "release-key",
			},
		},
		"build_type": map[string]any{
			"release": map[string]any{"signing": "release"},
		},
	}, projectDir)
	if err != nil {
		t.Fatalf("decodeOptions() error = %v", err)
	}
	if options.SigningConfigs["release"] == nil {
		t.Fatal("release signing config is missing")
	}
	byName := make(map[string]Variant)
	for _, variant := range options.Variants {
		byName[variant.Name] = variant
	}
	if byName["release"].Signing == nil || byName["release"].Signing.Alias != "release-key" {
		t.Errorf("release variant signing = %+v", byName["release"].Signing)
	}
}

func TestDecodeOptionsFlavorSigningOverride(t *testing.T) {
	projectDir := newProject(t)
	dir := t.TempDir()
	debugKeystore := filepath.Join(dir, "debug.p12")
	releaseKeystore := filepath.Join(dir, "release.p12")
	for _, path := range []string{debugKeystore, releaseKeystore} {
		if err := os.WriteFile(path, []byte("keystore"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	options, err := decodeOptions(map[string]any{
		"build_types": []any{"debug", "release"},
		"flavors":     []any{"paid", "free"},
		"signing_config": map[string]any{
			"debug-key":   map[string]any{"keystore": debugKeystore, "alias": "debug-key"},
			"release-key": map[string]any{"keystore": releaseKeystore, "alias": "release-key"},
		},
		"build_type": map[string]any{
			"release": map[string]any{"signing": "release-key"},
		},
		"flavor": map[string]any{
			"paid": map[string]any{"signing": "debug-key"},
		},
	}, projectDir)
	if err != nil {
		t.Fatalf("decodeOptions() error = %v", err)
	}
	byName := make(map[string]Variant)
	for _, variant := range options.Variants {
		byName[variant.Name] = variant
	}
	paid := byName["paidRelease"]
	if paid.Signing == nil || paid.Signing.Alias != "debug-key" {
		t.Errorf("paidRelease signing = %+v, want flavor debug-key", paid.Signing)
	}
	free := byName["freeRelease"]
	if free.Signing == nil || free.Signing.Alias != "release-key" {
		t.Errorf("freeRelease signing = %+v, want build-type release-key", free.Signing)
	}
}

func TestDecodeOptionsGitVersionCode(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not available")
	}
	projectDir := newProject(t)
	runGit(t, projectDir, "init")
	runGit(t, projectDir, "add", ".")
	runGit(t, projectDir, "-c", "user.email=kine@example.com", "-c", "user.name=Kine", "commit", "-m", "first")

	options, err := decodeOptions(map[string]any{"version_code_strategy": "git"}, projectDir)
	if err != nil {
		t.Fatalf("decodeOptions() error = %v", err)
	}
	if options.VersionCode != 1 {
		t.Errorf("VersionCode = %d, want 1", options.VersionCode)
	}
	if options.VersionCodeStrategy != "git" {
		t.Errorf("VersionCodeStrategy = %q", options.VersionCodeStrategy)
	}
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", dir}, args...)...)
	command.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=Kine", "GIT_AUTHOR_EMAIL=kine@example.com",
		"GIT_COMMITTER_NAME=Kine", "GIT_COMMITTER_EMAIL=kine@example.com",
	)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, output)
	}
}

func TestDecodeOptionsMissingSigningConfig(t *testing.T) {
	projectDir := newProject(t)
	_, err := decodeOptions(map[string]any{
		"build_types": []any{"debug", "release"},
		"build_type": map[string]any{
			"release": map[string]any{"signing": "missing"},
		},
	}, projectDir)
	if err == nil {
		t.Fatal("decodeOptions() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "missing") {
		t.Errorf("error %q does not name the signing config", err)
	}
}

func TestDecodeOptionsTimestampVersionCode(t *testing.T) {
	projectDir := newProject(t)
	options, err := decodeOptions(map[string]any{"version_code_strategy": "timestamp"}, projectDir)
	if err != nil {
		t.Fatalf("decodeOptions() error = %v", err)
	}
	if options.VersionCode <= 0 {
		t.Errorf("VersionCode = %d, want positive", options.VersionCode)
	}
	if options.VersionCodeStrategy != "timestamp" {
		t.Errorf("VersionCodeStrategy = %q", options.VersionCodeStrategy)
	}
}

func TestDecodeOptionsSigningMissingKeystore(t *testing.T) {
	projectDir := newProject(t)

	_, err := decodeOptions(map[string]any{
		"signing": map[string]any{"keystore": "missing.keystore"},
	}, projectDir)
	if err == nil {
		t.Fatal("decodeOptions() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "missing.keystore") {
		t.Errorf("error %q does not name the keystore", err)
	}
}

func TestDecodeOptionsUnknownKey(t *testing.T) {
	projectDir := newProject(t)

	_, err := decodeOptions(map[string]any{"extra": true}, projectDir)
	if err == nil {
		t.Fatal("decodeOptions() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "strict mode") {
		t.Errorf("error %q does not report the unknown key", err)
	}
}

func TestDecodeOptionsValidation(t *testing.T) {
	tests := []struct {
		name   string
		values map[string]any
		want   string
	}{
		{name: "negative compile", values: map[string]any{"compile_sdk": -1}, want: "compile_sdk"},
		{name: "negative min", values: map[string]any{"min_sdk": -1}, want: "min_sdk"},
		{name: "negative target", values: map[string]any{"target_sdk": -1}, want: "target_sdk"},
		{name: "old java", values: map[string]any{"java": 7}, want: "java"},
		{name: "zero version", values: map[string]any{"version_code": -1}, want: "version_code"},
		{name: "bad version name", values: map[string]any{"version_name": "1 0"}, want: "version_name"},
		{name: "absolute manifest", values: map[string]any{"sources": map[string]any{"manifest": "/tmp/AndroidManifest.xml"}}, want: "relative"},
		{name: "escaping manifest", values: map[string]any{"sources": map[string]any{"manifest": "../AndroidManifest.xml"}}, want: "escapes"},
		{name: "escaping java", values: map[string]any{"sources": map[string]any{"java": []any{"../java"}}}, want: "escapes"},
		{name: "bad dependency", values: map[string]any{"dependencies": map[string]any{"compile": []any{"nope"}}}, want: "nope"},
		{name: "bad build tools", values: map[string]any{"build_tools": "../tools"}, want: "build_tools"},
		{name: "bad extra package", values: map[string]any{"extra_packages": []any{"com..lib"}}, want: "extra_packages"},
		{name: "bad abi", values: map[string]any{"native": map[string]any{"abi": []any{"mips"}}}, want: "abi"},
		{name: "bad build type", values: map[string]any{"native": map[string]any{"build_type": "Fast"}}, want: "build_type"},
		{name: "bad stl", values: map[string]any{"native": map[string]any{"stl": "c++ shared"}}, want: "stl"},
		{name: "missing cmake", values: map[string]any{"native": map[string]any{"cmake": "src/main/cpp/Missing.txt"}}, want: "Missing.txt"},
		{name: "escaping assets", values: map[string]any{"sources": map[string]any{"assets": []any{"../assets"}}}, want: "escapes"},
		{name: "empty signing", values: map[string]any{"signing": map[string]any{}}, want: "keystore"},
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

func TestDecodeOptionsMissingManifest(t *testing.T) {
	projectDir := t.TempDir()

	_, err := decodeOptions(nil, projectDir)
	if err == nil {
		t.Fatal("decodeOptions() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "AndroidManifest.xml") {
		t.Errorf("error %q does not name the manifest", err)
	}
}

func TestDecodeOptionsSkipsMissingSourceDirs(t *testing.T) {
	projectDir := newProject(t)
	if err := os.RemoveAll(filepath.Join(projectDir, "src", "main", "res")); err != nil {
		t.Fatal(err)
	}

	options, err := decodeOptions(nil, projectDir)
	if err != nil {
		t.Fatalf("decodeOptions() error = %v", err)
	}
	if len(options.ResDirs) != 0 {
		t.Errorf("ResDirs = %v, want empty", options.ResDirs)
	}
}

func TestDecodeOptionsSourceFileRejected(t *testing.T) {
	projectDir := newProject(t)
	path := filepath.Join(projectDir, "src", "main", "java")
	if err := os.RemoveAll(path); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := decodeOptions(nil, projectDir)
	if err == nil {
		t.Fatal("decodeOptions() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "not a directory") {
		t.Errorf("error %q does not mention the directory", err)
	}
}

func newProject(t *testing.T) string {
	t.Helper()

	projectDir := t.TempDir()
	for _, dir := range []string{"src/main/java", "src/main/res"} {
		if err := os.MkdirAll(filepath.Join(projectDir, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	manifest := `<manifest xmlns:android="http://schemas.android.com/apk/res/android" package="com.example.app">
  <application android:label="App" />
</manifest>
`
	if err := os.WriteFile(filepath.Join(projectDir, "src", "main", "AndroidManifest.xml"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	return projectDir
}
