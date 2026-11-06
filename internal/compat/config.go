package compat

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/pelletier/go-toml/v2"

	"github.com/gabriel-aplok/kine-build-java/internal/jvm"
)

type configFile struct {
	Project configProject `toml:"project"`
}

type configProject struct {
	Provider string        `toml:"provider"`
	Options  configOptions `toml:"options"`
}

type configOptions struct {
	Java         int                 `toml:"java,omitempty"`
	Kotlin       string              `toml:"kotlin,omitempty"`
	MainClass    string              `toml:"main_class,omitempty"`
	Sources      *configSources      `toml:"sources,omitempty"`
	Dependencies *configDependencies `toml:"dependencies,omitempty"`
}

type configSources struct {
	Java     []string `toml:"java,omitempty"`
	TestJava []string `toml:"test_java,omitempty"`
}

type configDependencies struct {
	Compile  []string `toml:"compile,omitempty"`
	Runtime  []string `toml:"runtime,omitempty"`
	Test     []string `toml:"test,omitempty"`
	Provided []string `toml:"provided,omitempty"`
}

var invalidIDCharacters = regexp.MustCompile(`[^a-z0-9-]+`)

func generateConfig(build *Build, projectID string) ([]byte, error) {
	options := configOptions{
		Java:      build.Java,
		Kotlin:    build.Kotlin,
		MainClass: build.MainClass,
	}

	sources := configSources{}
	needsSources := false
	if !sameStrings(build.JavaDirs, []string{"src/main/java"}) {
		sources.Java = build.JavaDirs
		needsSources = true
	}
	if !sameStrings(build.TestJavaDirs, []string{"src/test/java"}) {
		sources.TestJava = build.TestJavaDirs
		needsSources = true
	}
	if needsSources {
		options.Sources = &sources
	}

	if build.dependencyCount() > 0 {
		options.Dependencies = &configDependencies{
			Compile:  build.Compile,
			Runtime:  build.Runtime,
			Test:     build.Test,
			Provided: build.Provided,
		}
	}

	file := configFile{
		Project: configProject{
			Provider: "jvm",
			Options:  options,
		},
	}
	data, err := toml.Marshal(file)
	if err != nil {
		return nil, fmt.Errorf("encode kine.toml: %w", err)
	}

	var decoded struct {
		Project struct {
			Options map[string]any `toml:"options"`
		} `toml:"project"`
	}
	if err := toml.Unmarshal(data, &decoded); err != nil {
		return nil, fmt.Errorf("decode generated kine.toml: %w", err)
	}
	if _, err := jvm.DecodeOptions(decoded.Project.Options, projectID); err != nil {
		return nil, fmt.Errorf("generated config is invalid: %w\n%s", err, data)
	}
	return data, nil
}

func sameStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func projectID(dir string) string {
	base := strings.ToLower(filepath.Base(dir))
	base = invalidIDCharacters.ReplaceAllString(base, "-")
	base = strings.Trim(base, "-")
	for strings.Contains(base, "--") {
		base = strings.ReplaceAll(base, "--", "-")
	}
	if base == "" {
		return "app"
	}
	if base[0] < 'a' || base[0] > 'z' {
		return "app-" + base
	}
	return base
}
