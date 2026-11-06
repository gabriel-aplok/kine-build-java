package jvm

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/gabriel-aplok/kine-build-java/internal/core"
	"github.com/gabriel-aplok/kine-build-java/internal/deps"
	"github.com/gabriel-aplok/kine-build-java/internal/toolchain"
)

func (p *Provider) addTestTasks(ctx context.Context, b *build) error {
	hasJava := len(b.sources.TestJava) > 0
	hasKotlin := len(b.sources.TestKotlin) > 0
	hasResources := len(b.sources.TestResources) > 0
	if !hasJava && !hasKotlin && !hasResources {
		return nil
	}

	junit, err := p.resolveJUnit(ctx, b.options.JUnit)
	if err != nil {
		return err
	}
	b.junit = junit

	taskDeps := mainCompileTaskDeps(b)
	if hasKotlin {
		if err := p.addTestKotlinTask(b, taskDeps); err != nil {
			return err
		}
		taskDeps = append(taskDeps, b.project.ID+":compile-test-kotlin")
	}
	if hasJava {
		if err := p.addTestJavaTask(b, taskDeps); err != nil {
			return err
		}
		taskDeps = append(taskDeps, b.project.ID+":compile-test-java")
	}
	if hasResources {
		taskDeps = append(taskDeps, b.project.ID+":process-test-resources")
	}
	return p.addTestRunTask(b, taskDeps)
}

func mainCompileTaskDeps(b *build) []string {
	var result []string
	if len(b.sources.Java) > 0 {
		result = append(result, b.project.ID+":compile-java")
	}
	if len(b.sources.Kotlin) > 0 {
		result = append(result, b.project.ID+":compile-kotlin")
	}
	if len(b.sources.Resources) > 0 {
		result = append(result, b.project.ID+":process-resources")
	}
	return result
}

func (p *Provider) addTestKotlinTask(b *build, taskDeps []string) error {
	inputs, err := p.compileInputs(
		b,
		b.sources.TestKotlin,
		b.artifacts,
		deps.ScopeCompile,
		deps.ScopeRuntime,
		deps.ScopeTest,
	)
	if err != nil {
		return err
	}
	for _, path := range b.kotlin.kotlinClasspath {
		relative, err := relativePath(p.workspaceRoot(b.project), path)
		if err != nil {
			return err
		}
		inputs = append(inputs, core.Input{Path: relative, Mode: core.ModeFile})
	}
	if b.kotlin.composePlugin != "" {
		relative, err := relativePath(p.workspaceRoot(b.project), b.kotlin.composePlugin)
		if err != nil {
			return err
		}
		inputs = append(inputs, core.Input{Path: relative, Mode: core.ModeFile})
	}

	output := filepath.Join(b.project.Dir, "build", "classes", "kotlin", "test")
	relativeOutput, err := relativePath(p.workspaceRoot(b.project), output)
	if err != nil {
		return err
	}

	b.tasks = append(b.tasks, &core.Task{
		ID:      "compile-test-kotlin",
		Deps:    append([]string(nil), taskDeps...),
		Inputs:  inputs,
		Outputs: []core.Output{{Path: relativeOutput, Mode: core.ModeTree}},
		Key: fmt.Sprintf(
			"kotlin-test %s jvm-target %d compose=%t",
			b.options.Kotlin,
			b.options.Java,
			b.options.Compose,
		),
		Run: func(ctx context.Context) error {
			return p.compileKotlinSourceSet(
				ctx,
				b,
				b.sources.TestKotlin,
				b.sources.TestJava,
				output,
				b.testCompile,
			)
		},
	})
	return nil
}

func (p *Provider) addTestJavaTask(b *build, taskDeps []string) error {
	inputs, err := p.compileInputs(
		b,
		b.sources.TestJava,
		b.artifacts,
		deps.ScopeCompile,
		deps.ScopeRuntime,
		deps.ScopeTest,
	)
	if err != nil {
		return err
	}

	output := filepath.Join(b.project.Dir, "build", "classes", "java", "test")
	relativeOutput, err := relativePath(p.workspaceRoot(b.project), output)
	if err != nil {
		return err
	}

	b.tasks = append(b.tasks, &core.Task{
		ID:     "compile-test-java",
		Deps:   append([]string(nil), taskDeps...),
		Inputs: inputs,
		Outputs: []core.Output{
			{Path: relativeOutput, Mode: core.ModeTree},
			{Path: relativeOutput + ".javac-state.json", Mode: core.ModeFile},
		},
		Key: fmt.Sprintf("javac-test jdk=%s release=%d", b.jdk.Version, b.options.Java),
		Run: func(ctx context.Context) error {
			return p.compileJavaSourceSet(ctx, b, b.sources.TestJava, output, b.testCompile)
		},
	})
	return nil
}

func (p *Provider) addTestRunTask(b *build, taskDeps []string) error {
	inputs, err := p.testInputs(b)
	if err != nil {
		return err
	}

	output := filepath.Join(b.project.Dir, "build", "test-results")
	relativeOutput, err := relativePath(p.workspaceRoot(b.project), output)
	if err != nil {
		return err
	}

	b.tasks = append(b.tasks, &core.Task{
		ID:      "test",
		Deps:    append([]string(nil), taskDeps...),
		Inputs:  inputs,
		Outputs: []core.Output{{Path: relativeOutput, Mode: core.ModeTree}},
		Key:     "junit " + b.options.JUnit,
		Run: func(ctx context.Context) error {
			return p.runTests(ctx, b)
		},
	})
	return nil
}

func (p *Provider) testInputs(b *build) ([]core.Input, error) {
	var inputs []core.Input

	add := func(path string) error {
		relative, err := relativePath(p.workspaceRoot(b.project), path)
		if err != nil {
			return err
		}
		inputs = append(inputs, core.Input{Path: relative, Mode: core.ModeTree})
		return nil
	}

	if len(b.sources.TestJava) > 0 {
		if err := add(filepath.Join(b.project.Dir, "build", "classes", "java", "test")); err != nil {
			return nil, err
		}
	}
	if len(b.sources.TestKotlin) > 0 {
		if err := add(filepath.Join(b.project.Dir, "build", "classes", "kotlin", "test")); err != nil {
			return nil, err
		}
	}
	if len(b.sources.TestResources) > 0 {
		if err := add(filepath.Join(b.project.Dir, "build", "resources", "test")); err != nil {
			return nil, err
		}
	}
	if len(b.sources.Resources) > 0 {
		if err := add(filepath.Join(b.project.Dir, "build", "resources", "main")); err != nil {
			return nil, err
		}
	}
	return inputs, nil
}

func (p *Provider) runTests(ctx context.Context, b *build) error {
	if b.junit == nil {
		return fmt.Errorf("junit tools are not resolved")
	}

	output := filepath.Join(b.project.Dir, "build", "test-results")
	if err := os.MkdirAll(output, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", output, err)
	}
	tmpDir := filepath.Join(b.project.Dir, ".kine", "tmp")
	if err := os.MkdirAll(tmpDir, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", tmpDir, err)
	}

	result, err := p.run(ctx, toolchain.Command{
		Path: b.jdk.Java,
		Args: junitArgs(b.junit, b.testRuntime, testClassDirs(b), output),
		Dir:  b.project.Dir,
		Env:  toolchain.BuildEnv(b.jdk, tmpDir, nil),
	})
	if err != nil {
		return compilerError("junit", result, err)
	}
	return nil
}

func junitArgs(tools *toolSet, classpath, scanDirs []string, results string) []string {
	args := []string{
		toolchain.HeapArg("-Xmx", toolchain.DefaultCompilerHeap),
		"-jar", tools.junitConsole,
		"--class-path", strings.Join(classpath, string(os.PathListSeparator)),
	}
	if len(scanDirs) > 0 {
		args = append(args, "--scan-class-path="+strings.Join(scanDirs, string(os.PathListSeparator)))
	}
	return append(args, "--reports-dir", results, "--disable-banner")
}

func testClassDirs(b *build) []string {
	var dirs []string
	if len(b.sources.TestJava) > 0 {
		dirs = append(dirs, filepath.Join(b.project.Dir, "build", "classes", "java", "test"))
	}
	if len(b.sources.TestKotlin) > 0 {
		dirs = append(dirs, filepath.Join(b.project.Dir, "build", "classes", "kotlin", "test"))
	}
	return dirs
}
