package android

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gabriel-aplok/kine-build-java/internal/core"
	"github.com/gabriel-aplok/kine-build-java/internal/toolchain"
)

func TestProviderName(t *testing.T) {
	provider := NewProvider(Config{})
	if provider.Name() != "android" {
		t.Errorf("Name() = %q, want android", provider.Name())
	}
}

func TestTasks(t *testing.T) {
	workspace, projectDir := providerProject(t)
	sdk := &SDK{
		Platform:   "/fake/platforms/android-36",
		AndroidJar: "/fake/platforms/android-36/android.jar",
		BuildTools: "36.0.0",
		Aapt2:      "/fake/aapt2",
		D8:         "/fake/d8",
		Zipalign:   "/fake/zipalign",
		ApkSigner:  "/fake/apksigner",
	}
	jdk := testJDK()

	provider := &Provider{
		config:  Config{Workspace: &core.Workspace{Root: workspace}},
		findSDK: fakeFindSDK(sdk),
		findJDK: fakeFindJDK(jdk),
		run: func(ctx context.Context, command toolchain.Command) (toolchain.Result, error) {
			return toolchain.Result{}, nil
		},
	}
	project := &core.Project{
		ID:      "app",
		Dir:     projectDir,
		Options: map[string]any{"compile_sdk": 36},
	}

	tasks, err := provider.Tasks(context.Background(), project)
	if err != nil {
		t.Fatalf("Tasks() error = %v", err)
	}
	if len(tasks) != 5 {
		t.Fatalf("tasks = %d, want 5", len(tasks))
	}
	if tasks[0].ID != "android-resources" || tasks[1].ID != "compile-java" || tasks[2].ID != "dex" || tasks[3].ID != "apk" || tasks[4].ID != "lint" {
		t.Errorf("ids = %q, %q, %q, %q, %q", tasks[0].ID, tasks[1].ID, tasks[2].ID, tasks[3].ID, tasks[4].ID)
	}
	if len(tasks[1].Deps) != 1 || tasks[1].Deps[0] != "app:android-resources" {
		t.Errorf("compile deps = %v", tasks[1].Deps)
	}
	if len(tasks[2].Deps) != 1 || tasks[2].Deps[0] != "app:compile-java" {
		t.Errorf("dex deps = %v", tasks[2].Deps)
	}
	if len(tasks[3].Deps) != 2 || tasks[3].Deps[0] != "app:android-resources" || tasks[3].Deps[1] != "app:dex" {
		t.Errorf("apk deps = %v", tasks[3].Deps)
	}
	if !strings.Contains(tasks[0].Key, "target=36") {
		t.Errorf("resource key = %q, want target 36", tasks[0].Key)
	}
	if tasks[3].Outputs[0].Path != "app/build/outputs/apk/debug/app-debug.apk" {
		t.Errorf("apk output = %q", tasks[3].Outputs[0].Path)
	}
}

func TestTasksVariants(t *testing.T) {
	workspace, projectDir := providerProject(t)
	provider := &Provider{
		config:  Config{Workspace: &core.Workspace{Root: workspace}},
		findSDK: fakeFindSDK(testSDK()),
		findJDK: fakeFindJDK(testJDK()),
	}
	project := &core.Project{
		ID:  "app",
		Dir: projectDir,
		Options: map[string]any{
			"compile_sdk": 36,
			"build_types": []any{"debug", "release"},
			"flavors":     []any{"free", "paid"},
		},
	}

	tasks, err := provider.Tasks(context.Background(), project)
	if err != nil {
		t.Fatalf("Tasks() error = %v", err)
	}
	if len(tasks) != 20 {
		t.Fatalf("tasks = %d, want 20", len(tasks))
	}

	ids := make(map[string]*core.Task)
	for _, task := range tasks {
		ids[task.ID] = task
	}
	for _, id := range []string{
		"android-resources-free-debug",
		"compile-java-free-debug",
		"dex-free-debug",
		"apk-free-debug",
		"apk-paid-release",
	} {
		if ids[id] == nil {
			t.Errorf("task %q is missing: %v", id, ids)
		}
	}
	if ids["compile-java-free-debug"].Deps[0] != "app:android-resources-free-debug" {
		t.Errorf("compile deps = %v", ids["compile-java-free-debug"].Deps)
	}
	if ids["apk-free-debug"].Deps[1] != "app:dex-free-debug" {
		t.Errorf("apk deps = %v", ids["apk-free-debug"].Deps)
	}
	if ids["apk-free-debug"].Outputs[0].Path != "app/build/outputs/apk/free/debug/app-free-debug.apk" {
		t.Errorf("apk output = %q", ids["apk-free-debug"].Outputs[0].Path)
	}
}

func TestTasksSingleRelease(t *testing.T) {
	workspace, projectDir := providerProject(t)
	provider := &Provider{
		config:  Config{Workspace: &core.Workspace{Root: workspace}},
		findSDK: fakeFindSDK(testSDK()),
		findJDK: fakeFindJDK(testJDK()),
	}
	project := &core.Project{
		ID:  "app",
		Dir: projectDir,
		Options: map[string]any{
			"compile_sdk": 36,
			"build_types": []any{"release"},
		},
	}

	tasks, err := provider.Tasks(context.Background(), project)
	if err != nil {
		t.Fatalf("Tasks() error = %v", err)
	}
	if len(tasks) != 5 {
		t.Fatalf("tasks = %d, want 5", len(tasks))
	}
	for index, want := range []string{"android-resources-release", "compile-java-release", "dex-release", "apk-release", "lint-release"} {
		if tasks[index].ID != want {
			t.Errorf("tasks[%d].ID = %q, want %q", index, tasks[index].ID, want)
		}
	}
	if tasks[3].Outputs[0].Path != "app/build/outputs/apk/release/app-release-unsigned.apk" {
		t.Errorf("apk output = %q", tasks[3].Outputs[0].Path)
	}
}

func TestTasksVariantsSources(t *testing.T) {
	workspace, projectDir := providerProject(t)
	debugSource := filepath.Join(projectDir, "src", "debug", "java")
	if err := os.MkdirAll(debugSource, 0o755); err != nil {
		t.Fatal(err)
	}
	provider := &Provider{
		config:  Config{Workspace: &core.Workspace{Root: workspace}},
		findSDK: fakeFindSDK(testSDK()),
		findJDK: fakeFindJDK(testJDK()),
	}
	project := &core.Project{
		ID:  "app",
		Dir: projectDir,
		Options: map[string]any{
			"compile_sdk": 36,
			"build_types": []any{"debug"},
			"flavors":     []any{"free"},
		},
	}

	tasks, err := provider.Tasks(context.Background(), project)
	if err != nil {
		t.Fatalf("Tasks() error = %v", err)
	}
	compile := tasks[1]
	for _, input := range compile.Inputs {
		if input.Path == "app/src/debug/java" {
			return
		}
	}
	t.Errorf("compile inputs = %+v, want the debug source set", compile.Inputs)
}

func TestTasksNative(t *testing.T) {
	workspace, projectDir := providerProject(t)
	cppDir := filepath.Join(projectDir, "src", "main", "cpp")
	if err := os.MkdirAll(cppDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cppDir, "CMakeLists.txt"), []byte("project(demo)\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	sdk := testSDK()
	sdk.NDK = "/fake/ndk/30.0.16248370"
	sdk.NDKVersion = "30.0.16248370"
	sdk.CMake = "/fake/cmake/3.22.1/bin/cmake"
	sdk.CMakeVersion = "3.22.1"
	sdk.Ninja = "/fake/cmake/3.22.1/bin/ninja"

	provider := &Provider{
		config:  Config{Workspace: &core.Workspace{Root: workspace}},
		findSDK: fakeFindSDK(sdk),
		findJDK: fakeFindJDK(testJDK()),
	}
	project := &core.Project{
		ID:  "app",
		Dir: projectDir,
		Options: map[string]any{
			"compile_sdk": 36,
			"native":      map[string]any{},
		},
	}

	tasks, err := provider.Tasks(context.Background(), project)
	if err != nil {
		t.Fatalf("Tasks() error = %v", err)
	}
	if len(tasks) != 6 {
		t.Fatalf("tasks = %d, want 6", len(tasks))
	}
	var nativeTask, apkTask *core.Task
	for _, task := range tasks {
		switch task.ID {
		case "android-native":
			nativeTask = task
		case "apk":
			apkTask = task
		}
	}
	if nativeTask == nil {
		t.Error("native task is missing")
	}
	if apkTask == nil || !contains(apkTask.Deps, "app:android-native") {
		t.Errorf("apk deps = %v", apkTask)
	}
}

func TestTasksExplicitTarget(t *testing.T) {
	workspace, projectDir := providerProject(t)
	provider := &Provider{
		config:  Config{Workspace: &core.Workspace{Root: workspace}},
		findSDK: fakeFindSDK(testSDK()),
		findJDK: fakeFindJDK(testJDK()),
	}
	project := &core.Project{
		ID:      "app",
		Dir:     projectDir,
		Options: map[string]any{"compile_sdk": 36, "target_sdk": 34},
	}

	tasks, err := provider.Tasks(context.Background(), project)
	if err != nil {
		t.Fatalf("Tasks() error = %v", err)
	}
	if !strings.Contains(tasks[0].Key, "target=34") {
		t.Errorf("resource key = %q, want target 34", tasks[0].Key)
	}
}

func TestTasksMissingSDK(t *testing.T) {
	workspace, projectDir := providerProject(t)
	provider := &Provider{
		config: Config{Workspace: &core.Workspace{Root: workspace}},
		findSDK: func(Requirement, ...string) (*SDK, error) {
			return nil, ErrSDKNotFound
		},
	}
	project := &core.Project{ID: "app", Dir: projectDir, Options: map[string]any{"compile_sdk": 36}}

	_, err := provider.Tasks(context.Background(), project)
	if err == nil {
		t.Fatal("Tasks() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "SDK") {
		t.Errorf("error %q does not mention the SDK", err)
	}
}

func TestTasksMissingJDK(t *testing.T) {
	workspace, projectDir := providerProject(t)
	provider := &Provider{
		config:  Config{Workspace: &core.Workspace{Root: workspace}},
		findSDK: fakeFindSDK(testSDK()),
		findJDK: func(toolchain.Requirement, ...string) (*toolchain.JDK, error) {
			return nil, errors.New("no jdk")
		},
	}
	project := &core.Project{ID: "app", Dir: projectDir}

	_, err := provider.Tasks(context.Background(), project)
	if err == nil {
		t.Fatal("Tasks() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "no jdk") {
		t.Errorf("error %q does not carry the JDK failure", err)
	}
}

func TestTasksOldJDK(t *testing.T) {
	workspace, projectDir := providerProject(t)
	provider := &Provider{
		config:  Config{Workspace: &core.Workspace{Root: workspace}},
		findSDK: fakeFindSDK(testSDK()),
		findJDK: fakeFindJDK(&toolchain.JDK{Major: 11, Version: "11.0.1"}),
	}
	project := &core.Project{ID: "app", Dir: projectDir}

	_, err := provider.Tasks(context.Background(), project)
	if err == nil {
		t.Fatal("Tasks() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "17") {
		t.Errorf("error %q does not mention the required JDK", err)
	}
}

func TestTasksBadOptions(t *testing.T) {
	workspace := t.TempDir()
	projectDir := filepath.Join(workspace, "app")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}
	provider := &Provider{
		config:  Config{Workspace: &core.Workspace{Root: workspace}},
		findSDK: fakeFindSDK(testSDK()),
		findJDK: fakeFindJDK(testJDK()),
	}

	_, err := provider.Tasks(context.Background(), &core.Project{ID: "app", Dir: projectDir})
	if err == nil {
		t.Fatal("Tasks() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "AndroidManifest.xml") {
		t.Errorf("error %q does not mention the manifest", err)
	}
}

func providerProject(t *testing.T) (string, string) {
	t.Helper()

	workspace := t.TempDir()
	projectDir := filepath.Join(workspace, "app")
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
	return workspace, projectDir
}

func testSDK() *SDK {
	return &SDK{
		Platform:   "/fake/platforms/android-36",
		AndroidJar: "/fake/platforms/android-36/android.jar",
		BuildTools: "36.0.0",
		Aapt2:      "/fake/aapt2",
		D8:         "/fake/d8",
		Zipalign:   "/fake/zipalign",
		ApkSigner:  "/fake/apksigner",
	}
}

func testJDK() *toolchain.JDK {
	return &toolchain.JDK{Home: "/fake/jdk", Version: "21.0.1", Major: 21, Java: "/fake/jdk/bin/java", Javac: "/fake/jdk/bin/javac"}
}

func fakeFindSDK(sdk *SDK) func(Requirement, ...string) (*SDK, error) {
	return func(Requirement, ...string) (*SDK, error) {
		return sdk, nil
	}
}

func fakeFindJDK(jdk *toolchain.JDK) func(toolchain.Requirement, ...string) (*toolchain.JDK, error) {
	return func(toolchain.Requirement, ...string) (*toolchain.JDK, error) {
		return jdk, nil
	}
}
