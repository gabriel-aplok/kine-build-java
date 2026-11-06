package android

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/gabriel-aplok/kine-build-java/internal/deps"
)

const aarMarker = ".complete"

// aarContent lists the extracted parts of one AAR.
type aarContent struct {
	ClassesJar string
	ResDir     string
	AssetsDir  string
	Manifest   string
	Proguard   string
	NativeDir  string
}

// classpathJars returns the jars used for compilation and dexing.
// AAR artifacts contribute their classes.jar.
func classpathJars(artifacts []deps.Artifact, workDir string) ([]string, error) {
	// Kotlin multiplatform publishes a metadata jar and a -jvm jar with the
	// same classes. Keep only the platform artifact.
	preferJVM := make(map[string]struct{})
	for _, artifact := range artifacts {
		if strings.HasSuffix(artifact.Artifact, "-jvm") {
			preferJVM[artifact.Group+":"+strings.TrimSuffix(artifact.Artifact, "-jvm")] = struct{}{}
		}
	}

	result := make([]string, 0, len(artifacts))
	for _, artifact := range artifacts {
		if strings.HasPrefix(artifact.Classifier, "natives-") {
			continue
		}
		if _, ok := preferJVM[artifact.Group+":"+artifact.Artifact]; ok {
			continue
		}
		if artifact.Extension != "aar" {
			result = append(result, artifact.Path)
			continue
		}
		content, err := extractAAR(artifact, workDir)
		if err != nil {
			return nil, err
		}
		if content.ClassesJar != "" {
			result = append(result, content.ClassesJar)
		}
	}
	return result, nil
}

func extractAAR(artifact deps.Artifact, workDir string) (*aarContent, error) {
	dir := filepath.Join(workDir, artifact.SHA256)
	marker := filepath.Join(dir, aarMarker)
	if isRegular(marker) {
		return aarContentFromDir(dir), nil
	}
	if err := os.RemoveAll(dir); err != nil {
		return nil, fmt.Errorf("clean %s: %w", dir, err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create %s: %w", dir, err)
	}

	reader, err := zip.OpenReader(artifact.Path)
	if err != nil {
		return nil, fmt.Errorf("open aar %s: %w", artifact.Path, err)
	}
	defer reader.Close()

	for _, entry := range reader.File {
		if entry.FileInfo().IsDir() {
			continue
		}
		name := entry.Name
		if !safeArchivePath(name) {
			return nil, fmt.Errorf("aar %s: unsafe entry %q", artifact.Coordinate, name)
		}
		switch {
		case name == "classes.jar",
			name == "AndroidManifest.xml",
			name == "proguard.txt",
			name == "R.txt",
			name == "public.txt",
			strings.HasPrefix(name, "res/"),
			strings.HasPrefix(name, "assets/"),
			strings.HasPrefix(name, "jni/"):
		default:
			continue
		}
		if err := extractZipEntry(entry, filepath.Join(dir, filepath.FromSlash(name))); err != nil {
			return nil, fmt.Errorf("aar %s: %w", artifact.Coordinate, err)
		}
	}

	if err := os.WriteFile(marker, []byte(artifact.SHA256), 0o644); err != nil {
		return nil, fmt.Errorf("write aar marker %s: %w", marker, err)
	}
	return aarContentFromDir(dir), nil
}

func aarContentFromDir(dir string) *aarContent {
	content := &aarContent{}
	if path := filepath.Join(dir, "classes.jar"); isRegular(path) {
		content.ClassesJar = path
	}
	if info, err := os.Stat(filepath.Join(dir, "res")); err == nil && info.IsDir() {
		content.ResDir = filepath.Join(dir, "res")
	}
	if info, err := os.Stat(filepath.Join(dir, "assets")); err == nil && info.IsDir() {
		content.AssetsDir = filepath.Join(dir, "assets")
	}
	if path := filepath.Join(dir, "AndroidManifest.xml"); isRegular(path) {
		content.Manifest = path
	}
	if path := filepath.Join(dir, "proguard.txt"); isRegular(path) {
		content.Proguard = path
	}
	if info, err := os.Stat(filepath.Join(dir, "jni")); err == nil && info.IsDir() {
		content.NativeDir = filepath.Join(dir, "jni")
	}
	return content
}

func safeArchivePath(name string) bool {
	if name == "" || strings.Contains(name, "\\") {
		return false
	}
	if strings.HasPrefix(name, "/") || filepath.IsAbs(name) {
		return false
	}
	clean := filepath.Clean(filepath.FromSlash(name))
	return clean != ".." && !strings.HasPrefix(clean, ".."+string(filepath.Separator))
}

func extractZipEntry(entry *zip.File, target string) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Dir(target), err)
	}
	source, err := entry.Open()
	if err != nil {
		return fmt.Errorf("open %s: %w", entry.Name, err)
	}
	defer source.Close()

	output, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("create %s: %w", target, err)
	}
	if _, err := io.Copy(output, source); err != nil {
		output.Close()
		return fmt.Errorf("extract %s: %w", entry.Name, err)
	}
	if err := output.Close(); err != nil {
		return fmt.Errorf("close %s: %w", target, err)
	}
	return nil
}

func (p *Provider) prepareAARs(projectDir string, options *Options, artifacts []deps.Artifact) error {
	workDir := filepath.Join(projectDir, "build", "android", "aar")
	for _, artifact := range artifacts {
		if artifact.Extension != "aar" || strings.HasPrefix(artifact.Classifier, "natives-") {
			continue
		}
		content, err := extractAAR(artifact, workDir)
		if err != nil {
			return err
		}
		if content.ResDir != "" {
			options.AARResDirs = append(options.AARResDirs, content.ResDir)
		}
		if content.AssetsDir != "" {
			options.AARAssetDirs = append(options.AARAssetDirs, content.AssetsDir)
		}
		if content.Manifest != "" {
			options.AARManifests = append(options.AARManifests, content.Manifest)
		}
		if content.NativeDir != "" {
			options.AARNativeDirs = append(options.AARNativeDirs, content.NativeDir)
		}
		if content.Proguard != "" {
			options.AARProguard = append(options.AARProguard, content.Proguard)
		}
	}
	return nil
}
