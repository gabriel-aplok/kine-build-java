package jvm

import (
	"testing"

	"github.com/pelletier/go-toml/v2"

	"github.com/gabriel-aplok/kine-build-java/internal/deps"
)

func TestDecodeOptionsDefaults(t *testing.T) {
	options, err := decodeOptions(nil, "app")
	if err != nil {
		t.Fatalf("decodeOptions() error = %v", err)
	}
	if options.Jar != "app.jar" {
		t.Errorf("Jar = %q, want app.jar", options.Jar)
	}
	if options.JUnit != defaultJUnit {
		t.Errorf("JUnit = %q, want %q", options.JUnit, defaultJUnit)
	}
	if len(options.Sources.Java) != 1 || options.Sources.Java[0] != "src/main/java" {
		t.Errorf("Java sources = %v", options.Sources.Java)
	}
	if len(options.Sources.TestKotlin) != 1 || options.Sources.TestKotlin[0] != "src/test/kotlin" {
		t.Errorf("TestKotlin sources = %v", options.Sources.TestKotlin)
	}
}

func TestDecodeOptions(t *testing.T) {
	values := optionsFromTOML(t, `
java = 21
kotlin = "2.0.21"
main_class = "demo.App"
jar = "demo.jar"
compose = true
junit = "1.11.4"

[sources]
java = ["src/java"]
kotlin = ["src/kotlin"]
resources = ["src/resources"]
test_java = ["src/test/java"]
test_kotlin = ["src/test/kotlin"]
test_resources = ["src/test/resources"]

[dependencies]
compile = ["com.example:lib:1.0"]
runtime = ["com.example:runtime:1.0"]
test = ["org.junit.jupiter:junit-jupiter:5.11.4"]
provided = ["com.example:provided:1.0"]
`)

	options, err := decodeOptions(values, "app")
	if err != nil {
		t.Fatalf("decodeOptions() error = %v", err)
	}
	if options.Java != 21 || options.Kotlin != "2.0.21" {
		t.Errorf("java/kotlin = %d/%q", options.Java, options.Kotlin)
	}
	if options.MainClass != "demo.App" || options.Jar != "demo.jar" {
		t.Errorf("main/jar = %q/%q", options.MainClass, options.Jar)
	}
	if !options.Compose {
		t.Error("Compose = false, want true")
	}
	if options.Sources.Java[0] != "src/java" {
		t.Errorf("Java sources = %v", options.Sources.Java)
	}

	dependencies, err := options.dependencyList()
	if err != nil {
		t.Fatalf("dependencyList() error = %v", err)
	}
	if len(dependencies) != 4 {
		t.Fatalf("dependencies = %d, want 4", len(dependencies))
	}
	wantScopes := []deps.Scope{
		deps.ScopeCompile,
		deps.ScopeRuntime,
		deps.ScopeTest,
		deps.ScopeProvided,
	}
	for i, want := range wantScopes {
		if dependencies[i].Scope != want {
			t.Errorf("dependencies[%d].Scope = %q, want %q", i, dependencies[i].Scope, want)
		}
	}
}

func TestDecodeOptionsUnknownKey(t *testing.T) {
	_, err := decodeOptions(optionsFromTOML(t, "unknown = true"), "app")
	if err == nil {
		t.Fatal("decodeOptions() error = nil, want error")
	}
}

func TestDecodeOptionsComposeWithoutKotlin(t *testing.T) {
	_, err := decodeOptions(optionsFromTOML(t, "compose = true"), "app")
	if err == nil {
		t.Fatal("decodeOptions() error = nil, want error")
	}
}

func TestDecodeOptionsBadDependency(t *testing.T) {
	_, err := decodeOptions(optionsFromTOML(t, `
[dependencies]
compile = ["nope"]
`), "app")
	if err == nil {
		t.Fatal("decodeOptions() error = nil, want error")
	}
}

func TestDecodeOptionsJarEscape(t *testing.T) {
	_, err := decodeOptions(optionsFromTOML(t, `jar = "../app.jar"`), "app")
	if err == nil {
		t.Fatal("decodeOptions() error = nil, want error")
	}
}

func TestDecodeOptionsNegativeJava(t *testing.T) {
	_, err := decodeOptions(optionsFromTOML(t, "java = -1"), "app")
	if err == nil {
		t.Fatal("decodeOptions() error = nil, want error")
	}
}

func optionsFromTOML(t *testing.T, data string) map[string]any {
	t.Helper()

	var values map[string]any
	if err := toml.Unmarshal([]byte(data), &values); err != nil {
		t.Fatal(err)
	}
	return values
}
