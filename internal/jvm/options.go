package jvm

import (
	"bytes"
	"fmt"
	"path/filepath"

	"github.com/pelletier/go-toml/v2"

	"github.com/gabriel-aplok/kine-build-java/internal/deps"
)

const defaultJUnit = "1.11.4"

// Options is the decoded jvm project configuration.
type Options struct {
	Java         int          `toml:"java"`
	Kotlin       string       `toml:"kotlin"`
	MainClass    string       `toml:"main_class"`
	Jar          string       `toml:"jar"`
	Compose      bool         `toml:"compose"`
	JUnit        string       `toml:"junit"`
	Sources      Sources      `toml:"sources"`
	Dependencies Dependencies `toml:"dependencies"`
}

// Sources lists project-relative source directories.
type Sources struct {
	Java          []string `toml:"java"`
	Kotlin        []string `toml:"kotlin"`
	Resources     []string `toml:"resources"`
	TestJava      []string `toml:"test_java"`
	TestKotlin    []string `toml:"test_kotlin"`
	TestResources []string `toml:"test_resources"`
}

// Dependencies lists Maven coordinates per scope.
type Dependencies struct {
	Compile  []string `toml:"compile"`
	Runtime  []string `toml:"runtime"`
	Test     []string `toml:"test"`
	Provided []string `toml:"provided"`
}

// DecodeOptions decodes jvm project options with defaults and validation.
func DecodeOptions(values map[string]any, projectID string) (Options, error) {
	return decodeOptions(values, projectID)
}

func decodeOptions(values map[string]any, projectID string) (Options, error) {
	var options Options
	if len(values) > 0 {
		data, err := toml.Marshal(values)
		if err != nil {
			return Options{}, fmt.Errorf("encode options: %w", err)
		}
		decoder := toml.NewDecoder(bytes.NewReader(data))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&options); err != nil {
			return Options{}, fmt.Errorf("decode options: %w", err)
		}
	}

	if options.Jar == "" {
		options.Jar = projectID + ".jar"
	}
	if options.JUnit == "" {
		options.JUnit = defaultJUnit
	}
	applySourceDefaults(&options.Sources)

	if err := validateOptions(options); err != nil {
		return Options{}, err
	}
	return options, nil
}

func applySourceDefaults(sources *Sources) {
	if len(sources.Java) == 0 {
		sources.Java = []string{"src/main/java"}
	}
	if len(sources.Kotlin) == 0 {
		sources.Kotlin = []string{"src/main/kotlin"}
	}
	if len(sources.Resources) == 0 {
		sources.Resources = []string{"src/main/resources"}
	}
	if len(sources.TestJava) == 0 {
		sources.TestJava = []string{"src/test/java"}
	}
	if len(sources.TestKotlin) == 0 {
		sources.TestKotlin = []string{"src/test/kotlin"}
	}
	if len(sources.TestResources) == 0 {
		sources.TestResources = []string{"src/test/resources"}
	}
}

func validateOptions(options Options) error {
	if options.Java < 0 {
		return fmt.Errorf("java major must not be negative")
	}
	if options.Compose && options.Kotlin == "" {
		return fmt.Errorf("compose requires kotlin")
	}
	if options.Jar == "" {
		return fmt.Errorf("jar is empty")
	}
	if filepath.Base(options.Jar) != options.Jar || options.Jar == "." || options.Jar == ".." {
		return fmt.Errorf("jar %q must be a file name", options.Jar)
	}
	if options.JUnit == "" {
		return fmt.Errorf("junit is empty")
	}
	if _, err := options.dependencyList(); err != nil {
		return err
	}
	return nil
}

func (o Options) dependencyList() ([]deps.Dependency, error) {
	var result []deps.Dependency

	groups := []struct {
		scope  deps.Scope
		values []string
	}{
		{deps.ScopeCompile, o.Dependencies.Compile},
		{deps.ScopeRuntime, o.Dependencies.Runtime},
		{deps.ScopeTest, o.Dependencies.Test},
		{deps.ScopeProvided, o.Dependencies.Provided},
	}

	for _, group := range groups {
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
