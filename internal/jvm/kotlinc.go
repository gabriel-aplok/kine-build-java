package jvm

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gabriel-aplok/kine-build-java/internal/core"
	"github.com/gabriel-aplok/kine-build-java/internal/deps"
	"github.com/gabriel-aplok/kine-build-java/internal/toolchain"
)

func (p *Provider) addKotlinTask(b *build) error {
	if len(b.sources.Kotlin) == 0 {
		return nil
	}
	if b.kotlin == nil {
		return fmt.Errorf("kotlin tools are not resolved")
	}

	inputs, err := p.compileInputs(b, b.sources.Kotlin, b.artifacts, deps.ScopeCompile, deps.ScopeProvided)
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

	output := filepath.Join(b.project.Dir, "build", "classes", "kotlin", "main")
	relativeOutput, err := relativePath(p.workspaceRoot(b.project), output)
	if err != nil {
		return err
	}

	b.tasks = append(b.tasks, &core.Task{
		ID:      "compile-kotlin",
		Deps:    projectTaskDependencies(b.project),
		Inputs:  inputs,
		Outputs: []core.Output{{Path: relativeOutput, Mode: core.ModeTree}},
		Key: fmt.Sprintf(
			"kotlin %s jvm-target %d compose=%t",
			b.options.Kotlin,
			b.options.Java,
			b.options.Compose,
		),
		Run: func(ctx context.Context) error {
			return p.compileKotlin(ctx, b)
		},
	})
	return nil
}

func (p *Provider) compileKotlin(ctx context.Context, b *build) error {
	return p.compileKotlinSourceSet(
		ctx,
		b,
		b.sources.Kotlin,
		b.sources.Java,
		filepath.Join(b.project.Dir, "build", "classes", "kotlin", "main"),
		b.mainCompile,
	)
}

func (p *Provider) compileKotlinSourceSet(ctx context.Context, b *build, sourceDirs, javaSources []string, output string, classpath []string) error {
	if b.kotlin == nil {
		return fmt.Errorf("kotlin tools are not resolved")
	}

	if err := os.MkdirAll(output, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", output, err)
	}

	sources, err := sourceFiles(sourceDirs, ".kt")
	if err != nil {
		return err
	}
	if len(sources) == 0 {
		return nil
	}

	tmpDir := filepath.Join(b.project.Dir, ".kine", "tmp")
	if err := os.MkdirAll(tmpDir, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", tmpDir, err)
	}

	result, err := p.run(ctx, toolchain.Command{
		Path: b.jdk.Java,
		Args: kotlincArgs(
			b.kotlin,
			b.options.Java,
			output,
			classpath,
			javaSources,
			sources,
			b.options.Compose,
		),
		Dir: b.project.Dir,
		Env: toolchain.BuildEnv(b.jdk, tmpDir, nil),
	})
	if err != nil {
		return compilerError("kotlin", result, err)
	}
	return nil
}

func kotlincArgs(tools *toolSet, release int, output string, classpath, javaSources, sources []string, compose bool) []string {
	args := []string{
		toolchain.HeapArg("-Xmx", toolchain.DefaultCompilerHeap),
		"-cp", strings.Join(tools.kotlinClasspath, string(os.PathListSeparator)),
		"org.jetbrains.kotlin.cli.jvm.K2JVMCompiler",
		"-no-stdlib",
		"-no-reflect",
	}
	if target := kotlinJvmTarget(release); target != "" {
		args = append(args, "-jvm-target", target)
	}
	if len(classpath) > 0 {
		args = append(args, "-classpath", strings.Join(classpath, string(os.PathListSeparator)))
	}
	args = append(args, "-d", output)
	if len(javaSources) > 0 {
		args = append(args, "-Xjava-source-roots="+strings.Join(javaSources, string(os.PathListSeparator)))
	}
	if compose {
		args = append(args, "-Xplugin="+tools.composePlugin)
	}
	return append(args, sources...)
}

func kotlinJvmTarget(major int) string {
	if major <= 0 {
		return ""
	}
	if major == 8 {
		return "1.8"
	}
	return strconv.Itoa(major)
}
