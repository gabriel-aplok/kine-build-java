// Package android builds Android APKs.
package android

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// ErrSDKNotFound is returned when no usable Android SDK is found.
var ErrSDKNotFound = errors.New("android SDK not found")

// SDK holds the selected Android tools.
type SDK struct {
	Root         string
	Platform     string
	BuildTools   string
	AndroidJar   string
	Aapt2        string
	D8           string
	R8Jar        string
	Zipalign     string
	ApkSigner    string
	NDK          string
	NDKVersion   string
	CMake        string
	CMakeVersion string
	Ninja        string
	Lint         string
}

// Requirement selects an Android platform and optional build tools.
type Requirement struct {
	CompileSDK   int
	BuildTools   string
	Native       bool
	NDKVersion   string
	CMakeVersion string
}

var (
	platformPattern   = regexp.MustCompile(`^android-([0-9]+)(?:\.([0-9]+))?$`)
	buildToolsPattern = regexp.MustCompile(`^[0-9]+(\.[0-9]+)*$`)
	buildToolNames    = []string{"aapt2", "d8", "zipalign", "apksigner"}
	buildToolSuffixes = []string{"", ".exe", ".bat", ".cmd"}
)

// FindSDK locates an SDK, a platform, and a complete build-tools directory.
func FindSDK(requirement Requirement, roots ...string) (*SDK, error) {
	candidates := roots
	if len(candidates) == 0 {
		candidates = sdkRoots()
	}

	var found []string
	seen := make(map[string]struct{})
	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		absolute, err := filepath.Abs(candidate)
		if err != nil {
			continue
		}
		resolved, err := filepath.EvalSymlinks(absolute)
		if err != nil {
			continue
		}
		info, err := os.Stat(resolved)
		if err != nil || !info.IsDir() {
			continue
		}
		if _, ok := seen[resolved]; ok {
			continue
		}
		seen[resolved] = struct{}{}
		found = append(found, resolved)
	}
	if len(found) == 0 {
		return nil, ErrSDKNotFound
	}

	var reasons []string
	for _, root := range found {
		sdk, err := findInRoot(root, requirement)
		if err == nil {
			return sdk, nil
		}
		reasons = append(reasons, fmt.Sprintf("%s: %v", root, err))
	}
	return nil, fmt.Errorf("%w: %s", ErrSDKNotFound, strings.Join(reasons, "; "))
}

func sdkRoots() []string {
	roots := []string{os.Getenv("ANDROID_HOME"), os.Getenv("ANDROID_SDK_ROOT")}
	if home, err := os.UserHomeDir(); err == nil {
		roots = append(roots,
			filepath.Join(home, "Android", "Sdk"),
			filepath.Join(home, "android-sdk"),
		)
	}
	if dir := os.Getenv("LOCALAPPDATA"); dir != "" {
		roots = append(roots, filepath.Join(dir, "Android", "Sdk"))
	}
	return append(roots, "/usr/lib/android-sdk", "/opt/android-sdk")
}

func findInRoot(root string, requirement Requirement) (*SDK, error) {
	platform, err := findPlatform(root, requirement.CompileSDK)
	if err != nil {
		return nil, err
	}
	buildTools, tools, err := findBuildTools(root, requirement.BuildTools)
	if err != nil {
		return nil, err
	}
	sdk := &SDK{
		Root:       root,
		Platform:   platform,
		BuildTools: buildTools,
		AndroidJar: filepath.Join(platform, "android.jar"),
		Aapt2:      tools[0],
		D8:         tools[1],
		Zipalign:   tools[2],
		ApkSigner:  tools[3],
		Lint:       findLint(root),
	}
	r8Jar := filepath.Join(buildTools, "lib", "d8.jar")
	if isRegular(r8Jar) {
		sdk.R8Jar = r8Jar
	}
	if !requirement.Native {
		return sdk, nil
	}

	ndk, ndkVersion, err := findNDK(root, requirement.NDKVersion)
	if err != nil {
		return nil, err
	}
	_, cmakeVersion, cmakeTool, ninja, err := findCMake(root, requirement.CMakeVersion)
	if err != nil {
		return nil, err
	}
	sdk.NDK = ndk
	sdk.NDKVersion = ndkVersion
	sdk.CMake = cmakeTool
	sdk.CMakeVersion = cmakeVersion
	sdk.Ninja = ninja
	return sdk, nil
}

func findPlatform(root string, compileSDK int) (string, error) {
	dir := filepath.Join(root, "platforms")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", fmt.Errorf("read platforms: %w", err)
	}

	bestMajor, bestMinor := -1, -1
	best := ""
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		match := platformPattern.FindStringSubmatch(entry.Name())
		if match == nil {
			continue
		}
		major, err := strconv.Atoi(match[1])
		if err != nil {
			continue
		}
		minor := 0
		if match[2] != "" {
			minor, _ = strconv.Atoi(match[2])
		}
		if compileSDK > 0 && major != compileSDK {
			continue
		}
		if major > bestMajor || (major == bestMajor && minor > bestMinor) {
			bestMajor = major
			bestMinor = minor
			best = filepath.Join(dir, entry.Name())
		}
	}

	if best == "" {
		if compileSDK > 0 {
			return "", fmt.Errorf("platform android-%d is not installed", compileSDK)
		}
		return "", fmt.Errorf("no android platform is installed")
	}
	if !isRegular(filepath.Join(best, "android.jar")) {
		return "", fmt.Errorf("platform %s has no android.jar", best)
	}
	return best, nil
}

func findBuildTools(root, wanted string) (string, []string, error) {
	dir := filepath.Join(root, "build-tools")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", nil, fmt.Errorf("read build-tools: %w", err)
	}

	var versions []string
	for _, entry := range entries {
		if entry.IsDir() && buildToolsPattern.MatchString(entry.Name()) {
			versions = append(versions, entry.Name())
		}
	}
	sort.Slice(versions, func(i, j int) bool {
		return compareVersions(versions[i], versions[j]) > 0
	})

	if wanted != "" {
		path := filepath.Join(dir, wanted)
		if tools := toolsIn(path); tools != nil {
			return path, tools, nil
		}
		return "", nil, fmt.Errorf("build-tools %s is not installed or is incomplete", wanted)
	}

	for _, version := range versions {
		path := filepath.Join(dir, version)
		if tools := toolsIn(path); tools != nil {
			return path, tools, nil
		}
	}
	return "", nil, fmt.Errorf("no build-tools directory has aapt2, d8, zipalign, and apksigner")
}

func findLint(root string) string {
	return findCmdlineTool(root, "lint")
}

func findCmdlineTool(root, name string) string {
	for _, relative := range []string{
		filepath.Join("cmdline-tools", "latest", "bin", name),
		filepath.Join("tools", "bin", name),
	} {
		path := filepath.Join(root, relative)
		if isRegular(path) {
			return path
		}
	}
	return ""
}

func findNDK(root, wanted string) (string, string, error) {
	dir := filepath.Join(root, "ndk")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", "", fmt.Errorf("read NDK: %w", err)
	}

	if wanted != "" {
		path := filepath.Join(dir, wanted)
		if !isRegular(filepath.Join(path, "build", "cmake", "android.toolchain.cmake")) {
			return "", "", fmt.Errorf("NDK %s is not installed", wanted)
		}
		return path, wanted, nil
	}

	var versions []string
	for _, entry := range entries {
		if entry.IsDir() && buildToolsPattern.MatchString(entry.Name()) {
			if isRegular(filepath.Join(dir, entry.Name(), "build", "cmake", "android.toolchain.cmake")) {
				versions = append(versions, entry.Name())
			}
		}
	}
	if len(versions) == 0 {
		return "", "", fmt.Errorf("no NDK is installed under %s", dir)
	}
	sort.Slice(versions, func(i, j int) bool {
		return compareVersions(versions[i], versions[j]) > 0
	})
	return filepath.Join(dir, versions[0]), versions[0], nil
}

func findCMake(root, wanted string) (string, string, string, string, error) {
	dir := filepath.Join(root, "cmake")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", "", "", "", fmt.Errorf("read cmake: %w", err)
	}

	if wanted != "" {
		path := filepath.Join(dir, wanted)
		cmake, ninja, ok := cmakeTools(path)
		if !ok {
			return "", "", "", "", fmt.Errorf("cmake %s is not installed or has no ninja", wanted)
		}
		return path, wanted, cmake, ninja, nil
	}

	var versions []string
	for _, entry := range entries {
		if entry.IsDir() && buildToolsPattern.MatchString(entry.Name()) {
			versions = append(versions, entry.Name())
		}
	}
	sort.Slice(versions, func(i, j int) bool {
		return compareVersions(versions[i], versions[j]) > 0
	})
	for _, version := range versions {
		path := filepath.Join(dir, version)
		if cmake, ninja, ok := cmakeTools(path); ok {
			return path, version, cmake, ninja, nil
		}
	}
	return "", "", "", "", fmt.Errorf("no cmake is installed under %s", dir)
}

func cmakeTools(root string) (string, string, bool) {
	cmake, ok := toolPath(filepath.Join(root, "bin"), "cmake")
	if !ok {
		return "", "", false
	}
	ninja, ok := toolPath(filepath.Join(root, "bin"), "ninja")
	if !ok {
		return "", "", false
	}
	return cmake, ninja, true
}

func toolsIn(dir string) []string {
	tools := make([]string, 0, len(buildToolNames))
	for _, name := range buildToolNames {
		path, ok := toolPath(dir, name)
		if !ok {
			return nil
		}
		tools = append(tools, path)
	}
	return tools
}

func toolPath(dir, name string) (string, bool) {
	for _, suffix := range buildToolSuffixes {
		path := filepath.Join(dir, name+suffix)
		if isRegular(path) {
			return path, true
		}
	}
	return "", false
}

func compareVersions(a, b string) int {
	left := strings.Split(a, ".")
	right := strings.Split(b, ".")
	for i := 0; i < len(left) || i < len(right); i++ {
		var lv, rv int
		if i < len(left) {
			lv, _ = strconv.Atoi(left[i])
		}
		if i < len(right) {
			rv, _ = strconv.Atoi(right[i])
		}
		if lv != rv {
			if lv < rv {
				return -1
			}
			return 1
		}
	}
	return 0
}

func platformMajor(platform string) int {
	name := strings.TrimPrefix(filepath.Base(platform), "android-")
	if index := strings.Index(name, "."); index >= 0 {
		name = name[:index]
	}
	major, _ := strconv.Atoi(name)
	return major
}

func isRegular(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}
