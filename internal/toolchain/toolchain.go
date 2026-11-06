// Package toolchain discovers JDKs and runs child processes.
package toolchain

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// ErrJDKNotFound is returned when no JDK matches a requirement.
var ErrJDKNotFound = errors.New("JDK not found")

// JDK describes one installed Java development kit.
type JDK struct {
	Home    string
	Version string
	Major   int
	Vendor  string
	Java    string
	Javac   string
}

// Requirement selects a JDK by major version.
type Requirement struct {
	Major int
}

// Discover scans roots for JDK homes.
func Discover(roots ...string) ([]JDK, error) {
	if len(roots) == 0 {
		roots = defaultRoots()
	}

	var jdks []JDK
	seen := make(map[string]struct{})
	for _, root := range roots {
		if root == "" {
			continue
		}
		info, err := os.Stat(root)
		if err != nil || !info.IsDir() {
			continue
		}

		if jdk, ok := readJDK(root); ok {
			addJDK(&jdks, seen, jdk)
			continue
		}
		children, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		for _, child := range children {
			if !child.IsDir() {
				continue
			}
			if jdk, ok := readJDK(filepath.Join(root, child.Name())); ok {
				addJDK(&jdks, seen, jdk)
			}
		}
	}
	sortJDKs(jdks)
	return jdks, nil
}

// Find returns the highest JDK matching the requirement.
func Find(requirement Requirement, roots ...string) (*JDK, error) {
	jdks, err := Discover(roots...)
	if err != nil {
		return nil, err
	}
	for i := range jdks {
		if requirement.Major == 0 || jdks[i].Major == requirement.Major {
			return &jdks[i], nil
		}
	}
	return nil, fmt.Errorf("%w: major %d", ErrJDKNotFound, requirement.Major)
}

func defaultRoots() []string {
	var roots []string
	if home := os.Getenv("JAVA_HOME"); home != "" {
		roots = append(roots, home)
	}
	if home := os.Getenv("JDK_HOME"); home != "" {
		roots = append(roots, home)
	}
	roots = append(roots, platformRoots()...)
	if home, err := os.UserHomeDir(); err == nil {
		roots = append(roots,
			filepath.Join(home, ".sdkman", "candidates", "java"),
			filepath.Join(home, ".asdf", "installs", "java"),
			filepath.Join(home, ".jdks"),
		)
	}
	if path, err := exec.LookPath(executableName("javac")); err == nil {
		roots = append(roots, filepath.Dir(filepath.Dir(path)))
	}
	return roots
}

func readJDK(home string) (JDK, bool) {
	javac, ok := findExecutable(filepath.Join(home, "bin"), "javac")
	if !ok {
		return JDK{}, false
	}

	data, err := os.ReadFile(filepath.Join(home, "release"))
	if err != nil {
		return JDK{}, false
	}
	values := parseRelease(data)
	version := values["JAVA_VERSION"]
	if version == "" {
		return JDK{}, false
	}
	major := majorVersion(version)
	if major == 0 {
		return JDK{}, false
	}

	return JDK{
		Home:    home,
		Version: version,
		Major:   major,
		Vendor:  values["IMPLEMENTOR"],
		Java:    filepath.Join(home, "bin", executableName("java")),
		Javac:   javac,
	}, true
}

func parseRelease(data []byte) map[string]string {
	values := make(map[string]string)
	for _, line := range strings.Split(string(data), "\n") {
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		values[key] = strings.Trim(strings.TrimSpace(value), `"`)
	}
	return values
}

func addJDK(jdks *[]JDK, seen map[string]struct{}, jdk JDK) {
	if real, err := filepath.EvalSymlinks(jdk.Home); err == nil {
		jdk.Home = real
		jdk.Java = filepath.Join(real, "bin", executableName("java"))
		jdk.Javac = filepath.Join(real, "bin", executableName("javac"))
	}
	if _, ok := seen[jdk.Home]; ok {
		return
	}
	seen[jdk.Home] = struct{}{}
	*jdks = append(*jdks, jdk)
}

func findExecutable(dir, name string) (string, bool) {
	for _, candidate := range executableCandidates(name) {
		path := filepath.Join(dir, candidate)
		info, err := os.Stat(path)
		if err == nil && !info.IsDir() {
			return path, true
		}
	}
	return "", false
}

// ToolPath returns an executable from the JDK bin directory.
func ToolPath(jdk *JDK, name string) (string, error) {
	path, ok := findExecutable(filepath.Join(jdk.Home, "bin"), name)
	if !ok {
		return "", fmt.Errorf("JDK %s has no %s", jdk.Version, name)
	}
	return path, nil
}
