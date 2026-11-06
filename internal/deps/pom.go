package deps

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"strings"
)

type pomProject struct {
	XMLName              xml.Name        `xml:"project"`
	Parent               pomParent       `xml:"parent"`
	GroupID              string          `xml:"groupId"`
	ArtifactID           string          `xml:"artifactId"`
	Version              string          `xml:"version"`
	Packaging            string          `xml:"packaging"`
	Properties           pomProperties   `xml:"properties"`
	DependencyManagement pomDependencies `xml:"dependencyManagement>dependencies"`
	Dependencies         pomDependencies `xml:"dependencies"`
}

type pomParent struct {
	GroupID    string `xml:"groupId"`
	ArtifactID string `xml:"artifactId"`
	Version    string `xml:"version"`
}

type pomProperties map[string]string

func (p *pomProperties) UnmarshalXML(decoder *xml.Decoder, start xml.StartElement) error {
	values := make(map[string]string)
	for {
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		switch element := token.(type) {
		case xml.StartElement:
			var value string
			if err := decoder.DecodeElement(&value, &element); err != nil {
				return err
			}
			values[element.Name.Local] = strings.TrimSpace(value)
		case xml.EndElement:
			if element.Name == start.Name {
				*p = values
				return nil
			}
		}
	}
}

type pomDependencies struct {
	Items []pomDependency `xml:"dependency"`
}

type pomDependency struct {
	GroupID    string         `xml:"groupId"`
	ArtifactID string         `xml:"artifactId"`
	Version    string         `xml:"version"`
	Type       string         `xml:"type"`
	Classifier string         `xml:"classifier"`
	Scope      string         `xml:"scope"`
	Optional   string         `xml:"optional"`
	Exclusions []pomExclusion `xml:"exclusions>exclusion"`
}

type pomExclusion struct {
	GroupID    string `xml:"groupId"`
	ArtifactID string `xml:"artifactId"`
}

type resolvedPOM struct {
	GroupID              string
	ArtifactID           string
	Version              string
	Packaging            string
	Properties           map[string]string
	DependencyManagement []pomDependency
	Dependencies         []pomDependency
}

func parsePOM(data []byte) (*pomProject, error) {
	var project pomProject
	decoder := xml.NewDecoder(bytes.NewReader(data))
	decoder.CharsetReader = pomCharsetReader
	if err := decoder.Decode(&project); err != nil {
		return nil, fmt.Errorf("parse POM: %w", err)
	}
	return &project, nil
}

func pomCharsetReader(charset string, input io.Reader) (io.Reader, error) {
	switch strings.ToLower(strings.TrimSpace(charset)) {
	case "utf-8", "utf8", "us-ascii", "ascii":
		return input, nil
	case "iso-8859-1", "iso8859-1", "latin1", "latin-1":
		return singleByteReader(input, nil)
	case "windows-1252", "cp1252":
		return singleByteReader(input, windows1252)
	default:
		return nil, fmt.Errorf("unsupported XML charset %q", charset)
	}
}

func singleByteReader(input io.Reader, table map[byte]rune) (io.Reader, error) {
	data, err := io.ReadAll(input)
	if err != nil {
		return nil, err
	}
	runes := make([]rune, len(data))
	for index, value := range data {
		if table != nil {
			if replacement, ok := table[value]; ok {
				runes[index] = replacement
				continue
			}
		}
		runes[index] = rune(value)
	}
	return strings.NewReader(string(runes)), nil
}

var windows1252 = map[byte]rune{
	0x80: '€', 0x82: '‚', 0x83: 'ƒ', 0x84: '„', 0x85: '…', 0x86: '†', 0x87: '‡',
	0x88: 'ˆ', 0x89: '‰', 0x8a: 'Š', 0x8b: '‹', 0x8c: 'Œ', 0x8e: 'Ž', 0x91: '‘',
	0x92: '’', 0x93: '“', 0x94: '”', 0x95: '•', 0x96: '–', 0x97: '—', 0x98: '˜',
	0x99: '™', 0x9a: 'š', 0x9b: '›', 0x9c: 'œ', 0x9e: 'ž', 0x9f: 'Ÿ',
}

func projectProperties(group, artifact, version string, properties map[string]string) map[string]string {
	merged := make(map[string]string, len(properties)+6)
	for key, value := range properties {
		merged[key] = value
	}
	merged["project.groupId"] = group
	merged["project.artifactId"] = artifact
	merged["project.version"] = version
	merged["pom.groupId"] = group
	merged["pom.artifactId"] = artifact
	merged["pom.version"] = version
	return merged
}

func interpolate(value string, properties map[string]string) string {
	for i := 0; i < 10; i++ {
		replaced := false
		for key, replacement := range properties {
			token := "${" + key + "}"
			if strings.Contains(value, token) {
				value = strings.ReplaceAll(value, token, replacement)
				replaced = true
			}
		}
		if !replaced {
			return value
		}
	}
	return value
}

func resolveDependency(raw pomDependency, properties map[string]string) pomDependency {
	resolved := pomDependency{
		GroupID:    interpolate(raw.GroupID, properties),
		ArtifactID: interpolate(raw.ArtifactID, properties),
		Version:    interpolate(raw.Version, properties),
		Type:       interpolate(raw.Type, properties),
		Classifier: interpolate(raw.Classifier, properties),
		Scope:      interpolate(raw.Scope, properties),
		Optional:   interpolate(raw.Optional, properties),
		Exclusions: make([]pomExclusion, 0, len(raw.Exclusions)),
	}
	for _, exclusion := range raw.Exclusions {
		resolved.Exclusions = append(resolved.Exclusions, pomExclusion{
			GroupID:    interpolate(exclusion.GroupID, properties),
			ArtifactID: interpolate(exclusion.ArtifactID, properties),
		})
	}
	return resolved
}

func resolveDependencies(raw []pomDependency, properties map[string]string) []pomDependency {
	resolved := make([]pomDependency, 0, len(raw))
	for _, dependency := range raw {
		resolved = append(resolved, resolveDependency(dependency, properties))
	}
	return resolved
}

func dependencyKey(dependency pomDependency) string {
	kind := dependency.Type
	if kind == "" {
		kind = "jar"
	}
	return dependency.GroupID + ":" + dependency.ArtifactID + ":" + kind + ":" + dependency.Classifier
}

func mergeDependencies(parent, child []pomDependency) []pomDependency {
	merged := make(map[string]pomDependency, len(parent)+len(child))
	order := make([]string, 0, len(parent)+len(child))
	for _, dependency := range parent {
		key := dependencyKey(dependency)
		if _, ok := merged[key]; !ok {
			order = append(order, key)
		}
		merged[key] = dependency
	}
	for _, dependency := range child {
		key := dependencyKey(dependency)
		if _, ok := merged[key]; !ok {
			order = append(order, key)
		}
		merged[key] = dependency
	}

	result := make([]pomDependency, 0, len(order))
	for _, key := range order {
		result = append(result, merged[key])
	}
	return result
}
