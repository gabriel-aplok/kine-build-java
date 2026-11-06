package toolchain

import (
	"sort"
	"strconv"
	"strings"
)

func majorVersion(version string) int {
	parts := strings.Split(version, ".")
	if len(parts) == 0 {
		return 0
	}
	first := leadingInt(parts[0])
	if first == 1 && len(parts) > 1 {
		return leadingInt(parts[1])
	}
	return first
}

func leadingInt(value string) int {
	end := 0
	for end < len(value) && value[end] >= '0' && value[end] <= '9' {
		end++
	}
	if end == 0 {
		return 0
	}
	number, err := strconv.Atoi(value[:end])
	if err != nil {
		return 0
	}
	return number
}

func compareJDKVersions(a, b string) int {
	aParts := versionParts(a)
	bParts := versionParts(b)
	length := len(aParts)
	if len(bParts) > length {
		length = len(bParts)
	}

	for i := 0; i < length; i++ {
		aPart := partAt(aParts, i)
		bPart := partAt(bParts, i)
		aNumeric := aPart != "" && isNumeric(aPart)
		bNumeric := bPart != "" && isNumeric(bPart)

		switch {
		case aPart == "" && bNumeric:
			if compareNumeric("0", bPart) == 0 {
				continue
			}
			return -1
		case bPart == "" && aNumeric:
			if compareNumeric(aPart, "0") == 0 {
				continue
			}
			return 1
		case aPart == "":
			return -1
		case bPart == "":
			return 1
		case aNumeric && bNumeric:
			if cmp := compareNumeric(aPart, bPart); cmp != 0 {
				return cmp
			}
		case aNumeric:
			return 1
		case bNumeric:
			return -1
		case aPart < bPart:
			return -1
		case aPart > bPart:
			return 1
		}
	}
	return 0
}

func versionParts(value string) []string {
	return strings.FieldsFunc(value, func(r rune) bool {
		return r == '.' || r == '_' || r == '-' || r == '+'
	})
}

func partAt(parts []string, index int) string {
	if index >= len(parts) {
		return ""
	}
	return parts[index]
}

func isNumeric(value string) bool {
	if value == "" {
		return false
	}
	for i := 0; i < len(value); i++ {
		if value[i] < '0' || value[i] > '9' {
			return false
		}
	}
	return true
}

func compareNumeric(a, b string) int {
	a = strings.TrimLeft(a, "0")
	b = strings.TrimLeft(b, "0")
	if a == "" {
		a = "0"
	}
	if b == "" {
		b = "0"
	}
	if len(a) != len(b) {
		if len(a) < len(b) {
			return -1
		}
		return 1
	}
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}

func sortJDKs(jdks []JDK) {
	sort.Slice(jdks, func(i, j int) bool {
		if jdks[i].Major != jdks[j].Major {
			return jdks[i].Major > jdks[j].Major
		}
		if cmp := compareJDKVersions(jdks[i].Version, jdks[j].Version); cmp != 0 {
			return cmp > 0
		}
		return jdks[i].Home < jdks[j].Home
	})
}
