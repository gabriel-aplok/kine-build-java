package minecraft

import (
	"bufio"
	"bytes"
	"fmt"
	"strings"
)

// Mappings is the validated header of a Tiny v2 mappings file.
type Mappings struct {
	Namespaces []string
}

const maxMappingLine = 1 << 20

// ParseMappings validates a Tiny v2 mappings file and returns its namespaces.
func ParseMappings(data []byte) (Mappings, error) {
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 64<<10), maxMappingLine)

	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return Mappings{}, fmt.Errorf("read mappings: %w", err)
		}
		return Mappings{}, fmt.Errorf("mappings are empty")
	}
	header := strings.Split(scanner.Text(), "\t")
	if len(header) < 4 || header[0] != "tiny" || header[1] != "2" || header[2] != "0" {
		return Mappings{}, fmt.Errorf("line 1: expected a tiny 2 0 header")
	}

	namespaces := header[3:]
	seen := make(map[string]struct{}, len(namespaces))
	for _, namespace := range namespaces {
		if namespace == "" {
			return Mappings{}, fmt.Errorf("line 1: namespace is empty")
		}
		if _, ok := seen[namespace]; ok {
			return Mappings{}, fmt.Errorf("line 1: duplicate namespace %q", namespace)
		}
		seen[namespace] = struct{}{}
	}

	lineNumber := 1
	for scanner.Scan() {
		lineNumber++
		line := scanner.Text()
		if line == "" {
			continue
		}
		if line[0] == '\t' {
			fields := strings.Split(strings.TrimLeft(line, "\t"), "\t")
			switch fields[0] {
			case "m", "f", "c", "p":
			default:
				return Mappings{}, fmt.Errorf("line %d: unknown member type %q", lineNumber, fields[0])
			}
			if len(fields) < 2 {
				return Mappings{}, fmt.Errorf("line %d: member has no names", lineNumber)
			}
			continue
		}

		fields := strings.Split(line, "\t")
		if len(fields) != len(namespaces)+1 || fields[0] != "c" {
			return Mappings{}, fmt.Errorf("line %d: expected a class line with %d names", lineNumber, len(namespaces))
		}
	}
	if err := scanner.Err(); err != nil {
		return Mappings{}, fmt.Errorf("read mappings: %w", err)
	}
	return Mappings{Namespaces: namespaces}, nil
}
