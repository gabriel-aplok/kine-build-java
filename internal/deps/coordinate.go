package deps

import (
	"fmt"
	"regexp"
	"strings"
)

// Scope selects a dependency classpath.
type Scope string

const (
	ScopeCompile  Scope = "compile"
	ScopeRuntime  Scope = "runtime"
	ScopeTest     Scope = "test"
	ScopeProvided Scope = "provided"
)

// Coordinate identifies one Maven artifact.
type Coordinate struct {
	Group      string
	Artifact   string
	Version    string
	Classifier string
	Extension  string
	File       string
}

// Exclusion removes a transitive dependency on one path.
type Exclusion struct {
	Group    string `json:"group"`
	Artifact string `json:"artifact"`
}

// Dependency is a root dependency declaration.
type Dependency struct {
	Coordinate
	Scope      Scope
	Exclusions []Exclusion
}

var coordinatePartPattern = regexp.MustCompile(`^[A-Za-z0-9._+-]+$`)

// ParseCoordinate parses group:artifact:version[:classifier][@extension].
func ParseCoordinate(value string) (Coordinate, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return Coordinate{}, fmt.Errorf("coordinate %q: empty", value)
	}

	extension := "jar"
	if index := strings.LastIndex(trimmed, "@"); index >= 0 {
		extension = trimmed[index+1:]
		trimmed = trimmed[:index]
	}

	parts := strings.Split(trimmed, ":")
	if len(parts) != 3 && len(parts) != 4 {
		return Coordinate{}, fmt.Errorf("coordinate %q: want group:artifact:version[:classifier][@extension]", value)
	}

	version, err := normalizeVersion(parts[2])
	if err != nil {
		return Coordinate{}, fmt.Errorf("coordinate %q: %w", value, err)
	}
	coord := Coordinate{
		Group:     parts[0],
		Artifact:  parts[1],
		Version:   version,
		Extension: extension,
	}
	if len(parts) == 4 {
		coord.Classifier = parts[3]
		if coord.Classifier == "" {
			return Coordinate{}, fmt.Errorf("coordinate %q: classifier is empty", value)
		}
	}

	fields := []struct {
		name  string
		value string
	}{
		{"group", coord.Group},
		{"artifact", coord.Artifact},
		{"version", coord.Version},
		{"classifier", coord.Classifier},
		{"extension", coord.Extension},
	}
	for _, field := range fields {
		if field.value == "" {
			if field.name == "classifier" {
				continue
			}
			return Coordinate{}, fmt.Errorf("coordinate %q: %s is empty", value, field.name)
		}
		if field.value == "." || field.value == ".." {
			return Coordinate{}, fmt.Errorf("coordinate %q: %s %q is invalid", value, field.name, field.value)
		}
		if !coordinatePartPattern.MatchString(field.value) {
			return Coordinate{}, fmt.Errorf("coordinate %q: %s %q has invalid characters", value, field.name, field.value)
		}
	}
	if err := validateCoordinateVersion(value, coord.Version); err != nil {
		return Coordinate{}, err
	}
	return coord, nil
}

// normalizeVersion accepts exact Maven ranges such as [1.1.0] and uses the
// lower bound of a bounded range such as [1.1.0,2.0). This keeps transitive
// POMs usable without implementing a full Maven version solver.
func normalizeVersion(version string) (string, error) {
	if !strings.ContainsAny(version, "[](),") {
		return version, nil
	}
	if len(version) < 3 {
		return "", fmt.Errorf("invalid version range %q", version)
	}
	open, close := version[0], version[len(version)-1]
	if (open != '[' && open != '(') || (close != ']' && close != ')') {
		return "", fmt.Errorf("invalid version range %q", version)
	}
	inner := version[1 : len(version)-1]
	if !strings.Contains(inner, ",") {
		if open != '[' || close != ']' || inner == "" {
			return "", fmt.Errorf("version range %q has no lower bound", version)
		}
		return strings.TrimSpace(inner), nil
	}
	lower := strings.TrimSpace(inner[:strings.Index(inner, ",")])
	if lower == "" {
		return "", fmt.Errorf("version range %q has no lower bound", version)
	}
	return lower, nil
}

func validateCoordinateVersion(coordinate, version string) error {
	if strings.HasSuffix(version, "-SNAPSHOT") {
		return fmt.Errorf("coordinate %q: snapshot versions are not supported", coordinate)
	}
	if strings.ContainsAny(version, "[](),") {
		return fmt.Errorf("coordinate %q: version ranges are not supported", coordinate)
	}
	return nil
}

// FileName returns the Maven artifact file name. File overrides the
// conventional name when Gradle Module Metadata points at a custom file.
func (c Coordinate) FileName() string {
	if c.File != "" {
		return c.File
	}
	name := c.Artifact + "-" + c.Version
	if c.Classifier != "" {
		name += "-" + c.Classifier
	}
	return name + "." + c.Extension
}

// Path returns the Maven repository path for the artifact.
func (c Coordinate) Path() string {
	return strings.ReplaceAll(c.Group, ".", "/") + "/" + c.Artifact + "/" + c.Version + "/" + c.FileName()
}

// POMPath returns the Maven repository path for the POM.
func (c Coordinate) POMPath() string {
	return strings.ReplaceAll(c.Group, ".", "/") + "/" + c.Artifact + "/" + c.Version + "/" + c.Artifact + "-" + c.Version + ".pom"
}

// Key identifies a module for conflict resolution.
func (c Coordinate) Key() string {
	return c.Group + ":" + c.Artifact + ":" + c.Classifier + ":" + c.Extension
}

func (c Coordinate) String() string {
	value := c.Group + ":" + c.Artifact + ":" + c.Version
	if c.Classifier != "" {
		value += ":" + c.Classifier
	}
	if c.Extension != "" && c.Extension != "jar" {
		value += "@" + c.Extension
	}
	return value
}

// Valid reports whether the scope is known.
func (s Scope) Valid() bool {
	switch s {
	case ScopeCompile, ScopeRuntime, ScopeTest, ScopeProvided:
		return true
	default:
		return false
	}
}
