package android

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"
)

var variantNamePattern = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

// BuildTypeOptions configures one build type.
type BuildTypeOptions struct {
	Minify          bool
	ShrinkResources bool
	Proguard        []string
	Debug           bool
	Signing         string
}

// FlavorOptions configures one product flavor.
type FlavorOptions struct {
	ApplicationIDSuffix string
	VersionNameSuffix   string
	VersionCodeOffset   int
	Signing             string
}

// VariantSources lists the resolved source sets for one variant.
type VariantSources struct {
	JavaDirs   []string
	KotlinDirs []string
	ResDirs    []string
	AssetDirs  []string
	Manifests  []string
}

// Variant is one product flavor and build type pair.
type Variant struct {
	Flavor           string
	BuildType        string
	Name             string
	Suffix           string
	BuildTypeOptions BuildTypeOptions
	FlavorOptions    FlavorOptions
	Signing          *SigningOptions
	Sources          VariantSources
}

func resolveVariants(projectDir string, file optionsFile, main VariantSources, buildTypes map[string]BuildTypeOptions, flavors map[string]FlavorOptions, signingConfigs map[string]*SigningOptions) ([]Variant, error) {
	variants := make([]Variant, 0, len(buildTypes)*len(flavors))
	for _, flavor := range file.Flavors {
		for _, buildType := range file.BuildTypes {
			variant := Variant{
				Flavor:           flavor,
				BuildType:        buildType,
				Name:             variantName(flavor, buildType),
				Suffix:           variantSuffix(flavor, buildType),
				BuildTypeOptions: buildTypes[buildType],
				FlavorOptions:    flavors[flavor],
			}
			if name := variant.FlavorOptions.Signing; name != "" {
				variant.Signing = signingConfigs[name]
			} else if name := variant.BuildTypeOptions.Signing; name != "" {
				variant.Signing = signingConfigs[name]
			}
			variant.Sources = resolveVariantSources(projectDir, main, buildType, flavor)
			variants = append(variants, variant)
		}
	}
	return variants, nil
}

func resolveVariantSources(projectDir string, main VariantSources, buildType, flavor string) VariantSources {
	sources := VariantSources{
		JavaDirs:   append([]string(nil), main.JavaDirs...),
		KotlinDirs: append([]string(nil), main.KotlinDirs...),
		ResDirs:    append([]string(nil), main.ResDirs...),
		AssetDirs:  append([]string(nil), main.AssetDirs...),
		Manifests:  append([]string(nil), main.Manifests...),
	}

	sets := []string{buildType}
	if flavor != "" {
		sets = append(sets, flavor, flavor+capitalize(buildType))
	}
	for _, set := range sets {
		base := filepath.Join(projectDir, "src", set)
		sources.JavaDirs = appendExistingDir(sources.JavaDirs, filepath.Join(base, "java"))
		sources.KotlinDirs = appendExistingDir(sources.KotlinDirs, filepath.Join(base, "kotlin"))
		sources.ResDirs = appendExistingDir(sources.ResDirs, filepath.Join(base, "res"))
		sources.AssetDirs = appendExistingDir(sources.AssetDirs, filepath.Join(base, "assets"))
		sources.Manifests = appendExistingFile(sources.Manifests, filepath.Join(base, "AndroidManifest.xml"))
	}

	return sources
}

func appendExistingDir(values []string, path string) []string {
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		return values
	}
	return appendUnique(values, path)
}

func appendExistingFile(values []string, path string) []string {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return values
	}
	return appendUnique(values, path)
}

func appendUnique(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func variantName(flavor, buildType string) string {
	if flavor == "" {
		return buildType
	}
	return flavor + capitalize(buildType)
}

func capitalize(value string) string {
	if value == "" {
		return ""
	}
	return strings.ToUpper(value[:1]) + value[1:]
}

func variantSuffix(flavor, buildType string) string {
	if flavor == "" {
		return buildType
	}
	return flavor + "-" + buildType
}

func resolveBuildTypeOptions(projectDir string, file optionsFile) (map[string]BuildTypeOptions, error) {
	result := make(map[string]BuildTypeOptions, len(file.BuildTypes))
	for _, name := range file.BuildTypes {
		result[name] = BuildTypeOptions{Debug: name == "debug"}
	}
	for name, value := range file.BuildTypeConfig {
		option := result[name]
		option.Minify = value.Minify
		option.ShrinkResources = value.ShrinkResources
		if value.Debug != nil {
			option.Debug = *value.Debug
		}
		option.Signing = value.Signing
		for _, proguard := range value.Proguard {
			path, err := resolvePath(projectDir, proguard)
			if err != nil {
				return nil, fmt.Errorf("build_type %q: proguard: %w", name, err)
			}
			info, err := os.Stat(path)
			if err != nil {
				return nil, fmt.Errorf("build_type %q: proguard %q: %w", name, proguard, err)
			}
			if !info.Mode().IsRegular() {
				return nil, fmt.Errorf("build_type %q: proguard %q is not a regular file", name, proguard)
			}
			option.Proguard = append(option.Proguard, path)
		}
		result[name] = option
	}
	return result, nil
}

func resolveFlavorOptions(file optionsFile) map[string]FlavorOptions {
	result := make(map[string]FlavorOptions, len(file.Flavors))
	for _, name := range file.Flavors {
		value := file.FlavorConfig[name]
		result[name] = FlavorOptions{
			ApplicationIDSuffix: value.ApplicationIDSuffix,
			VersionNameSuffix:   value.VersionNameSuffix,
			VersionCodeOffset:   value.VersionCodeOffset,
			Signing:             value.Signing,
		}
	}
	return result
}

// taskID adds the variant suffix when the project is not the legacy debug build.
func selectVariant(variants []Variant, name string) (Variant, error) {
	var available []string
	for _, variant := range variants {
		if strings.EqualFold(variant.Suffix, name) || strings.EqualFold(variant.Name, name) {
			return variant, nil
		}
		available = append(available, fmt.Sprintf("%s (%s)", variant.Name, variant.Suffix))
	}
	return Variant{}, fmt.Errorf("variant %q not found; available: %s", name, strings.Join(available, ", "))
}

func (o Options) taskID(base string) string {
	if o.Legacy || o.Variant.Suffix == "" {
		return base
	}
	return base + "-" + o.Variant.Suffix
}

func (o Options) androidBuildDir() string {
	if o.Legacy || o.Variant.Suffix == "" {
		return filepath.Join("build", "android")
	}
	return filepath.Join("build", "android", o.Variant.Suffix)
}

func (o Options) androidPath(projectDir string, parts ...string) string {
	base := filepath.Join(projectDir, filepath.FromSlash(o.androidBuildDir()))
	return filepath.Join(append([]string{base}, parts...)...)
}

func (o Options) apkOutputPath(projectDir string) string {
	return filepath.Join(projectDir, filepath.FromSlash(o.apkOutputDir()), o.apkOutputName())
}

func (o Options) apkOutputName() string {
	name := o.apkFileName()
	if o.Variant.BuildType == "release" && o.signingConfig() == nil {
		return strings.TrimSuffix(name, ".apk") + "-unsigned.apk"
	}
	return name
}

func (o Options) bundleOutputDir() string {
	if o.Legacy || o.Variant.Suffix == "" {
		return filepath.Join("build", "outputs", "bundle", "debug")
	}
	dir := filepath.Join("build", "outputs", "bundle")
	if o.Variant.Flavor != "" {
		dir = filepath.Join(dir, o.Variant.Flavor)
	}
	return filepath.Join(dir, o.Variant.BuildType)
}

func (o Options) bundleFileName() string {
	if o.Legacy || o.Variant.Suffix == "" {
		return "app-debug.aab"
	}
	if o.Variant.Flavor != "" {
		return fmt.Sprintf("app-%s-%s.aab", o.Variant.Flavor, o.Variant.BuildType)
	}
	return "app-" + o.Variant.BuildType + ".aab"
}

func (o Options) bundleOutputName() string {
	name := o.bundleFileName()
	if o.Variant.BuildType == "release" && o.signingConfig() == nil {
		return strings.TrimSuffix(name, ".aab") + "-unsigned.aab"
	}
	return name
}

func (o Options) bundleOutputPath(projectDir string) string {
	return filepath.Join(projectDir, filepath.FromSlash(o.bundleOutputDir()), o.bundleOutputName())
}

func (o Options) effectiveVersionCode() int {
	return o.VersionCode + o.Variant.FlavorOptions.VersionCodeOffset
}

func (o Options) effectiveVersionName() string {
	return o.VersionName + o.Variant.FlavorOptions.VersionNameSuffix
}

func (o Options) signingConfig() *SigningOptions {
	if o.Variant.Signing != nil {
		return o.Variant.Signing
	}
	if o.Variant.BuildType == "release" {
		return o.ReleaseSigning
	}
	return o.Signing
}

func (o Options) resourcesAPKPath(projectDir string) string {
	return o.androidPath(projectDir, resourcesAPKName)
}

func (o Options) resourcesProtoAPKPath(projectDir string) string {
	return o.androidPath(projectDir, "resources-proto.apk")
}

func (o Options) apkOutputDir() string {
	if o.Legacy || o.Variant.Suffix == "" {
		return filepath.Join("build", "outputs", "apk", "debug")
	}
	dir := filepath.Join("build", "outputs", "apk")
	if o.Variant.Flavor != "" {
		dir = filepath.Join(dir, o.Variant.Flavor)
	}
	return filepath.Join(dir, o.Variant.BuildType)
}

func (o Options) apkFileName() string {
	if o.Legacy || o.Variant.Suffix == "" {
		return "app-debug.apk"
	}
	if o.Variant.Flavor != "" {
		return fmt.Sprintf("app-%s-%s.apk", o.Variant.Flavor, o.Variant.BuildType)
	}
	return "app-" + o.Variant.BuildType + ".apk"
}

func (o Options) variantLabel() string {
	if o.Variant.Name == "" {
		return "debug"
	}
	return o.Variant.Name
}

func validateVariantNames(file optionsFile) error {
	buildTypes := make(map[string]struct{}, len(file.BuildTypes))
	for _, name := range file.BuildTypes {
		if !variantNamePattern.MatchString(name) {
			return fmt.Errorf("build_types entry %q must match %s", name, variantNamePattern)
		}
		if _, ok := buildTypes[name]; ok {
			return fmt.Errorf("duplicate build type %q", name)
		}
		buildTypes[name] = struct{}{}
	}

	flavors := make(map[string]struct{}, len(file.Flavors))
	for _, name := range file.Flavors {
		if name != "" && !variantNamePattern.MatchString(name) {
			return fmt.Errorf("flavors entry %q must match %s", name, variantNamePattern)
		}
		if _, ok := flavors[name]; ok {
			return fmt.Errorf("duplicate flavor %q", name)
		}
		flavors[name] = struct{}{}
	}

	for name := range file.BuildTypeConfig {
		if _, ok := buildTypes[name]; !ok {
			return fmt.Errorf("build_type %q is not listed in build_types", name)
		}
	}
	for name := range file.FlavorConfig {
		if _, ok := flavors[name]; !ok {
			return fmt.Errorf("flavor %q is not listed in flavors", name)
		}
	}
	return nil
}

func validateVariantOptions(file optionsFile) error {
	for name, value := range file.BuildTypeConfig {
		if value.ShrinkResources && !value.Minify {
			return fmt.Errorf("build_type %q: shrink_resources requires minify", name)
		}
		for _, path := range value.Proguard {
			if strings.TrimSpace(path) == "" {
				return fmt.Errorf("build_type %q: proguard path is empty", name)
			}
			if filepath.IsAbs(path) {
				return fmt.Errorf("build_type %q: proguard path %q must be relative", name, path)
			}
			if strings.HasPrefix(filepath.Clean(path), "..") {
				return fmt.Errorf("build_type %q: proguard path %q escapes the project", name, path)
			}
		}
	}
	for name, value := range file.FlavorConfig {
		if strings.ContainsFunc(value.ApplicationIDSuffix, unicode.IsSpace) {
			return fmt.Errorf("flavor %q: application_id_suffix has whitespace", name)
		}
		if strings.ContainsFunc(value.VersionNameSuffix, unicode.IsSpace) {
			return fmt.Errorf("flavor %q: version_name_suffix has whitespace", name)
		}
	}
	return nil
}
