package android

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/gabriel-aplok/kine-build-java/internal/core"
	"github.com/gabriel-aplok/kine-build-java/internal/toolchain"
)

const (
	nativeDirName  = "native"
	nativeBuildDir = "native-build"
)

func (p *Provider) nativeTask(workspaceRoot string, project *core.Project, options Options, sdk *SDK, jdk *toolchain.JDK) (*core.Task, error) {
	if options.Native == nil {
		return nil, nil
	}
	if sdk.NDK == "" || sdk.CMake == "" || sdk.Ninja == "" {
		return nil, fmt.Errorf("native build requires NDK and CMake")
	}

	sourceDir, err := relativePath(workspaceRoot, filepath.Dir(options.Native.CMake))
	if err != nil {
		return nil, err
	}
	output, err := relativePath(workspaceRoot, filepath.Join(project.Dir, "build", "android", nativeDirName))
	if err != nil {
		return nil, err
	}

	return &core.Task{
		ID:      "android-native",
		Inputs:  []core.Input{{Path: sourceDir, Mode: core.ModeTree}},
		Outputs: []core.Output{{Path: output, Mode: core.ModeTree}},
		Key: fmt.Sprintf(
			"cmake %s v=%d ndk=%s abi=%s build=%s stl=%s min=%d cmake_file=%s",
			sdk.CMakeVersion,
			androidTaskVersion,
			sdk.NDKVersion,
			strings.Join(options.Native.ABIs, ","),
			options.Native.BuildType,
			options.Native.STL,
			options.MinSDK,
			filepath.ToSlash(options.Native.CMake),
		),
		Run: func(ctx context.Context) error {
			return p.buildNative(ctx, project, options, sdk, jdk)
		},
	}, nil
}

func (p *Provider) buildNative(ctx context.Context, project *core.Project, options Options, sdk *SDK, jdk *toolchain.JDK) error {
	return p.buildNativeOptions(ctx, project, options.Native, options.MinSDK, sdk, jdk,
		filepath.Join(project.Dir, "build", "android", nativeDirName),
		filepath.Join(project.Dir, "build", "android", nativeBuildDir))
}

func (p *Provider) buildNativeOptions(ctx context.Context, project *core.Project, native *NativeOptions, minSDK int, sdk *SDK, jdk *toolchain.JDK, outputRoot, buildRoot string) error {
	if native == nil {
		return fmt.Errorf("native options are nil")
	}
	if err := os.RemoveAll(outputRoot); err != nil {
		return fmt.Errorf("clean %s: %w", outputRoot, err)
	}
	if err := os.RemoveAll(buildRoot); err != nil {
		return fmt.Errorf("clean %s: %w", buildRoot, err)
	}

	tmpDir := filepath.Join(project.Dir, ".kine", "tmp")
	if err := os.MkdirAll(tmpDir, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", tmpDir, err)
	}
	env := toolchain.BuildEnv(jdk, tmpDir, nil)

	for _, abi := range native.ABIs {
		outputDir := filepath.Join(outputRoot, abi)
		buildDir := filepath.Join(buildRoot, abi)
		if err := os.MkdirAll(outputDir, 0o755); err != nil {
			return fmt.Errorf("create %s: %w", outputDir, err)
		}
		if err := os.MkdirAll(buildDir, 0o755); err != nil {
			return fmt.Errorf("create %s: %w", buildDir, err)
		}

		result, err := p.run(ctx, toolchain.Command{
			Path: sdk.CMake,
			Args: nativeConfigureArgs(native, minSDK, sdk, abi, buildDir, outputDir),
			Dir:  project.Dir,
			Env:  env,
		})
		if err != nil {
			return toolError("cmake configure", result, err)
		}

		result, err = p.run(ctx, toolchain.Command{
			Path: sdk.CMake,
			Args: []string{"--build", buildDir},
			Dir:  project.Dir,
			Env:  env,
		})
		if err != nil {
			return toolError("cmake build", result, err)
		}

		if err := collectSharedLibraries(buildDir, outputDir); err != nil {
			return err
		}
	}
	return nil
}

func nativeConfigureArgs(native *NativeOptions, minSDK int, sdk *SDK, abi, buildDir, outputDir string) []string {
	args := []string{
		"-S", filepath.Dir(native.CMake),
		"-B", buildDir,
		"-G", "Ninja",
		"-DCMAKE_MAKE_PROGRAM=" + sdk.Ninja,
		"-DCMAKE_TOOLCHAIN_FILE=" + filepath.Join(sdk.NDK, "build", "cmake", "android.toolchain.cmake"),
		"-DANDROID_ABI=" + abi,
		"-DANDROID_PLATFORM=android-" + strconv.Itoa(minSDK),
		"-DCMAKE_BUILD_TYPE=" + native.BuildType,
		"-DCMAKE_LIBRARY_OUTPUT_DIRECTORY=" + outputDir,
		"-DCMAKE_RUNTIME_OUTPUT_DIRECTORY=" + outputDir,
	}
	if native.STL != "" {
		args = append(args, "-DANDROID_STL="+native.STL)
	}
	return args
}

func collectSharedLibraries(buildDir, outputDir string) error {
	found := make(map[string]string)
	err := filepath.WalkDir(buildDir, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".so") {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		found[entry.Name()] = path
		return nil
	})
	if err != nil {
		return fmt.Errorf("scan native build: %w", err)
	}
	if len(found) == 0 {
		if hasSharedLibrary(outputDir) {
			return nil
		}
		return fmt.Errorf("cmake build produced no shared libraries in %s", buildDir)
	}

	names := make([]string, 0, len(found))
	for name := range found {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		source := found[name]
		target := filepath.Join(outputDir, name)
		if source == target {
			continue
		}
		if err := copyFile(source, target); err != nil {
			return err
		}
	}
	if !hasSharedLibrary(outputDir) {
		return fmt.Errorf("cmake build produced no shared libraries in %s", outputDir)
	}
	return nil
}

func hasSharedLibrary(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".so") {
			continue
		}
		info, err := entry.Info()
		if err == nil && info.Mode().IsRegular() {
			return true
		}
	}
	return false
}

func copyFile(source, target string) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Dir(target), err)
	}
	input, err := os.Open(source)
	if err != nil {
		return fmt.Errorf("open %s: %w", source, err)
	}
	defer input.Close()

	output, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return fmt.Errorf("create %s: %w", target, err)
	}
	if _, err := io.Copy(output, input); err != nil {
		output.Close()
		return fmt.Errorf("copy %s: %w", source, err)
	}
	if err := output.Close(); err != nil {
		return fmt.Errorf("close %s: %w", target, err)
	}
	return nil
}

func localNativeLibraries(root string) ([]nativeLibrary, error) {
	if !isDir(root) {
		return nil, nil
	}
	return nativeLibrariesFromDirs([]string{root})
}

func nativeLibrariesFromDirs(roots []string) ([]nativeLibrary, error) {
	var result []nativeLibrary
	for _, root := range roots {
		if !isDir(root) {
			continue
		}
		err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".so") {
				return nil
			}
			relative, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			parts := strings.Split(filepath.ToSlash(relative), "/")
			if len(parts) != 2 {
				return fmt.Errorf("native library %s must be under one ABI directory", path)
			}
			result = append(result, nativeLibrary{
				abi:  parts[0],
				name: parts[1],
				path: path,
			})
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("scan native libraries: %w", err)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].abi != result[j].abi {
			return result[i].abi < result[j].abi
		}
		return result[i].name < result[j].name
	})
	return result, nil
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
