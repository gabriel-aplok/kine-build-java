package android

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var allTools = []string{"aapt2", "d8", "zipalign", "apksigner"}

func TestFindSDK(t *testing.T) {
	root := fakeSDK(t,
		[]string{"android-34", "android-35"},
		map[string][]string{
			"34.0.0": allTools,
			"36.0.0": allTools,
		},
	)

	sdk, err := FindSDK(Requirement{CompileSDK: 35}, root)
	if err != nil {
		t.Fatalf("FindSDK() error = %v", err)
	}
	if sdk.Root != root {
		t.Errorf("Root = %q, want %q", sdk.Root, root)
	}
	if sdk.Platform != filepath.Join(root, "platforms", "android-35") {
		t.Errorf("Platform = %q", sdk.Platform)
	}
	if sdk.BuildTools != filepath.Join(root, "build-tools", "36.0.0") {
		t.Errorf("BuildTools = %q, want 36.0.0", sdk.BuildTools)
	}
	if sdk.AndroidJar != filepath.Join(root, "platforms", "android-35", "android.jar") {
		t.Errorf("AndroidJar = %q", sdk.AndroidJar)
	}
	for _, path := range []string{sdk.Aapt2, sdk.D8, sdk.Zipalign, sdk.ApkSigner} {
		if !filepath.IsAbs(path) {
			t.Errorf("%q is not absolute", path)
		}
		if !isRegular(path) {
			t.Errorf("%q is not a regular file", path)
		}
	}
}

func TestFindSDKHighestPlatform(t *testing.T) {
	root := fakeSDK(t, []string{"android-33", "android-36"}, map[string][]string{
		"36.0.0": allTools,
	})

	sdk, err := FindSDK(Requirement{}, root)
	if err != nil {
		t.Fatalf("FindSDK() error = %v", err)
	}
	if sdk.Platform != filepath.Join(root, "platforms", "android-36") {
		t.Errorf("Platform = %q", sdk.Platform)
	}
}

func TestFindSDKMinorPlatform(t *testing.T) {
	root := fakeSDK(t, []string{"android-37.0"}, map[string][]string{
		"36.0.0": allTools,
	})

	sdk, err := FindSDK(Requirement{CompileSDK: 37}, root)
	if err != nil {
		t.Fatalf("FindSDK() error = %v", err)
	}
	if sdk.Platform != filepath.Join(root, "platforms", "android-37.0") {
		t.Errorf("Platform = %q", sdk.Platform)
	}
}

func TestFindSDKSkipsIncompleteBuildTools(t *testing.T) {
	root := fakeSDK(t,
		[]string{"android-35"},
		map[string][]string{
			"34.0.0": allTools,
			"36.0.0": {"aapt2"},
			"35.0.0": {"aapt2", "d8"},
		},
	)

	sdk, err := FindSDK(Requirement{CompileSDK: 35}, root)
	if err != nil {
		t.Fatalf("FindSDK() error = %v", err)
	}
	if sdk.BuildTools != filepath.Join(root, "build-tools", "34.0.0") {
		t.Errorf("BuildTools = %q, want 34.0.0", sdk.BuildTools)
	}
}

func TestFindSDKBuildTools(t *testing.T) {
	root := fakeSDK(t, []string{"android-33"}, map[string][]string{
		"33.0.0": allTools,
		"37.0.0": allTools,
	})

	sdk, err := FindSDK(Requirement{CompileSDK: 33, BuildTools: "33.0.0"}, root)
	if err != nil {
		t.Fatalf("FindSDK() error = %v", err)
	}
	if sdk.BuildTools != filepath.Join(root, "build-tools", "33.0.0") {
		t.Errorf("BuildTools = %q", sdk.BuildTools)
	}

	_, err = FindSDK(Requirement{CompileSDK: 33, BuildTools: "34.0.0"}, root)
	if err == nil {
		t.Fatal("FindSDK() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "34.0.0") {
		t.Errorf("error %q does not name the build tools", err)
	}
}

func TestFindSDKNative(t *testing.T) {
	root := fakeSDK(t, []string{"android-33"}, map[string][]string{"33.0.0": allTools})
	addFakeNDK(t, root, "25.2.9519653")
	addFakeNDK(t, root, "30.0.16248370")
	addFakeCMake(t, root, "3.22.1")

	sdk, err := FindSDK(Requirement{CompileSDK: 33, Native: true}, root)
	if err != nil {
		t.Fatalf("FindSDK() error = %v", err)
	}
	if sdk.NDK != filepath.Join(root, "ndk", "30.0.16248370") || sdk.NDKVersion != "30.0.16248370" {
		t.Errorf("NDK = %q %q", sdk.NDK, sdk.NDKVersion)
	}
	if sdk.CMakeVersion != "3.22.1" {
		t.Errorf("CMakeVersion = %q", sdk.CMakeVersion)
	}
	for _, path := range []string{sdk.CMake, sdk.Ninja} {
		if !filepath.IsAbs(path) || !isRegular(path) {
			t.Errorf("%q is not an absolute regular file", path)
		}
	}
}

func TestFindSDKNativeVersions(t *testing.T) {
	root := fakeSDK(t, []string{"android-33"}, map[string][]string{"33.0.0": allTools})
	addFakeNDK(t, root, "30.0.16248370")
	addFakeCMake(t, root, "3.22.1")
	addFakeCMake(t, root, "3.31.6")

	sdk, err := FindSDK(Requirement{
		CompileSDK:   33,
		Native:       true,
		NDKVersion:   "30.0.16248370",
		CMakeVersion: "3.22.1",
	}, root)
	if err != nil {
		t.Fatalf("FindSDK() error = %v", err)
	}
	if sdk.CMakeVersion != "3.22.1" {
		t.Errorf("CMakeVersion = %q", sdk.CMakeVersion)
	}
}

func TestFindSDKNativeMissing(t *testing.T) {
	root := fakeSDK(t, []string{"android-33"}, map[string][]string{"33.0.0": allTools})

	_, err := FindSDK(Requirement{CompileSDK: 33, Native: true}, root)
	if err == nil {
		t.Fatal("FindSDK() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "NDK") {
		t.Errorf("error %q does not mention the NDK", err)
	}
}

func TestFindSDKNativeCMakeMissing(t *testing.T) {
	root := fakeSDK(t, []string{"android-33"}, map[string][]string{"33.0.0": allTools})
	addFakeNDK(t, root, "30.0.16248370")

	_, err := FindSDK(Requirement{CompileSDK: 33, Native: true}, root)
	if err == nil {
		t.Fatal("FindSDK() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "cmake") {
		t.Errorf("error %q does not mention cmake", err)
	}
}

func TestFindSDKMissingPlatform(t *testing.T) {
	root := fakeSDK(t, []string{"android-35"}, map[string][]string{"36.0.0": allTools})

	_, err := FindSDK(Requirement{CompileSDK: 40}, root)
	if err == nil {
		t.Fatal("FindSDK() error = nil, want error")
	}
	if !errors.Is(err, ErrSDKNotFound) {
		t.Errorf("error %v is not ErrSDKNotFound", err)
	}
}

func TestFindSDKNoRoot(t *testing.T) {
	root := t.TempDir()

	_, err := FindSDK(Requirement{}, root)
	if err == nil {
		t.Fatal("FindSDK() error = nil, want error")
	}
	if !errors.Is(err, ErrSDKNotFound) {
		t.Errorf("error %v is not ErrSDKNotFound", err)
	}
}

func TestFindSDKMissingTools(t *testing.T) {
	root := fakeSDK(t, []string{"android-35"}, nil)

	_, err := FindSDK(Requirement{CompileSDK: 35}, root)
	if err == nil {
		t.Fatal("FindSDK() error = nil, want error")
	}
	if !errors.Is(err, ErrSDKNotFound) {
		t.Errorf("error %v is not ErrSDKNotFound", err)
	}
}

func fakeSDK(t *testing.T, platforms []string, buildTools map[string][]string) string {
	t.Helper()

	root := t.TempDir()
	for _, platform := range platforms {
		dir := filepath.Join(root, "platforms", platform)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "android.jar"), []byte("jar"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for version, tools := range buildTools {
		dir := filepath.Join(root, "build-tools", version)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		for _, tool := range tools {
			if err := os.WriteFile(filepath.Join(dir, tool), []byte("#!/bin/sh\n"), 0o755); err != nil {
				t.Fatal(err)
			}
		}
	}
	return root
}

func addFakeNDK(t *testing.T, root, version string) {
	t.Helper()

	dir := filepath.Join(root, "ndk", version, "build", "cmake")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "android.toolchain.cmake"), []byte("toolchain"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func addFakeCMake(t *testing.T, root, version string) {
	t.Helper()

	dir := filepath.Join(root, "cmake", version, "bin")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, tool := range []string{"cmake", "ninja"} {
		if err := os.WriteFile(filepath.Join(dir, tool), []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
}

func TestToolsInWindowsSuffixes(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"aapt2.exe":     "aapt2",
		"zipalign.exe":  "zipalign",
		"d8.bat":        "d8",
		"apksigner.bat": "apksigner",
	}
	for name := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("tool"), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	tools := toolsIn(dir)
	want := []string{
		filepath.Join(dir, "aapt2.exe"),
		filepath.Join(dir, "d8.bat"),
		filepath.Join(dir, "zipalign.exe"),
		filepath.Join(dir, "apksigner.bat"),
	}
	if len(tools) != len(want) {
		t.Fatalf("tools = %v, want %v", tools, want)
	}
	for i := range want {
		if tools[i] != want[i] {
			t.Errorf("tools[%d] = %q, want %q", i, tools[i], want[i])
		}
	}
}
