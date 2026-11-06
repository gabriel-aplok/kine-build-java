package jvm

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/gabriel-aplok/kine-build-java/internal/core"
	"github.com/gabriel-aplok/kine-build-java/internal/toolchain"
)

func (p *Provider) addRunTask(b *build) error {
	if b.options.MainClass == "" {
		return nil
	}

	inputs, err := p.runInputs(b)
	if err != nil {
		return err
	}

	b.tasks = append(b.tasks, &core.Task{
		ID:     "run",
		Deps:   mainCompileTaskDeps(b),
		Inputs: inputs,
		Key:    "run " + b.options.MainClass,
		Run: func(ctx context.Context) error {
			return p.runMain(ctx, b)
		},
	})
	return nil
}

func (p *Provider) runInputs(b *build) ([]core.Input, error) {
	var inputs []core.Input

	add := func(path string) error {
		relative, err := relativePath(p.workspaceRoot(b.project), path)
		if err != nil {
			return err
		}
		inputs = append(inputs, core.Input{Path: relative, Mode: core.ModeTree})
		return nil
	}

	if len(b.sources.Java) > 0 {
		if err := add(filepath.Join(b.project.Dir, "build", "classes", "java", "main")); err != nil {
			return nil, err
		}
	}
	if len(b.sources.Kotlin) > 0 {
		if err := add(filepath.Join(b.project.Dir, "build", "classes", "kotlin", "main")); err != nil {
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

func runArgs(classpath []string, mainClass string, extra []string) []string {
	args := []string{
		"-cp",
		strings.Join(classpath, string(os.PathListSeparator)),
		mainClass,
	}
	return append(args, extra...)
}

func (p *Provider) runMain(ctx context.Context, b *build) error {
	if b.options.MainClass == "" {
		return fmt.Errorf("main class is empty")
	}

	tmpDir := filepath.Join(b.project.Dir, ".kine", "tmp")
	if err := os.MkdirAll(tmpDir, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", tmpDir, err)
	}

	result, err := p.run(ctx, toolchain.Command{
		Path:   b.jdk.Java,
		Args:   runArgs(b.mainRuntime, b.options.MainClass, p.runArgs),
		Dir:    b.project.Dir,
		Env:    toolchain.BuildEnv(b.jdk, tmpDir, displayEnv()),
		Stdout: os.Stdout,
		Stderr: os.Stderr,
	})
	if err != nil {
		return compilerError("java", result, err)
	}
	return nil
}

var displayVariables = []string{"DISPLAY", "WAYLAND_DISPLAY", "XDG_RUNTIME_DIR", "XAUTHORITY"}

func displayEnv() []string {
	var env []string
	for _, key := range displayVariables {
		if value := os.Getenv(key); value != "" {
			env = append(env, key+"="+value)
		}
	}
	return env
}
