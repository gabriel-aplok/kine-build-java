package jvm

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/gabriel-aplok/kine-build-java/internal/core"
	"github.com/gabriel-aplok/kine-build-java/internal/toolchain"
)

func TestKotlincArgs(t *testing.T) {
	separator := string(os.PathListSeparator)
	tools := &toolSet{kotlinClasspath: []string{"/tools/compiler.jar", "/tools/stdlib.jar"}}

	args := kotlincArgs(
		tools,
		21,
		"/out",
		[]string{"/deps/a.jar"},
		[]string{"/src/main/java"},
		[]string{"/src/main/kotlin/A.kt"},
		false,
	)
	want := []string{
		"-Xmx" + strconv.FormatInt(toolchain.DefaultCompilerHeap, 10),
		"-cp", "/tools/compiler.jar" + separator + "/tools/stdlib.jar",
		"org.jetbrains.kotlin.cli.jvm.K2JVMCompiler",
		"-no-stdlib",
		"-no-reflect",
		"-jvm-target", "21",
		"-classpath", "/deps/a.jar",
		"-d", "/out",
		"-Xjava-source-roots=/src/main/java",
		"/src/main/kotlin/A.kt",
	}
	if !reflect.DeepEqual(args, want) {
		t.Errorf("kotlincArgs() = %v, want %v", args, want)
	}
}

func TestKotlincArgsCompose(t *testing.T) {
	tools := &toolSet{
		kotlinClasspath: []string{"/tools/compiler.jar"},
		composePlugin:   "/tools/compose.jar",
	}

	args := kotlincArgs(tools, 0, "/out", nil, nil, []string{"/A.kt"}, true)
	if !contains(args, "-Xplugin=/tools/compose.jar") {
		t.Errorf("args = %v, want the compose plugin", args)
	}
	if contains(args, "-jvm-target") {
		t.Errorf("args = %v, want no jvm target", args)
	}
}

func TestKotlinJvmTarget(t *testing.T) {
	tests := []struct {
		major int
		want  string
	}{
		{major: 8, want: "1.8"},
		{major: 17, want: "17"},
		{major: 21, want: "21"},
		{major: 0, want: ""},
	}
	for _, tt := range tests {
		if got := kotlinJvmTarget(tt.major); got != tt.want {
			t.Errorf("kotlinJvmTarget(%d) = %q, want %q", tt.major, got, tt.want)
		}
	}
}

func TestCompileKotlin(t *testing.T) {
	root := t.TempDir()
	srcDir := filepath.Join(root, "src/main/kotlin")
	writeFile(t, filepath.Join(srcDir, "A.kt"), "fun main() {}")

	provider := newTestProvider(t, root)
	var recorded toolchain.Command
	provider.run = func(ctx context.Context, command toolchain.Command) (toolchain.Result, error) {
		recorded = command
		return toolchain.Result{}, nil
	}

	jdk := &toolchain.JDK{Home: "/fake/jdk", Version: "21.0.1", Major: 21, Java: "/fake/jdk/bin/java"}
	build := &build{
		project: &core.Project{ID: "app", Dir: root, Provider: "jvm"},
		options: mustOptions(t, optionsFromTOML(t, `kotlin = "2.0.21"`)),
		sources: sourceDirs{Kotlin: []string{srcDir}},
		jdk:     jdk,
		kotlin:  &toolSet{kotlinClasspath: []string{"/tools/compiler.jar"}},
	}

	if err := provider.compileKotlin(context.Background(), build); err != nil {
		t.Fatalf("compileKotlin() error = %v", err)
	}
	if recorded.Path != jdk.Java {
		t.Errorf("Path = %q, want %q", recorded.Path, jdk.Java)
	}
	if !contains(recorded.Args, "org.jetbrains.kotlin.cli.jvm.K2JVMCompiler") {
		t.Errorf("args = %v, want the compiler main class", recorded.Args)
	}
	if !contains(recorded.Args, filepath.Join(srcDir, "A.kt")) {
		t.Errorf("args = %v, want the source", recorded.Args)
	}
	if !contains(recorded.Args, filepath.Join(root, "build", "classes", "kotlin", "main")) {
		t.Errorf("args = %v, want the output", recorded.Args)
	}
	if env := envMap(recorded.Env); env["JAVA_HOME"] != "/fake/jdk" {
		t.Errorf("JAVA_HOME = %q", env["JAVA_HOME"])
	}
}

func TestTasksIncludeKotlin(t *testing.T) {
	provider, workspaceRoot := newToolTestProvider(t)
	projectDir := filepath.Join(workspaceRoot, "app")
	writeFile(t, filepath.Join(projectDir, "src/main/kotlin", "A.kt"), "fun main() {}")

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

	task := findTask(tasks, "compile-kotlin")
	if task == nil {
		t.Fatalf("compile-kotlin missing: %v", taskIDs(tasks))
	}
	if len(task.Outputs) != 1 || task.Outputs[0].Path != "app/build/classes/kotlin/main" {
		t.Errorf("Outputs = %+v", task.Outputs)
	}
	if !strings.Contains(task.Key, "kotlin 2.0.21") {
		t.Errorf("Key = %q", task.Key)
	}
	if !hasInput(task.Inputs, "app/src/main/kotlin") {
		t.Errorf("Inputs = %+v, want the kotlin source tree", task.Inputs)
	}

	jar := findTask(tasks, "jar")
	if jar == nil || !contains(jar.Deps, "app:compile-kotlin") {
		t.Errorf("jar deps = %v, want compile-kotlin", jar)
	}
}

func TestTasksIncludeKotlinAndJava(t *testing.T) {
	provider, workspaceRoot := newToolTestProvider(t)
	projectDir := filepath.Join(workspaceRoot, "app")
	writeFile(t, filepath.Join(projectDir, "src/main/kotlin", "A.kt"), "fun main() {}")
	writeFile(t, filepath.Join(projectDir, "src/main/java", "B.java"), "class B {}")

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

	javaTask := findTask(tasks, "compile-java")
	if javaTask == nil || !contains(javaTask.Deps, "app:compile-kotlin") {
		t.Errorf("compile-java deps = %v, want compile-kotlin", javaTask)
	}
	jar := findTask(tasks, "jar")
	if jar == nil {
		t.Fatal("jar task missing")
	}
	if !contains(jar.Deps, "app:compile-java") || !contains(jar.Deps, "app:compile-kotlin") {
		t.Errorf("jar deps = %v, want both compile tasks", jar.Deps)
	}
}

func hasInput(inputs []core.Input, path string) bool {
	for _, input := range inputs {
		if input.Path == path {
			return true
		}
	}
	return false
}
