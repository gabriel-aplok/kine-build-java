package android

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/pelletier/go-toml/v2"

	"github.com/gabriel-aplok/kine-build-java/internal/core"
	"github.com/gabriel-aplok/kine-build-java/internal/deps"
)

const (
	defaultMinSDK      = 24
	defaultVersionCode = 1
	defaultVersionName = "1.0"
	defaultManifest    = "src/main/AndroidManifest.xml"
	defaultJava        = 17

	defaultNativeCMake           = "src/main/cpp/CMakeLists.txt"
	defaultNativeABI             = "arm64-v8a"
	defaultNativeBuildType       = "Debug"
	defaultNativeSTL             = "c++_static"
	defaultBundletoolVersion     = "1.18.3"
	defaultManifestMergerVersion = "32.5.0"
	defaultJUnitVersion          = "1.11.4"
)

var (
	packagePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*(\.[A-Za-z_][A-Za-z0-9_]*)*$`)
	supportedABIs  = map[string]bool{
		"armeabi-v7a": true,
		"arm64-v8a":   true,
		"x86":         true,
		"x86_64":      true,
		"riscv64":     true,
	}
)

// Sources lists project-relative source locations.
type Sources struct {
	Java                 []string `toml:"java"`
	Kotlin               []string `toml:"kotlin"`
	Res                  []string `toml:"res"`
	Manifest             string   `toml:"manifest"`
	Assets               []string `toml:"assets"`
	Test                 []string `toml:"test"`
	TestResources        []string `toml:"test_resources"`
	AndroidTest          []string `toml:"android_test"`
	AndroidTestResources []string `toml:"android_test_resources"`
	AndroidTestManifest  string   `toml:"android_test_manifest"`
}

type buildTypeFile struct {
	Minify          bool     `toml:"minify"`
	ShrinkResources bool     `toml:"shrink_resources"`
	Proguard        []string `toml:"proguard"`
	Debug           *bool    `toml:"debug"`
	Signing         string   `toml:"signing"`
}

type moduleFile struct {
	Type      string         `toml:"type"`
	Manifest  string         `toml:"manifest"`
	Java      []string       `toml:"java"`
	Kotlin    []string       `toml:"kotlin"`
	Res       []string       `toml:"res"`
	Assets    []string       `toml:"assets"`
	PackageID int            `toml:"package_id"`
	DependsOn []string       `toml:"depends_on"`
	Native    *NativeOptions `toml:"native"`
}

type flavorFile struct {
	ApplicationIDSuffix string `toml:"application_id_suffix"`
	VersionNameSuffix   string `toml:"version_name_suffix"`
	VersionCodeOffset   int    `toml:"version_code_offset"`
	Signing             string `toml:"signing"`
}

// Dependencies lists compile dependencies.
type Dependencies struct {
	Compile []string `toml:"compile"`
	Runtime []string `toml:"runtime"`
}

// SigningOptions selects an existing debug keystore.
type SigningOptions struct {
	Keystore      string `toml:"keystore"`
	Alias         string `toml:"alias"`
	StorePassword string `toml:"store_password"`
	KeyPassword   string `toml:"key_password"`
	Unresolved    bool   `toml:"-"`
}

// NativeOptions configures an optional CMake build through the NDK.
type NativeOptions struct {
	CMake        string   `toml:"cmake"`
	ABIs         []string `toml:"abi"`
	BuildType    string   `toml:"build_type"`
	STL          string   `toml:"stl"`
	NDKVersion   string   `toml:"ndk_version"`
	CMakeVersion string   `toml:"cmake_version"`
}

// Options is the resolved android project configuration.
type Options struct {
	CompileSDK              int
	BuildTools              string
	MinSDK                  int
	TargetSDK               int
	Java                    int
	Kotlin                  string
	KotlinHome              string
	Compose                 bool
	KotlinCompiler          string
	KotlinStdlib            string
	KotlinVersion           string
	ComposePlugin           string
	Bundle                  bool
	BundletoolVersion       string
	ManifestMerger          string
	ManifestMergerVersion   string
	Modules                 []Module
	SigningConfigs          map[string]*SigningOptions
	VersionCodeStrategy     string
	BundletoolMain          string
	BundletoolClasspath     []string
	VersionCode             int
	VersionName             string
	PackageName             string
	ApplicationID           string
	JavaDirs                []string
	KotlinDirs              []string
	ResDirs                 []string
	AssetDirs               []string
	Manifest                string
	BaseManifest            string
	Manifests               []string
	AARResDirs              []string
	AARAssetDirs            []string
	AARManifests            []string
	AARNativeDirs           []string
	AARProguard             []string
	BuildTypes              []string
	Flavors                 []string
	BuildTypeConfig         map[string]BuildTypeOptions
	FlavorConfig            map[string]FlavorOptions
	Variants                []Variant
	Variant                 Variant
	Legacy                  bool
	ExtraPackages           []string
	Dependencies            []deps.Dependency
	TestDependencies        []deps.Dependency
	TestDirs                []string
	TestResourceDirs        []string
	AndroidTestDirs         []string
	AndroidTestResourceDirs []string
	AndroidTestManifest     string
	JUnitVersion            string
	JUnitJar                string
	Native                  *NativeOptions
	Signing                 *SigningOptions
	ReleaseSigning          *SigningOptions
}

type optionsFile struct {
	CompileSDK       int                       `toml:"compile_sdk"`
	BuildTools       string                    `toml:"build_tools"`
	MinSDK           int                       `toml:"min_sdk"`
	TargetSDK        int                       `toml:"target_sdk"`
	Java             int                       `toml:"java"`
	Kotlin           string                    `toml:"kotlin"`
	KotlinHome       string                    `toml:"kotlin_home"`
	Compose          bool                      `toml:"compose"`
	Bundle           bool                      `toml:"bundle"`
	Bundletool       string                    `toml:"bundletool_version"`
	ManifestMerger   string                    `toml:"manifest_merger"`
	MergerVersion    string                    `toml:"manifest_merger_version"`
	Modules          map[string]moduleFile     `toml:"module"`
	SigningConfigs   map[string]SigningOptions `toml:"signing_config"`
	VersionStrategy  string                    `toml:"version_code_strategy"`
	VersionCode      int                       `toml:"version_code"`
	VersionName      string                    `toml:"version_name"`
	ExtraPackages    []string                  `toml:"extra_packages"`
	BuildTypes       []string                  `toml:"build_types"`
	Flavors          []string                  `toml:"flavors"`
	BuildTypeConfig  map[string]buildTypeFile  `toml:"build_type"`
	FlavorConfig     map[string]flavorFile     `toml:"flavor"`
	Sources          Sources                   `toml:"sources"`
	Dependencies     Dependencies              `toml:"dependencies"`
	TestDependencies Dependencies              `toml:"test_dependencies"`
	JUnitVersion     string                    `toml:"junit_version"`
	Native           *NativeOptions            `toml:"native"`
	Signing          *SigningOptions           `toml:"signing"`
	ReleaseSigning   *SigningOptions           `toml:"release_signing"`
}

func decodeOptions(values map[string]any, projectDir string) (Options, error) {
	var file optionsFile
	if len(values) > 0 {
		data, err := toml.Marshal(values)
		if err != nil {
			return Options{}, fmt.Errorf("encode options: %w", err)
		}
		decoder := toml.NewDecoder(bytes.NewReader(data))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&file); err != nil {
			return Options{}, fmt.Errorf("decode options: %w", err)
		}
	}

	applyOptionDefaults(&file)
	if err := validateOptions(file); err != nil {
		return Options{}, err
	}

	manifest, err := resolvePath(projectDir, file.Sources.Manifest)
	if err != nil {
		return Options{}, fmt.Errorf("manifest: %w", err)
	}
	info, err := os.Stat(manifest)
	if err != nil {
		return Options{}, fmt.Errorf("manifest %q: %w", file.Sources.Manifest, err)
	}
	if !info.Mode().IsRegular() {
		return Options{}, fmt.Errorf("manifest %q is not a regular file", file.Sources.Manifest)
	}
	rootManifest, err := parseManifest(manifest)
	if err != nil {
		return Options{}, err
	}
	packageName := attributeValue(rootManifest, "", "package")
	if packageName == "" {
		return Options{}, fmt.Errorf("manifest %q: package is missing", file.Sources.Manifest)
	}

	javaDirs, err := resolveDirs(projectDir, file.Sources.Java)
	if err != nil {
		return Options{}, fmt.Errorf("java sources: %w", err)
	}
	resDirs, err := resolveDirs(projectDir, file.Sources.Res)
	if err != nil {
		return Options{}, fmt.Errorf("resources: %w", err)
	}
	assetDirs, err := resolveDirs(projectDir, file.Sources.Assets)
	if err != nil {
		return Options{}, fmt.Errorf("assets: %w", err)
	}
	kotlinDirs, err := resolveDirs(projectDir, file.Sources.Kotlin)
	if err != nil {
		return Options{}, fmt.Errorf("kotlin sources: %w", err)
	}
	testDirs, err := resolveDirs(projectDir, file.Sources.Test)
	if err != nil {
		return Options{}, fmt.Errorf("test sources: %w", err)
	}
	testResourceDirs, err := resolveDirs(projectDir, file.Sources.TestResources)
	if err != nil {
		return Options{}, fmt.Errorf("test resources: %w", err)
	}
	androidTestDirs, err := resolveDirs(projectDir, file.Sources.AndroidTest)
	if err != nil {
		return Options{}, fmt.Errorf("android test sources: %w", err)
	}
	androidTestResourceDirs, err := resolveDirs(projectDir, file.Sources.AndroidTestResources)
	if err != nil {
		return Options{}, fmt.Errorf("android test resources: %w", err)
	}
	androidTestManifest, err := resolvePath(projectDir, file.Sources.AndroidTestManifest)
	if err != nil {
		return Options{}, fmt.Errorf("android test manifest: %w", err)
	}
	if !isRegular(androidTestManifest) {
		androidTestManifest = ""
	}
	if file.Compose && file.Kotlin == "" {
		return Options{}, fmt.Errorf("compose requires kotlin")
	}

	var signing *SigningOptions
	if file.Signing != nil {
		signing, err = resolveSigning(projectDir, file.Signing, false)
		if err != nil {
			return Options{}, fmt.Errorf("signing: %w", err)
		}
	}
	var releaseSigning *SigningOptions
	if file.ReleaseSigning != nil {
		releaseSigning, err = resolveSigning(projectDir, file.ReleaseSigning, true)
		if err != nil {
			return Options{}, fmt.Errorf("release_signing: %w", err)
		}
	}
	signingConfigs := make(map[string]*SigningOptions, len(file.SigningConfigs))
	for name, config := range file.SigningConfigs {
		resolved, err := resolveSigning(projectDir, &config, true)
		if err != nil {
			return Options{}, fmt.Errorf("signing_config %q: %w", name, err)
		}
		signingConfigs[name] = resolved
	}
	versionCode, err := resolveVersionCode(projectDir, file)
	if err != nil {
		return Options{}, err
	}

	buildTypeConfig, err := resolveBuildTypeOptions(projectDir, file)
	if err != nil {
		return Options{}, fmt.Errorf("build_type: %w", err)
	}
	flavorConfig := resolveFlavorOptions(file)
	variants, err := resolveVariants(projectDir, file, VariantSources{
		JavaDirs:   javaDirs,
		KotlinDirs: kotlinDirs,
		ResDirs:    resDirs,
		AssetDirs:  assetDirs,
		Manifests:  []string{manifest},
	}, buildTypeConfig, flavorConfig, signingConfigs)
	if err != nil {
		return Options{}, fmt.Errorf("variants: %w", err)
	}
	for _, variant := range variants {
		for _, name := range []string{variant.FlavorOptions.Signing, variant.BuildTypeOptions.Signing} {
			if name != "" && signingConfigs[name] == nil {
				return Options{}, fmt.Errorf("signing_config %q is not defined", name)
			}
		}
	}
	modules, err := resolveModules(projectDir, file.Modules)
	if err != nil {
		return Options{}, fmt.Errorf("modules: %w", err)
	}
	if len(modules) > 0 && !file.Bundle {
		return Options{}, fmt.Errorf("modules require bundle = true")
	}
	legacy := len(variants) == 1 && variants[0].Flavor == "" && variants[0].BuildType == "debug"
	if file.Kotlin == "" {
		for _, variant := range variants {
			if len(variant.Sources.KotlinDirs) > 0 {
				return Options{}, fmt.Errorf("kotlin sources require the kotlin option")
			}
		}
	}

	dependencies, err := dependencyList(file.Dependencies)
	if err != nil {
		return Options{}, err
	}
	testDependencies, err := dependencyList(file.TestDependencies)
	if err != nil {
		return Options{}, fmt.Errorf("test_dependencies: %w", err)
	}

	var native *NativeOptions
	if file.Native != nil {
		native, err = resolveNative(projectDir, file.Native)
		if err != nil {
			return Options{}, fmt.Errorf("native: %w", err)
		}
	}
	return Options{
		CompileSDK:              file.CompileSDK,
		BuildTools:              file.BuildTools,
		MinSDK:                  file.MinSDK,
		TargetSDK:               file.TargetSDK,
		Java:                    file.Java,
		Kotlin:                  file.Kotlin,
		KotlinHome:              file.KotlinHome,
		Compose:                 file.Compose,
		Bundle:                  file.Bundle,
		BundletoolVersion:       file.Bundletool,
		ManifestMerger:          file.ManifestMerger,
		ManifestMergerVersion:   file.MergerVersion,
		Modules:                 modules,
		VersionCode:             versionCode,
		VersionName:             file.VersionName,
		VersionCodeStrategy:     versionStrategy(file),
		SigningConfigs:          signingConfigs,
		PackageName:             packageName,
		JavaDirs:                javaDirs,
		KotlinDirs:              kotlinDirs,
		ResDirs:                 resDirs,
		AssetDirs:               assetDirs,
		Manifest:                manifest,
		BaseManifest:            manifest,
		Manifests:               []string{manifest},
		BuildTypes:              append([]string(nil), file.BuildTypes...),
		Flavors:                 append([]string(nil), file.Flavors...),
		BuildTypeConfig:         buildTypeConfig,
		FlavorConfig:            flavorConfig,
		Variants:                variants,
		Legacy:                  legacy,
		ExtraPackages:           file.ExtraPackages,
		Dependencies:            dependencies,
		TestDependencies:        testDependencies,
		TestDirs:                testDirs,
		TestResourceDirs:        testResourceDirs,
		AndroidTestDirs:         androidTestDirs,
		AndroidTestResourceDirs: androidTestResourceDirs,
		AndroidTestManifest:     androidTestManifest,
		JUnitVersion:            file.JUnitVersion,
		Native:                  native,
		Signing:                 signing,
		ReleaseSigning:          releaseSigning,
	}, nil
}

// forVariant returns a per-variant copy of the options.
func (o Options) forVariant(variant Variant, legacy bool) Options {
	copied := o
	copied.Variant = variant
	copied.Legacy = legacy
	copied.ApplicationID = o.PackageName + variant.FlavorOptions.ApplicationIDSuffix
	copied.JavaDirs = variant.Sources.JavaDirs
	copied.KotlinDirs = variant.Sources.KotlinDirs
	copied.ResDirs = variant.Sources.ResDirs
	copied.AssetDirs = variant.Sources.AssetDirs
	copied.Manifests = variant.Sources.Manifests
	if len(copied.Manifests) > 0 {
		copied.Manifest = copied.Manifests[len(copied.Manifests)-1]
	}
	return copied
}

func versionStrategy(file optionsFile) string {
	if file.VersionStrategy == "" {
		return "manual"
	}
	return file.VersionStrategy
}

func resolveVersionCode(projectDir string, file optionsFile) (int, error) {
	switch versionStrategy(file) {
	case "manual":
		return file.VersionCode, nil
	case "git":
		command := exec.Command("git", "-C", projectDir, "rev-list", "--count", "HEAD")
		output, err := command.Output()
		if err != nil {
			return 0, fmt.Errorf("version_code_strategy git: %w", err)
		}
		value, err := strconv.Atoi(strings.TrimSpace(string(output)))
		if err != nil || value <= 0 {
			return 0, fmt.Errorf("version_code_strategy git: invalid count %q", strings.TrimSpace(string(output)))
		}
		return value, nil
	case "timestamp":
		value := int(time.Now().Unix() / 60)
		if value <= 0 {
			return 0, fmt.Errorf("version_code_strategy timestamp produced %d", value)
		}
		return value, nil
	default:
		return 0, fmt.Errorf("version_code_strategy must be manual, git, or timestamp")
	}
}

func resolveSigning(projectDir string, file *SigningOptions, allowMissingEnv bool) (*SigningOptions, error) {
	if strings.TrimSpace(file.Keystore) == "" {
		return nil, fmt.Errorf("keystore is required")
	}
	keystore, err := expandEnvironment(file.Keystore)
	if err != nil {
		if allowMissingEnv {
			option := *file
			option.Unresolved = true
			return &option, nil
		}
		return nil, err
	}
	path, err := expandPath(projectDir, keystore)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("keystore %q: %w", file.Keystore, err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("keystore %q is not a regular file", file.Keystore)
	}

	option := *file
	option.Keystore = path
	if option.Alias == "" {
		option.Alias = debugAlias
	}
	if option.StorePassword == "" {
		option.StorePassword = debugPass
	}
	if option.KeyPassword == "" {
		option.KeyPassword = debugPass
	}
	for _, field := range []*string{&option.Alias, &option.StorePassword, &option.KeyPassword} {
		value, err := expandEnvironment(*field)
		if err != nil {
			if allowMissingEnv {
				option.Unresolved = true
				return &option, nil
			}
			return nil, err
		}
		*field = value
	}
	return &option, nil
}

func expandEnvironment(value string) (string, error) {
	if !strings.Contains(value, "$") {
		return value, nil
	}
	var missing string
	expanded := os.Expand(value, func(key string) string {
		replacement, ok := os.LookupEnv(key)
		if !ok {
			missing = key
			return ""
		}
		return replacement
	})
	if missing != "" {
		return "", fmt.Errorf("environment variable %s is not set", missing)
	}
	return expanded, nil
}

func expandPath(projectDir, value string) (string, error) {
	if value == "~" || strings.HasPrefix(value, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("home directory: %w", err)
		}
		value = filepath.Join(home, strings.TrimPrefix(value, "~"))
	}
	if !filepath.IsAbs(value) {
		value = filepath.Join(projectDir, value)
	}
	return filepath.Clean(value), nil
}

func resolveNative(projectDir string, file *NativeOptions) (*NativeOptions, error) {
	cmake, err := resolvePath(projectDir, file.CMake)
	if err != nil {
		return nil, fmt.Errorf("cmake: %w", err)
	}
	info, err := os.Stat(cmake)
	if err != nil {
		return nil, fmt.Errorf("cmake %q: %w", file.CMake, err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("cmake %q is not a regular file", file.CMake)
	}

	option := *file
	option.CMake = cmake
	option.ABIs = append([]string(nil), file.ABIs...)
	return &option, nil
}

func applyOptionDefaults(file *optionsFile) {
	if file.MinSDK == 0 {
		file.MinSDK = defaultMinSDK
	}
	if file.TargetSDK == 0 {
		file.TargetSDK = file.CompileSDK
	}
	if file.Java == 0 {
		file.Java = defaultJava
	}
	if file.VersionCode == 0 {
		file.VersionCode = defaultVersionCode
	}
	if file.VersionName == "" {
		file.VersionName = defaultVersionName
	}
	if len(file.Sources.Java) == 0 {
		file.Sources.Java = []string{"src/main/java"}
	}
	if len(file.Sources.Kotlin) == 0 {
		file.Sources.Kotlin = []string{"src/main/kotlin"}
	}
	if len(file.Sources.Res) == 0 {
		file.Sources.Res = []string{"src/main/res"}
	}
	if file.Sources.Manifest == "" {
		file.Sources.Manifest = defaultManifest
	}
	if len(file.Sources.Test) == 0 {
		file.Sources.Test = []string{"src/test/java", "src/test/kotlin"}
	}
	if len(file.Sources.TestResources) == 0 {
		file.Sources.TestResources = []string{"src/test/resources"}
	}
	if len(file.Sources.AndroidTest) == 0 {
		file.Sources.AndroidTest = []string{"src/androidTest/java", "src/androidTest/kotlin"}
	}
	if len(file.Sources.AndroidTestResources) == 0 {
		file.Sources.AndroidTestResources = []string{"src/androidTest/res"}
	}
	if file.Sources.AndroidTestManifest == "" {
		file.Sources.AndroidTestManifest = "src/androidTest/AndroidManifest.xml"
	}
	if file.JUnitVersion == "" {
		file.JUnitVersion = defaultJUnitVersion
	}
	if len(file.BuildTypes) == 0 {
		file.BuildTypes = []string{"debug"}
	}
	if len(file.Flavors) == 0 {
		file.Flavors = []string{""}
	}
	if file.Bundle && file.Bundletool == "" {
		file.Bundletool = defaultBundletoolVersion
	}
	if file.ManifestMerger == "" {
		file.ManifestMerger = "auto"
	}
	if file.MergerVersion == "" {
		file.MergerVersion = defaultManifestMergerVersion
	}
	applyNativeDefaults(file.Native)
}

func applyNativeDefaults(native *NativeOptions) {
	if native == nil {
		return
	}
	if native.CMake == "" {
		native.CMake = defaultNativeCMake
	}
	if len(native.ABIs) == 0 {
		native.ABIs = []string{defaultNativeABI}
	}
	if native.BuildType == "" {
		native.BuildType = defaultNativeBuildType
	}
	if native.STL == "" {
		native.STL = defaultNativeSTL
	}
}

func validateOptions(file optionsFile) error {
	if file.CompileSDK < 0 {
		return fmt.Errorf("compile_sdk must not be negative")
	}
	if file.MinSDK <= 0 {
		return fmt.Errorf("min_sdk must be positive")
	}
	if file.TargetSDK < 0 {
		return fmt.Errorf("target_sdk must not be negative")
	}
	if file.Java < 8 {
		return fmt.Errorf("java must be 8 or later")
	}
	if file.Kotlin != "" && strings.ContainsFunc(file.Kotlin, unicode.IsSpace) {
		return fmt.Errorf("kotlin has whitespace")
	}
	if file.Bundletool != "" && strings.ContainsFunc(file.Bundletool, unicode.IsSpace) {
		return fmt.Errorf("bundletool_version has whitespace")
	}
	if file.ManifestMerger != "" && file.ManifestMerger != "auto" &&
		file.ManifestMerger != "kine" && file.ManifestMerger != "android" {
		return fmt.Errorf("manifest_merger must be auto, kine, or android")
	}
	if file.MergerVersion != "" && strings.ContainsFunc(file.MergerVersion, unicode.IsSpace) {
		return fmt.Errorf("manifest_merger_version has whitespace")
	}
	if file.VersionCode <= 0 {
		return fmt.Errorf("version_code must be positive")
	}
	if file.VersionName == "" || strings.ContainsFunc(file.VersionName, unicode.IsSpace) {
		return fmt.Errorf("version_name is empty or has whitespace")
	}
	if file.BuildTools != "" && !buildToolsPattern.MatchString(file.BuildTools) {
		return fmt.Errorf("build_tools %q is not a version", file.BuildTools)
	}
	for _, name := range file.ExtraPackages {
		if !packagePattern.MatchString(name) {
			return fmt.Errorf("extra_packages entry %q is not a Java package", name)
		}
	}
	if file.Native != nil {
		if err := validateNative(file.Native); err != nil {
			return err
		}
	}
	if err := validateSigning(file.Signing, "signing"); err != nil {
		return err
	}
	if err := validateSigning(file.ReleaseSigning, "release_signing"); err != nil {
		return err
	}
	if err := validateVariantNames(file); err != nil {
		return err
	}
	if err := validateVariantOptions(file); err != nil {
		return err
	}
	return nil
}

func validateSigning(signing *SigningOptions, section string) error {
	if signing == nil {
		return nil
	}
	if strings.TrimSpace(signing.Keystore) == "" {
		return fmt.Errorf("%s keystore is required", section)
	}
	if signing.Alias != "" && strings.ContainsFunc(signing.Alias, unicode.IsSpace) {
		return fmt.Errorf("%s alias has whitespace", section)
	}
	return nil
}

func validateNative(native *NativeOptions) error {
	for _, abi := range native.ABIs {
		if !supportedABIs[abi] {
			return fmt.Errorf("native abi %q is not supported", abi)
		}
	}
	switch native.BuildType {
	case "Debug", "Release", "RelWithDebInfo", "MinSizeRel":
	default:
		return fmt.Errorf("native build_type %q is not Debug, Release, RelWithDebInfo, or MinSizeRel", native.BuildType)
	}
	if strings.TrimSpace(native.STL) == "" || strings.ContainsFunc(native.STL, unicode.IsSpace) {
		return fmt.Errorf("native stl is empty or has whitespace")
	}
	if native.NDKVersion != "" && strings.ContainsFunc(native.NDKVersion, unicode.IsSpace) {
		return fmt.Errorf("native ndk_version has whitespace")
	}
	if native.CMakeVersion != "" && strings.ContainsFunc(native.CMakeVersion, unicode.IsSpace) {
		return fmt.Errorf("native cmake_version has whitespace")
	}
	return nil
}

func resolveDirs(projectDir string, values []string) ([]string, error) {
	var result []string
	for _, value := range values {
		path, err := resolvePath(projectDir, value)
		if err != nil {
			return nil, err
		}
		info, err := os.Stat(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("stat %s: %w", path, err)
		}
		if !info.IsDir() {
			return nil, fmt.Errorf("%s: not a directory", path)
		}
		result = append(result, path)
	}
	return result, nil
}

func resolvePath(projectDir, value string) (string, error) {
	if strings.TrimSpace(value) == "" {
		return "", fmt.Errorf("path is empty")
	}
	if filepath.IsAbs(value) {
		return "", fmt.Errorf("path %q must be relative", value)
	}
	clean := filepath.Clean(filepath.FromSlash(value))
	if clean == "." {
		return "", fmt.Errorf("path %q is the project root", value)
	}
	full := filepath.Join(projectDir, clean)
	if !core.Within(projectDir, full) {
		return "", fmt.Errorf("path %q escapes the project", value)
	}
	return full, nil
}

func dependencyList(values Dependencies) ([]deps.Dependency, error) {
	result := make([]deps.Dependency, 0, len(values.Compile)+len(values.Runtime))
	for _, group := range []struct {
		scope  deps.Scope
		values []string
	}{
		{deps.ScopeCompile, values.Compile},
		{deps.ScopeRuntime, values.Runtime},
	} {
		for _, value := range group.values {
			coordinate, err := deps.ParseCoordinate(value)
			if err != nil {
				return nil, err
			}
			result = append(result, deps.Dependency{
				Coordinate: coordinate,
				Scope:      group.scope,
			})
		}
	}
	return result, nil
}
