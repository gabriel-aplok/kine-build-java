package plugins

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/gabriel-aplok/kine-build-java/internal/fsutil"
	"github.com/gabriel-aplok/kine-build-java/internal/toolchain"
)

//go:embed java
var javaSources embed.FS

const (
	// MinHostJDK is the oldest JDK that can run the plugin host.
	MinHostJDK       = 17
	hostCompilerHeap = 256 << 20
	hostToolDir      = "plugin-host"
	apiJarName       = "kine-plugin-api.jar"
)

var fixedJarTime = time.Date(1980, 1, 1, 0, 0, 0, 0, time.UTC)

// HostBuild is the compiled host classes and the plugin API jar.
type HostBuild struct {
	Classes string
	APIJar  string
}

// CompileHost compiles the embedded host and API sources with the given JDK.
func CompileHost(ctx context.Context, workspaceRoot string, jdk *toolchain.JDK) (HostBuild, error) {
	return compileHost(ctx, workspaceRoot, jdk, toolchain.Run)
}

func compileHost(ctx context.Context, workspaceRoot string, jdk *toolchain.JDK, run func(context.Context, toolchain.Command) (toolchain.Result, error)) (HostBuild, error) {
	if jdk == nil {
		return HostBuild{}, fmt.Errorf("plugin host requires a JDK")
	}
	if jdk.Major < MinHostJDK {
		return HostBuild{}, fmt.Errorf("plugin host requires JDK %d or later, found %d", MinHostJDK, jdk.Major)
	}
	if jdk.Javac == "" {
		return HostBuild{}, fmt.Errorf("plugin host requires javac")
	}

	sources, hash, err := readSources()
	if err != nil {
		return HostBuild{}, err
	}

	parent := filepath.Join(workspaceRoot, ".kine", "tools", hostToolDir)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return HostBuild{}, fmt.Errorf("create plugin host cache %s: %w", parent, err)
	}
	target := filepath.Join(parent, hash)
	if build, ok := completeHostBuild(target); ok {
		return build, nil
	}

	temp, err := os.MkdirTemp(parent, "build-")
	if err != nil {
		return HostBuild{}, fmt.Errorf("create plugin host temp dir: %w", err)
	}
	defer os.RemoveAll(temp)

	sourceDir := filepath.Join(temp, "src")
	classDir := filepath.Join(temp, "classes")
	if err := writeSources(sourceDir, sources); err != nil {
		return HostBuild{}, err
	}
	if err := os.MkdirAll(classDir, 0o755); err != nil {
		return HostBuild{}, fmt.Errorf("create plugin host classes: %w", err)
	}

	result, err := run(ctx, toolchain.Command{
		Path: jdk.Javac,
		Args: javacHostArgs(sourceDir, classDir, sources),
		Dir:  temp,
		Env:  toolchain.BuildEnv(jdk, temp, nil),
	})
	if err != nil {
		return HostBuild{}, hostCompilerError(result, err)
	}

	apiJar := filepath.Join(temp, apiJarName)
	if err := writeAPIJar(classDir, apiJar); err != nil {
		return HostBuild{}, err
	}

	if err := fsutil.ReplaceDir(temp, target); err != nil {
		if build, ok := completeHostBuild(target); ok {
			return build, nil
		}
		return HostBuild{}, fmt.Errorf("install plugin host: %w", err)
	}
	return HostBuild{
		Classes: filepath.Join(target, "classes"),
		APIJar:  filepath.Join(target, apiJarName),
	}, nil
}

type sourceFile struct {
	path string
	data []byte
}

func readSources() ([]sourceFile, string, error) {
	var files []sourceFile
	err := fs.WalkDir(javaSources, "java", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".java") {
			return nil
		}
		data, err := javaSources.ReadFile(path)
		if err != nil {
			return err
		}
		files = append(files, sourceFile{
			path: strings.TrimPrefix(path, "java/"),
			data: data,
		})
		return nil
	})
	if err != nil {
		return nil, "", fmt.Errorf("read embedded plugin sources: %w", err)
	}
	sort.Slice(files, func(i, j int) bool {
		return files[i].path < files[j].path
	})

	hasher := sha256.New()
	for _, file := range files {
		hasher.Write([]byte(file.path))
		hasher.Write([]byte{0})
		hasher.Write(file.data)
		hasher.Write([]byte{0})
	}
	return files, hex.EncodeToString(hasher.Sum(nil)), nil
}

func writeSources(root string, sources []sourceFile) error {
	for _, source := range sources {
		path := filepath.Join(root, filepath.FromSlash(source.path))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return fmt.Errorf("create plugin source dir: %w", err)
		}
		if err := os.WriteFile(path, source.data, 0o644); err != nil {
			return fmt.Errorf("write plugin source %s: %w", source.path, err)
		}
	}
	return nil
}

func javacHostArgs(sourceDir, classDir string, sources []sourceFile) []string {
	args := []string{
		toolchain.HeapArg("-J-Xmx", hostCompilerHeap),
		"--release", "17",
		"-encoding", "UTF-8",
		"-d", classDir,
	}
	for _, source := range sources {
		args = append(args, filepath.Join(sourceDir, filepath.FromSlash(source.path)))
	}
	return args
}

func completeHostBuild(target string) (HostBuild, bool) {
	classes := filepath.Join(target, "classes")
	apiJar := filepath.Join(target, apiJarName)
	mainClass := filepath.Join(classes, "dev", "kine", "plugin", "host", "Main.class")
	if !fileExists(mainClass) || !fileExists(apiJar) {
		return HostBuild{}, false
	}
	return HostBuild{Classes: classes, APIJar: apiJar}, true
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

func writeAPIJar(classDir, jarPath string) error {
	root := filepath.Join(classDir, "dev", "kine", "plugin")
	var entries []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		name, err := filepath.Rel(classDir, path)
		if err != nil {
			return err
		}
		name = filepath.ToSlash(name)
		if strings.HasPrefix(name, "dev/kine/plugin/host/") {
			return nil
		}
		entries = append(entries, name)
		return nil
	})
	if err != nil {
		return fmt.Errorf("collect plugin api classes: %w", err)
	}
	sort.Strings(entries)

	file, err := os.Create(jarPath)
	if err != nil {
		return fmt.Errorf("create plugin api jar: %w", err)
	}
	writer := zip.NewWriter(file)
	for _, name := range entries {
		header := &zip.FileHeader{Name: name, Method: zip.Deflate, Modified: fixedJarTime}
		header.SetMode(0o644)
		entry, err := writer.CreateHeader(header)
		if err != nil {
			writer.Close()
			file.Close()
			return fmt.Errorf("create plugin api entry %s: %w", name, err)
		}
		data, err := os.ReadFile(filepath.Join(classDir, filepath.FromSlash(name)))
		if err != nil {
			writer.Close()
			file.Close()
			return fmt.Errorf("read plugin api class %s: %w", name, err)
		}
		if _, err := entry.Write(data); err != nil {
			writer.Close()
			file.Close()
			return fmt.Errorf("write plugin api entry %s: %w", name, err)
		}
	}
	if err := writer.Close(); err != nil {
		file.Close()
		return fmt.Errorf("close plugin api jar: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close plugin api jar file: %w", err)
	}
	return nil
}

func hostCompilerError(result toolchain.Result, err error) error {
	output := strings.TrimSpace(string(result.Stdout) + "\n" + string(result.Stderr))
	if output == "" {
		return fmt.Errorf("compile plugin host: %w", err)
	}
	return fmt.Errorf("compile plugin host: %w\n%s", err, output)
}
