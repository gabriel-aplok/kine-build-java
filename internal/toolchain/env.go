package toolchain

import (
	"os"
	"path/filepath"
	"strings"
)

// BuildEnv returns a minimal, deterministic environment.
func BuildEnv(jdk *JDK, tmpDir string, extra []string) []string {
	values := make(map[string]string)
	order := make([]string, 0, 7+len(extra))

	add := func(key, value string) {
		if _, ok := values[key]; !ok {
			order = append(order, key)
		}
		values[key] = value
	}

	if jdk != nil {
		add("PATH", filepath.Join(jdk.Home, "bin")+string(os.PathListSeparator)+platformPath())
		add("JAVA_HOME", jdk.Home)
	}
	if home, err := os.UserHomeDir(); err == nil {
		add("HOME", home)
	}
	add("TMPDIR", tmpDir)
	addPlatformEnv(add, tmpDir)

	for _, entry := range extra {
		key, value, ok := strings.Cut(entry, "=")
		if !ok || key == "" {
			continue
		}
		add(key, value)
	}

	result := make([]string, 0, len(order))
	for _, key := range order {
		result = append(result, key+"="+values[key])
	}
	return result
}
