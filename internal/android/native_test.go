package android

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/gabriel-aplok/kine-build-java/internal/core"
	"github.com/gabriel-aplok/kine-build-java/internal/toolchain"
)

func TestNativeTask(t *testing.T) {
	workspace := t.TempDir()
	projectDir := filepath.Join(workspace, "app")
	cppDir := filepath.Join(projectDir, "src", "main", "cpp")
	if err := os.MkdirAll(cppDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cmakeFile := filepath.Join(cppDir, "CMakeLists.txt")
	if err := os.WriteFile(cmakeFile, []byte("project(demo)\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	sdk := &SDK{
		NDK:          "/sdk/ndk/30.0.16248370",
		NDKVersion:   "30.0.16248370",
		CMake:        "/sdk/cmake/3.22.1/bin/cmake",
		CMakeVersion: "3.22.1",
		Ninja:        "/sdk/cmake/3.22.1/bin/ninja",
	}
	options := Options{
		MinSDK: 21,
		Native: &NativeOptions{
			CMake:     cmakeFile,
			ABIs:      []string{"arm64-v8a"},
			BuildType: "Debug",
			STL:       "c++_static",
		},
	}

	var commands []toolchain.Command
	provider := &Provider{run: func(ctx context.Context, command toolchain.Command) (toolchain.Result, error) {
		commands = append(commands, command)
		if len(command.Args) > 0 && command.Args[0] == "--build" {
			outputDir := configureOutputDir(commands[len(commands)-2].Args)
			if err := os.WriteFile(filepath.Join(outputDir, "libkolpa.so"), []byte("native"), 0o755); err != nil {
				return toolchain.Result{}, err
			}
		}
		return toolchain.Result{}, nil
	}}

	task, err := provider.nativeTask(workspace, &core.Project{ID: "app", Dir: projectDir}, options, sdk, testJDK())
	if err != nil {
		t.Fatalf("nativeTask() error = %v", err)
	}
	if task.ID != "android-native" {
		t.Errorf("ID = %q", task.ID)
	}
	if len(task.Inputs) != 1 || task.Inputs[0].Path != "app/src/main/cpp" || task.Inputs[0].Mode != core.ModeTree {
		t.Errorf("Inputs = %+v", task.Inputs)
	}
	if len(task.Outputs) != 1 || task.Outputs[0].Path != "app/build/android/native" || task.Outputs[0].Mode != core.ModeTree {
		t.Errorf("Outputs = %+v", task.Outputs)
	}
	for _, want := range []string{"cmake 3.22.1", "ndk=30.0.16248370", "abi=arm64-v8a", "min=21"} {
		if !strings.Contains(task.Key, want) {
			t.Errorf("Key = %q, want %q", task.Key, want)
		}
	}

	if err := task.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(commands) != 2 {
		t.Fatalf("commands = %d, want 2", len(commands))
	}

	configure := commands[0]
	if configure.Path != sdk.CMake {
		t.Errorf("configure path = %q", configure.Path)
	}
	for _, want := range []string{
		"-S", cppDir,
		"-B", filepath.Join(projectDir, "build", "android", "native-build", "arm64-v8a"),
		"-G", "Ninja",
		"-DCMAKE_MAKE_PROGRAM=" + sdk.Ninja,
		"-DCMAKE_TOOLCHAIN_FILE=" + filepath.Join(sdk.NDK, "build", "cmake", "android.toolchain.cmake"),
		"-DANDROID_ABI=arm64-v8a",
		"-DANDROID_PLATFORM=android-21",
		"-DANDROID_STL=c++_static",
	} {
		if !contains(configure.Args, want) {
			t.Errorf("configure args %v do not contain %q", configure.Args, want)
		}
	}

	wantBuild := []string{"--build", filepath.Join(projectDir, "build", "android", "native-build", "arm64-v8a")}
	if commands[1].Path != sdk.CMake || !reflect.DeepEqual(commands[1].Args, wantBuild) {
		t.Errorf("build = %q %v", commands[1].Path, commands[1].Args)
	}

	library := filepath.Join(projectDir, "build", "android", "native", "arm64-v8a", "libkolpa.so")
	if _, err := os.Stat(library); err != nil {
		t.Fatalf("native library: %v", err)
	}
}

func TestNativeBuildIntegration(t *testing.T) {
	sdk, err := FindSDK(Requirement{Native: true})
	if err != nil {
		t.Skipf("no Android NDK or CMake: %v", err)
	}
	jdk, err := toolchain.Find(toolchain.Requirement{})
	if err != nil {
		t.Skipf("no JDK: %v", err)
	}
	if jdk.Major < MinBuildJDK {
		t.Skipf("JDK %d is older than %d", jdk.Major, MinBuildJDK)
	}

	workspace := t.TempDir()
	projectDir := filepath.Join(workspace, "app")
	cppDir := filepath.Join(projectDir, "src", "main", "cpp")
	if err := os.MkdirAll(cppDir, 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"CMakeLists.txt": `cmake_minimum_required(VERSION 3.22.1)
project(demo CXX)
add_library(demo SHARED demo.cpp)
`,
		"demo.cpp": `extern "C" int demo_value() { return 42; }
`,
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(cppDir, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	options := Options{
		MinSDK: 24,
		Native: &NativeOptions{
			CMake:     filepath.Join(cppDir, "CMakeLists.txt"),
			ABIs:      []string{"arm64-v8a"},
			BuildType: "Debug",
			STL:       "c++_static",
		},
	}
	provider := &Provider{run: toolchain.Run}
	task, err := provider.nativeTask(workspace, &core.Project{ID: "app", Dir: projectDir}, options, sdk, jdk)
	if err != nil {
		t.Fatalf("nativeTask() error = %v", err)
	}
	if err := task.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	library := filepath.Join(projectDir, "build", "android", "native", "arm64-v8a", "libdemo.so")
	if _, err := os.Stat(library); err != nil {
		t.Fatalf("native library: %v", err)
	}
}

func TestNativeTaskNone(t *testing.T) {
	provider := &Provider{}
	task, err := provider.nativeTask(t.TempDir(), &core.Project{ID: "app"}, Options{}, &SDK{}, testJDK())
	if err != nil {
		t.Fatalf("nativeTask() error = %v", err)
	}
	if task != nil {
		t.Errorf("task = %+v, want nil", task)
	}
}

func TestNativeTaskMissingTools(t *testing.T) {
	provider := &Provider{}
	_, err := provider.nativeTask(t.TempDir(), &core.Project{ID: "app"}, Options{Native: &NativeOptions{}}, &SDK{}, testJDK())
	if err == nil {
		t.Fatal("nativeTask() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "NDK") {
		t.Errorf("error %q does not mention the NDK", err)
	}
}

func TestCollectSharedLibrariesCopiesNestedOutput(t *testing.T) {
	buildDir := t.TempDir()
	nested := filepath.Join(buildDir, "obj", "arm64-v8a")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nested, "libnested.so"), []byte("native"), 0o755); err != nil {
		t.Fatal(err)
	}

	outputDir := t.TempDir()
	if err := collectSharedLibraries(buildDir, outputDir); err != nil {
		t.Fatalf("collectSharedLibraries() error = %v", err)
	}
	if data, err := os.ReadFile(filepath.Join(outputDir, "libnested.so")); err != nil || string(data) != "native" {
		t.Fatalf("copied library = %q, %v", data, err)
	}
}

func TestLocalNativeLibraries(t *testing.T) {
	root := t.TempDir()
	library := filepath.Join(root, "arm64-v8a", "libkolpa.so")
	if err := os.MkdirAll(filepath.Dir(library), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(library, []byte("native"), 0o755); err != nil {
		t.Fatal(err)
	}

	natives, err := localNativeLibraries(root)
	if err != nil {
		t.Fatalf("localNativeLibraries() error = %v", err)
	}
	if len(natives) != 1 || natives[0].abi != "arm64-v8a" || natives[0].name != "libkolpa.so" || natives[0].path != library {
		t.Errorf("natives = %+v", natives)
	}
}

func TestMergeNativesLocalWins(t *testing.T) {
	merged := mergeNatives(
		[]nativeLibrary{{abi: "arm64-v8a", name: "libkolpa.so", jar: "/deps.jar", entry: "libkolpa.so"}},
		[]nativeLibrary{{abi: "arm64-v8a", name: "libkolpa.so", path: "/build/libkolpa.so"}},
	)
	if len(merged) != 1 || merged[0].path != "/build/libkolpa.so" {
		t.Errorf("merged = %+v", merged)
	}
}

func configureOutputDir(args []string) string {
	const prefix = "-DCMAKE_LIBRARY_OUTPUT_DIRECTORY="
	for _, arg := range args {
		if strings.HasPrefix(arg, prefix) {
			return strings.TrimPrefix(arg, prefix)
		}
	}
	return ""
}
