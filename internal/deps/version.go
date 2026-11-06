package deps

import "strings"

// CompareVersions compares two Maven-style versions.
// It returns -1 when a is older, 0 when equal, and 1 when a is newer.
func CompareVersions(a, b string) int {
	aTokens := versionTokens(a)
	bTokens := versionTokens(b)

	length := len(aTokens)
	if len(bTokens) > length {
		length = len(bTokens)
	}

	for i := 0; i < length; i++ {
		aToken := tokenAt(aTokens, i)
		bToken := tokenAt(bTokens, i)
		aNumeric := aToken != "" && isNumericToken(aToken)
		bNumeric := bToken != "" && isNumericToken(bToken)

		switch {
		case aToken == "" && bNumeric:
			if compareNumeric("0", bToken) == 0 {
				continue
			}
			return -1
		case bToken == "" && aNumeric:
			if compareNumeric(aToken, "0") == 0 {
				continue
			}
			return 1
		case aToken == "":
			return compareQualifier("", bToken)
		case bToken == "":
			return compareQualifier(aToken, "")
		case aNumeric && bNumeric:
			if cmp := compareNumeric(aToken, bToken); cmp != 0 {
				return cmp
			}
		case aNumeric:
			return 1
		case bNumeric:
			return -1
		default:
			if cmp := compareQualifier(aToken, bToken); cmp != 0 {
				return cmp
			}
		}
	}
	return 0
}

func versionTokens(value string) []string {
	var tokens []string
	for _, part := range strings.FieldsFunc(value, func(r rune) bool {
		return r == '.' || r == '-' || r == '_' || r == '+'
	}) {
		start := 0
		for i := 1; i <= len(part); i++ {
			if i == len(part) || isDigit(part[i]) != isDigit(part[i-1]) {
				tokens = append(tokens, part[start:i])
				start = i
			}
		}
	}

	for len(tokens) > 0 {
		last := tokens[len(tokens)-1]
		if !isNumericToken(last) || compareNumeric(last, "0") != 0 {
			break
		}
		tokens = tokens[:len(tokens)-1]
	}
	return tokens
}

func tokenAt(tokens []string, index int) string {
	if index >= len(tokens) {
		return ""
	}
	return tokens[index]
}

func isDigit(value byte) bool {
	return value >= '0' && value <= '9'
}

func isNumericToken(value string) bool {
	if value == "" {
		return false
	}
	for i := 0; i < len(value); i++ {
		if !isDigit(value[i]) {
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

func compareQualifier(a, b string) int {
	a = normalizeQualifier(a)
	b = normalizeQualifier(b)

	rankA := qualifierRank(a)
	rankB := qualifierRank(b)
	if rankA != rankB {
		if rankA < rankB {
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

func normalizeQualifier(value string) string {
	value = strings.ToLower(value)
	switch value {
	case "final", "ga", "release":
		return ""
	default:
		return value
	}
}

func qualifierRank(value string) int {
	switch value {
	case "alpha", "a":
		return 0
	case "beta", "b":
		return 1
	case "milestone", "m":
		return 2
	case "rc", "cr":
		return 3
	case "snapshot":
		return 4
	case "":
		return 6
	case "sp":
		return 7
	default:
		return 5
	}
}
