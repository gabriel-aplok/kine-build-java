package desktop

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/gabriel-aplok/kine-build-java/internal/core"
	"github.com/gabriel-aplok/kine-build-java/internal/jvm"
	"github.com/gabriel-aplok/kine-build-java/internal/toolchain"
)

const desktopHeap = 512 << 20

func findJpackage(jdk *toolchain.JDK) (string, error) {
	path, err := toolchain.ToolPath(jdk, "jpackage")
	if err != nil {
		return "", fmt.Errorf("JDK %s has no jpackage, requires JDK 17 or later", jdk.Version)
	}
	return path, nil
}

func (p *Provider) packageTask(workspaceRoot string, project *core.Project, options Options, jvmOptions jvm.Options, jdk *toolchain.JDK, jpackage string) (*core.Task, error) {
	jarPath := filepath.Join(project.Dir, "build", "libs", jvmOptions.Jar)
	jar, err := relativePath(workspaceRoot, jarPath)
	if err != nil {
		return nil, err
	}

	inputs := []core.Input{{Path: jar, Mode: core.ModeFile}}
	if options.Package.Icon != "" {
		icon, err := relativePath(workspaceRoot, options.Package.Icon)
		if err != nil {
			return nil, err
		}
		inputs = append(inputs, core.Input{Path: icon, Mode: core.ModeFile})
	}

	outputPath := filepath.Join(project.Dir, filepath.FromSlash(options.Package.Dest), options.Package.Name)
	output, err := relativePath(workspaceRoot, outputPath)
	if err != nil {
		return nil, err
	}

	return &core.Task{
		ID:      "package",
		Deps:    []string{project.ID + ":jar"},
		Inputs:  inputs,
		Outputs: []core.Output{{Path: output, Mode: core.ModeTree}},
		Key:     packageCacheKey(jdk, options.Package, jvmOptions),
		Run: func(ctx context.Context) error {
			return p.packageApp(ctx, project, options, jvmOptions, jdk, jpackage)
		},
	}, nil
}

func packageCacheKey(jdk *toolchain.JDK, options PackageOptions, jvmOptions jvm.Options) string {
	return fmt.Sprintf(
		"jpackage %s name=%s version=%s vendor=%s type=%s main=%s jar=%s options=%s",
		jdk.Version,
		options.Name,
		options.Version,
		options.Vendor,
		options.Type,
		jvmOptions.MainClass,
		jvmOptions.Jar,
		strings.Join(options.JavaOptions, "\x00"),
	)
}

func (p *Provider) packageApp(ctx context.Context, project *core.Project, options Options, jvmOptions jvm.Options, jdk *toolchain.JDK, jpackage string) error {
	dest := filepath.Join(project.Dir, filepath.FromSlash(options.Package.Dest))
	image := filepath.Join(dest, options.Package.Name)
	if err := os.RemoveAll(image); err != nil {
		return fmt.Errorf("remove old image %s: %w", image, err)
	}
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", dest, err)
	}

	tmpDir := filepath.Join(project.Dir, ".kine", "tmp", "jpackage")
	if err := os.MkdirAll(tmpDir, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", tmpDir, err)
	}

	result, err := p.run(ctx, toolchain.Command{
		Path: jpackage,
		Args: packageArgs(project, options, jvmOptions, tmpDir),
		Dir:  project.Dir,
		Env:  toolchain.BuildEnv(jdk, tmpDir, nil),
	})
	if err != nil {
		return jpackageError(result, err)
	}
	return nil
}

func packageArgs(project *core.Project, options Options, jvmOptions jvm.Options, tmpDir string) []string {
	args := []string{
		toolchain.HeapArg("-J-Xmx", desktopHeap),
		"--type", options.Package.Type,
		"--name", options.Package.Name,
		"--app-version", options.Package.Version,
		"--vendor", options.Package.Vendor,
		"--input", filepath.Join(project.Dir, "build", "libs"),
		"--main-jar", jvmOptions.Jar,
		"--main-class", jvmOptions.MainClass,
		"--dest", filepath.Join(project.Dir, filepath.FromSlash(options.Package.Dest)),
		"--temp", tmpDir,
	}
	if options.Package.Icon != "" {
		args = append(args, "--icon", options.Package.Icon)
	}
	for _, option := range options.Package.JavaOptions {
		args = append(args, "--java-options", option)
	}
	return args
}

func jpackageError(result toolchain.Result, err error) error {
	output := strings.TrimSpace(string(result.Stdout) + "\n" + string(result.Stderr))
	if output == "" {
		return fmt.Errorf("jpackage: %w", err)
	}
	return fmt.Errorf("jpackage: %w\n%s", err, output)
}

func relativePath(root, path string) (string, error) {
	relative, err := filepath.Rel(root, path)
	if err != nil {
		return "", fmt.Errorf("relative path %s: %w", path, err)
	}
	if relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path %q escapes the workspace", path)
	}
	return filepath.ToSlash(relative), nil
}
