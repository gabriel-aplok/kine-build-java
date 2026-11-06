package jvm

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/gabriel-aplok/kine-build-java/internal/core"
	"github.com/gabriel-aplok/kine-build-java/internal/toolchain"
)

func TestJunitArgs(t *testing.T) {
	separator := string(os.PathListSeparator)
	tools := &toolSet{junitConsole: "/tools/junit.jar"}

	args := junitArgs(
		tools,
		[]string{"/cp/a.jar", "/cp/b.jar"},
		[]string{"/test/classes"},
		"/results",
	)
	want := []string{
		"-Xmx" + strconv.FormatInt(toolchain.DefaultCompilerHeap, 10),
		"-jar", "/tools/junit.jar",
		"--class-path", "/cp/a.jar" + separator + "/cp/b.jar",
		"--scan-class-path=/test/classes",
		"--reports-dir", "/results",
		"--disable-banner",
	}
	if !reflect.DeepEqual(args, want) {
		t.Errorf("junitArgs() = %v, want %v", args, want)
	}
}

func TestRunTests(t *testing.T) {
	root := t.TempDir()
	srcDir := filepath.Join(root, "src/test/java")
	writeFile(t, filepath.Join(srcDir, "AppTest.java"), "class AppTest {}")

	provider := newTestProvider(t, root)
	var recorded toolchain.Command
	provider.run = func(ctx context.Context, command toolchain.Command) (toolchain.Result, error) {
		recorded = command
		return toolchain.Result{}, nil
	}

	jdk := &toolchain.JDK{Home: "/fake/jdk", Version: "21.0.1", Major: 21, Java: "/fake/jdk/bin/java"}
	build := &build{
		project:     &core.Project{ID: "app", Dir: root, Provider: "jvm"},
		options:     mustOptions(t, nil),
		sources:     sourceDirs{TestJava: []string{srcDir}},
		jdk:         jdk,
		junit:       &toolSet{junitConsole: "/tools/junit.jar"},
		testRuntime: []string{"/cp/a.jar"},
	}

	if err := provider.runTests(context.Background(), build); err != nil {
		t.Fatalf("runTests() error = %v", err)
	}
	if recorded.Path != jdk.Java {
		t.Errorf("Path = %q, want %q", recorded.Path, jdk.Java)
	}
	if !contains(recorded.Args, "-jar") || !contains(recorded.Args, "/tools/junit.jar") {
		t.Errorf("args = %v, want -jar console", recorded.Args)
	}
	if !contains(recorded.Args, "/cp/a.jar") {
		t.Errorf("args = %v, want the classpath", recorded.Args)
	}
	scan := filepath.Join(root, "build", "classes", "java", "test")
	if !contains(recorded.Args, "--scan-class-path="+scan) {
		t.Errorf("args = %v, want the scan path", recorded.Args)
	}
	if !contains(recorded.Args, filepath.Join(root, "build", "test-results")) {
		t.Errorf("args = %v, want the results dir", recorded.Args)
	}
}

func TestRunTestsError(t *testing.T) {
	root := t.TempDir()
	srcDir := filepath.Join(root, "src/test/java")
	writeFile(t, filepath.Join(srcDir, "AppTest.java"), "class AppTest {}")

	provider := newTestProvider(t, root)
	provider.run = func(ctx context.Context, command toolchain.Command) (toolchain.Result, error) {
		return toolchain.Result{Stderr: []byte("test failed")}, &toolchain.ExitError{Code: 1}
	}
	build := &build{
		project: &core.Project{ID: "app", Dir: root, Provider: "jvm"},
		options: mustOptions(t, nil),
		sources: sourceDirs{TestJava: []string{srcDir}},
		jdk:     &toolchain.JDK{Home: "/fake/jdk", Version: "21.0.1", Major: 21, Java: "/fake/jdk/bin/java"},
		junit:   &toolSet{junitConsole: "/tools/junit.jar"},
	}

	err := provider.runTests(context.Background(), build)
	if err == nil {
		t.Fatal("runTests() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "test failed") {
		t.Errorf("error %q does not contain test output", err)
	}
	var exitErr *toolchain.ExitError
	if !errors.As(err, &exitErr) {
		t.Errorf("error %v is not an ExitError", err)
	}
}

func TestTasksIncludeTest(t *testing.T) {
	provider, workspaceRoot := newToolTestProvider(t)
	projectDir := filepath.Join(workspaceRoot, "app")
	writeFile(t, filepath.Join(projectDir, "src/test/java", "AppTest.java"), "class AppTest {}")

	project := &core.Project{ID: "app", Dir: projectDir, Provider: "jvm"}
	tasks, err := provider.Tasks(context.Background(), project)
	if err != nil {
		t.Fatalf("Tasks() error = %v", err)
	}

	compileTest := findTask(tasks, "compile-test-java")
	if compileTest == nil {
		t.Fatalf("compile-test-java missing: %v", taskIDs(tasks))
	}
	if len(compileTest.Outputs) != 2 || compileTest.Outputs[0].Path != "app/build/classes/java/test" ||
		compileTest.Outputs[1].Path != "app/build/classes/java/test.javac-state.json" {
		t.Errorf("Outputs = %+v", compileTest.Outputs)
	}

	testTask := findTask(tasks, "test")
	if testTask == nil {
		t.Fatalf("test missing: %v", taskIDs(tasks))
	}
	if len(testTask.Outputs) != 1 || testTask.Outputs[0].Path != "app/build/test-results" {
		t.Errorf("Outputs = %+v", testTask.Outputs)
	}
	if !contains(testTask.Deps, "app:compile-test-java") {
		t.Errorf("Deps = %v, want compile-test-java", testTask.Deps)
	}
	if testTask.Key != "junit "+defaultJUnit {
		t.Errorf("Key = %q", testTask.Key)
	}
}

func TestTasksIncludeTestKotlin(t *testing.T) {
	provider, workspaceRoot := newToolTestProvider(t)
	projectDir := filepath.Join(workspaceRoot, "app")
	writeFile(t, filepath.Join(projectDir, "src/test/kotlin", "AppTest.kt"), "class AppTest")

	project := &core.Project{
		ID:       "app",
		Dir:      projectDir,
		Provider: "jvm",
		Options:  optionsFromTOML(t, `kotlin = "2.0.21"`),
	}
	tasks, err := provider.Tasks(context.Background(), project)
	if err != nil {
		t.Fatalf("Tasks() error = %v", err)
	}

	compileTest := findTask(tasks, "compile-test-kotlin")
	if compileTest == nil {
		t.Fatalf("compile-test-kotlin missing: %v", taskIDs(tasks))
	}
	if len(compileTest.Outputs) != 1 || compileTest.Outputs[0].Path != "app/build/classes/kotlin/test" {
		t.Errorf("Outputs = %+v", compileTest.Outputs)
	}

	testTask := findTask(tasks, "test")
	if testTask == nil {
		t.Fatal("test task missing")
	}
	if !contains(testTask.Deps, "app:compile-test-kotlin") {
		t.Errorf("Deps = %v, want compile-test-kotlin", testTask.Deps)
	}
}
