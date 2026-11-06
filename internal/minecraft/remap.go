package minecraft

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/gabriel-aplok/kine-build-java/internal/core"
	"github.com/gabriel-aplok/kine-build-java/internal/hash"
	"github.com/gabriel-aplok/kine-build-java/internal/toolchain"
)

const (
	fromNamespace = "named"
	toNamespace   = "intermediary"
)

func (p *Provider) remapTask(workspaceRoot string, project *core.Project, options Options, jdk *toolchain.JDK) (*core.Task, error) {
	data, err := os.ReadFile(options.Mappings)
	if err != nil {
		return nil, fmt.Errorf("read mappings: %w", err)
	}
	mappings, err := ParseMappings(data)
	if err != nil {
		return nil, fmt.Errorf("mappings %s: %w", options.Mappings, err)
	}
	for _, namespace := range []string{fromNamespace, toNamespace} {
		if !containsString(mappings.Namespaces, namespace) {
			return nil, fmt.Errorf("mappings %s has no %q namespace", options.Mappings, namespace)
		}
	}

	mappingsHash, err := hash.File(options.Mappings)
	if err != nil {
		return nil, fmt.Errorf("mappings: %w", err)
	}
	remapperHash, err := hash.File(options.Remapper)
	if err != nil {
		return nil, fmt.Errorf("remapper: %w", err)
	}

	input, err := relativePath(workspaceRoot, filepath.Join(project.Dir, "build", "libs", options.ModID+"-"+options.ModVersion+".jar"))
	if err != nil {
		return nil, err
	}
	output, err := relativePath(workspaceRoot, filepath.Join(project.Dir, "build", "libs", options.ModID+"-"+options.ModVersion+"-remapped.jar"))
	if err != nil {
		return nil, err
	}

	return &core.Task{
		ID:      "remap",
		Deps:    []string{project.ID + ":mod-jar"},
		Inputs:  []core.Input{{Path: input, Mode: core.ModeFile}},
		Outputs: []core.Output{{Path: output, Mode: core.ModeFile}},
		Key: fmt.Sprintf(
			"remap %s %s %s=%s mappings=%s remapper=%s",
			options.ModID,
			options.ModVersion,
			fromNamespace,
			toNamespace,
			mappingsHash,
			remapperHash,
		),
		Run: func(ctx context.Context) error {
			return p.remap(ctx, project, options, jdk)
		},
	}, nil
}

func (p *Provider) remap(ctx context.Context, project *core.Project, options Options, jdk *toolchain.JDK) error {
	libs := filepath.Join(project.Dir, "build", "libs")
	input := filepath.Join(libs, options.ModID+"-"+options.ModVersion+".jar")
	output := filepath.Join(libs, options.ModID+"-"+options.ModVersion+"-remapped.jar")
	if err := os.Remove(output); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("clean %s: %w", output, err)
	}

	tmpDir := filepath.Join(project.Dir, ".kine", "tmp")
	if err := os.MkdirAll(tmpDir, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", tmpDir, err)
	}

	result, err := p.run(ctx, toolchain.Command{
		Path: jdk.Java,
		Args: remapArgs(options, input, output),
		Dir:  project.Dir,
		Env:  toolchain.BuildEnv(jdk, tmpDir, nil),
	})
	if err != nil {
		return toolError("tiny-remapper", result, err)
	}
	return nil
}

func remapArgs(options Options, input, output string) []string {
	args := []string{
		toolchain.HeapArg("-Xmx", toolchain.DefaultCompilerHeap),
		"-cp", options.Remapper,
		"net.fabricmc.tinyremapper.Main",
		input,
		output,
		options.Mappings,
		fromNamespace,
		toNamespace,
	}
	args = append(args, options.MinecraftJar)
	return append(args, options.Classpath...)
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
