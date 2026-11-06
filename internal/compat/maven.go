package compat

import (
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/gabriel-aplok/kine-build-java/internal/deps"
)

var propertyPattern = regexp.MustCompile(`\$\{([^}]+)\}`)

type pomFile struct {
	GroupID      string          `xml:"groupId"`
	ArtifactID   string          `xml:"artifactId"`
	Version      string          `xml:"version"`
	Packaging    string          `xml:"packaging"`
	Parent       *pomParent      `xml:"parent"`
	Properties   pomProperties   `xml:"properties"`
	Dependencies []pomDependency `xml:"dependencies>dependency"`
	Build        pomBuild        `xml:"build"`
}

type pomParent struct {
	GroupID    string `xml:"groupId"`
	ArtifactID string `xml:"artifactId"`
	Version    string `xml:"version"`
}

type pomProperties struct {
	Entries []pomProperty `xml:",any"`
}

type pomProperty struct {
	XMLName xml.Name
	Value   string `xml:",chardata"`
}

type pomDependency struct {
	GroupID    string         `xml:"groupId"`
	ArtifactID string         `xml:"artifactId"`
	Version    string         `xml:"version"`
	Scope      string         `xml:"scope"`
	Type       string         `xml:"type"`
	Classifier string         `xml:"classifier"`
	Exclusions []pomExclusion `xml:"exclusions>exclusion"`
}

type pomExclusion struct {
	GroupID    string `xml:"groupId"`
	ArtifactID string `xml:"artifactId"`
}

type pomBuild struct {
	SourceDirectory     string      `xml:"sourceDirectory"`
	TestSourceDirectory string      `xml:"testSourceDirectory"`
	Plugins             []pomPlugin `xml:"plugins>plugin"`
}

type pomPlugin struct {
	ArtifactID    string `xml:"artifactId"`
	Configuration struct {
		Release string `xml:"release"`
		Source  string `xml:"source"`
		Archive struct {
			Manifest struct {
				MainClass string `xml:"mainClass"`
			} `xml:"manifest"`
		} `xml:"archive"`
	} `xml:"configuration"`
}

func parseMaven(dir string) (*Build, error) {
	path := filepath.Join(dir, "pom.xml")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}

	var file pomFile
	if err := xml.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if strings.TrimSpace(file.ArtifactID) == "" {
		return nil, fmt.Errorf("%s: artifactId is missing", path)
	}
	if packaging := strings.TrimSpace(file.Packaging); packaging != "" && packaging != "jar" {
		return nil, fmt.Errorf("%s: packaging %q is not supported", path, packaging)
	}

	build := &Build{Source: "maven", File: path}
	properties := mavenProperties(file, dir)
	resolve := func(value string) (string, bool) {
		return resolveProperties(strings.TrimSpace(value), properties)
	}

	build.Java = mavenJava(file, resolve)
	build.MainClass = mavenMainClass(file, resolve)
	build.JavaDirs = mavenSources(dir, file.Build.SourceDirectory, "src/main/java")
	build.TestJavaDirs = mavenSources(dir, file.Build.TestSourceDirectory, "src/test/java")

	for index, dependency := range file.Dependencies {
		coordinate, err := mavenCoordinate(dependency, resolve)
		if err != nil {
			build.warn("dependency %d skipped: %v", index, err)
			continue
		}
		scope, ok := mavenScope(dependency.Scope)
		if !ok {
			build.warn("dependency %s skipped: scope %q is not supported", coordinate, dependency.Scope)
			continue
		}
		if dependency.Type != "" && dependency.Type != "jar" {
			build.warn("dependency %s skipped: type %q is not supported", coordinate, dependency.Type)
			continue
		}
		if len(dependency.Exclusions) > 0 {
			build.warn("dependency %s: exclusions are not applied", coordinate)
		}
		switch scope {
		case "compile":
			build.Compile = append(build.Compile, coordinate)
		case "runtime":
			build.Runtime = append(build.Runtime, coordinate)
		case "test":
			build.Test = append(build.Test, coordinate)
		case "provided":
			build.Provided = append(build.Provided, coordinate)
		}
	}
	if build.Java == 0 {
		build.warn("java version not found, the selected JDK version is used")
	}
	return build, nil
}

func mavenProperties(file pomFile, dir string) map[string]string {
	values := make(map[string]string)
	for _, entry := range file.Properties.Entries {
		values[entry.XMLName.Local] = strings.TrimSpace(entry.Value)
	}

	groupId := resolveValue(file.GroupID, values)
	if groupId == "" && file.Parent != nil {
		groupId = resolveValue(file.Parent.GroupID, values)
	}
	version := resolveValue(file.Version, values)
	if version == "" && file.Parent != nil {
		version = resolveValue(file.Parent.Version, values)
	}
	values["project.groupId"] = groupId
	values["pom.groupId"] = groupId
	values["project.version"] = version
	values["pom.version"] = version
	values["project.artifactId"] = file.ArtifactID
	values["pom.artifactId"] = file.ArtifactID
	values["project.basedir"] = dir
	return values
}

func resolveValue(value string, properties map[string]string) string {
	resolved, _ := resolveProperties(strings.TrimSpace(value), properties)
	return resolved
}

func resolveProperties(value string, properties map[string]string) (string, bool) {
	for i := 0; i < 10; i++ {
		if !strings.Contains(value, "${") {
			return value, true
		}
		replaced := propertyPattern.ReplaceAllStringFunc(value, func(match string) string {
			key := match[2 : len(match)-1]
			if replacement, ok := properties[key]; ok {
				return replacement
			}
			return match
		})
		if replaced == value {
			return value, false
		}
		value = replaced
	}
	return value, !strings.Contains(value, "${")
}

func mavenCoordinate(dependency pomDependency, resolve func(string) (string, bool)) (string, error) {
	group, ok := resolve(dependency.GroupID)
	if !ok || group == "" {
		return "", fmt.Errorf("unresolved groupId %q", dependency.GroupID)
	}
	artifact, ok := resolve(dependency.ArtifactID)
	if !ok || artifact == "" {
		return "", fmt.Errorf("unresolved artifactId %q", dependency.ArtifactID)
	}
	version, ok := resolve(dependency.Version)
	if !ok || version == "" {
		return "", fmt.Errorf("unresolved version %q", dependency.Version)
	}

	coordinate := group + ":" + artifact + ":" + version
	if classifier := strings.TrimSpace(dependency.Classifier); classifier != "" {
		coordinate += ":" + classifier
	}
	if _, err := deps.ParseCoordinate(coordinate); err != nil {
		return "", err
	}
	return coordinate, nil
}

func mavenScope(value string) (string, bool) {
	switch strings.TrimSpace(value) {
	case "", "compile":
		return "compile", true
	case "runtime":
		return "runtime", true
	case "test":
		return "test", true
	case "provided":
		return "provided", true
	default:
		return "", false
	}
}

func mavenJava(file pomFile, resolve func(string) (string, bool)) int {
	candidates := []string{
		file.Properties.lookup("maven.compiler.release"),
		file.Properties.lookup("maven.compiler.source"),
		file.Properties.lookup("java.version"),
	}
	for _, plugin := range file.Build.Plugins {
		if plugin.ArtifactID == "maven-compiler-plugin" {
			candidates = append(candidates, plugin.Configuration.Release, plugin.Configuration.Source)
		}
	}
	for _, candidate := range candidates {
		if value, ok := resolve(candidate); ok && value != "" {
			if major := parseJavaMajor(value); major > 0 {
				return major
			}
		}
	}
	return 0
}

func (properties pomProperties) lookup(key string) string {
	for _, entry := range properties.Entries {
		if entry.XMLName.Local == key {
			return strings.TrimSpace(entry.Value)
		}
	}
	return ""
}

func mavenMainClass(file pomFile, resolve func(string) (string, bool)) string {
	for _, plugin := range file.Build.Plugins {
		value := strings.TrimSpace(plugin.Configuration.Archive.Manifest.MainClass)
		if value == "" {
			continue
		}
		if resolved, ok := resolve(value); ok {
			return resolved
		}
	}
	return ""
}

func mavenSources(dir, value, fallback string) []string {
	value = strings.TrimSpace(value)
	if value == "" || strings.Contains(value, "${") {
		return []string{fallback}
	}
	if filepath.IsAbs(value) {
		relative, err := filepath.Rel(dir, value)
		if err != nil || strings.HasPrefix(relative, "..") {
			return []string{fallback}
		}
		value = relative
	}
	return []string{filepath.ToSlash(filepath.Clean(value))}
}

func parseJavaMajor(value string) int {
	value = strings.TrimSpace(strings.TrimPrefix(value, "VERSION_"))
	value = strings.ReplaceAll(value, "_", ".")
	if value == "" {
		return 0
	}
	parts := strings.Split(value, ".")
	index := 0
	if parts[0] == "1" && len(parts) > 1 {
		index = 1
	}
	major, err := strconv.Atoi(parts[index])
	if err != nil {
		return 0
	}
	return major
}
