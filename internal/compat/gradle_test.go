package compat

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestParseGradleGroovy(t *testing.T) {
	dir := t.TempDir()
	build := `plugins {
    id 'java'
    id 'application'
}

group = 'com.example'
version = '1.0'

java {
    toolchain {
        languageVersion = JavaLanguageVersion.of(21)
    }
}

repositories {
    mavenCentral()
}

dependencies {
    implementation 'com.example:lib:2.0'
    api 'com.example:api:1.0'
    compileOnly 'com.example:annotations:1.0'
    runtimeOnly 'com.example:runtime:1.0'
    testImplementation 'org.junit.jupiter:junit-jupiter:5.11.4'
    implementation project(':lib')
    implementation platform('com.example:bom:1.0')
    implementation 'com.example:dynamic:1.+'
}

application {
    mainClass = 'demo.App'
}
`
	writeFile(t, filepath.Join(dir, "build.gradle"), build)

	parsed, err := parseGradle(dir)
	if err != nil {
		t.Fatalf("parseGradle() error = %v", err)
	}
	if parsed.Java != 21 {
		t.Errorf("Java = %d, want 21", parsed.Java)
	}
	if parsed.MainClass != "demo.App" {
		t.Errorf("MainClass = %q", parsed.MainClass)
	}
	if !reflect.DeepEqual(parsed.Compile, []string{"com.example:lib:2.0", "com.example:api:1.0"}) {
		t.Errorf("Compile = %v", parsed.Compile)
	}
	if !reflect.DeepEqual(parsed.Provided, []string{"com.example:annotations:1.0"}) {
		t.Errorf("Provided = %v", parsed.Provided)
	}
	if !reflect.DeepEqual(parsed.Runtime, []string{"com.example:runtime:1.0"}) {
		t.Errorf("Runtime = %v", parsed.Runtime)
	}
	if !reflect.DeepEqual(parsed.Test, []string{"org.junit.jupiter:junit-jupiter:5.11.4"}) {
		t.Errorf("Test = %v", parsed.Test)
	}

	warnings := strings.Join(parsed.Warnings, "\n")
	for _, want := range []string{"project", "platform", "dynamic", "repositories"} {
		if !strings.Contains(warnings, want) {
			t.Errorf("warnings do not contain %q:\n%s", want, warnings)
		}
	}
}

func TestParseGradleKotlin(t *testing.T) {
	dir := t.TempDir()
	build := `plugins {
    id("org.jetbrains.kotlin.jvm") version "2.0.21"
}

java {
    toolchain {
        languageVersion.set(JavaLanguageVersion.of(17))
    }
}

dependencies {
    implementation("com.example:lib:1.0")
    testImplementation("org.junit.jupiter:junit-jupiter:5.11.4")
}

application {
    mainClass.set("demo.App")
}
`
	writeFile(t, filepath.Join(dir, "build.gradle.kts"), build)

	parsed, err := parseGradle(dir)
	if err != nil {
		t.Fatalf("parseGradle() error = %v", err)
	}
	if parsed.Java != 17 || parsed.Kotlin != "2.0.21" {
		t.Errorf("java/kotlin = %d/%q", parsed.Java, parsed.Kotlin)
	}
	if parsed.MainClass != "demo.App" {
		t.Errorf("MainClass = %q", parsed.MainClass)
	}
	if !reflect.DeepEqual(parsed.Compile, []string{"com.example:lib:1.0"}) {
		t.Errorf("Compile = %v", parsed.Compile)
	}
	if !reflect.DeepEqual(parsed.Test, []string{"org.junit.jupiter:junit-jupiter:5.11.4"}) {
		t.Errorf("Test = %v", parsed.Test)
	}
}

func TestParseGradleMultiModule(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "build.gradle"), "plugins { id 'java' }\n")
	writeFile(t, filepath.Join(dir, "settings.gradle"), "include 'app'\n")

	parsed, err := parseGradle(dir)
	if err != nil {
		t.Fatalf("parseGradle() error = %v", err)
	}
	warnings := strings.Join(parsed.Warnings, "\n")
	if !strings.Contains(warnings, "multi-module") {
		t.Errorf("warnings do not mention multi-module:\n%s", warnings)
	}
}

func TestParseGradleMissingFile(t *testing.T) {
	if _, err := parseGradle(t.TempDir()); err == nil {
		t.Fatal("parseGradle() error = nil, want error")
	}
}

func TestParseGradleUnknownPlugin(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "build.gradle"), "plugins { id 'com.android.application' }\n")

	parsed, err := parseGradle(dir)
	if err != nil {
		t.Fatalf("parseGradle() error = %v", err)
	}
	if !strings.Contains(strings.Join(parsed.Warnings, "\n"), "com.android.application") {
		t.Errorf("warnings do not mention the plugin: %v", parsed.Warnings)
	}
}
