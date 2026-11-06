package toolchain

import (
	"strings"
	"testing"
)

func TestBuildEnv(t *testing.T) {
	jdk := &JDK{Home: "/opt/jdk-21"}
	tmp := t.TempDir()

	env := BuildEnv(jdk, tmp, []string{"FOO=bar", "LANG=pt_BR.UTF-8"})
	values := envValues(env)

	if values["JAVA_HOME"] != "/opt/jdk-21" {
		t.Errorf("JAVA_HOME = %q", values["JAVA_HOME"])
	}
	if values["PATH"] != "/opt/jdk-21/bin:/usr/bin:/bin" {
		t.Errorf("PATH = %q", values["PATH"])
	}
	if values["TMPDIR"] != tmp {
		t.Errorf("TMPDIR = %q, want %q", values["TMPDIR"], tmp)
	}
	if values["LANG"] != "pt_BR.UTF-8" {
		t.Errorf("LANG = %q, want the override", values["LANG"])
	}
	if values["LC_ALL"] != "C" {
		t.Errorf("LC_ALL = %q, want C", values["LC_ALL"])
	}
	if values["TZ"] != "UTC" {
		t.Errorf("TZ = %q, want UTC", values["TZ"])
	}
	if values["FOO"] != "bar" {
		t.Errorf("FOO = %q, want bar", values["FOO"])
	}
	if values["HOME"] == "" {
		t.Error("HOME is empty")
	}

	for _, key := range []string{
		"CLASSPATH",
		"JAVA_TOOL_OPTIONS",
		"_JAVA_OPTIONS",
		"JDK_JAVA_OPTIONS",
		"GRADLE_OPTS",
		"MAVEN_OPTS",
		"KOTLIN_OPTS",
	} {
		if _, ok := values[key]; ok {
			t.Errorf("%s leaked into the environment", key)
		}
	}

	if !strings.HasPrefix(env[0], "PATH=") {
		t.Errorf("first entry = %q, want PATH", env[0])
	}
}

func envValues(env []string) map[string]string {
	values := make(map[string]string, len(env))
	for _, entry := range env {
		key, value, ok := strings.Cut(entry, "=")
		if ok {
			values[key] = value
		}
	}
	return values
}
