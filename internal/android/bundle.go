package android

import (
	"archive/zip"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/gabriel-aplok/kine-build-java/internal/core"
	"github.com/gabriel-aplok/kine-build-java/internal/deps"
	"github.com/gabriel-aplok/kine-build-java/internal/toolchain"
)

func (p *Provider) bundleTask(workspaceRoot string, project *core.Project, options Options, sdk *SDK, jdk *toolchain.JDK, artifacts []deps.Artifact, hasLibs bool, moduleZips, moduleTaskIDs []string) (*core.Task, error) {
	if !options.Bundle {
		return nil, nil
	}
	if options.BundletoolMain == "" || len(options.BundletoolClasspath) == 0 {
		return nil, fmt.Errorf("bundletool is not resolved")
	}

	output, err := relativePath(workspaceRoot, options.bundleOutputPath(project.Dir))
	if err != nil {
		return nil, err
	}
	inputs := []core.Input{
		{Path: mustRelative(workspaceRoot, options.androidPath(project.Dir, "AndroidManifest.xml")), Mode: core.ModeFile},
	}
	if options.Variant.BuildTypeOptions.ShrinkResources {
		inputs = append(inputs, core.Input{
			Path: mustRelative(workspaceRoot, options.resourcesProtoAPKPath(project.Dir)),
			Mode: core.ModeFile,
		})
	}
	for _, dir := range append(append([]string(nil), options.AARResDirs...), options.ResDirs...) {
		inputs = append(inputs, core.Input{Path: mustRelative(workspaceRoot, dir), Mode: core.ModeTree})
	}
	for _, dir := range append([]string{options.androidPath(project.Dir, "dex")}, func() []string {
		if hasLibs {
			return []string{filepath.Join(project.Dir, "build", "android", "dex-libs")}
		}
		return nil
	}()...) {
		inputs = append(inputs, core.Input{Path: mustRelative(workspaceRoot, dir), Mode: core.ModeTree})
	}
	for _, dir := range append(append([]string(nil), options.AARAssetDirs...), options.AssetDirs...) {
		inputs = append(inputs, core.Input{Path: mustRelative(workspaceRoot, dir), Mode: core.ModeTree})
	}
	for _, dir := range options.AARNativeDirs {
		inputs = append(inputs, core.Input{Path: mustRelative(workspaceRoot, dir), Mode: core.ModeTree})
	}
	if options.Native != nil {
		inputs = append(inputs, core.Input{Path: mustRelative(workspaceRoot, filepath.Join(project.Dir, "build", "android", nativeDirName)), Mode: core.ModeTree})
	}
	for _, zip := range moduleZips {
		inputs = append(inputs, core.Input{Path: mustRelative(workspaceRoot, zip), Mode: core.ModeFile})
	}
	for _, artifact := range artifacts {
		inputs = append(inputs, core.Input{Path: mustRelative(workspaceRoot, artifact.Path), Mode: core.ModeFile})
	}

	deps := []string{project.ID + ":" + options.taskID("android-resources"), project.ID + ":" + options.taskID("dex")}
	if hasLibs {
		deps = append(deps, project.ID+":dex-libs")
	}
	if options.Native != nil {
		deps = append(deps, project.ID+":android-native")
	}
	deps = append(deps, moduleTaskIDs...)

	signing, err := signingKey(options)
	if err != nil {
		return nil, err
	}

	return &core.Task{
		ID:      options.taskID("bundle"),
		Deps:    deps,
		Inputs:  inputs,
		Outputs: []core.Output{{Path: output, Mode: core.ModeFile}},
		Key: fmt.Sprintf(
			"bundle bundletool=%s v=%d signer=%s shrink=%t code=%d name=%s variant=%s",
			options.BundletoolVersion,
			androidTaskVersion,
			signing,
			options.Variant.BuildTypeOptions.ShrinkResources,
			options.effectiveVersionCode(),
			options.effectiveVersionName(),
			options.variantLabel(),
		),
		Run: func(ctx context.Context) error {
			return p.buildBundle(ctx, workspaceRoot, project, options, sdk, jdk, artifacts, hasLibs, moduleZips)
		},
	}, nil
}

func mustRelative(root, path string) string {
	relative, err := relativePath(root, path)
	if err != nil {
		return path
	}
	return relative
}

func (p *Provider) buildBundle(ctx context.Context, workspaceRoot string, project *core.Project, options Options, sdk *SDK, jdk *toolchain.JDK, artifacts []deps.Artifact, hasLibs bool, moduleZips []string) error {
	output := options.bundleOutputPath(project.Dir)
	if err := os.MkdirAll(filepath.Dir(output), 0o755); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Dir(output), err)
	}
	if err := os.Remove(output); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("clean %s: %w", output, err)
	}

	var protoAPK string
	if options.Variant.BuildTypeOptions.ShrinkResources {
		protoAPK = options.resourcesProtoAPKPath(project.Dir)
		if !isRegular(protoAPK) {
			return fmt.Errorf("shrunken proto resource APK is missing: %s", protoAPK)
		}
	} else {
		var err error
		protoAPK, err = p.buildProtoAPK(ctx, project, options, sdk, jdk)
		if err != nil {
			return err
		}
	}
	baseDir := options.androidPath(project.Dir, "bundle", "base")
	if err := os.RemoveAll(baseDir); err != nil {
		return fmt.Errorf("clean %s: %w", baseDir, err)
	}
	if err := os.MkdirAll(baseDir, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", baseDir, err)
	}
	if err := stageProtoAPK(protoAPK, baseDir); err != nil {
		return err
	}

	dexDirs := []string{options.androidPath(project.Dir, "dex")}
	if hasLibs {
		dexDirs = append(dexDirs, filepath.Join(project.Dir, "build", "android", "dex-libs"))
	}
	if err := stageBundleDex(baseDir, dexDirs); err != nil {
		return err
	}
	if err := stageBundleAssets(baseDir, options); err != nil {
		return err
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
	localNatives, err := localNativeLibraries(filepath.Join(project.Dir, "build", "android", nativeDirName))
	if err != nil {
		return err
	}
	natives = mergeNatives(natives, localNatives)
	if err := stageBundleNatives(baseDir, natives); err != nil {
		return err
	}
	jarResources, err := dependencyResources(artifacts)
	if err != nil {
		return err
	}
	if err := stageBundleRoot(baseDir, jarResources); err != nil {
		return err
	}

	moduleZip := options.androidPath(project.Dir, "bundle", "base.zip")
	if err := zipDirectory(baseDir, moduleZip); err != nil {
		return err
	}
	moduleFiles := append([]string{moduleZip}, moduleZips...)
	if err := p.runBundletool(ctx, project, options, jdk, moduleFiles, output); err != nil {
		return err
	}

	releaseUnsigned := options.Variant.BuildType == "release" && options.signingConfig() == nil
	if releaseUnsigned {
		return nil
	}
	signing := options.signingConfig()
	if signing != nil && signing.Unresolved {
		return fmt.Errorf("release signing environment variables are not set")
	}
	tmpDir := filepath.Join(project.Dir, ".kine", "tmp")
	if err := os.MkdirAll(tmpDir, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", tmpDir, err)
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
	jarsigner, err := toolchain.ToolPath(jdk, "jarsigner")
	if err != nil {
		return err
	}
	result, err := p.run(ctx, toolchain.Command{
		Path: jarsigner,
		Args: []string{
			"-keystore", keystore,
			"-storepass", storePassword,
			"-keypass", keyPassword,
			"-sigalg", "SHA256withRSA",
			"-digestalg", "SHA-256",
			output,
			alias,
		},
		Dir: project.Dir,
		Env: toolchain.BuildEnv(jdk, tmpDir, nil),
	})
	if err != nil {
		return toolError("jarsigner", result, err)
	}
	return nil
}

func (p *Provider) buildProtoAPK(ctx context.Context, project *core.Project, options Options, sdk *SDK, jdk *toolchain.JDK) (string, error) {
	buildDir := options.androidPath(project.Dir, "bundle")
	if err := os.MkdirAll(buildDir, 0o755); err != nil {
		return "", fmt.Errorf("create %s: %w", buildDir, err)
	}
	output := filepath.Join(buildDir, "proto.apk")
	if err := os.Remove(output); err != nil && !os.IsNotExist(err) {
		return "", fmt.Errorf("clean %s: %w", output, err)
	}

	resDirs := append(append([]string(nil), options.AARResDirs...), options.ResDirs...)
	flats := make([]string, 0, len(resDirs))
	for index, dir := range resDirs {
		flat := filepath.Join(buildDir, fmt.Sprintf("res-%d.zip", index))
		result, err := p.run(ctx, toolchain.Command{
			Path: sdk.Aapt2,
			Args: []string{"compile", "--dir", dir, "-o", flat},
			Dir:  project.Dir,
			Env:  toolchain.BuildEnv(jdk, filepath.Join(project.Dir, ".kine", "tmp"), nil),
		})
		if err != nil {
			return "", toolError("aapt2 compile", result, err)
		}
		flats = append(flats, flat)
	}

	manifest := options.androidPath(project.Dir, "AndroidManifest.xml")
	args := []string{
		"link",
		"-o", output,
		"-I", sdk.AndroidJar,
		"--manifest", manifest,
		"--proto-format",
		"--min-sdk-version", strconv.Itoa(options.MinSDK),
		"--version-code", strconv.Itoa(options.effectiveVersionCode()),
		"--version-name", options.effectiveVersionName(),
	}
	if options.TargetSDK > 0 {
		args = append(args, "--target-sdk-version", strconv.Itoa(options.TargetSDK))
	}
	if options.ApplicationID != "" && options.ApplicationID != options.PackageName {
		args = append(args,
			"--rename-manifest-package", options.ApplicationID,
			"--custom-package", options.PackageName,
		)
	}
	if len(flats) > 1 {
		args = append(args, "--auto-add-overlay")
	}
	if len(flats) > 0 {
		args = append(args, flats[0])
		for _, flat := range flats[1:] {
			args = append(args, "-R", flat)
		}
	}

	result, err := p.run(ctx, toolchain.Command{
		Path: sdk.Aapt2,
		Args: args,
		Dir:  project.Dir,
		Env:  toolchain.BuildEnv(jdk, filepath.Join(project.Dir, ".kine", "tmp"), nil),
	})
	if err != nil {
		return "", toolError("aapt2 link --proto-format", result, err)
	}
	if !isRegular(output) {
		return "", fmt.Errorf("aapt2 link --proto-format produced no %s", output)
	}
	return output, nil
}

func stageProtoAPK(protoAPK, baseDir string) error {
	reader, err := zip.OpenReader(protoAPK)
	if err != nil {
		return fmt.Errorf("open %s: %w", protoAPK, err)
	}
	defer reader.Close()

	foundManifest := false
	foundResources := false
	for _, entry := range reader.File {
		if entry.FileInfo().IsDir() {
			continue
		}
		target := ""
		switch {
		case entry.Name == "AndroidManifest.xml":
			target = filepath.Join(baseDir, "manifest", "AndroidManifest.xml")
			foundManifest = true
		case entry.Name == "resources.pb":
			target = filepath.Join(baseDir, "resources.pb")
			foundResources = true
		case strings.HasPrefix(entry.Name, "res/"):
			target = filepath.Join(baseDir, filepath.FromSlash(entry.Name))
		default:
			continue
		}
		if err := copyZipEntry(entry, target); err != nil {
			return err
		}
	}
	if !foundManifest || !foundResources {
		return fmt.Errorf("proto APK is missing AndroidManifest.xml or resources.pb")
	}
	return nil
}

func copyZipEntry(entry *zip.File, target string) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Dir(target), err)
	}
	source, err := entry.Open()
	if err != nil {
		return fmt.Errorf("open %s: %w", entry.Name, err)
	}
	defer source.Close()
	output, err := os.Create(target)
	if err != nil {
		return fmt.Errorf("create %s: %w", target, err)
	}
	if _, err := io.Copy(output, source); err != nil {
		output.Close()
		return fmt.Errorf("copy %s: %w", entry.Name, err)
	}
	if err := output.Close(); err != nil {
		return fmt.Errorf("close %s: %w", target, err)
	}
	return nil
}

func stageBundleDex(baseDir string, dirs []string) error {
	index := 1
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
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
			target := filepath.Join(baseDir, "dex", "classes.dex")
			if index > 1 {
				target = filepath.Join(baseDir, "dex", fmt.Sprintf("classes%d.dex", index))
			}
			index++
			if err := copyFile(filepath.Join(dir, name), target); err != nil {
				return err
			}
		}
	}
	if index == 1 {
		return fmt.Errorf("no dex files in %v", dirs)
	}
	return nil
}

func stageBundleAssets(baseDir string, options Options) error {
	assets, err := assetFiles(append(append([]string(nil), options.AARAssetDirs...), options.AssetDirs...))
	if err != nil {
		return err
	}
	for _, asset := range assets {
		target := filepath.Join(baseDir, filepath.FromSlash(asset.name))
		if err := copyFile(asset.path, target); err != nil {
			return err
		}
	}
	return nil
}

func stageBundleNatives(baseDir string, natives []nativeLibrary) error {
	for _, native := range natives {
		target := filepath.Join(baseDir, "lib", native.abi, native.name)
		if native.path != "" {
			if err := copyFile(native.path, target); err != nil {
				return err
			}
			continue
		}
		reader, err := zip.OpenReader(native.jar)
		if err != nil {
			return fmt.Errorf("open natives %s: %w", native.jar, err)
		}
		var source io.ReadCloser
		for _, entry := range reader.File {
			if entry.Name == native.entry {
				source, err = entry.Open()
				if err != nil {
					reader.Close()
					return fmt.Errorf("open native %s: %w", native.entry, err)
				}
				break
			}
		}
		if source == nil {
			reader.Close()
			return fmt.Errorf("native %s is missing from %s", native.entry, native.jar)
		}
		if err := copyReader(source, target); err != nil {
			source.Close()
			reader.Close()
			return err
		}
		source.Close()
		reader.Close()
	}
	return nil
}

func copyReader(source io.Reader, target string) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Dir(target), err)
	}
	output, err := os.Create(target)
	if err != nil {
		return fmt.Errorf("create %s: %w", target, err)
	}
	if _, err := io.Copy(output, source); err != nil {
		output.Close()
		return fmt.Errorf("write %s: %w", target, err)
	}
	if err := output.Close(); err != nil {
		return fmt.Errorf("close %s: %w", target, err)
	}
	return nil
}

func stageBundleRoot(baseDir string, resources []jarResource) error {
	for _, resource := range resources {
		target := filepath.Join(baseDir, "root", filepath.FromSlash(resource.name))
		reader, err := zip.OpenReader(resource.jar)
		if err != nil {
			return fmt.Errorf("open %s: %w", resource.jar, err)
		}
		for _, entry := range reader.File {
			if entry.Name != resource.entry {
				continue
			}
			source, err := entry.Open()
			if err != nil {
				reader.Close()
				return fmt.Errorf("open %s: %w", resource.entry, err)
			}
			err = copyReader(source, target)
			source.Close()
			reader.Close()
			if err != nil {
				return err
			}
			break
		}
	}
	return nil
}

func zipDirectory(root, output string) error {
	var paths []string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		paths = append(paths, path)
		return nil
	})
	if err != nil {
		return fmt.Errorf("walk %s: %w", root, err)
	}
	sort.Strings(paths)

	if err := os.MkdirAll(filepath.Dir(output), 0o755); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Dir(output), err)
	}
	file, err := os.Create(output)
	if err != nil {
		return fmt.Errorf("create %s: %w", output, err)
	}
	writer := zip.NewWriter(file)
	for _, path := range paths {
		relative, err := filepath.Rel(root, path)
		if err != nil {
			writer.Close()
			file.Close()
			return err
		}
		name := filepath.ToSlash(relative)
		header := &zip.FileHeader{Name: name, Method: zip.Deflate, Modified: apkFixedTime}
		if strings.HasSuffix(name, ".so") {
			header.Method = zip.Store
		}
		header.SetMode(0o644)
		target, err := writer.CreateHeader(header)
		if err != nil {
			writer.Close()
			file.Close()
			return fmt.Errorf("create %s: %w", name, err)
		}
		source, err := os.Open(path)
		if err != nil {
			writer.Close()
			file.Close()
			return fmt.Errorf("open %s: %w", path, err)
		}
		_, copyErr := io.Copy(target, source)
		source.Close()
		if copyErr != nil {
			writer.Close()
			file.Close()
			return fmt.Errorf("write %s: %w", name, copyErr)
		}
	}
	if err := writer.Close(); err != nil {
		file.Close()
		return fmt.Errorf("close %s: %w", output, err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close %s: %w", output, err)
	}
	return nil
}

func (p *Provider) runBundletool(ctx context.Context, project *core.Project, options Options, jdk *toolchain.JDK, moduleZips []string, output string) error {
	classpath := strings.Join(options.BundletoolClasspath, string(os.PathListSeparator))
	result, err := p.run(ctx, toolchain.Command{
		Path: jdk.Java,
		Args: []string{
			"-cp", classpath,
			options.BundletoolMain,
			"build-bundle",
			"--modules=" + strings.Join(moduleZips, ","),
			"--output=" + output,
		},
		Dir: project.Dir,
		Env: toolchain.BuildEnv(jdk, filepath.Join(project.Dir, ".kine", "tmp"), nil),
	})
	if err != nil {
		return toolError("bundletool", result, err)
	}
	return nil
}
