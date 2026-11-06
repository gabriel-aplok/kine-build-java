package compat

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/gabriel-aplok/kine-build-java/internal/deps"
)

var (
	gradlePluginPattern     = regexp.MustCompile(`(?m)(?:^|[{\s])id\s*\(?\s*['"]([^'"]+)['"]\s*\)?\s*(?:version\s*\(?\s*['"]([^'"]+)['"])?`)
	gradleDependencyPattern = regexp.MustCompile(`^(implementation|api|compileOnly|runtimeOnly|testImplementation|testRuntimeOnly|testCompileOnly|annotationProcessor|kapt)\s*\(?\s*['"]([^'"]+)['"]`)
	gradleSpecialDependency = regexp.MustCompile(`^(implementation|api|compileOnly|runtimeOnly|testImplementation|testRuntimeOnly|testCompileOnly)\s*\(?\s*(project|platform|files|fileTree)\s*\(`)
	gradleCatalogPattern    = regexp.MustCompile(`^(implementation|api|compileOnly|runtimeOnly|testImplementation|testRuntimeOnly|testCompileOnly)\s*\(?\s*libs\.`)
	gradleJavaVersion       = regexp.MustCompile(`JavaLanguageVersion\.of\(\s*([0-9]+)\s*\)`)
	gradleToolchain         = regexp.MustCompile(`jvmToolchain\(\s*([0-9]+)\s*\)`)
	gradleCompatibility     = regexp.MustCompile(`(?m)^\s*(?:source|target)Compatibility\s*=\s*(?:JavaVersion\.)?(?:VERSION_)?['"]?([0-9._]+)['"]?`)
	gradleMainClass         = []*regexp.Regexp{
		regexp.MustCompile(`mainClass\s*=\s*['"]([^'"]+)['"]`),
		regexp.MustCompile(`mainClass\.set\(\s*['"]([^'"]+)['"]\s*\)`),
	}
	gradleRepository = regexp.MustCompile(`mavenCentral\(\)|google\(\)|mavenLocal\(\)|maven\s*\{`)
)

func parseGradle(dir string) (*Build, error) {
	path, err := findGradleFile(dir)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	text := string(data)

	build := &Build{Source: "gradle", File: path}
	supportedPlugins := 0
	for _, match := range gradlePluginPattern.FindAllStringSubmatch(text, -1) {
		name := match[1]
		version := match[2]
		switch name {
		case "java", "java-library", "application":
			supportedPlugins++
		case "org.jetbrains.kotlin.jvm":
			supportedPlugins++
			if version == "" {
				build.warn("kotlin plugin version not found, set kotlin in kine.toml")
			} else {
				build.Kotlin = version
			}
		default:
			build.warn("plugin %q is not supported", name)
		}
	}
	if supportedPlugins == 0 {
		build.warn("no supported JVM plugin found")
	}

	build.Java = gradleJavaVersionOf(text)
	if build.Java == 0 {
		build.warn("java version not found, the selected JDK version is used")
	}
	build.MainClass = gradleMainClassOf(text)

	for _, line := range strings.Split(text, "\n") {
		addGradleDependency(build, strings.TrimSpace(line))
	}

	if gradleMultiModule(dir) {
		build.warn("multi-module Gradle builds are not supported, importing the root project only")
	}
	if strings.Contains(text, "sourceSets") {
		build.warn("custom source sets are not imported")
	}
	if gradleRepository.MatchString(text) {
		build.warn("repositories are not copied, configure repositories.toml")
	}

	build.JavaDirs = []string{"src/main/java"}
	build.TestJavaDirs = []string{"src/test/java"}
	return build, nil
}

func findGradleFile(dir string) (string, error) {
	for _, name := range []string{"build.gradle", "build.gradle.kts"} {
		path := filepath.Join(dir, name)
		if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() {
			return path, nil
		}
	}
	return "", fmt.Errorf("build.gradle or build.gradle.kts not found in %s", dir)
}

func gradleJavaVersionOf(text string) int {
	for _, pattern := range []*regexp.Regexp{gradleJavaVersion, gradleToolchain, gradleCompatibility} {
		match := pattern.FindStringSubmatch(text)
		if match == nil {
			continue
		}
		if major := parseJavaMajor(match[1]); major > 0 {
			return major
		}
	}
	return 0
}

func gradleMainClassOf(text string) string {
	for _, pattern := range gradleMainClass {
		if match := pattern.FindStringSubmatch(text); match != nil {
			return match[1]
		}
	}
	return ""
}

func addGradleDependency(build *Build, line string) {
	match := gradleDependencyPattern.FindStringSubmatch(line)
	if match == nil {
		if gradleSpecialDependency.MatchString(line) {
			build.warn("dependency skipped: %s", line)
		} else if gradleCatalogPattern.MatchString(line) {
			build.warn("version catalog dependency skipped: %s", line)
		}
		return
	}

	configuration := match[1]
	coordinate := match[2]
	switch configuration {
	case "annotationProcessor", "kapt":
		build.warn("annotation processor %s skipped", coordinate)
		return
	}
	if strings.ContainsAny(coordinate, "+[]()@") {
		build.warn("dependency %s skipped: dynamic versions and artifacts are not supported", coordinate)
		return
	}
	if _, err := deps.ParseCoordinate(coordinate); err != nil {
		build.warn("dependency %s skipped: %v", coordinate, err)
		return
	}

	switch configuration {
	case "implementation", "api":
		build.Compile = append(build.Compile, coordinate)
	case "compileOnly":
		build.Provided = append(build.Provided, coordinate)
	case "runtimeOnly":
		build.Runtime = append(build.Runtime, coordinate)
	case "testImplementation", "testCompileOnly", "testRuntimeOnly":
		build.Test = append(build.Test, coordinate)
	}
}

func gradleMultiModule(dir string) bool {
	for _, name := range []string{"settings.gradle", "settings.gradle.kts"} {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err == nil && strings.Contains(string(data), "include") {
			return true
		}
	}
	return false
}
