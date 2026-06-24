// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"strconv"
	"strings"
)

func composerConstraintContains(raw string, observed string) (bool, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" ||
		strings.Contains(strings.ToLower(raw), " as ") ||
		strings.HasPrefix(strings.ToLower(observed), "dev-") {
		return false, false
	}
	malformed := false
	for _, branch := range comparatorRangeBranches(raw) {
		branch = strings.TrimSpace(branch)
		if branch == "" || strings.Contains(strings.ToLower(branch), "dev-") {
			malformed = true
			continue
		}
		contains, ok := composerConstraintBranchContains(branch, observed)
		if contains {
			return true, true
		}
		if !ok {
			malformed = true
		}
	}
	return false, !malformed
}

func composerConstraintBranchContains(branch string, observed string) (bool, bool) {
	tokens := strings.Fields(strings.ReplaceAll(branch, ",", " "))
	if len(tokens) == 0 {
		return false, false
	}
	for _, token := range tokens {
		contains, ok := composerConstraintTokenContains(token, observed)
		if !ok || !contains {
			return false, ok
		}
	}
	return true, true
}

func composerConstraintTokenContains(token string, observed string) (bool, bool) {
	token = stripComposerStability(strings.TrimSpace(token))
	switch {
	case strings.HasPrefix(token, "^"):
		return composerCaretContains(strings.TrimPrefix(token, "^"), observed)
	case strings.HasPrefix(token, "~"):
		return composerTildeContains(strings.TrimPrefix(token, "~"), observed)
	case strings.ContainsAny(token, "*xX"):
		return composerWildcardContains(token, observed)
	default:
		return comparatorConstraintContains(token, observed, compareComposerVersion)
	}
}

func composerCaretContains(version string, observed string) (bool, bool) {
	lower, ok := normalizeComposerVersion(version)
	if !ok {
		return false, false
	}
	upper, ok := composerCaretUpperBound(version)
	if !ok {
		return false, false
	}
	return composerWithinBounds(observed, lower, upper)
}

func composerTildeContains(version string, observed string) (bool, bool) {
	lower, ok := normalizeComposerVersion(version)
	if !ok {
		return false, false
	}
	upper, ok := composerTildeUpperBound(version)
	if !ok {
		return false, false
	}
	return composerWithinBounds(observed, lower, upper)
}

func composerWildcardContains(token string, observed string) (bool, bool) {
	parts := strings.Split(stripComposerStability(token), ".")
	prefix := make([]int, 0, len(parts))
	for _, part := range parts {
		if part == "*" || part == "x" || part == "X" {
			break
		}
		value, err := strconv.Atoi(part)
		if err != nil {
			return false, false
		}
		prefix = append(prefix, value)
	}
	if len(prefix) == 0 {
		return true, true
	}
	lowerParts := append([]int(nil), prefix...)
	for len(lowerParts) < 3 {
		lowerParts = append(lowerParts, 0)
	}
	upperParts := append([]int(nil), lowerParts...)
	index := len(prefix) - 1
	upperParts[index]++
	for i := index + 1; i < len(upperParts); i++ {
		upperParts[i] = 0
	}
	return composerWithinBounds(observed, composerVersionString(lowerParts), composerVersionString(upperParts))
}

func composerWithinBounds(observed string, lower string, upper string) (bool, bool) {
	if ok, valid := comparatorConstraintContains(">="+lower, observed, compareComposerVersion); !valid || !ok {
		return false, valid
	}
	if ok, valid := comparatorConstraintContains("<"+upper, observed, compareComposerVersion); !valid || !ok {
		return false, valid
	}
	return true, true
}

func composerCaretUpperBound(version string) (string, bool) {
	parts, ok := composerNumericParts(version)
	if !ok {
		return "", false
	}
	index := 0
	for index < len(parts)-1 && parts[index] == 0 {
		index++
	}
	parts[index]++
	for i := index + 1; i < len(parts); i++ {
		parts[i] = 0
	}
	return composerVersionString(parts), true
}

func composerTildeUpperBound(version string) (string, bool) {
	parts, ok := composerNumericParts(version)
	if !ok {
		return "", false
	}
	index := 0
	if len(strings.Split(strings.SplitN(version, "-", 2)[0], ".")) >= 3 {
		index = 1
	}
	parts[index]++
	for i := index + 1; i < len(parts); i++ {
		parts[i] = 0
	}
	return composerVersionString(parts), true
}

func composerNumericParts(raw string) ([]int, bool) {
	core := strings.TrimPrefix(strings.TrimPrefix(strings.SplitN(stripComposerStability(raw), "-", 2)[0], "v"), "V")
	rawParts := strings.Split(core, ".")
	if len(rawParts) == 0 || len(rawParts) > 3 {
		return nil, false
	}
	parts := make([]int, 0, 3)
	for _, part := range rawParts {
		value, err := strconv.Atoi(part)
		if err != nil {
			return nil, false
		}
		parts = append(parts, value)
	}
	for len(parts) < 3 {
		parts = append(parts, 0)
	}
	return parts, true
}

func composerVersionString(parts []int) string {
	out := make([]string, len(parts))
	for i, part := range parts {
		out[i] = strconv.Itoa(part)
	}
	return strings.Join(out, ".")
}

func stripComposerStability(raw string) string {
	if before, _, ok := strings.Cut(raw, "@"); ok {
		return strings.TrimSpace(before)
	}
	return strings.TrimSpace(raw)
}
