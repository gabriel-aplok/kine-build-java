package android

import (
	"archive/zip"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/gabriel-aplok/kine-build-java/internal/core"
	"github.com/gabriel-aplok/kine-build-java/internal/deps"
	"github.com/gabriel-aplok/kine-build-java/internal/toolchain"
)

const (
	moduleTypeFeature   = "feature"
	moduleTypeAssetPack = "asset_pack"
	firstModulePackage  = 0x80
)

// Module is one dynamic feature or asset-pack module in an Android project.
type Module struct {
	Name       string
	Type       string
	Manifest   string
	JavaDirs   []string
	KotlinDirs []string
	ResDirs    []string
	AssetDirs  []string
	PackageID  int
	DependsOn  []string
	Native     *NativeOptions
}

func resolveModules(projectDir string, values map[string]moduleFile) ([]Module, error) {
	names := make([]string, 0, len(values))
	for name := range values {
		names = append(names, name)
	}
	sort.Strings(names)

	modules := make([]Module, 0, len(names))
	usedPackages := make(map[int]bool)
	nextPackage := firstModulePackage
	for _, name := range names {
		value := values[name]
		if !variantNamePattern.MatchString(name) || strings.Contains(name, "-") {
			return nil, fmt.Errorf("module %q must match [a-z][a-z0-9_]*", name)
		}
		switch value.Type {
		case moduleTypeFeature, moduleTypeAssetPack:
		default:
			return nil, fmt.Errorf("module %q: type must be feature or asset_pack", name)
		}
		manifest, err := resolvePath(projectDir, value.Manifest)
		if err != nil {
			return nil, fmt.Errorf("module %q: manifest: %w", name, err)
		}
		info, err := os.Stat(manifest)
		if err != nil {
			return nil, fmt.Errorf("module %q: manifest %q: %w", name, value.Manifest, err)
		}
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("module %q: manifest %q is not a regular file", name, value.Manifest)
		}

		javaDirs, err := resolveDirs(projectDir, value.Java)
		if err != nil {
			return nil, fmt.Errorf("module %q: java: %w", name, err)
		}
		kotlinDirs, err := resolveDirs(projectDir, value.Kotlin)
		if err != nil {
			return nil, fmt.Errorf("module %q: kotlin: %w", name, err)
		}
		resDirs, err := resolveDirs(projectDir, value.Res)
		if err != nil {
			return nil, fmt.Errorf("module %q: res: %w", name, err)
		}
		assetDirs, err := resolveDirs(projectDir, value.Assets)
		if err != nil {
			return nil, fmt.Errorf("module %q: assets: %w", name, err)
		}
		if value.Type == moduleTypeAssetPack && (len(javaDirs) > 0 || len(kotlinDirs) > 0 || len(resDirs) > 0 || value.Native != nil) {
			return nil, fmt.Errorf("module %q: asset packs support assets only", name)
		}
		var moduleNative *NativeOptions
		if value.Native != nil {
			applyNativeDefaults(value.Native)
			if err := validateNative(value.Native); err != nil {
				return nil, fmt.Errorf("module %q: %w", name, err)
			}
			moduleNative, err = resolveNative(projectDir, value.Native)
			if err != nil {
				return nil, fmt.Errorf("module %q: native: %w", name, err)
			}
		}

		packageID := value.PackageID
		if value.Type == moduleTypeFeature {
			if packageID == 0 {
				for usedPackages[nextPackage] {
					nextPackage++
				}
				packageID = nextPackage
				nextPackage++
			}
			if packageID < 0x7f || packageID > 0xfe {
				return nil, fmt.Errorf("module %q: package_id must be between 0x7f and 0xfe", name)
			}
			if usedPackages[packageID] {
				return nil, fmt.Errorf("module %q: package_id 0x%x is already used", name, packageID)
			}
			usedPackages[packageID] = true
		}

		modules = append(modules, Module{
			Name:       name,
			Type:       value.Type,
			Manifest:   manifest,
			JavaDirs:   javaDirs,
			KotlinDirs: kotlinDirs,
			ResDirs:    resDirs,
			AssetDirs:  assetDirs,
			PackageID:  packageID,
			DependsOn:  append([]string(nil), value.DependsOn...),
			Native:     moduleNative,
		})
	}

	byName := make(map[string]Module, len(modules))
	for _, module := range modules {
		byName[module.Name] = module
	}
	for _, module := range modules {
		for _, dependency := range module.DependsOn {
			if dependency == module.Name {
				return nil, fmt.Errorf("module %q depends on itself", module.Name)
			}
			if _, ok := byName[dependency]; !ok {
				return nil, fmt.Errorf("module %q depends on unknown module %q", module.Name, dependency)
			}
		}
	}
	ordered, err := orderModules(modules, byName)
	if err != nil {
		return nil, err
	}
	return ordered, nil
}

func orderModules(modules []Module, byName map[string]Module) ([]Module, error) {
	state := make(map[string]int, len(modules))
	ordered := make([]Module, 0, len(modules))
	var visit func(string) error
	visit = func(name string) error {
		switch state[name] {
		case 1:
			return fmt.Errorf("module dependency cycle at %q", name)
		case 2:
			return nil
		}
		state[name] = 1
		module := byName[name]
		for _, dependency := range module.DependsOn {
			if err := visit(dependency); err != nil {
				return err
			}
		}
		state[name] = 2
		ordered = append(ordered, module)
		return nil
	}
	for _, module := range modules {
		if err := visit(module.Name); err != nil {
			return nil, err
		}
	}
	return ordered, nil
}

func (m Module) taskID(options Options, base string) string {
	return options.taskID("module-" + m.Name + "-" + base)
}

func (m Module) dir(options Options, projectDir string) string {
	return options.androidPath(projectDir, "modules", m.Name)
}

func (p *Provider) prepareModuleManifest(module Module, options Options, projectDir string) (string, error) {
	root, err := parseManifest(module.Manifest)
	if err != nil {
		return "", err
	}
	if packageName := attributeValue(root, "", "package"); packageName == "" {
		return "", fmt.Errorf("module %q: manifest package is missing", module.Name)
	}
	setManifestAttribute(root, "", "split", module.Name)
	ensureManifestNamespace(root, "dist", "http://schemas.android.com/apk/distribution")
	for _, dependency := range module.DependsOn {
		if hasUsesSplit(root, dependency) {
			continue
		}
		node := &manifestNode{Name: xmlName("", "uses-split")}
		setManifestAttribute(node, "android", "name", dependency)
		root.Children = append(root.Children, node)
	}

	if module.Type == moduleTypeAssetPack {
		removeManifestAttribute(root, "android", "versionCode")
		removeManifestAttribute(root, "android", "versionName")
	}

	distModule := findChildByNamespace(root, "dist", "module")
	if distModule == nil {
		distModule = &manifestNode{Name: xmlName("dist", "module")}
		root.Children = append(root.Children, distModule)
	}
	if module.Type == moduleTypeAssetPack {
		setManifestAttribute(distModule, "dist", "type", "asset-pack")
	} else {
		setManifestAttribute(root, "android", "isFeatureSplit", "true")
		setManifestAttribute(distModule, "dist", "instant", "false")
		if attributeValue(distModule, "dist", "title") == "" {
			setManifestAttribute(distModule, "dist", "title", module.Name)
		}
		application := findManifestChild(root, "application")
		if application == nil {
			application = &manifestNode{Name: xmlName("", "application")}
			root.Children = append(root.Children, application)
		}
		if attributeValue(application, "android", "hasCode") == "" {
			hasCode := len(module.JavaDirs) > 0
			setManifestAttribute(application, "android", "hasCode", strconv.FormatBool(hasCode))
		}
	}

	if findChildByNamespace(distModule, "dist", "fusing") == nil {
		fusing := &manifestNode{Name: xmlName("dist", "fusing")}
		setManifestAttribute(fusing, "dist", "include", "true")
		distModule.Children = append(distModule.Children, fusing)
	}
	if findChildByNamespace(distModule, "dist", "delivery") == nil {
		delivery := &manifestNode{Name: xmlName("dist", "delivery")}
		delivery.Children = append(delivery.Children, &manifestNode{Name: xmlName("dist", "install-time")})
		distModule.Children = append(distModule.Children, delivery)
	}

	target := filepath.Join(module.dir(options, projectDir), "AndroidManifest.xml")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return "", fmt.Errorf("create %s: %w", filepath.Dir(target), err)
	}
	if err := writeManifestFile(target, root); err != nil {
		return "", err
	}
	return target, nil
}

func findChildByNamespace(parent *manifestNode, namespace, name string) *manifestNode {
	for _, child := range parent.Children {
		if child.Name.Space == namespace && child.Name.Local == name {
			return child
		}
	}
	return nil
}

func hasUsesSplit(root *manifestNode, name string) bool {
	for _, child := range root.Children {
		if child.Name.Local == "uses-split" && attributeValue(child, "android", "name") == name {
			return true
		}
	}
	return false
}

func ensureManifestNamespace(root *manifestNode, prefix, uri string) {
	for _, attr := range root.Attr {
		if attr.Name.Space == "xmlns" && attr.Name.Local == prefix {
			return
		}
	}
	root.Attr = append(root.Attr, xmlAttr("xmlns", prefix, uri))
}

func removeManifestAttribute(node *manifestNode, namespace, name string) {
	attributes := node.Attr[:0]
	for _, attr := range node.Attr {
		if attr.Name.Space == namespace && attr.Name.Local == name {
			continue
		}
		attributes = append(attributes, attr)
	}
	node.Attr = attributes
}

func moduleDependencyModules(options Options, module Module) []Module {
	byName := make(map[string]Module, len(options.Modules))
	for _, candidate := range options.Modules {
		byName[candidate.Name] = candidate
	}
	result := make([]Module, 0, len(module.DependsOn))
	for _, name := range module.DependsOn {
		if dependency, ok := byName[name]; ok {
			result = append(result, dependency)
		}
	}
	return result
}

func (p *Provider) moduleTasks(workspaceRoot string, project *core.Project, options Options, base Options, sdk *SDK, jdk *toolchain.JDK, artifacts []deps.Artifact) ([]*core.Task, []string, []string, error) {
	var tasks []*core.Task
	var zips []string
	var zipTaskIDs []string
	for _, module := range options.Modules {
		moduleTasks, zip, zipID, err := p.moduleTaskSet(workspaceRoot, project, options, base, sdk, jdk, artifacts, module)
		if err != nil {
			return nil, nil, nil, err
		}
		tasks = append(tasks, moduleTasks...)
		zips = append(zips, zip)
		zipTaskIDs = append(zipTaskIDs, zipID)
	}
	return tasks, zips, zipTaskIDs, nil
}

func (p *Provider) moduleTaskSet(workspaceRoot string, project *core.Project, options Options, base Options, sdk *SDK, jdk *toolchain.JDK, artifacts []deps.Artifact, module Module) ([]*core.Task, string, string, error) {
	dir := module.dir(options, project.Dir)
	protoAPK := filepath.Join(dir, "proto.apk")
	genDir := filepath.Join(dir, "gen")
	classesDir := filepath.Join(dir, "classes")
	kotlinDir := filepath.Join(dir, "classes-kotlin")
	dexDir := filepath.Join(dir, "dex")
	nativeDir := filepath.Join(dir, "native")
	nativeBuildDir := filepath.Join(dir, "native-build")
	zipPath := filepath.Join(dir, module.Name+".zip")

	var tasks []*core.Task
	dependencies := moduleDependencyModules(options, module)
	resourceID := module.taskID(options, "resources")
	inputs := []core.Input{{Path: mustRelative(workspaceRoot, module.Manifest), Mode: core.ModeFile}}
	for _, res := range module.ResDirs {
		inputs = append(inputs, core.Input{Path: mustRelative(workspaceRoot, res), Mode: core.ModeTree})
	}
	outputs := []core.Output{
		{Path: mustRelative(workspaceRoot, protoAPK), Mode: core.ModeFile},
		{Path: mustRelative(workspaceRoot, filepath.Join(dir, "AndroidManifest.xml")), Mode: core.ModeFile},
	}
	if module.Type == moduleTypeFeature {
		outputs = append(outputs, core.Output{Path: mustRelative(workspaceRoot, genDir), Mode: core.ModeTree})
	}
	if module.Type == moduleTypeFeature && options.Variant.BuildTypeOptions.ShrinkResources {
		outputs = append(outputs, core.Output{Path: mustRelative(workspaceRoot, filepath.Join(dir, "res-shrunk")), Mode: core.ModeTree})
		for _, sourceDir := range append(append([]string(nil), module.JavaDirs...), module.KotlinDirs...) {
			inputs = append(inputs, core.Input{Path: mustRelative(workspaceRoot, sourceDir), Mode: core.ModeTree})
		}
	}
	resourceDeps := []string(nil)
	if module.Type == moduleTypeFeature {
		resourceDeps = append(resourceDeps, project.ID+":"+base.taskID("android-resources"))
	}
	for _, dependency := range dependencies {
		resourceDeps = append(resourceDeps, project.ID+":"+dependency.taskID(options, "resources"))
	}
	resources := &core.Task{
		ID:      module.taskID(options, "resources"),
		Deps:    resourceDeps,
		Inputs:  inputs,
		Outputs: outputs,
		Key: fmt.Sprintf("module-resources %s type=%s package=%#x code=%d shrink=%t v=%d",
			module.Name, module.Type, module.PackageID, options.effectiveVersionCode(),
			options.Variant.BuildTypeOptions.ShrinkResources, androidTaskVersion),
		Run: func(ctx context.Context) error {
			return p.buildModuleResources(ctx, project, options, sdk, jdk, module)
		},
	}
	tasks = append(tasks, resources)

	var kotlinID, compileID, dexID, nativeID string
	if module.Type == moduleTypeFeature && len(module.KotlinDirs) > 0 {
		kotlinID = module.taskID(options, "compile-kotlin")
		kotlinDeps := []string{project.ID + ":" + resourceID, project.ID + ":" + base.taskID("compile-java")}
		if len(base.KotlinDirs) > 0 {
			kotlinDeps = append(kotlinDeps, project.ID+":"+base.taskID("compile-kotlin"))
		}
		for _, dependency := range dependencies {
			if len(dependency.JavaDirs) > 0 {
				kotlinDeps = append(kotlinDeps, project.ID+":"+dependency.taskID(options, "compile-java"))
			}
			if len(dependency.KotlinDirs) > 0 {
				kotlinDeps = append(kotlinDeps, project.ID+":"+dependency.taskID(options, "compile-kotlin"))
			}
		}
		kotlinInputs := []core.Input{
			{Path: mustRelative(workspaceRoot, genDir), Mode: core.ModeTree},
			{Path: mustRelative(workspaceRoot, base.androidPath(project.Dir, "classes")), Mode: core.ModeTree},
			{Path: mustRelative(workspaceRoot, options.KotlinStdlib), Mode: core.ModeFile},
		}
		for _, dir := range append(append([]string(nil), module.KotlinDirs...), module.JavaDirs...) {
			kotlinInputs = append(kotlinInputs, core.Input{Path: mustRelative(workspaceRoot, dir), Mode: core.ModeTree})
		}
		if len(base.KotlinDirs) > 0 {
			kotlinInputs = append(kotlinInputs, core.Input{Path: mustRelative(workspaceRoot, base.androidPath(project.Dir, "classes-kotlin")), Mode: core.ModeTree})
		}
		if options.ComposePlugin != "" {
			kotlinInputs = append(kotlinInputs, core.Input{Path: mustRelative(workspaceRoot, options.ComposePlugin), Mode: core.ModeFile})
		}
		for _, artifact := range artifacts {
			if strings.HasPrefix(artifact.Classifier, "natives-") {
				continue
			}
			kotlinInputs = append(kotlinInputs, core.Input{Path: mustRelative(workspaceRoot, artifact.Path), Mode: core.ModeFile})
		}
		kotlin := &core.Task{
			ID:      kotlinID,
			Deps:    kotlinDeps,
			Inputs:  kotlinInputs,
			Outputs: []core.Output{{Path: mustRelative(workspaceRoot, kotlinDir), Mode: core.ModeTree}},
			Key:     fmt.Sprintf("module-kotlinc %s %s java=%d compose=%t v=%d", module.Name, options.KotlinVersion, javaVersion(options), options.Compose, androidTaskVersion),
			Run: func(ctx context.Context) error {
				return p.compileModuleKotlin(ctx, project, options, base, sdk, jdk, artifacts, module, genDir, kotlinDir)
			},
		}
		tasks = append(tasks, kotlin)
	}

	if module.Type == moduleTypeFeature && len(module.JavaDirs) > 0 {
		compileID = module.taskID(options, "compile-java")
		compileDeps := []string{project.ID + ":" + resourceID, project.ID + ":" + base.taskID("compile-java")}
		if kotlinID != "" {
			compileDeps = append(compileDeps, project.ID+":"+kotlinID)
		}
		for _, dependency := range dependencies {
			if len(dependency.JavaDirs) > 0 {
				compileDeps = append(compileDeps, project.ID+":"+dependency.taskID(options, "compile-java"))
			}
			if len(dependency.KotlinDirs) > 0 {
				compileDeps = append(compileDeps, project.ID+":"+dependency.taskID(options, "compile-kotlin"))
			}
		}
		compileInputs := []core.Input{
			{Path: mustRelative(workspaceRoot, genDir), Mode: core.ModeTree},
			{Path: mustRelative(workspaceRoot, base.androidPath(project.Dir, "classes")), Mode: core.ModeTree},
		}
		if kotlinID != "" {
			compileInputs = append(compileInputs, core.Input{Path: mustRelative(workspaceRoot, kotlinDir), Mode: core.ModeTree})
		}
		for _, javaDir := range module.JavaDirs {
			compileInputs = append(compileInputs, core.Input{Path: mustRelative(workspaceRoot, javaDir), Mode: core.ModeTree})
		}
		for _, artifact := range artifacts {
			if strings.HasPrefix(artifact.Classifier, "natives-") {
				continue
			}
			compileInputs = append(compileInputs, core.Input{Path: mustRelative(workspaceRoot, artifact.Path), Mode: core.ModeFile})
		}
		compile := &core.Task{
			ID:      compileID,
			Deps:    compileDeps,
			Inputs:  compileInputs,
			Outputs: []core.Output{{Path: mustRelative(workspaceRoot, classesDir), Mode: core.ModeTree}},
			Key:     fmt.Sprintf("module-javac %s v=%d", module.Name, androidTaskVersion),
			Run: func(ctx context.Context) error {
				return p.compileModuleJava(ctx, project, options, base, sdk, jdk, artifacts, module, genDir, kotlinDir, classesDir)
			},
		}
		tasks = append(tasks, compile)
	}

	if module.Type == moduleTypeFeature && (compileID != "" || kotlinID != "") {
		dexID = module.taskID(options, "dex")
		dexDeps := []string{project.ID + ":" + base.taskID("dex")}
		for _, dependency := range dependencies {
			if len(dependency.JavaDirs) > 0 || len(dependency.KotlinDirs) > 0 {
				dexDeps = append(dexDeps, project.ID+":"+dependency.taskID(options, "dex"))
			}
		}
		var classesDirs []string
		if compileID != "" {
			dexDeps = append(dexDeps, project.ID+":"+compileID)
			classesDirs = append(classesDirs, classesDir)
		}
		if kotlinID != "" {
			dexDeps = append(dexDeps, project.ID+":"+kotlinID)
			classesDirs = append(classesDirs, kotlinDir)
		}
		dexInputs := []core.Input{{Path: mustRelative(workspaceRoot, base.androidPath(project.Dir, "classes.jar")), Mode: core.ModeFile}}
		for _, dir := range classesDirs {
			dexInputs = append(dexInputs, core.Input{Path: mustRelative(workspaceRoot, dir), Mode: core.ModeTree})
		}
		if options.Variant.BuildTypeOptions.Minify {
			dexInputs = append(dexInputs,
				core.Input{Path: mustRelative(workspaceRoot, filepath.Join(dir, "AndroidManifest.xml")), Mode: core.ModeFile},
			)
			for _, rule := range append(append([]string(nil), options.Variant.BuildTypeOptions.Proguard...), options.AARProguard...) {
				dexInputs = append(dexInputs, core.Input{Path: mustRelative(workspaceRoot, rule), Mode: core.ModeFile})
			}
		}
		dexOutputs := []core.Output{
			{Path: mustRelative(workspaceRoot, dexDir), Mode: core.ModeTree},
			{Path: mustRelative(workspaceRoot, filepath.Join(dir, "classes.jar")), Mode: core.ModeFile},
		}
		dex := &core.Task{
			ID:      dexID,
			Deps:    dexDeps,
			Inputs:  dexInputs,
			Outputs: dexOutputs,
			Key: fmt.Sprintf("module-dex %s v=%d min=%d minify=%t r8=%s rules=%d",
				module.Name, androidTaskVersion, options.MinSDK, options.Variant.BuildTypeOptions.Minify, sdk.R8Jar, r8RulesVersion),
			Run: func(ctx context.Context) error {
				return p.dexModule(ctx, project, options, base, sdk, jdk, artifacts, module, classesDirs, dexDir)
			},
		}
		tasks = append(tasks, dex)
	}

	if module.Native != nil {
		nativeID = module.taskID(options, "native")
		sourceDir, err := relativePath(workspaceRoot, filepath.Dir(module.Native.CMake))
		if err != nil {
			return nil, "", "", err
		}
		native := &core.Task{
			ID:      nativeID,
			Inputs:  []core.Input{{Path: sourceDir, Mode: core.ModeTree}},
			Outputs: []core.Output{{Path: mustRelative(workspaceRoot, nativeDir), Mode: core.ModeTree}},
			Key: fmt.Sprintf("module-cmake %s %s ndk=%s abi=%s build=%s min=%d v=%d",
				module.Name, sdk.CMakeVersion, sdk.NDKVersion, strings.Join(module.Native.ABIs, ","), module.Native.BuildType, options.MinSDK, androidTaskVersion),
			Run: func(ctx context.Context) error {
				return p.buildNativeOptions(ctx, project, module.Native, options.MinSDK, sdk, jdk, nativeDir, nativeBuildDir)
			},
		}
		tasks = append(tasks, native)
	}

	zipID := module.taskID(options, "zip")
	zipInputs := []core.Input{
		{Path: mustRelative(workspaceRoot, protoAPK), Mode: core.ModeFile},
		{Path: mustRelative(workspaceRoot, filepath.Join(dir, "AndroidManifest.xml")), Mode: core.ModeFile},
	}
	deps := []string{project.ID + ":" + resourceID}
	for _, dependency := range dependencies {
		deps = append(deps, project.ID+":"+dependency.taskID(options, "zip"))
	}
	if dexID != "" {
		zipInputs = append(zipInputs, core.Input{Path: mustRelative(workspaceRoot, dexDir), Mode: core.ModeTree})
		deps = append(deps, project.ID+":"+dexID)
	}
	if nativeID != "" {
		zipInputs = append(zipInputs, core.Input{Path: mustRelative(workspaceRoot, nativeDir), Mode: core.ModeTree})
		deps = append(deps, project.ID+":"+nativeID)
	}
	for _, assets := range module.AssetDirs {
		zipInputs = append(zipInputs, core.Input{Path: mustRelative(workspaceRoot, assets), Mode: core.ModeTree})
	}
	zipTask := &core.Task{
		ID:      zipID,
		Deps:    deps,
		Inputs:  zipInputs,
		Outputs: []core.Output{{Path: mustRelative(workspaceRoot, zipPath), Mode: core.ModeFile}},
		Key:     fmt.Sprintf("module-zip %s type=%s shrink=%t v=%d", module.Name, module.Type, options.Variant.BuildTypeOptions.ShrinkResources, androidTaskVersion),
		Run: func(ctx context.Context) error {
			return p.buildModuleZip(ctx, project, options, module, protoAPK, dexDir, nativeDir, zipPath)
		},
	}
	tasks = append(tasks, zipTask)
	return tasks, zipPath, zipID, nil
}

func (p *Provider) buildModuleResources(ctx context.Context, project *core.Project, options Options, sdk *SDK, jdk *toolchain.JDK, module Module) error {
	dir := module.dir(options, project.Dir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}
	manifest, err := p.prepareModuleManifest(module, options, project.Dir)
	if err != nil {
		return err
	}
	resDirs := module.ResDirs
	if module.Type == moduleTypeFeature && options.Variant.BuildTypeOptions.ShrinkResources {
		shrunk := filepath.Join(dir, "res-shrunk")
		filtered, err := shrinkFeatureResources(module, shrunk)
		if err != nil {
			return err
		}
		resDirs = filtered
	}
	flats := make([]string, 0, len(resDirs))
	for index, res := range resDirs {
		flat := filepath.Join(dir, fmt.Sprintf("res-%d.zip", index))
		result, err := p.run(ctx, toolchain.Command{
			Path: sdk.Aapt2,
			Args: []string{"compile", "--dir", res, "-o", flat},
			Dir:  project.Dir,
			Env:  toolchain.BuildEnv(jdk, filepath.Join(project.Dir, ".kine", "tmp"), nil),
		})
		if err != nil {
			return toolError("aapt2 compile", result, err)
		}
		flats = append(flats, flat)
	}

	proto := filepath.Join(dir, "proto.apk")
	args := []string{
		"link", "-o", proto,
		"-I", sdk.AndroidJar,
		"--manifest", manifest,
		"--proto-format",
	}
	if module.Type == moduleTypeFeature {
		args = append(args, "-I", options.androidPath(project.Dir, resourcesAPKName))
		args = append(args,
			"--package-id", fmt.Sprintf("0x%x", module.PackageID),
			"--java", filepath.Join(dir, "gen"),
			"--min-sdk-version", strconv.Itoa(options.MinSDK),
			"--version-code", strconv.Itoa(options.effectiveVersionCode()),
			"--version-name", options.effectiveVersionName(),
		)
		if options.TargetSDK > 0 {
			args = append(args, "--target-sdk-version", strconv.Itoa(options.TargetSDK))
		}
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
		return toolError("aapt2 link", result, err)
	}
	return nil
}

func (p *Provider) compileModuleJava(ctx context.Context, project *core.Project, options Options, base Options, sdk *SDK, jdk *toolchain.JDK, artifacts []deps.Artifact, module Module, genDir, kotlinDir, output string) error {
	if err := os.RemoveAll(output); err != nil {
		return fmt.Errorf("clean %s: %w", output, err)
	}
	if err := os.MkdirAll(output, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", output, err)
	}
	sources, err := sourceFiles(module.JavaDirs, ".java")
	if err != nil {
		return err
	}
	generated, err := sourceFiles([]string{genDir}, ".java")
	if err != nil {
		return err
	}
	sources = append(sources, generated...)
	sort.Strings(sources)
	if len(sources) == 0 {
		return nil
	}
	jars, err := classpathJars(artifacts, filepath.Join(project.Dir, "build", "android", "aar"))
	if err != nil {
		return err
	}
	classpath := []string{sdk.AndroidJar, base.androidPath(project.Dir, "classes")}
	if isDir(kotlinDir) {
		classpath = append(classpath, kotlinDir)
	}
	if len(base.KotlinDirs) > 0 {
		classpath = append(classpath, base.androidPath(project.Dir, "classes-kotlin"))
	}
	for _, dependency := range moduleDependencyModules(options, module) {
		if len(dependency.JavaDirs) > 0 {
			classpath = append(classpath, filepath.Join(dependency.dir(options, project.Dir), "classes"))
		}
		if len(dependency.KotlinDirs) > 0 {
			classpath = append(classpath, filepath.Join(dependency.dir(options, project.Dir), "classes-kotlin"))
		}
	}
	classpath = append(classpath, jars...)
	result, err := p.run(ctx, toolchain.Command{
		Path: jdk.Javac,
		Args: javacArgs(output, classpath, sources, javaVersion(options)),
		Dir:  project.Dir,
		Env:  toolchain.BuildEnv(jdk, filepath.Join(project.Dir, ".kine", "tmp"), nil),
	})
	if err != nil {
		return toolError("module javac", result, err)
	}
	return nil
}

func (p *Provider) compileModuleKotlin(ctx context.Context, project *core.Project, options Options, base Options, sdk *SDK, jdk *toolchain.JDK, artifacts []deps.Artifact, module Module, genDir, output string) error {
	if err := os.RemoveAll(output); err != nil {
		return fmt.Errorf("clean %s: %w", output, err)
	}
	if err := os.MkdirAll(output, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", output, err)
	}
	sources, err := sourceFiles(module.KotlinDirs, ".kt")
	if err != nil {
		return err
	}
	if len(sources) == 0 {
		return nil
	}
	jars, err := classpathJars(artifacts, filepath.Join(project.Dir, "build", "android", "aar"))
	if err != nil {
		return err
	}
	classpath := []string{options.KotlinStdlib, sdk.AndroidJar, base.androidPath(project.Dir, "classes")}
	if len(base.KotlinDirs) > 0 {
		classpath = append(classpath, base.androidPath(project.Dir, "classes-kotlin"))
	}
	for _, dependency := range moduleDependencyModules(options, module) {
		if len(dependency.JavaDirs) > 0 {
			classpath = append(classpath, filepath.Join(dependency.dir(options, project.Dir), "classes"))
		}
		if len(dependency.KotlinDirs) > 0 {
			classpath = append(classpath, filepath.Join(dependency.dir(options, project.Dir), "classes-kotlin"))
		}
	}
	classpath = append(classpath, jars...)
	javaRoots := append([]string(nil), module.JavaDirs...)
	javaRoots = append(javaRoots, genDir)

	result, err := p.run(ctx, toolchain.Command{
		Path: options.KotlinCompiler,
		Args: kotlinArgs(options, output, classpath, javaRoots, sources),
		Dir:  project.Dir,
		Env:  toolchain.BuildEnv(jdk, filepath.Join(project.Dir, ".kine", "tmp"), nil),
	})
	if err != nil {
		return toolError("module kotlinc", result, err)
	}
	return nil
}

func (p *Provider) runModuleR8(ctx context.Context, project *core.Project, options Options, base Options, sdk *SDK, jdk *toolchain.JDK, artifacts []deps.Artifact, module Module, classesDirs []string, output string) error {
	if sdk.R8Jar == "" {
		return fmt.Errorf("R8 jar is missing from build-tools %s", sdk.BuildTools)
	}
	if err := os.RemoveAll(output); err != nil {
		return fmt.Errorf("clean %s: %w", output, err)
	}
	if err := os.MkdirAll(output, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", output, err)
	}
	classesJar := filepath.Join(module.dir(options, project.Dir), "classes.jar")
	if err := jarClasses(classesDirs, classesJar); err != nil {
		return err
	}
	rules, err := p.writeModuleR8Rules(project, options, module)
	if err != nil {
		return err
	}
	mappingDir := filepath.Join(project.Dir, "build", "outputs", "mapping", options.variantLabel())
	if err := os.MkdirAll(mappingDir, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", mappingDir, err)
	}
	jars, err := classpathJars(artifacts, filepath.Join(project.Dir, "build", "android", "aar"))
	if err != nil {
		return err
	}
	if options.KotlinStdlib != "" {
		jars = append(jars, options.KotlinStdlib)
	}
	args := []string{
		"-cp", sdk.R8Jar,
		"com.android.tools.r8.R8",
		"--release",
		"--min-api", strconv.Itoa(options.MinSDK),
		"--lib", sdk.AndroidJar,
		"--output", output,
		"--pg-conf", rules,
		"--pg-map-output", filepath.Join(mappingDir, module.Name+"-mapping.txt"),
	}

	classpath := []string{base.androidPath(project.Dir, "classes.jar")}
	for _, dependency := range moduleDependencyModules(options, module) {
		classpath = append(classpath, filepath.Join(dependency.dir(options, project.Dir), "classes.jar"))
	}
	classpath = append(classpath, jars...)
	for _, jar := range classpath {
		args = append(args, "--classpath", jar)
	}
	args = append(args, classesJar)
	result, err := p.run(ctx, toolchain.Command{
		Path: jdk.Java,
		Args: args,
		Dir:  project.Dir,
		Env:  toolchain.BuildEnv(jdk, filepath.Join(project.Dir, ".kine", "tmp"), nil),
	})
	if err != nil {
		return toolError("module r8", result, err)
	}
	return nil
}

func (p *Provider) writeModuleR8Rules(project *core.Project, options Options, module Module) (string, error) {
	dir := filepath.Join(module.dir(options, project.Dir), "r8")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create %s: %w", dir, err)
	}
	var buffer strings.Builder
	buffer.WriteString(defaultR8Rules)
	manifestPath := filepath.Join(module.dir(options, project.Dir), "AndroidManifest.xml")
	if manifest, err := parseManifest(manifestPath); err == nil {
		if application := findManifestChild(manifest, "application"); application != nil {
			for _, child := range application.Children {
				if child.Name.Local != "activity" && child.Name.Local != "service" &&
					child.Name.Local != "receiver" && child.Name.Local != "provider" {
					continue
				}
				name := attributeValue(child, "android", "name")
				if name == "" {
					continue
				}
				if strings.HasPrefix(name, ".") {
					name = attributeValue(manifest, "", "package") + name
				}
				buffer.WriteString("-keep class " + name + " { *; }\n")
			}
		}
	}
	rules := append([]string(nil), options.Variant.BuildTypeOptions.Proguard...)
	rules = append(rules, options.AARProguard...)
	for _, rule := range rules {
		data, err := os.ReadFile(rule)
		if err != nil {
			return "", fmt.Errorf("read proguard rules %s: %w", rule, err)
		}
		buffer.WriteString("\n# " + filepath.Base(rule) + "\n")
		buffer.Write(data)
		if !strings.HasSuffix(string(data), "\n") {
			buffer.WriteByte('\n')
		}
	}
	target := filepath.Join(dir, "rules.pro")
	if err := os.WriteFile(target, []byte(buffer.String()), 0o644); err != nil {
		return "", fmt.Errorf("write %s: %w", target, err)
	}
	return target, nil
}

func (p *Provider) dexModule(ctx context.Context, project *core.Project, options Options, base Options, sdk *SDK, jdk *toolchain.JDK, artifacts []deps.Artifact, module Module, classesDirs []string, output string) error {
	if options.Variant.BuildTypeOptions.Minify {
		return p.runModuleR8(ctx, project, options, base, sdk, jdk, artifacts, module, classesDirs, output)
	}
	if err := os.RemoveAll(output); err != nil {
		return fmt.Errorf("clean %s: %w", output, err)
	}
	if err := os.MkdirAll(output, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", output, err)
	}
	classesJar := filepath.Join(module.dir(options, project.Dir), "classes.jar")
	if err := jarClasses(classesDirs, classesJar); err != nil {
		return err
	}
	jars, err := classpathJars(artifacts, filepath.Join(project.Dir, "build", "android", "aar"))
	if err != nil {
		return err
	}
	args := []string{
		"--lib", sdk.AndroidJar,
		"--min-api", strconv.Itoa(options.MinSDK),
		"--classpath", base.androidPath(project.Dir, "classes.jar"),
		"--output", output,
	}
	for _, dependency := range moduleDependencyModules(options, module) {
		args = append(args, "--classpath", filepath.Join(dependency.dir(options, project.Dir), "classes.jar"))
	}
	for _, jar := range jars {
		args = append(args, "--classpath", jar)
	}
	args = append(args, classesJar)
	result, err := p.run(ctx, toolchain.Command{
		Path: sdk.D8,
		Args: args,
		Dir:  project.Dir,
		Env:  toolchain.BuildEnv(jdk, filepath.Join(project.Dir, ".kine", "tmp"), nil),
	})
	if err != nil {
		return toolError("module d8", result, err)
	}
	return nil
}

func (p *Provider) buildModuleZip(ctx context.Context, project *core.Project, options Options, module Module, protoAPK, dexDir, nativeDir, output string) error {
	baseDir := filepath.Join(module.dir(options, project.Dir), "base")
	if err := os.RemoveAll(baseDir); err != nil {
		return fmt.Errorf("clean %s: %w", baseDir, err)
	}
	if err := os.MkdirAll(baseDir, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", baseDir, err)
	}
	if err := stageModuleProto(protoAPK, baseDir, module.Type == moduleTypeFeature); err != nil {
		return err
	}
	if module.Type == moduleTypeFeature && isDir(dexDir) {
		if err := stageModuleDex(baseDir, dexDir); err != nil {
			return err
		}
	}
	if module.Native != nil && isDir(nativeDir) {
		if err := stageModuleNatives(baseDir, nativeDir); err != nil {
			return err
		}
	}
	if err := stageModuleAssets(baseDir, module.AssetDirs); err != nil {
		return err
	}
	return zipDirectory(baseDir, output)
}

func stageModuleProto(protoAPK, baseDir string, includeResources bool) error {
	reader, err := zip.OpenReader(protoAPK)
	if err != nil {
		return fmt.Errorf("open %s: %w", protoAPK, err)
	}
	defer reader.Close()
	for _, entry := range reader.File {
		if entry.FileInfo().IsDir() {
			continue
		}
		var target string
		switch {
		case entry.Name == "AndroidManifest.xml":
			target = filepath.Join(baseDir, "manifest", "AndroidManifest.xml")
		case entry.Name == "resources.pb" && includeResources:
			target = filepath.Join(baseDir, "resources.pb")
		case strings.HasPrefix(entry.Name, "res/") && includeResources:
			target = filepath.Join(baseDir, filepath.FromSlash(entry.Name))
		default:
			continue
		}
		if err := copyZipEntry(entry, target); err != nil {
			return err
		}
	}
	return nil
}

func stageModuleDex(baseDir, dexDir string) error {
	entries, err := os.ReadDir(dexDir)
	if err != nil {
		return fmt.Errorf("read %s: %w", dexDir, err)
	}
	var names []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".dex") {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)
	for index, name := range names {
		target := filepath.Join(baseDir, "dex", "classes.dex")
		if index > 0 {
			target = filepath.Join(baseDir, "dex", fmt.Sprintf("classes%d.dex", index+1))
		}
		if err := copyFile(filepath.Join(dexDir, name), target); err != nil {
			return err
		}
	}
	return nil
}

func stageModuleNatives(baseDir, nativeDir string) error {
	natives, err := nativeLibrariesFromDirs([]string{nativeDir})
	if err != nil {
		return err
	}
	for _, native := range natives {
		if err := copyFile(native.path, filepath.Join(baseDir, "lib", native.abi, native.name)); err != nil {
			return err
		}
	}
	return nil
}

func stageModuleAssets(baseDir string, dirs []string) error {
	assets, err := assetFiles(dirs)
	if err != nil {
		return err
	}
	for _, asset := range assets {
		if err := copyFile(asset.path, filepath.Join(baseDir, filepath.FromSlash(asset.name))); err != nil {
			return err
		}
	}
	return nil
}
