// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"strconv"
	"strings"
)

func pypiSpecifierSetContains(raw string, observed string) (bool, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return false, true
	}
	for _, specifier := range splitVersionSpecifierSet(raw) {
		if specifier == "" {
			return false, false
		}
		contains, ok := pypiSpecifierContains(specifier, observed)
		if !ok || !contains {
			return false, ok
		}
	}
	return true, true
}

func splitVersionSpecifierSet(raw string) []string {
	tokens := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t' || r == '\n'
	})
	out := make([]string, 0, len(tokens))
	for index := 0; index < len(tokens); index++ {
		token := strings.TrimSpace(tokens[index])
		if token == "" {
			continue
		}
		if versionComparatorTokenIsOperator(token) && index+1 < len(tokens) {
			next := strings.TrimSpace(tokens[index+1])
			if next != "" {
				out = append(out, token+next)
				index++
				continue
			}
		}
		out = append(out, token)
	}
	return out
}

func versionComparatorTokenIsOperator(token string) bool {
	switch token {
	case "~=", ">=", "<=", "==", "!=", ">", "<", "=":
		return true
	default:
		return false
	}
}

func pypiSpecifierContains(specifier string, observed string) (bool, bool) {
	operator, version := splitVersionComparator(specifier)
	if operator == "~=" {
		return pypiCompatibleReleaseContains(version, observed)
	}
	if strings.Contains(version, "+") && operator != "==" && operator != "!=" {
		return false, false
	}
	return comparatorConstraintContains(specifier, observed, comparePyPIVersion)
}

func pypiCompatibleReleaseContains(version string, observed string) (bool, bool) {
	parsed, ok := parsePyPIVersion(version)
	if !ok || len(parsed.release) < 2 {
		return false, false
	}
	if atLeast, valid := pypiAtLeast(observed, version); !valid || !atLeast {
		return false, valid
	}
	upper := pypiCompatibleUpperBound(parsed)
	lessThanUpper, valid := pypiLessThan(observed, upper)
	if !valid {
		return false, false
	}
	return lessThanUpper, true
}

func pypiCompatibleUpperBound(version pypiVersion) string {
	upper := append([]int(nil), version.release...)
	switch len(upper) {
	case 0:
		return ""
	case 1:
		upper[0]++
	default:
		index := len(upper) - 2
		if len(upper) == 2 {
			index = 0
		}
		upper[index]++
		for i := index + 1; i < len(upper); i++ {
			upper[i] = 0
		}
	}
	parts := make([]string, len(upper))
	for i, part := range upper {
		parts[i] = strconv.Itoa(part)
	}
	out := strings.Join(parts, ".")
	if version.epoch > 0 {
		return strconv.Itoa(version.epoch) + "!" + out
	}
	return out
}
