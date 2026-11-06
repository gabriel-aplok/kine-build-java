package compat

import (
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"

	"github.com/gabriel-aplok/kine-build-java/internal/jvm"
)

func TestGenerateConfig(t *testing.T) {
	build := &Build{
		Java:         21,
		MainClass:    "demo.App",
		Compile:      []string{"com.example:lib:1.0"},
		Test:         []string{"org.junit.jupiter:junit-jupiter:5.11.4"},
		JavaDirs:     []string{"src/main/java"},
		TestJavaDirs: []string{"src/test/java"},
	}

	data, err := generateConfig(build, "app")
	if err != nil {
		t.Fatalf("generateConfig() error = %v\n%s", err, data)
	}
	text := string(data)
	for _, want := range []string{
		`provider = 'jvm'`,
		`java = 21`,
		`main_class = 'demo.App'`,
		`compile = ['com.example:lib:1.0']`,
		`test = ['org.junit.jupiter:junit-jupiter:5.11.4']`,
	} {
		if !strings.Contains(text, want) {
			t.Errorf("config does not contain %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "[project.options.sources]") {
		t.Errorf("default sources were emitted:\n%s", text)
	}

	var decoded struct {
		Project struct {
			Options map[string]any `toml:"options"`
		} `toml:"project"`
	}
	if err := toml.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	options, err := jvm.DecodeOptions(decoded.Project.Options, "app")
	if err != nil {
		t.Fatalf("jvm.DecodeOptions() error = %v", err)
	}
	if options.Java != 21 || options.MainClass != "demo.App" {
		t.Errorf("options = %+v", options)
	}
}

func TestGenerateConfigCustomSources(t *testing.T) {
	build := &Build{
		Java:         17,
		JavaDirs:     []string{"src/custom/java"},
		TestJavaDirs: []string{"src/test/java"},
	}

	data, err := generateConfig(build, "app")
	if err != nil {
		t.Fatalf("generateConfig() error = %v\n%s", err, data)
	}
	if !strings.Contains(string(data), `java = ['src/custom/java']`) {
		t.Errorf("config does not contain the custom source:\n%s", data)
	}
}

func TestGenerateConfigDependenciesOmitted(t *testing.T) {
	build := &Build{JavaDirs: []string{"src/main/java"}, TestJavaDirs: []string{"src/test/java"}}

	data, err := generateConfig(build, "app")
	if err != nil {
		t.Fatalf("generateConfig() error = %v", err)
	}
	if strings.Contains(string(data), "[project.options.dependencies]") {
		t.Errorf("empty dependency table was emitted:\n%s", data)
	}
}

func TestProjectID(t *testing.T) {
	tests := []struct {
		dir  string
		want string
	}{
		{dir: "/tmp/My App_1", want: "my-app-1"},
		{dir: "/tmp/app", want: "app"},
		{dir: "/tmp/123", want: "app-123"},
		{dir: "/", want: "app"},
	}
	for _, tt := range tests {
		if got := projectID(tt.dir); got != tt.want {
			t.Errorf("projectID(%q) = %q, want %q", tt.dir, got, tt.want)
		}
	}
}
