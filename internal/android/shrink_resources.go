package android

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

var (
	resourceReferencePattern = regexp.MustCompile(`@(?:\+)?(?:[A-Za-z0-9_.]+:)?([A-Za-z][A-Za-z0-9_]*)/([A-Za-z0-9_.]+)`)
	attrReferencePattern     = regexp.MustCompile(`\?(?:[A-Za-z0-9_.]+:)?attr/([A-Za-z0-9_]+)`)
	styleParentPattern       = regexp.MustCompile(`parent\s*=\s*"([A-Za-z0-9_.]+)"`)
	rReferencePattern        = regexp.MustCompile(`\bR\s*\.\s*([A-Za-z][A-Za-z0-9_]*)\s*\.\s*([A-Za-z][A-Za-z0-9_]*)`)
)

type resourceGraph struct {
	edges map[string]map[string]struct{}
}

func newResourceGraph() *resourceGraph {
	return &resourceGraph{edges: make(map[string]map[string]struct{})}
}

func (g *resourceGraph) add(key string, references ...string) {
	if key == "" {
		return
	}
	if g.edges[key] == nil {
		g.edges[key] = make(map[string]struct{})
	}
	for _, reference := range references {
		if reference != "" && reference != key {
			g.edges[key][reference] = struct{}{}
		}
	}
}

func (g *resourceGraph) addReferences(key, value string) {
	for _, reference := range resourceReferences(value) {
		g.add(key, reference)
	}
}

// shrinkFeatureResources copies feature resource directories to output,
// dropping values and files that are unreachable from module sources, its
// manifest, public resources, and tools:keep declarations.
func shrinkFeatureResources(module Module, output string) ([]string, error) {
	sourceDirs := append(append([]string(nil), module.JavaDirs...), module.KotlinDirs...)
	return shrinkResources(module.ResDirs, sourceDirs, module.Manifest, output)
}

func shrinkResources(resDirs, sourceDirs []string, manifest string, output string) ([]string, error) {
	if err := os.RemoveAll(output); err != nil {
		return nil, fmt.Errorf("clean %s: %w", output, err)
	}
	if err := os.MkdirAll(output, 0o755); err != nil {
		return nil, fmt.Errorf("create %s: %w", output, err)
	}
	if len(resDirs) == 0 {
		return nil, nil
	}

	graph := newResourceGraph()
	var valuesFiles []string
	var resourceFiles []string
	for _, resourceDir := range resDirs {
		err := filepath.WalkDir(resourceDir, func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() {
				return nil
			}
			relative, err := filepath.Rel(resourceDir, path)
			if err != nil {
				return err
			}
			parts := strings.Split(filepath.ToSlash(relative), "/")
			if len(parts) < 2 {
				return nil
			}
			kind := strings.Split(parts[0], "-")[0]
			if kind == "values" {
				valuesFiles = append(valuesFiles, path)
				return collectValuesGraph(graph, path)
			}
			key := kind + "/" + resourceNameFromFile(entry.Name())
			graph.add(key, resourceReferencesFromFile(path)...)
			resourceFiles = append(resourceFiles, path)
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("scan resources %s: %w", resourceDir, err)
		}
	}

	roots, err := resourceRoots(sourceDirs, manifest)
	if err != nil {
		return nil, err
	}
	for _, path := range valuesFiles {
		public, err := publicResourceKeys(path)
		if err != nil {
			return nil, err
		}
		roots = append(roots, public...)
	}
	for _, file := range resourceFiles {
		roots = append(roots, toolsKeepResources(file)...)
	}
	for _, path := range valuesFiles {
		roots = append(roots, toolsKeepResources(path)...)
	}
	reachable := reachableResources(graph, roots)

	filtered := make([]string, 0, len(resDirs))
	for index, resourceDir := range resDirs {
		outputDir := filepath.Join(output, strconv.Itoa(index))
		filtered = append(filtered, outputDir)
		err := filepath.WalkDir(resourceDir, func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() {
				return nil
			}
			relative, err := filepath.Rel(resourceDir, path)
			if err != nil {
				return err
			}
			parts := strings.Split(filepath.ToSlash(relative), "/")
			if len(parts) < 2 {
				return nil
			}
			kind := strings.Split(parts[0], "-")[0]
			target := filepath.Join(outputDir, filepath.FromSlash(relative))
			if kind == "values" {
				return writeShrunkValues(path, target, reachable)
			}
			if _, ok := reachable[kind+"/"+resourceNameFromFile(entry.Name())]; !ok {
				return nil
			}
			return copyFile(path, target)
		})
		if err != nil {
			return nil, fmt.Errorf("shrink resources %s: %w", resourceDir, err)
		}
	}
	return filtered, nil
}

func resourceRoots(sourceDirs []string, manifest string) ([]string, error) {
	var roots []string
	for _, sourceDir := range sourceDirs {
		err := filepath.WalkDir(sourceDir, func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() {
				return nil
			}
			switch filepath.Ext(entry.Name()) {
			case ".java", ".kt":
				data, err := os.ReadFile(path)
				if err != nil {
					return err
				}
				roots = append(roots, sourceResourceReferences(string(data))...)
			}
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("scan sources %s: %w", sourceDir, err)
		}
	}
	if manifest != "" {
		data, err := os.ReadFile(manifest)
		if err == nil {
			roots = append(roots, resourceReferences(string(data))...)
		}
	}
	return roots, nil
}

func collectValuesGraph(graph *resourceGraph, path string) error {
	root, err := parseXMLFile(path)
	if err != nil {
		return err
	}
	for _, child := range root.Children {
		keys := valueDefinitionKeys(child)
		if len(keys) == 0 {
			continue
		}
		references := referencesInNode(child)
		for _, key := range keys {
			graph.add(key, references...)
		}
	}
	return nil
}

func writeShrunkValues(source, target string, reachable map[string]struct{}) error {
	root, err := parseXMLFile(source)
	if err != nil {
		return err
	}
	kept := root.Children[:0]
	for _, child := range root.Children {
		keys := valueDefinitionKeys(child)
		if len(keys) == 0 {
			kept = append(kept, child)
			continue
		}
		keep := false
		for _, key := range keys {
			if _, ok := reachable[key]; ok {
				keep = true
				break
			}
		}
		if keep {
			kept = append(kept, child)
		}
	}
	root.Children = kept
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Dir(target), err)
	}
	return writeManifestFile(target, root)
}

func valueDefinitionKeys(node *manifestNode) []string {
	kind := node.Name.Local
	switch kind {
	case "public", "java-symbol", "overlayable", "staging-public-group", "eat-comment":
		return nil
	case "item":
		kind = attributeValue(node, "", "type")
		if kind == "" {
			return nil
		}
	}
	name := attributeValue(node, "", "name")
	if name == "" {
		return nil
	}
	switch {
	case strings.HasSuffix(kind, "-array"):
		kind = "array"
	case kind == "font-family":
		kind = "font"
	}
	keys := []string{kind + "/" + name}
	if kind == "declare-styleable" {
		for _, child := range node.Children {
			if child.Name.Local != "attr" {
				continue
			}
			if attrName := attributeValue(child, "", "name"); attrName != "" {
				keys = append(keys, "attr/"+attrName)
			}
		}
	}
	return keys
}

type publicResource struct {
	Key string
	ID  string
}

func publicResourceKeys(path string) ([]string, error) {
	entries, err := publicResourceEntries(path)
	if err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(entries))
	for _, entry := range entries {
		keys = append(keys, entry.Key)
	}
	return keys, nil
}

func publicResourceEntries(path string) ([]publicResource, error) {
	root, err := parseXMLFile(path)
	if err != nil {
		return nil, err
	}
	var entries []publicResource
	for _, child := range root.Children {
		if child.Name.Local != "public" {
			continue
		}
		kind := attributeValue(child, "", "type")
		name := attributeValue(child, "", "name")
		if kind == "" || name == "" {
			continue
		}
		entries = append(entries, publicResource{
			Key: kind + "/" + name,
			ID:  attributeValue(child, "", "id"),
		})
	}
	return entries, nil
}

func toolsKeepResources(path string) []string {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var result []string
	for _, match := range regexp.MustCompile(`tools:keep\s*=\s*"([^"]*)"`).FindAllStringSubmatch(string(data), -1) {
		result = append(result, resourceReferences(match[1])...)
	}
	return result
}

func referencesInNode(node *manifestNode) []string {
	var references []string
	for _, attr := range node.Attr {
		references = append(references, resourceReferences(attr.Value)...)
		if attr.Name.Local == "parent" {
			if match := styleParentPattern.FindStringSubmatch(`parent="` + attr.Value + `"`); len(match) == 2 && !strings.Contains(match[1], "/") {
				references = append(references, "style/"+match[1])
			}
		}
	}
	references = append(references, resourceReferences(node.Text)...)
	for _, child := range node.Children {
		references = append(references, referencesInNode(child)...)
	}
	return references
}

func resourceReferences(value string) []string {
	var result []string
	for _, match := range resourceReferencePattern.FindAllStringSubmatch(value, -1) {
		result = append(result, match[1]+"/"+match[2])
	}
	for _, match := range attrReferencePattern.FindAllStringSubmatch(value, -1) {
		result = append(result, "attr/"+match[1])
	}
	return result
}

func sourceResourceReferences(source string) []string {
	var result []string
	for _, match := range rReferencePattern.FindAllStringSubmatchIndex(source, -1) {
		start := match[0]
		prefix := strings.TrimSpace(source[maxInt(0, start-9):start])
		if strings.HasSuffix(prefix, "android.") {
			continue
		}
		result = append(result, source[match[2]:match[3]]+"/"+source[match[4]:match[5]])
	}
	return result
}

func resourceReferencesFromFile(path string) []string {
	if !strings.HasSuffix(strings.ToLower(path), ".xml") {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	return resourceReferences(string(data))
}

func resourceNameFromFile(name string) string {
	if index := strings.Index(name, "."); index >= 0 {
		return name[:index]
	}
	return name
}

func reachableResources(graph *resourceGraph, roots []string) map[string]struct{} {
	reachable := make(map[string]struct{})
	queue := append([]string(nil), roots...)
	sort.Strings(queue)
	for len(queue) > 0 {
		key := queue[0]
		queue = queue[1:]
		if key == "" {
			continue
		}
		if _, ok := reachable[key]; ok {
			continue
		}
		reachable[key] = struct{}{}
		for reference := range graph.edges[key] {
			if _, ok := reachable[reference]; !ok {
				queue = append(queue, reference)
			}
		}
	}
	return reachable
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
