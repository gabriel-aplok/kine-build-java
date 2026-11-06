package minecraft

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/pelletier/go-toml/v2"

	"github.com/gabriel-aplok/kine-build-java/internal/core"
	"github.com/gabriel-aplok/kine-build-java/internal/hash"
)

const (
	fabricLoader       = "fabric"
	defaultJavaDir     = "src/main/java"
	defaultResourceDir = "src/main/resources"
	metadataName       = "fabric.mod.json"
)

// Sources lists project-relative source locations.
type Sources struct {
	Java      []string `toml:"java"`
	Resources []string `toml:"resources"`
}

type optionsFile struct {
	Loader       string   `toml:"loader"`
	Version      string   `toml:"version"`
	ModID        string   `toml:"mod_id"`
	ModVersion   string   `toml:"mod_version"`
	MinecraftJar string   `toml:"minecraft_jar"`
	Classpath    []string `toml:"classpath"`
	Mappings     string   `toml:"mappings"`
	Remapper     string   `toml:"remapper"`
	Metadata     string   `toml:"metadata"`
	Sources      Sources  `toml:"sources"`
}

// Options is the resolved minecraft project configuration.
type Options struct {
	Loader        string
	Minecraft     string
	ModID         string
	ModVersion    string
	MinecraftJar  string
	Classpath     []string
	Mappings      string
	Remapper      string
	MetadataPath  string
	Metadata      FabricMetadata
	JavaDirs      []string
	ResourceDirs  []string
	MinecraftHash string
	ClasspathHash string
}

// CheckProject validates a minecraft project without building it.
func CheckProject(project *core.Project) error {
	if project == nil {
		return fmt.Errorf("project is nil")
	}
	_, err := decodeOptions(project.Options, project.ID, project.Dir)
	return err
}

func decodeOptions(values map[string]any, projectID, projectDir string) (Options, error) {
	var file optionsFile
	if len(values) > 0 {
		data, err := toml.Marshal(values)
		if err != nil {
			return Options{}, fmt.Errorf("encode options: %w", err)
		}
		decoder := toml.NewDecoder(bytes.NewReader(data))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&file); err != nil {
			return Options{}, fmt.Errorf("decode options: %w", err)
		}
	}

	if file.Loader == "" {
		file.Loader = fabricLoader
	}
	if file.Loader != fabricLoader {
		return Options{}, fmt.Errorf("loader %q is not supported, use %q", file.Loader, fabricLoader)
	}
	if strings.TrimSpace(file.Version) == "" || strings.ContainsFunc(file.Version, unicode.IsSpace) {
		return Options{}, fmt.Errorf("version is empty or has whitespace")
	}
	if len(file.Sources.Java) == 0 {
		file.Sources.Java = []string{defaultJavaDir}
	}
	if len(file.Sources.Resources) == 0 {
		file.Sources.Resources = []string{defaultResourceDir}
	}

	javaDirs, err := resolveProjectDirs(projectDir, file.Sources.Java)
	if err != nil {
		return Options{}, fmt.Errorf("java sources: %w", err)
	}
	resourceDirs, err := resolveProjectDirs(projectDir, file.Sources.Resources)
	if err != nil {
		return Options{}, fmt.Errorf("resources: %w", err)
	}

	metadataPath, err := findMetadata(projectDir, file.Metadata, resourceDirs)
	if err != nil {
		return Options{}, err
	}
	metadata, err := readMetadata(metadataPath)
	if err != nil {
		return Options{}, err
	}

	modID := file.ModID
	if modID == "" {
		modID = metadata.ID
	} else if modID != metadata.ID {
		return Options{}, fmt.Errorf("mod_id %q does not match metadata id %q", modID, metadata.ID)
	}
	modVersion := file.ModVersion
	if modVersion == "" {
		modVersion = metadata.Version
	} else if modVersion != metadata.Version {
		return Options{}, fmt.Errorf("mod_version %q does not match metadata version %q", modVersion, metadata.Version)
	}
	if strings.ContainsAny(modVersion, `/\`) || modVersion == "." || modVersion == ".." {
		return Options{}, fmt.Errorf("mod_version %q must not contain path separators", modVersion)
	}

	minecraftJar, err := resolveLocalFile(projectDir, file.MinecraftJar)
	if err != nil {
		return Options{}, fmt.Errorf("minecraft_jar: %w", err)
	}
	classpath := make([]string, 0, len(file.Classpath))
	classpathHash := make([]string, 0, len(file.Classpath))
	for index, value := range file.Classpath {
		path, err := resolveLocalFile(projectDir, value)
		if err != nil {
			return Options{}, fmt.Errorf("classpath %d: %w", index, err)
		}
		digest, err := hash.File(path)
		if err != nil {
			return Options{}, fmt.Errorf("classpath %d: %w", index, err)
		}
		classpath = append(classpath, path)
		classpathHash = append(classpathHash, digest)
	}

	mappings, err := resolveOptionalFile(projectDir, file.Mappings)
	if err != nil {
		return Options{}, fmt.Errorf("mappings: %w", err)
	}
	remapper, err := resolveOptionalFile(projectDir, file.Remapper)
	if err != nil {
		return Options{}, fmt.Errorf("remapper: %w", err)
	}
	if (mappings == "") != (remapper == "") {
		return Options{}, fmt.Errorf("mappings and remapper must be set together")
	}

	minecraftHash, err := hash.File(minecraftJar)
	if err != nil {
		return Options{}, fmt.Errorf("minecraft_jar: %w", err)
	}

	return Options{
		Loader:        file.Loader,
		Minecraft:     file.Version,
		ModID:         modID,
		ModVersion:    modVersion,
		MinecraftJar:  minecraftJar,
		Classpath:     classpath,
		Mappings:      mappings,
		Remapper:      remapper,
		MetadataPath:  metadataPath,
		Metadata:      metadata,
		JavaDirs:      javaDirs,
		ResourceDirs:  resourceDirs,
		MinecraftHash: minecraftHash,
		ClasspathHash: strings.Join(classpathHash, ","),
	}, nil
}

func findMetadata(projectDir, value string, resourceDirs []string) (string, error) {
	if value != "" {
		path, err := resolveProjectPath(projectDir, value)
		if err != nil {
			return "", fmt.Errorf("metadata: %w", err)
		}
		info, err := os.Stat(path)
		if err != nil {
			return "", fmt.Errorf("metadata %q: %w", value, err)
		}
		if !info.Mode().IsRegular() {
			return "", fmt.Errorf("metadata %q is not a regular file", value)
		}
		return path, nil
	}

	for _, dir := range resourceDirs {
		path := filepath.Join(dir, metadataName)
		if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() {
			return path, nil
		}
	}
	return "", fmt.Errorf("%s not found in the resource directories", metadataName)
}

func readMetadata(path string) (FabricMetadata, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return FabricMetadata{}, fmt.Errorf("read %s: %w", path, err)
	}
	metadata, err := ParseFabricMetadata(data)
	if err != nil {
		return FabricMetadata{}, fmt.Errorf("%s: %w", path, err)
	}
	return metadata, nil
}

func resolveProjectDirs(projectDir string, values []string) ([]string, error) {
	var result []string
	for _, value := range values {
		path, err := resolveProjectPath(projectDir, value)
		if err != nil {
			return nil, err
		}
		info, err := os.Stat(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("stat %s: %w", path, err)
		}
		if !info.IsDir() {
			return nil, fmt.Errorf("%s: not a directory", path)
		}
		result = append(result, path)
	}
	return result, nil
}

func resolveProjectPath(projectDir, value string) (string, error) {
	if strings.TrimSpace(value) == "" {
		return "", fmt.Errorf("path is empty")
	}
	if filepath.IsAbs(value) {
		return "", fmt.Errorf("path %q must be relative", value)
	}
	clean := filepath.Clean(filepath.FromSlash(value))
	if clean == "." {
		return "", fmt.Errorf("path %q is the project root", value)
	}
	full := filepath.Join(projectDir, clean)
	if !core.Within(projectDir, full) {
		return "", fmt.Errorf("path %q escapes the project", value)
	}
	return full, nil
}

func resolveLocalFile(projectDir, value string) (string, error) {
	if strings.TrimSpace(value) == "" {
		return "", fmt.Errorf("path is empty")
	}
	path := value
	if !filepath.IsAbs(path) {
		path = filepath.Join(projectDir, filepath.FromSlash(value))
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("%q: %w", value, err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("%q is not a regular file", value)
	}
	return path, nil
}

func resolveOptionalFile(projectDir, value string) (string, error) {
	if value == "" {
		return "", nil
	}
	return resolveLocalFile(projectDir, value)
}
