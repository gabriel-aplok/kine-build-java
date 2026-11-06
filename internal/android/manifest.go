package android

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

const (
	androidNamespace = "http://schemas.android.com/apk/res/android"
	toolsNamespace   = "http://schemas.android.com/tools"
)

var placeholderPattern = regexp.MustCompile(`\$\{([A-Za-z0-9_]+)\}`)

type manifestNode struct {
	Name     xml.Name
	Attr     []xml.Attr
	Children []*manifestNode
	Text     string
}

func mergeManifestFiles(paths []string, values map[string]string) (*manifestNode, error) {
	if len(paths) == 0 {
		return nil, fmt.Errorf("no manifests")
	}

	var merged *manifestNode
	for index, path := range paths {
		node, err := parseManifest(path)
		if err != nil {
			return nil, err
		}
		node, err = expandManifest(node, values)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		if index == 0 {
			merged = node
			continue
		}
		if err := mergeManifestNode(merged, node, true); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
	}
	return merged, nil
}

func mergeVariantManifest(buildDir string, options Options) (string, error) {
	base := options.BaseManifest
	if base == "" && len(options.Manifests) > 0 {
		base = options.Manifests[0]
	}
	if base == "" {
		base = options.Manifest
	}
	values, err := manifestValues(base, options)
	if err != nil {
		return "", err
	}
	manifests := append(append([]string(nil), options.AARManifests...), options.Manifests...)
	if len(manifests) == 0 {
		manifests = []string{base}
	}
	merged, err := mergeManifestFiles(manifests, values)
	if err != nil {
		return "", err
	}
	if values["applicationId"] != values["packageName"] {
		qualifyRelativeComponentNames(merged, values["packageName"])
	}
	setManifestAttribute(merged, "", "package", values["packageName"])
	if options.Variant.Name != "" {
		if application := findManifestChild(merged, "application"); application != nil {
			setManifestAttribute(application, "android", "debuggable", strconv.FormatBool(options.Variant.BuildTypeOptions.Debug))
		}
	}
	target := filepath.Join(buildDir, "AndroidManifest.xml")
	if err := writeManifestFile(target, merged); err != nil {
		return "", err
	}
	return target, nil
}

func qualifyRelativeComponentNames(node *manifestNode, packageName string) {
	switch node.Name.Local {
	case "application", "activity", "activity-alias", "service", "receiver", "provider", "instrumentation":
		for index := range node.Attr {
			attr := &node.Attr[index]
			if attr.Name.Space != "android" {
				continue
			}
			switch attr.Name.Local {
			case "name", "targetActivity", "backupAgent", "manageSpaceActivity":
			default:
				continue
			}
			if strings.HasPrefix(attr.Value, ".") {
				attr.Value = packageName + attr.Value
			}
		}
	}
	for _, child := range node.Children {
		qualifyRelativeComponentNames(child, packageName)
	}
}

func setManifestAttribute(node *manifestNode, namespace, name, value string) {
	for index := range node.Attr {
		if node.Attr[index].Name.Space == namespace && node.Attr[index].Name.Local == name {
			node.Attr[index].Value = value
			return
		}
	}
	node.Attr = append(node.Attr, xml.Attr{Name: xml.Name{Space: namespace, Local: name}, Value: value})
}

func manifestValues(mainManifest string, options Options) (map[string]string, error) {
	root, err := parseManifest(mainManifest)
	if err != nil {
		return nil, err
	}
	packageName := attributeValue(root, "", "package")
	if packageName == "" {
		return nil, fmt.Errorf("%s: package is missing", mainManifest)
	}

	applicationID := packageName + options.Variant.FlavorOptions.ApplicationIDSuffix
	baseVersionCode := options.VersionCode
	if baseVersionCode == 0 {
		baseVersionCode = defaultVersionCode
	}
	baseVersionName := options.VersionName
	if baseVersionName == "" {
		baseVersionName = defaultVersionName
	}
	versionName := baseVersionName + options.Variant.FlavorOptions.VersionNameSuffix
	versionCode := baseVersionCode + options.Variant.FlavorOptions.VersionCodeOffset
	if versionCode <= 0 {
		return nil, fmt.Errorf("variant %s: version code is %d", options.variantLabel(), versionCode)
	}
	return map[string]string{
		"applicationId":     applicationID,
		"packageName":       packageName,
		"versionCode":       fmt.Sprintf("%d", versionCode),
		"versionName":       versionName,
		"flavor":            options.Variant.Flavor,
		"buildType":         options.Variant.BuildType,
		"variant":           options.Variant.Name,
		"minSdkVersion":     fmt.Sprintf("%d", options.MinSDK),
		"targetSdkVersion":  fmt.Sprintf("%d", options.TargetSDK),
		"compileSdkVersion": fmt.Sprintf("%d", options.CompileSDK),
	}, nil
}

func parseManifest(path string) (*manifestNode, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read manifest %s: %w", path, err)
	}
	return parseManifestData(path, data)
}

func parseXMLFile(path string) (*manifestNode, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read xml %s: %w", path, err)
	}
	return parseXMLData(path, data)
}

func parseManifestData(path string, data []byte) (*manifestNode, error) {
	root, err := parseXMLData(path, data)
	if err != nil {
		return nil, err
	}
	if root.Name.Local != "manifest" {
		return nil, fmt.Errorf("%s: root element is %q, want manifest", path, root.Name.Local)
	}
	return root, nil
}

func parseXMLData(path string, data []byte) (*manifestNode, error) {
	decoder := xml.NewDecoder(bytes.NewReader(data))
	var root *manifestNode
	var stack []*manifestNode

	for {
		token, err := decoder.RawToken()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		switch value := token.(type) {
		case xml.StartElement:
			node := &manifestNode{Name: value.Name, Attr: append([]xml.Attr(nil), value.Attr...)}
			if len(stack) == 0 {
				if root != nil {
					return nil, fmt.Errorf("%s: multiple root elements", path)
				}
				root = node
			} else {
				parent := stack[len(stack)-1]
				parent.Children = append(parent.Children, node)
			}
			stack = append(stack, node)
		case xml.EndElement:
			if len(stack) == 0 {
				return nil, fmt.Errorf("%s: unexpected end element %s", path, value.Name.Local)
			}
			stack = stack[:len(stack)-1]
		case xml.CharData:
			if len(stack) > 0 {
				stack[len(stack)-1].Text += string(value)
			}
		case xml.Comment, xml.Directive, xml.ProcInst:
		}
	}
	if root == nil {
		return nil, fmt.Errorf("%s: xml is empty", path)
	}
	return root, nil
}

func writeManifestFile(path string, node *manifestNode) error {
	var buffer bytes.Buffer
	buffer.WriteString(`<?xml version="1.0" encoding="utf-8"?>`)
	buffer.WriteByte('\n')
	writeManifestNode(&buffer, node, 0)
	if err := os.WriteFile(path, buffer.Bytes(), 0o644); err != nil {
		return fmt.Errorf("write manifest %s: %w", path, err)
	}
	return nil
}

func writeManifestNode(buffer *bytes.Buffer, node *manifestNode, depth int) {
	indent := strings.Repeat("\t", depth)
	buffer.WriteString(indent)
	buffer.WriteByte('<')
	writeXMLName(buffer, node.Name)
	for _, attr := range node.Attr {
		buffer.WriteByte(' ')
		writeXMLName(buffer, attr.Name)
		buffer.WriteString(`="`)
		xml.EscapeText(buffer, []byte(attr.Value))
		buffer.WriteByte('"')
	}

	if len(node.Children) == 0 && strings.TrimSpace(node.Text) == "" {
		buffer.WriteString("/>\n")
		return
	}
	buffer.WriteString(">")
	if strings.TrimSpace(node.Text) == "" {
		buffer.WriteByte('\n')
	} else {
		xml.EscapeText(buffer, []byte(node.Text))
	}
	for _, child := range node.Children {
		writeManifestNode(buffer, child, depth+1)
	}
	if strings.TrimSpace(node.Text) != "" {
		buffer.WriteString("</")
		writeXMLName(buffer, node.Name)
		buffer.WriteString(">\n")
		return
	}
	buffer.WriteString(indent)
	buffer.WriteString("</")
	writeXMLName(buffer, node.Name)
	buffer.WriteString(">\n")
}

func writeXMLName(buffer *bytes.Buffer, name xml.Name) {
	switch {
	case name.Space == "xmlns":
		buffer.WriteString("xmlns:")
		buffer.WriteString(name.Local)
	case name.Local == "xmlns":
		buffer.WriteString("xmlns")
	default:
		if name.Space != "" {
			buffer.WriteString(name.Space)
			buffer.WriteByte(':')
		}
		buffer.WriteString(name.Local)
	}
}

func expandManifest(node *manifestNode, values map[string]string) (*manifestNode, error) {
	for index := range node.Attr {
		value, err := expandPlaceholders(node.Attr[index].Value, values)
		if err != nil {
			return nil, err
		}
		node.Attr[index].Value = value
	}
	if node.Text != "" {
		value, err := expandPlaceholders(node.Text, values)
		if err != nil {
			return nil, err
		}
		node.Text = value
	}
	for _, child := range node.Children {
		if _, err := expandManifest(child, values); err != nil {
			return nil, err
		}
	}
	return node, nil
}

func expandPlaceholders(value string, values map[string]string) (string, error) {
	var missing string
	expanded := placeholderPattern.ReplaceAllStringFunc(value, func(match string) string {
		key := match[2 : len(match)-1]
		replacement, ok := values[key]
		if !ok {
			missing = key
			return match
		}
		return replacement
	})
	if missing != "" {
		return "", fmt.Errorf("unknown placeholder ${%s}", missing)
	}
	return expanded, nil
}

func mergeManifestNode(target, source *manifestNode, root bool) error {
	index := make(map[string]int, len(target.Attr))
	for position, attr := range target.Attr {
		index[attrKey(attr.Name)] = position
	}
	for _, attr := range source.Attr {
		key := attrKey(attr.Name)
		if root && key == "package" {
			continue
		}
		if attr.Name.Space == "tools" {
			continue
		}
		if position, ok := index[key]; ok {
			if attr.Name.Space == "xmlns" || (attr.Name.Space == "" && attr.Name.Local == "xmlns") {
				continue
			}
			target.Attr[position] = attr
			continue
		}
		index[key] = len(target.Attr)
		target.Attr = append(target.Attr, attr)
	}

	existing := make(map[string]*manifestNode, len(target.Children))
	for _, child := range target.Children {
		if key := manifestIdentity(child); key != "" {
			existing[key] = child
		}
	}

	for _, child := range source.Children {
		key := manifestIdentity(child)
		nodeRule := attributeValue(child, "tools", "node")
		replace := attributeValue(child, "tools", "replace")
		switch nodeRule {
		case "", "merge":
		case "remove":
			if key != "" {
				removeManifestChild(target, key)
				delete(existing, key)
			}
			continue
		case "removeAll":
			if key != "" {
				removeManifestChildren(target, key)
				delete(existing, key)
			}
			continue
		case "replace":
			if key != "" {
				removeManifestChild(target, key)
			}
			replacement := cloneManifest(child)
			sanitizeManifest(replacement)
			target.Children = append(target.Children, replacement)
			if key != "" {
				existing[key] = replacement
			}
			continue
		default:
			return fmt.Errorf("unsupported tools:node rule %q on <%s>", nodeRule, child.Name.Local)
		}

		if key != "" {
			if current, ok := existing[key]; ok {
				if err := mergeManifestNode(current, child, false); err != nil {
					return err
				}
				if replace != "" {
					applyToolsReplace(current, replace)
				}
				continue
			}
		}
		added := cloneManifest(child)
		sanitizeManifest(added)
		target.Children = append(target.Children, added)
		if key != "" {
			existing[key] = added
		}
	}
	return nil
}

func cloneManifest(node *manifestNode) *manifestNode {
	cloned := &manifestNode{
		Name: node.Name,
		Attr: append([]xml.Attr(nil), node.Attr...),
		Text: node.Text,
	}
	cloned.Children = make([]*manifestNode, 0, len(node.Children))
	for _, child := range node.Children {
		cloned.Children = append(cloned.Children, cloneManifest(child))
	}
	return cloned
}

func sanitizeManifest(node *manifestNode) {
	attributes := node.Attr[:0]
	for _, attr := range node.Attr {
		if attr.Name.Space == "tools" {
			continue
		}
		attributes = append(attributes, attr)
	}
	node.Attr = attributes
	node.Text = strings.TrimSpace(node.Text)
	for _, child := range node.Children {
		sanitizeManifest(child)
	}
}

func applyToolsReplace(node *manifestNode, names string) {
	// Incoming values already win during the merge; this exists to validate the
	// rule and to keep the merge behavior explicit.
	for _, name := range strings.Split(names, ",") {
		_ = strings.TrimSpace(name)
	}
}

func removeManifestChild(parent *manifestNode, key string) {
	for index, child := range parent.Children {
		if manifestIdentity(child) == key {
			parent.Children = append(parent.Children[:index], parent.Children[index+1:]...)
			return
		}
	}
}

func removeManifestChildren(parent *manifestNode, key string) {
	result := parent.Children[:0]
	for _, child := range parent.Children {
		if manifestIdentity(child) == key {
			continue
		}
		result = append(result, child)
	}
	parent.Children = result
}

func findManifestChild(parent *manifestNode, key string) *manifestNode {
	for _, child := range parent.Children {
		if manifestIdentity(child) == key {
			return child
		}
	}
	return nil
}

func hasManifestChild(parent *manifestNode, key string) bool {
	for _, child := range parent.Children {
		if manifestIdentity(child) == key {
			return true
		}
	}
	return false
}

func manifestIdentity(node *manifestNode) string {
	name := node.Name.Local
	switch name {
	case "uses-permission", "permission", "uses-feature", "uses-library", "uses-package",
		"activity", "activity-alias", "service", "receiver", "provider", "meta-data":
		return name + ":" + attributeValue(node, "android", "name")
	case "application", "queries", "uses-sdk":
		return name
	default:
		return canonicalManifest(node)
	}
}

func canonicalManifest(node *manifestNode) string {
	var buffer bytes.Buffer
	writeManifestNode(&buffer, node, 0)
	return buffer.String()
}

func attributeValue(node *manifestNode, namespace, name string) string {
	for _, attr := range node.Attr {
		if attr.Name.Space == namespace && attr.Name.Local == name {
			return attr.Value
		}
	}
	return ""
}

func xmlName(space, local string) xml.Name {
	return xml.Name{Space: space, Local: local}
}

func xmlAttr(space, local, value string) xml.Attr {
	return xml.Attr{Name: xml.Name{Space: space, Local: local}, Value: value}
}

func attrKey(name xml.Name) string {
	if name.Space == "" {
		return name.Local
	}
	return name.Space + ":" + name.Local
}
