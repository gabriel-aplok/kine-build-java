package android

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gabriel-aplok/kine-build-java/internal/core"
	"github.com/gabriel-aplok/kine-build-java/internal/deps"
	"github.com/gabriel-aplok/kine-build-java/internal/toolchain"
)

const (
	debugAlias = "androiddebugkey"
	debugPass  = "android"
)

var apkFixedTime = time.Date(1980, 1, 1, 0, 0, 0, 0, time.UTC)

func (p *Provider) apkTask(workspaceRoot string, project *core.Project, options Options, sdk *SDK, jdk *toolchain.JDK, artifacts []deps.Artifact, hasLibs bool) (*core.Task, error) {
	resources, err := relativePath(workspaceRoot, options.resourcesAPKPath(project.Dir))
	if err != nil {
		return nil, err
	}
	dexDir, err := relativePath(workspaceRoot, options.androidPath(project.Dir, "dex"))
	if err != nil {
		return nil, err
	}
	output, err := relativePath(workspaceRoot, options.apkOutputPath(project.Dir))
	if err != nil {
		return nil, err
	}

	inputs := []core.Input{
		{Path: resources, Mode: core.ModeFile},
		{Path: dexDir, Mode: core.ModeTree},
	}
	taskDeps := []string{project.ID + ":" + options.taskID("android-resources"), project.ID + ":" + options.taskID("dex")}
	if hasLibs {
		libsDir, err := relativePath(workspaceRoot, filepath.Join(project.Dir, "build", "android", "dex-libs"))
		if err != nil {
			return nil, err
		}
		inputs = append(inputs, core.Input{Path: libsDir, Mode: core.ModeTree})
		taskDeps = append(taskDeps, project.ID+":dex-libs")
	}
	if options.Native != nil {
		nativeDir, err := relativePath(workspaceRoot, filepath.Join(project.Dir, "build", "android", nativeDirName))
		if err != nil {
			return nil, err
		}
		inputs = append(inputs, core.Input{Path: nativeDir, Mode: core.ModeTree})
		taskDeps = append(taskDeps, project.ID+":android-native")
	}
	for _, dir := range append(append([]string(nil), options.AARAssetDirs...), options.AssetDirs...) {
		path, err := relativePath(workspaceRoot, dir)
		if err != nil {
			return nil, err
		}
		inputs = append(inputs, core.Input{Path: path, Mode: core.ModeTree})
	}
	for _, dir := range options.AARNativeDirs {
		path, err := relativePath(workspaceRoot, dir)
		if err != nil {
			return nil, err
		}
		inputs = append(inputs, core.Input{Path: path, Mode: core.ModeTree})
	}
	for _, artifact := range artifacts {
		path, err := relativePath(workspaceRoot, artifact.Path)
		if err != nil {
			return nil, err
		}
		inputs = append(inputs, core.Input{Path: path, Mode: core.ModeFile})
	}

	signing, err := signingKey(options)
	if err != nil {
		return nil, err
	}

	return &core.Task{
		ID:      options.taskID("apk"),
		Deps:    taskDeps,
		Inputs:  inputs,
		Outputs: []core.Output{{Path: output, Mode: core.ModeFile}},
		Key: fmt.Sprintf(
			"apk %s v=%d platform=%s code=%d name=%s signing=%s variant=%s",
			sdk.BuildTools,
			androidTaskVersion,
			filepath.Base(sdk.Platform),
			options.effectiveVersionCode(),
			options.effectiveVersionName(),
			signing,
			options.variantLabel(),
		),
		Run: func(ctx context.Context) error {
			return p.buildAPK(ctx, workspaceRoot, project, options, sdk, jdk, artifacts, hasLibs)
		},
	}, nil
}

func (p *Provider) buildAPK(ctx context.Context, workspaceRoot string, project *core.Project, options Options, sdk *SDK, jdk *toolchain.JDK, artifacts []deps.Artifact, hasLibs bool) error {
	resources := options.resourcesAPKPath(project.Dir)
	dexDirs := []string{options.androidPath(project.Dir, "dex")}
	if hasLibs {
		dexDirs = append(dexDirs, filepath.Join(project.Dir, "build", "android", "dex-libs"))
	}
	outputDir := filepath.Join(project.Dir, filepath.FromSlash(options.apkOutputDir()))
	if err := os.RemoveAll(outputDir); err != nil {
		return fmt.Errorf("clean %s: %w", outputDir, err)
	}
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", outputDir, err)
	}

	baseName := strings.TrimSuffix(options.apkOutputName(), ".apk")
	unsigned := filepath.Join(outputDir, baseName+"-unsigned.apk")
	aligned := filepath.Join(outputDir, baseName+"-aligned.apk")
	final := options.apkOutputPath(project.Dir)
	signing := options.signingConfig()
	if signing != nil && signing.Unresolved {
		return fmt.Errorf("release signing environment variables are not set")
	}
	releaseUnsigned := options.Variant.BuildType == "release" && options.signingConfig() == nil
	if releaseUnsigned {
		aligned = final
	}

	natives, err := nativeLibraries(artifacts)
	if err != nil {
		return err
	}
	aarNatives, err := nativeLibrariesFromDirs(options.AARNativeDirs)
	if err != nil {
		return err
	}
	natives = mergeNatives(natives, aarNatives)
	local, err := localNativeLibraries(filepath.Join(project.Dir, "build", "android", nativeDirName))
	if err != nil {
		return err
	}
	natives = mergeNatives(natives, local)
	jarResources, err := dependencyResources(artifacts)
	if err != nil {
		return err
	}
	assets, err := assetFiles(append(append([]string(nil), options.AARAssetDirs...), options.AssetDirs...))
	if err != nil {
		return err
	}
	if err := assembleAPK(resources, dexDirs, natives, jarResources, assets, unsigned); err != nil {
		return err
	}

	tmpDir := filepath.Join(workspaceRoot, ".kine", "tmp")
	if err := os.MkdirAll(tmpDir, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", tmpDir, err)
	}
	env := toolchain.BuildEnv(jdk, tmpDir, nil)

	result, err := p.run(ctx, toolchain.Command{
		Path: sdk.Zipalign,
		Args: zipalignArgs(sdk, unsigned, aligned),
		Dir:  project.Dir,
		Env:  env,
	})
	if err != nil {
		return toolError("zipalign", result, err)
	}

	if releaseUnsigned {
		if err := os.Remove(unsigned); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove %s: %w", unsigned, err)
		}
		return nil
	}

	keystore, err := p.ensureKeystore(ctx, workspaceRoot, jdk, tmpDir, signing)
	if err != nil {
		return err
	}
	alias, storePassword, keyPassword := debugAlias, debugPass, debugPass
	if signing != nil {
		alias = signing.Alias
		storePassword = signing.StorePassword
		keyPassword = signing.KeyPassword
	}

	result, err = p.run(ctx, toolchain.Command{
		Path: sdk.ApkSigner,
		Args: []string{
			"sign",
			"--ks", keystore,
			"--ks-key-alias", alias,
			"--ks-pass", "pass:" + storePassword,
			"--key-pass", "pass:" + keyPassword,
			"--v4-signing-enabled", "false",
			"--out", final,
			aligned,
		},
		Dir: project.Dir,
		Env: env,
	})
	if err != nil {
		return toolError("apksigner", result, err)
	}

	if err := os.Remove(unsigned); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove %s: %w", unsigned, err)
	}
	if err := os.Remove(aligned); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove %s: %w", aligned, err)
	}
	return nil
}

func assembleAPK(resources string, dexDirs []string, natives []nativeLibrary, jarResources []jarResource, assets []assetFile, output string) error {
	reader, err := zip.OpenReader(resources)
	if err != nil {
		return fmt.Errorf("open %s: %w", resources, err)
	}
	defer reader.Close()

	file, err := os.Create(output)
	if err != nil {
		return fmt.Errorf("create %s: %w", output, err)
	}
	writer := zip.NewWriter(file)

	for _, entry := range reader.File {
		if entry.Name == "classes.dex" {
			continue
		}
		header := entry.FileHeader
		target, err := writer.CreateHeader(&header)
		if err != nil {
			writer.Close()
			file.Close()
			return fmt.Errorf("create apk entry %s: %w", entry.Name, err)
		}
		source, err := entry.Open()
		if err != nil {
			writer.Close()
			file.Close()
			return fmt.Errorf("open apk entry %s: %w", entry.Name, err)
		}
		_, copyErr := io.Copy(target, source)
		source.Close()
		if copyErr != nil {
			writer.Close()
			file.Close()
			return fmt.Errorf("copy apk entry %s: %w", entry.Name, copyErr)
		}
	}

	written := make(map[string]struct{})
	for _, entry := range reader.File {
		written[entry.Name] = struct{}{}
	}

	dexIndex := 1
	for _, dir := range dexDirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			writer.Close()
			file.Close()
			return fmt.Errorf("read %s: %w", dir, err)
		}
		var names []string
		for _, entry := range entries {
			if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".dex") {
				names = append(names, entry.Name())
			}
		}
		sort.Strings(names)
		for _, name := range names {
			apkName := "classes.dex"
			if dexIndex > 1 {
				apkName = fmt.Sprintf("classes%d.dex", dexIndex)
			}
			dexIndex++
			if _, ok := written[apkName]; ok {
				continue
			}
			if err := writeDex(writer, filepath.Join(dir, name), apkName); err != nil {
				writer.Close()
				file.Close()
				return err
			}
			written[apkName] = struct{}{}
		}
	}
	if dexIndex == 1 {
		writer.Close()
		file.Close()
		return fmt.Errorf("no dex files in %v", dexDirs)
	}
	for _, native := range natives {
		name := "lib/" + native.abi + "/" + native.name
		if _, ok := written[name]; ok {
			continue
		}
		if err := writeNative(writer, native); err != nil {
			writer.Close()
			file.Close()
			return err
		}
		written[name] = struct{}{}
	}
	for _, asset := range assets {
		if _, ok := written[asset.name]; ok {
			continue
		}
		if err := writeAsset(writer, asset); err != nil {
			writer.Close()
			file.Close()
			return err
		}
		written[asset.name] = struct{}{}
	}
	for _, resource := range jarResources {
		if _, ok := written[resource.name]; ok {
			continue
		}
		if err := writeJarResource(writer, resource); err != nil {
			writer.Close()
			file.Close()
			return err
		}
		written[resource.name] = struct{}{}
	}

	if err := writer.Close(); err != nil {
		file.Close()
		return fmt.Errorf("close apk: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close apk file: %w", err)
	}
	return nil
}

func zipalignArgs(sdk *SDK, unsigned, aligned string) []string {
	args := []string{"-f"}
	if buildToolsMajor(sdk) >= 35 {
		// 16 KB page alignment for uncompressed native libraries on Android 15+.
		args = append(args, "-P", "16")
	} else {
		args = append(args, "-p")
	}
	return append(args, "4", unsigned, aligned)
}

func buildToolsMajor(sdk *SDK) int {
	if sdk == nil || sdk.BuildTools == "" {
		return 0
	}
	name := filepath.Base(sdk.BuildTools)
	if index := strings.Index(name, "."); index >= 0 {
		name = name[:index]
	}
	major, _ := strconv.Atoi(name)
	return major
}

func writeDex(writer *zip.Writer, path, name string) error {
	source, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	defer source.Close()

	header := &zip.FileHeader{Name: name, Method: zip.Deflate, Modified: apkFixedTime}
	header.SetMode(0o644)
	target, err := writer.CreateHeader(header)
	if err != nil {
		return fmt.Errorf("create %s: %w", name, err)
	}
	if _, err := io.Copy(target, source); err != nil {
		return fmt.Errorf("write %s: %w", name, err)
	}
	return nil
}

type jarResource struct {
	name  string
	jar   string
	entry string
}

func dependencyResources(artifacts []deps.Artifact) ([]jarResource, error) {
	var result []jarResource
	for _, artifact := range artifacts {
		if artifact.Extension == "aar" {
			continue
		}
		reader, err := zip.OpenReader(artifact.Path)
		if err != nil {
			return nil, fmt.Errorf("open %s: %w", artifact.Path, err)
		}
		for _, entry := range reader.File {
			if entry.FileInfo().IsDir() {
				continue
			}
			name := entry.Name
			if strings.HasSuffix(name, ".class") || strings.HasSuffix(name, ".so") {
				continue
			}
			if strings.HasPrefix(name, "META-INF/") && !strings.HasPrefix(name, "META-INF/services/") {
				continue
			}
			result = append(result, jarResource{name: name, jar: artifact.Path, entry: name})
		}
		reader.Close()
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].name < result[j].name
	})
	return result, nil
}

func writeJarResource(writer *zip.Writer, resource jarResource) error {
	reader, err := zip.OpenReader(resource.jar)
	if err != nil {
		return fmt.Errorf("open %s: %w", resource.jar, err)
	}
	defer reader.Close()

	for _, entry := range reader.File {
		if entry.Name != resource.entry {
			continue
		}
		source, err := entry.Open()
		if err != nil {
			return fmt.Errorf("open resource %s: %w", resource.entry, err)
		}
		defer source.Close()

		header := &zip.FileHeader{Name: resource.name, Method: zip.Deflate, Modified: apkFixedTime}
		header.SetMode(0o644)
		target, err := writer.CreateHeader(header)
		if err != nil {
			return fmt.Errorf("create %s: %w", resource.name, err)
		}
		if _, err := io.Copy(target, source); err != nil {
			return fmt.Errorf("write %s: %w", resource.name, err)
		}
		return nil
	}
	return fmt.Errorf("resource %s is missing from %s", resource.entry, resource.jar)
}

type nativeLibrary struct {
	abi   string
	name  string
	jar   string
	entry string
	path  string
}

func mergeNatives(fromArtifacts, local []nativeLibrary) []nativeLibrary {
	merged := make(map[string]nativeLibrary)
	for _, native := range fromArtifacts {
		merged[native.abi+"/"+native.name] = native
	}
	for _, native := range local {
		merged[native.abi+"/"+native.name] = native
	}

	result := make([]nativeLibrary, 0, len(merged))
	for _, native := range merged {
		result = append(result, native)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].abi != result[j].abi {
			return result[i].abi < result[j].abi
		}
		return result[i].name < result[j].name
	})
	return result
}

func nativeLibraries(artifacts []deps.Artifact) ([]nativeLibrary, error) {
	var result []nativeLibrary
	for _, artifact := range artifacts {
		if !strings.HasPrefix(artifact.Classifier, "natives-") {
			continue
		}
		abi := strings.TrimPrefix(artifact.Classifier, "natives-")
		if abi == "desktop" {
			continue
		}
		reader, err := zip.OpenReader(artifact.Path)
		if err != nil {
			return nil, fmt.Errorf("open natives %s: %w", artifact.Path, err)
		}
		for _, entry := range reader.File {
			if entry.FileInfo().IsDir() || !strings.HasSuffix(entry.Name, ".so") {
				continue
			}
			result = append(result, nativeLibrary{
				abi:   abi,
				name:  filepath.Base(entry.Name),
				jar:   artifact.Path,
				entry: entry.Name,
			})
		}
		reader.Close()
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].abi != result[j].abi {
			return result[i].abi < result[j].abi
		}
		return result[i].name < result[j].name
	})
	return result, nil
}

func writeNative(writer *zip.Writer, native nativeLibrary) error {
	var source io.ReadCloser
	if native.path != "" {
		file, err := os.Open(native.path)
		if err != nil {
			return fmt.Errorf("open native %s: %w", native.path, err)
		}
		source = file
	} else {
		reader, err := zip.OpenReader(native.jar)
		if err != nil {
			return fmt.Errorf("open natives %s: %w", native.jar, err)
		}
		defer reader.Close()

		for _, entry := range reader.File {
			if entry.Name == native.entry {
				source, err = entry.Open()
				if err != nil {
					return fmt.Errorf("open native %s: %w", native.entry, err)
				}
				break
			}
		}
		if source == nil {
			return fmt.Errorf("native %s is missing from %s", native.entry, native.jar)
		}
	}
	defer source.Close()

	header := &zip.FileHeader{
		Name:     "lib/" + native.abi + "/" + native.name,
		Method:   zip.Store,
		Modified: apkFixedTime,
	}
	header.SetMode(0o644)
	target, err := writer.CreateHeader(header)
	if err != nil {
		return fmt.Errorf("create lib/%s/%s: %w", native.abi, native.name, err)
	}
	if _, err := io.Copy(target, source); err != nil {
		return fmt.Errorf("write lib/%s/%s: %w", native.abi, native.name, err)
	}
	return nil
}

func signingKey(options Options) (string, error) {
	config := options.signingConfig()
	if config == nil {
		if options.Variant.BuildType == "release" {
			return "none", nil
		}
		return "debug", nil
	}
	if config.Unresolved {
		return "unresolved", nil
	}
	return signingFingerprint(config)
}

func signingFingerprint(signing *SigningOptions) (string, error) {
	if signing == nil {
		return "debug", nil
	}
	file, err := os.Open(signing.Keystore)
	if err != nil {
		return "", fmt.Errorf("open keystore %s: %w", signing.Keystore, err)
	}
	defer file.Close()

	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", fmt.Errorf("read keystore %s: %w", signing.Keystore, err)
	}
	return fmt.Sprintf("custom:%x:%s", hash.Sum(nil), signing.Alias), nil
}

func (p *Provider) ensureKeystore(ctx context.Context, workspaceRoot string, jdk *toolchain.JDK, tmpDir string, signing *SigningOptions) (string, error) {
	if signing != nil {
		return signing.Keystore, nil
	}
	path := filepath.Join(workspaceRoot, ".kine", "android", "debug.keystore")
	if isRegular(path) {
		return path, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", fmt.Errorf("create keystore dir: %w", err)
	}

	keytool, err := toolchain.ToolPath(jdk, "keytool")
	if err != nil {
		return "", err
	}

	result, err := p.run(ctx, toolchain.Command{
		Path: keytool,
		Args: []string{
			"-genkeypair",
			"-noprompt",
			"-keystore", path,
			"-storepass", debugPass,
			"-keypass", debugPass,
			"-alias", debugAlias,
			"-keyalg", "RSA",
			"-keysize", "2048",
			"-validity", "10000",
			"-dname", "CN=Android Debug,O=Android,C=US",
		},
		Dir: workspaceRoot,
		Env: toolchain.BuildEnv(jdk, tmpDir, nil),
	})
	if err != nil {
		return "", toolError("keytool", result, err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return "", fmt.Errorf("chmod keystore: %w", err)
	}
	return path, nil
}
