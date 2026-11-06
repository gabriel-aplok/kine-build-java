package android

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

const (
	androidTestPackageSuffix = ".test"
	defaultInstrumentation   = "androidx.test.runner.AndroidJUnitRunner"
)

func (p *Provider) prepareJUnit(ctx context.Context, workspaceRoot string, options *Options) error {
	if options.JUnitVersion == "" {
		options.JUnitVersion = defaultJUnitVersion
	}
	if p.config.Cache == nil {
		return fmt.Errorf("unit tests require the workspace cache")
	}
	toolRoot := filepath.Join(workspaceRoot, ".kine", "tools", "junit", options.JUnitVersion)
	if err := os.MkdirAll(toolRoot, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", toolRoot, err)
	}
	repositories, err := deps.ToolRepositories(workspaceRoot)
	if err != nil {
		return err
	}
	result, err := deps.Resolve(ctx, toolRoot, p.config.Cache, []deps.Dependency{{
		Coordinate: deps.Coordinate{
			Group:     "org.junit.platform",
			Artifact:  "junit-platform-console-standalone",
			Version:   options.JUnitVersion,
			Extension: "jar",
		},
		Scope: deps.ScopeRuntime,
	}}, deps.Options{
		Repositories: repositories,
		Update:       p.config.Update,
		Offline:      p.config.Offline,
		Logger:       p.config.Logger,
	})
	if err != nil {
		return fmt.Errorf("resolve junit: %w", err)
	}
	for _, artifact := range result.Artifacts {
		if artifact.Extension == "jar" && artifact.Classifier == "" {
			options.JUnitJar = artifact.Path
			return nil
		}
	}
	return fmt.Errorf("junit console jar was not resolved")
}

func (p *Provider) testTask(workspaceRoot string, project *core.Project, options Options, sdk *SDK, jdk *toolchain.JDK, artifacts, testArtifacts []deps.Artifact) (*core.Task, error) {
	classes := options.androidPath(project.Dir, "classes")
	reportDir := filepath.Join(project.Dir, "build", "reports", "tests", options.variantLabel())
	inputs := []core.Input{{Path: mustRelative(workspaceRoot, classes), Mode: core.ModeTree}}
	for _, dir := range append(append([]string(nil), options.TestDirs...), options.TestResourceDirs...) {
		inputs = append(inputs, core.Input{Path: mustRelative(workspaceRoot, dir), Mode: core.ModeTree})
	}
	for _, artifact := range append(append([]deps.Artifact(nil), artifacts...), testArtifacts...) {
		if artifact.Extension == "aar" || strings.HasPrefix(artifact.Classifier, "natives-") {
			continue
		}
		inputs = append(inputs, core.Input{Path: mustRelative(workspaceRoot, artifact.Path), Mode: core.ModeFile})
	}
	if options.KotlinStdlib != "" {
		inputs = append(inputs, core.Input{Path: mustRelative(workspaceRoot, options.KotlinStdlib), Mode: core.ModeFile})
	}
	if options.JUnitJar != "" {
		inputs = append(inputs, core.Input{Path: mustRelative(workspaceRoot, options.JUnitJar), Mode: core.ModeFile})
	}

	return &core.Task{
		ID:      options.taskID("test"),
		Deps:    []string{project.ID + ":" + options.taskID("compile-java")},
		Inputs:  inputs,
		Outputs: []core.Output{{Path: mustRelative(workspaceRoot, reportDir), Mode: core.ModeTree}},
		Key: fmt.Sprintf(
			"android-test junit=%s v=%d java=%d variant=%s",
			options.JUnitVersion,
			androidTaskVersion,
			javaVersion(options),
			options.variantLabel(),
		),
		Run: func(ctx context.Context) error {
			return p.runUnitTests(ctx, project, options, sdk, jdk, artifacts, testArtifacts)
		},
	}, nil
}

func (p *Provider) runUnitTests(ctx context.Context, project *core.Project, options Options, sdk *SDK, jdk *toolchain.JDK, artifacts, testArtifacts []deps.Artifact) error {
	if options.JUnitJar == "" {
		return fmt.Errorf("JUnit console jar is missing")
	}
	buildDir := options.androidPath(project.Dir, "test")
	classesDir := filepath.Join(buildDir, "classes")
	kotlinDir := filepath.Join(buildDir, "classes-kotlin")
	if err := os.RemoveAll(buildDir); err != nil {
		return fmt.Errorf("clean %s: %w", buildDir, err)
	}
	if err := os.MkdirAll(classesDir, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", classesDir, err)
	}

	classpath, err := p.testClasspath(project, options, sdk, artifacts, testArtifacts)
	if err != nil {
		return err
	}
	classpath = append(classpath, options.JUnitJar)
	if err := p.compileTestSources(ctx, project, options, sdk, jdk, options.TestDirs, classpath, classesDir, kotlinDir); err != nil {
		return err
	}
	classpath = append(classpath, options.TestResourceDirs...)

	reportDir := filepath.Join(project.Dir, "build", "reports", "tests", options.variantLabel())
	if err := os.RemoveAll(reportDir); err != nil {
		return fmt.Errorf("clean %s: %w", reportDir, err)
	}
	if err := os.MkdirAll(reportDir, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", reportDir, err)
	}
	runClasspath := append([]string{classesDir}, classpath...)
	if isDir(kotlinDir) {
		runClasspath = append([]string{kotlinDir}, runClasspath...)
	}
	result, err := p.run(ctx, toolchain.Command{
		Path: jdk.Java,
		Args: []string{
			"-jar", options.JUnitJar,
			"--class-path", strings.Join(runClasspath, string(os.PathListSeparator)),
			"--scan-class-path", classesDir,
			"--reports-dir", reportDir,
			"--details", "tree",
			"--disable-banner",
		},
		Dir: project.Dir,
		Env: toolchain.BuildEnv(jdk, filepath.Join(project.Dir, ".kine", "tmp"), nil),
	})
	if err != nil {
		return toolError("junit", result, err)
	}
	return nil
}

func (p *Provider) testClasspath(project *core.Project, options Options, sdk *SDK, artifacts, testArtifacts []deps.Artifact) ([]string, error) {
	classpath := []string{
		sdk.AndroidJar,
		options.androidPath(project.Dir, "classes"),
	}
	if len(options.KotlinDirs) > 0 {
		classpath = append(classpath, options.androidPath(project.Dir, "classes-kotlin"))
	}
	if options.KotlinStdlib != "" {
		classpath = append(classpath, options.KotlinStdlib)
	}
	for _, list := range [][]deps.Artifact{artifacts, testArtifacts} {
		jars, err := classpathJars(list, filepath.Join(project.Dir, "build", "android", "aar"))
		if err != nil {
			return nil, err
		}
		classpath = append(classpath, jars...)
	}
	return classpath, nil
}

func (p *Provider) compileTestSources(ctx context.Context, project *core.Project, options Options, sdk *SDK, jdk *toolchain.JDK, dirs, classpath []string, classesDir, kotlinDir string) error {
	kotlinSources, err := sourceFiles(dirs, ".kt")
	if err != nil {
		return err
	}
	javaSources, err := sourceFiles(dirs, ".java")
	if err != nil {
		return err
	}
	if len(kotlinSources) == 0 && len(javaSources) == 0 {
		return fmt.Errorf("no test sources in %v", dirs)
	}
	env := toolchain.BuildEnv(jdk, filepath.Join(project.Dir, ".kine", "tmp"), nil)

	if len(kotlinSources) > 0 && options.KotlinCompiler != "" {
		if err := os.MkdirAll(kotlinDir, 0o755); err != nil {
			return fmt.Errorf("create %s: %w", kotlinDir, err)
		}
		result, err := p.run(ctx, toolchain.Command{
			Path: options.KotlinCompiler,
			Args: kotlinArgs(options, kotlinDir, classpath, nil, kotlinSources),
			Dir:  project.Dir,
			Env:  env,
		})
		if err != nil {
			return toolError("test kotlinc", result, err)
		}
		classpath = append([]string{kotlinDir}, classpath...)
	}

	if len(javaSources) > 0 {
		result, err := p.run(ctx, toolchain.Command{
			Path: jdk.Javac,
			Args: javacArgs(classesDir, classpath, javaSources, javaVersion(options)),
			Dir:  project.Dir,
			Env:  env,
		})
		if err != nil {
			return toolError("test javac", result, err)
		}
	}
	if !hasClasses(classesDir) && !hasClasses(kotlinDir) {
		return fmt.Errorf("test compilation produced no classes")
	}
	return nil
}

func hasClasses(dir string) bool {
	found := false
	_ = filepath.WalkDir(dir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".class") {
			found = true
			return filepath.SkipAll
		}
		return nil
	})
	return found
}

func (p *Provider) androidTestAPKTask(workspaceRoot string, project *core.Project, options Options, sdk *SDK, jdk *toolchain.JDK, artifacts, testArtifacts []deps.Artifact) (*core.Task, error) {
	output, err := relativePath(workspaceRoot, options.androidTestAPKPath(project.Dir))
	if err != nil {
		return nil, err
	}
	classes := options.androidPath(project.Dir, "classes")
	inputs := []core.Input{{Path: mustRelative(workspaceRoot, classes), Mode: core.ModeTree}}
	for _, dir := range append(append([]string(nil), options.AndroidTestDirs...), options.AndroidTestResourceDirs...) {
		inputs = append(inputs, core.Input{Path: mustRelative(workspaceRoot, dir), Mode: core.ModeTree})
	}
	if options.AndroidTestManifest != "" {
		inputs = append(inputs, core.Input{Path: mustRelative(workspaceRoot, options.AndroidTestManifest), Mode: core.ModeFile})
	}
	for _, artifact := range append(append([]deps.Artifact(nil), artifacts...), testArtifacts...) {
		if artifact.Extension == "aar" || strings.HasPrefix(artifact.Classifier, "natives-") {
			continue
		}
		inputs = append(inputs, core.Input{Path: mustRelative(workspaceRoot, artifact.Path), Mode: core.ModeFile})
	}

	return &core.Task{
		ID:      options.taskID("instrument-apk"),
		Deps:    []string{project.ID + ":" + options.taskID("compile-java")},
		Inputs:  inputs,
		Outputs: []core.Output{{Path: output, Mode: core.ModeFile}},
		Key: fmt.Sprintf(
			"android-test-apk v=%d buildtools=%s variant=%s",
			androidTaskVersion,
			sdk.BuildTools,
			options.variantLabel(),
		),
		Run: func(ctx context.Context) error {
			return p.buildAndroidTestAPK(ctx, project, options, sdk, jdk, artifacts, testArtifacts)
		},
	}, nil
}

func (p *Provider) buildAndroidTestAPK(ctx context.Context, project *core.Project, options Options, sdk *SDK, jdk *toolchain.JDK, artifacts, testArtifacts []deps.Artifact) error {
	buildDir := options.androidPath(project.Dir, "androidTest")
	classesDir := filepath.Join(buildDir, "classes")
	kotlinDir := filepath.Join(buildDir, "classes-kotlin")
	dexDir := filepath.Join(buildDir, "dex")
	if err := os.RemoveAll(buildDir); err != nil {
		return fmt.Errorf("clean %s: %w", buildDir, err)
	}
	if err := os.MkdirAll(classesDir, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", classesDir, err)
	}
	if err := os.MkdirAll(dexDir, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", dexDir, err)
	}

	classpath, err := p.testClasspath(project, options, sdk, artifacts, testArtifacts)
	if err != nil {
		return err
	}
	if err := p.compileTestSources(ctx, project, options, sdk, jdk, options.AndroidTestDirs, classpath, classesDir, kotlinDir); err != nil {
		return err
	}

	manifest, _, _, err := writeAndroidTestManifest(project, options, buildDir)
	if err != nil {
		return err
	}
	var flats []string
	for index, dir := range options.AndroidTestResourceDirs {
		flat := filepath.Join(buildDir, fmt.Sprintf("resources-%d.zip", index))
		result, err := p.run(ctx, toolchain.Command{
			Path: sdk.Aapt2,
			Args: []string{"compile", "--dir", dir, "-o", flat},
			Dir:  project.Dir,
			Env:  toolchain.BuildEnv(jdk, filepath.Join(project.Dir, ".kine", "tmp"), nil),
		})
		if err != nil {
			return toolError("androidTest aapt2 compile", result, err)
		}
		flats = append(flats, flat)
	}

	linked := filepath.Join(buildDir, "linked.apk")
	env := toolchain.BuildEnv(jdk, filepath.Join(project.Dir, ".kine", "tmp"), nil)
	linkArgs := []string{
		"link",
		"-o", linked,
		"-I", sdk.AndroidJar,
		"--manifest", manifest,
		"--min-sdk-version", strconv.Itoa(options.MinSDK),
		"--target-sdk-version", strconv.Itoa(options.TargetSDK),
	}
	if len(flats) > 1 {
		linkArgs = append(linkArgs, "--auto-add-overlay")
	}
	if len(flats) > 0 {
		linkArgs = append(linkArgs, flats[0])
		for _, flat := range flats[1:] {
			linkArgs = append(linkArgs, "-R", flat)
		}
	}
	result, err := p.run(ctx, toolchain.Command{
		Path: sdk.Aapt2,
		Args: linkArgs,
		Dir:  project.Dir,
		Env:  env,
	})
	if err != nil {
		return toolError("androidTest aapt2 link", result, err)
	}

	var classesDirs []string
	if hasClasses(classesDir) {
		classesDirs = append(classesDirs, classesDir)
	}
	if hasClasses(kotlinDir) {
		classesDirs = append(classesDirs, kotlinDir)
	}
	classesJar := filepath.Join(buildDir, "classes.jar")
	if err := jarClasses(classesDirs, classesJar); err != nil {
		return err
	}
	args := []string{
		"--lib", sdk.AndroidJar,
		"--min-api", strconv.Itoa(options.MinSDK),
		"--output", dexDir,
	}
	appJar := filepath.Join(buildDir, "app-classes.jar")
	if err := jarClasses([]string{options.androidPath(project.Dir, "classes")}, appJar); err != nil {
		return err
	}
	args = append(args, "--classpath", appJar)
	if len(options.KotlinDirs) > 0 && isDir(options.androidPath(project.Dir, "classes-kotlin")) {
		appKotlinJar := filepath.Join(buildDir, "app-kotlin-classes.jar")
		if err := jarClasses([]string{options.androidPath(project.Dir, "classes-kotlin")}, appKotlinJar); err != nil {
			return err
		}
		args = append(args, "--classpath", appKotlinJar)
	}
	aarDir := filepath.Join(project.Dir, "build", "android", "aar")
	projectJars, err := classpathJars(artifacts, aarDir)
	if err != nil {
		return err
	}
	for _, jar := range projectJars {
		args = append(args, "--classpath", jar)
	}
	testJars, err := classpathJars(testArtifacts, aarDir)
	if err != nil {
		return err
	}
	args = append(args, classesJar)
	args = append(args, testJars...)
	result, err = p.run(ctx, toolchain.Command{
		Path: sdk.D8,
		Args: args,
		Dir:  project.Dir,
		Env:  env,
	})
	if err != nil {
		return toolError("androidTest d8", result, err)
	}

	unsigned := filepath.Join(buildDir, "unsigned.apk")
	if err := assembleAPK(linked, []string{dexDir}, nil, nil, nil, unsigned); err != nil {
		return err
	}
	return p.signTestAPK(ctx, project, options, sdk, jdk, unsigned, options.androidTestAPKPath(project.Dir))
}

func writeAndroidTestManifest(project *core.Project, options Options, buildDir string) (string, string, string, error) {
	appPackage := options.ApplicationID
	if appPackage == "" {
		appPackage = options.PackageName
	}
	var root *manifestNode
	if options.AndroidTestManifest != "" {
		parsed, err := parseManifest(options.AndroidTestManifest)
		if err != nil {
			return "", "", "", err
		}
		root = parsed
	} else {
		root = &manifestNode{Name: xmlName("", "manifest")}
		root.Attr = append(root.Attr, xmlAttr("xmlns", "android", androidNamespace))
	}
	testPackage := attributeValue(root, "", "package")
	if testPackage == "" {
		testPackage = appPackage + androidTestPackageSuffix
		setManifestAttribute(root, "", "package", testPackage)
	}

	instrumentation := findManifestChild(root, "instrumentation")
	if instrumentation == nil {
		instrumentation = &manifestNode{Name: xmlName("", "instrumentation")}
		root.Children = append(root.Children, instrumentation)
	}
	runner := attributeValue(instrumentation, "android", "name")
	if runner == "" {
		runner = defaultInstrumentation
		setManifestAttribute(instrumentation, "android", "name", runner)
	}
	setManifestAttribute(instrumentation, "android", "targetPackage", appPackage)

	target := filepath.Join(buildDir, "AndroidManifest.xml")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return "", "", "", fmt.Errorf("create %s: %w", filepath.Dir(target), err)
	}
	if err := writeManifestFile(target, root); err != nil {
		return "", "", "", err
	}
	return target, testPackage, runner, nil
}

func (p *Provider) signTestAPK(ctx context.Context, project *core.Project, options Options, sdk *SDK, jdk *toolchain.JDK, unsigned, final string) error {
	tmpDir := filepath.Join(project.Dir, ".kine", "tmp")
	if err := os.MkdirAll(tmpDir, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", tmpDir, err)
	}
	env := toolchain.BuildEnv(jdk, tmpDir, nil)
	aligned := filepath.Join(filepath.Dir(unsigned), "aligned.apk")
	result, err := p.run(ctx, toolchain.Command{
		Path: sdk.Zipalign,
		Args: zipalignArgs(sdk, unsigned, aligned),
		Dir:  project.Dir,
		Env:  env,
	})
	if err != nil {
		return toolError("androidTest zipalign", result, err)
	}
	keystore, err := p.ensureKeystore(ctx, p.workspaceRoot(project), jdk, tmpDir, nil)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(final), 0o755); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Dir(final), err)
	}
	result, err = p.run(ctx, toolchain.Command{
		Path: sdk.ApkSigner,
		Args: []string{
			"sign",
			"--ks", keystore,
			"--ks-key-alias", debugAlias,
			"--ks-pass", "pass:" + debugPass,
			"--key-pass", "pass:" + debugPass,
			"--v4-signing-enabled", "false",
			"--out", final,
			aligned,
		},
		Dir: project.Dir,
		Env: env,
	})
	if err != nil {
		return toolError("androidTest apksigner", result, err)
	}
	for _, path := range []string{unsigned, aligned} {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove %s: %w", path, err)
		}
	}
	return nil
}

func (p *Provider) instrumentTask(workspaceRoot string, project *core.Project, options Options, sdk *SDK, jdk *toolchain.JDK) (*core.Task, error) {
	testAPK, err := relativePath(workspaceRoot, options.androidTestAPKPath(project.Dir))
	if err != nil {
		return nil, err
	}
	reportDir := filepath.Join(project.Dir, "build", "reports", "androidTests", options.variantLabel())
	appTask := options.taskID("apk")
	appInput := options.apkOutputPath(project.Dir)
	if len(options.Modules) > 0 {
		appTask = options.taskID("bundle")
		appInput = options.bundleOutputPath(project.Dir)
	}
	appAPK, err := relativePath(workspaceRoot, appInput)
	if err != nil {
		return nil, err
	}

	return &core.Task{
		ID:   options.taskID("instrument"),
		Deps: []string{project.ID + ":" + options.taskID("instrument-apk"), project.ID + ":" + appTask},
		Inputs: []core.Input{
			{Path: testAPK, Mode: core.ModeFile},
			{Path: appAPK, Mode: core.ModeFile},
		},
		Outputs: []core.Output{{Path: mustRelative(workspaceRoot, reportDir), Mode: core.ModeTree}},
		Key: fmt.Sprintf(
			"android-instrument v=%d variant=%s device=%s",
			androidTaskVersion,
			options.variantLabel(),
			p.device,
		),
		Run: func(ctx context.Context) error {
			return p.runInstrumentation(ctx, project, options, sdk, jdk)
		},
	}, nil
}

func (p *Provider) runInstrumentation(ctx context.Context, project *core.Project, options Options, sdk *SDK, jdk *toolchain.JDK) error {
	adb, err := findADB(sdk)
	if err != nil {
		return err
	}
	tmpDir := filepath.Join(project.Dir, ".kine", "tmp")
	if err := os.MkdirAll(tmpDir, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", tmpDir, err)
	}
	env := toolchain.BuildEnv(jdk, tmpDir, nil)
	serial, err := p.resolveDevice(ctx, adb, project, env)
	if err != nil {
		return err
	}
	_, testPackage, runner, err := writeAndroidTestManifest(project, options, filepath.Join(project.Dir, ".kine", "instrument"))
	if err != nil {
		return err
	}

	prefix := []string{"-s", serial}
	if len(options.Modules) > 0 {
		if err := p.installBundleSplits(ctx, project, options, sdk, jdk, adb, serial, env); err != nil {
			return err
		}
	} else {
		result, err := p.run(ctx, toolchain.Command{
			Path: adb,
			Args: append(append([]string(nil), prefix...), "install", "-r", options.apkOutputPath(project.Dir)),
			Dir:  project.Dir,
			Env:  env,
		})
		if err != nil {
			return toolError("adb install", result, err)
		}
	}
	result, err := p.run(ctx, toolchain.Command{
		Path: adb,
		Args: append(append([]string(nil), prefix...), "install", "-r", options.androidTestAPKPath(project.Dir)),
		Dir:  project.Dir,
		Env:  env,
	})
	if err != nil {
		return toolError("adb install", result, err)
	}

	reportDir := filepath.Join(project.Dir, "build", "reports", "androidTests", options.variantLabel())
	if err := os.RemoveAll(reportDir); err != nil {
		return fmt.Errorf("clean %s: %w", reportDir, err)
	}
	if err := os.MkdirAll(reportDir, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", reportDir, err)
	}
	result, err = p.run(ctx, toolchain.Command{
		Path: adb,
		Args: append(append([]string(nil), prefix...),
			"shell", "am", "instrument", "-w", "-r", testPackage+"/"+runner),
		Dir: project.Dir,
		Env: env,
	})
	output := append(append([]byte(nil), result.Stdout...), result.Stderr...)
	if writeErr := os.WriteFile(filepath.Join(reportDir, "instrument.txt"), output, 0o644); writeErr != nil {
		return fmt.Errorf("write instrumentation report: %w", writeErr)
	}
	if err != nil {
		return toolError("am instrument", result, err)
	}
	if !strings.Contains(string(output), "OK (") && !strings.Contains(string(output), "FAILURES!!!") {
		return fmt.Errorf("instrumentation did not report a result:\n%s", output)
	}
	if strings.Contains(string(output), "FAILURES!!!") {
		return fmt.Errorf("instrumentation failures:\n%s", output)
	}
	return nil
}

func (o Options) androidTestAPKPath(projectDir string) string {
	name := "app-" + o.variantSuffixForFile() + "-androidTest.apk"
	return filepath.Join(projectDir, "build", "outputs", "apk", "androidTest", o.variantSuffixForFile(), name)
}

func (o Options) variantSuffixForFile() string {
	if o.Variant.Suffix == "" {
		return "debug"
	}
	return o.Variant.Suffix
}
