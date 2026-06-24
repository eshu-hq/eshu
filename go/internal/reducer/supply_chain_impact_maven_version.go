// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import "strings"

func mavenRangeContains(raw string, observed string) (bool, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return false, true
	}
	if strings.HasPrefix(raw, "[") || strings.HasPrefix(raw, "(") {
		return mavenBracketRangeContains(raw, observed)
	}
	return comparatorRangeContains(raw, observed, compareMavenVersion)
}

func mavenBracketRangeContains(raw string, observed string) (bool, bool) {
	malformed := false
	for _, branch := range splitMavenBracketRangeBranches(raw) {
		branch = strings.TrimSpace(branch)
		if branch == "" {
			continue
		}
		ok, valid := mavenBracketBranchContains(branch, observed)
		if ok {
			return true, true
		}
		if !valid {
			malformed = true
		}
	}
	return false, !malformed
}

func splitMavenBracketRangeBranches(raw string) []string {
	var branches []string
	start := 0
	depth := 0
	for i, r := range raw {
		switch r {
		case '[', '(':
			depth++
		case ']', ')':
			if depth > 0 {
				depth--
			}
		case ',':
			if depth == 0 {
				branches = append(branches, raw[start:i])
				start = i + 1
			}
		}
	}
	branches = append(branches, raw[start:])
	return branches
}

func mavenBracketBranchContains(branch string, observed string) (bool, bool) {
	if len(branch) < 2 {
		return false, false
	}
	lowerInclusive := branch[0] == '['
	upperInclusive := branch[len(branch)-1] == ']'
	if !lowerInclusive && branch[0] != '(' {
		return false, false
	}
	if !upperInclusive && branch[len(branch)-1] != ')' {
		return false, false
	}
	inner := strings.TrimSpace(branch[1 : len(branch)-1])
	if !strings.Contains(inner, ",") {
		cmp, valid := compareMavenVersion(observed, inner)
		return valid && lowerInclusive && upperInclusive && cmp == 0, valid
	}
	lower, upper, _ := strings.Cut(inner, ",")
	if ok, valid := mavenBoundAllows(observed, strings.TrimSpace(lower), lowerInclusive, true); !valid {
		return false, false
	} else if !ok {
		return false, true
	}
	if ok, valid := mavenBoundAllows(observed, strings.TrimSpace(upper), upperInclusive, false); !valid {
		return false, false
	} else if !ok {
		return false, true
	}
	return true, true
}

func mavenBoundAllows(observed string, bound string, inclusive bool, lower bool) (bool, bool) {
	if bound == "" {
		return true, true
	}
	cmp, valid := compareMavenVersion(observed, bound)
	if !valid {
		return false, false
	}
	if lower {
		return cmp > 0 || (inclusive && cmp == 0), true
	}
	return cmp < 0 || (inclusive && cmp == 0), true
}

type mavenVersionToken struct {
	value   string
	numeric bool
}

func validMavenVersion(raw string) bool {
	_, ok := mavenVersionTokens(raw)
	return ok
}

func compareMavenVersion(left string, right string) (int, bool) {
	leftTokens, ok := mavenVersionTokens(left)
	if !ok {
		return 0, false
	}
	rightTokens, ok := mavenVersionTokens(right)
	if !ok {
		return 0, false
	}
	maxLen := len(leftTokens)
	if len(rightTokens) > maxLen {
		maxLen = len(rightTokens)
	}
	for i := 0; i < maxLen; i++ {
		cmp := compareMavenToken(mavenTokenAt(leftTokens, i), mavenTokenAt(rightTokens, i))
		if cmp != 0 {
			return cmp, true
		}
	}
	return 0, true
}

func mavenVersionTokens(raw string) ([]mavenVersionToken, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.ContainsAny(raw, " <>^~=|,") {
		return nil, false
	}
	var tokens []mavenVersionToken
	var current strings.Builder
	currentNumeric := false
	hasToken := false
	hasDigit := false
	flush := func() {
		if !hasToken {
			return
		}
		tokens = append(tokens, mavenVersionToken{
			value:   strings.ToLower(current.String()),
			numeric: currentNumeric,
		})
		current.Reset()
		hasToken = false
	}
	for _, r := range raw {
		switch {
		case r == '.' || r == '-' || r == '_':
			flush()
		case r >= '0' && r <= '9':
			hasDigit = true
			if hasToken && !currentNumeric {
				flush()
			}
			currentNumeric = true
			hasToken = true
			current.WriteRune(r)
		case (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z'):
			if hasToken && currentNumeric {
				flush()
			}
			currentNumeric = false
			hasToken = true
			current.WriteRune(r)
		default:
			return nil, false
		}
	}
	flush()
	if !hasDigit {
		return nil, false
	}
	tokens = trimMavenZeroPaddingBeforeQualifier(tokens)
	return trimTrailingMavenNulls(tokens), true
}

func trimMavenZeroPaddingBeforeQualifier(tokens []mavenVersionToken) []mavenVersionToken {
	qualifierIndex := -1
	for i, token := range tokens {
		if !token.numeric {
			qualifierIndex = i
			break
		}
	}
	if qualifierIndex <= 0 {
		return tokens
	}
	out := append([]mavenVersionToken(nil), tokens...)
	for qualifierIndex > 0 {
		previous := out[qualifierIndex-1]
		if !previous.numeric || compareNumericToken(previous.value, "0") != 0 {
			break
		}
		out = append(out[:qualifierIndex-1], out[qualifierIndex:]...)
		qualifierIndex--
	}
	return out
}

func trimTrailingMavenNulls(tokens []mavenVersionToken) []mavenVersionToken {
	for len(tokens) > 0 && mavenTokenIsNull(tokens[len(tokens)-1]) {
		tokens = tokens[:len(tokens)-1]
	}
	if len(tokens) == 0 {
		return []mavenVersionToken{{value: "0", numeric: true}}
	}
	return tokens
}

func mavenTokenAt(tokens []mavenVersionToken, index int) mavenVersionToken {
	if index >= len(tokens) {
		return mavenVersionToken{}
	}
	return tokens[index]
}

func compareMavenToken(left mavenVersionToken, right mavenVersionToken) int {
	if left.numeric && right.numeric {
		return compareNumericToken(left.value, right.value)
	}
	if !left.numeric && !right.numeric {
		return compareQualifierToken(left.value, right.value)
	}
	if !left.numeric {
		return -1
	}
	return 1
}

func compareNumericToken(left string, right string) int {
	left = strings.TrimLeft(left, "0")
	right = strings.TrimLeft(right, "0")
	if left == "" {
		left = "0"
	}
	if right == "" {
		right = "0"
	}
	if len(left) < len(right) {
		return -1
	}
	if len(left) > len(right) {
		return 1
	}
	return strings.Compare(left, right)
}

func compareQualifierToken(left string, right string) int {
	leftRank, leftKnown := mavenQualifierRank(left)
	rightRank, rightKnown := mavenQualifierRank(right)
	if leftKnown || rightKnown {
		if leftRank < rightRank {
			return -1
		}
		if leftRank > rightRank {
			return 1
		}
		return 0
	}
	return strings.Compare(left, right)
}

func mavenTokenIsNull(token mavenVersionToken) bool {
	if token.numeric {
		return compareNumericToken(token.value, "0") == 0
	}
	_, known := mavenQualifierRank(token.value)
	return known && mavenQualifierCanonical(token.value) == ""
}

func mavenQualifierRank(value string) (int, bool) {
	switch mavenQualifierCanonical(value) {
	case "alpha":
		return 1, true
	case "beta":
		return 2, true
	case "milestone":
		return 3, true
	case "rc":
		return 4, true
	case "snapshot":
		return 5, true
	case "":
		return 6, true
	case "sp":
		return 7, true
	default:
		return 8, false
	}
}

func mavenQualifierCanonical(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "a", "alpha":
		return "alpha"
	case "b", "beta":
		return "beta"
	case "m", "milestone":
		return "milestone"
	case "rc", "cr":
		return "rc"
	case "snapshot":
		return "snapshot"
	case "", "final", "ga", "release":
		return ""
	case "sp":
		return "sp"
	default:
		return value
	}
}
