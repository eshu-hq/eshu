// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"fmt"
	"strconv"
	"strings"
)

// evaluateNPMManifestAllowsFix decides whether the npm manifest range admits
// the candidate patched version. Returns ("allowed"|"blocked"|"unknown",
// missingEvidenceCode). The reducer expands the npm-specific caret/tilde
// notation before delegating to the existing comparator engine so callers do
// not have to learn semver shorthand to read the answer.
func evaluateNPMManifestAllowsFix(manifestRange string, candidate string) (string, string) {
	manifestRange = strings.TrimSpace(manifestRange)
	candidate = strings.TrimSpace(candidate)
	if candidate == "" {
		return SupplyChainRemediationManifestUnknown, SupplyChainRemediationMissingFixedVersion
	}
	if manifestRange == "" {
		return SupplyChainRemediationManifestUnknown, SupplyChainRemediationMissingManifestRange
	}
	expanded, ok := expandNPMRange(manifestRange)
	if !ok {
		return SupplyChainRemediationManifestUnknown, SupplyChainRemediationMissingManifestRangeMalformed
	}
	allows, valid := comparatorRangeContains(expanded, candidate, compareOSVSemver)
	if !valid {
		return SupplyChainRemediationManifestUnknown, SupplyChainRemediationMissingManifestRangeMalformed
	}
	if allows {
		return SupplyChainRemediationManifestAllowed, ""
	}
	return SupplyChainRemediationManifestBlocked, ""
}

// expandNPMRange rewrites an npm manifest range expression into the comparator
// form the reducer's comparatorRangeContains engine understands.
func expandNPMRange(rangeExpr string) (string, bool) {
	rangeExpr = strings.TrimSpace(rangeExpr)
	if rangeExpr == "" {
		return "", false
	}
	lower := strings.ToLower(rangeExpr)
	if lower == "*" || lower == "x" || lower == "latest" {
		return ">=0.0.0", true
	}
	if nonVersionDependencyPrefix(lower) {
		return "", false
	}
	branches := strings.Split(rangeExpr, "||")
	expanded := make([]string, 0, len(branches))
	for _, branch := range branches {
		out, ok := expandNPMBranch(strings.TrimSpace(branch))
		if !ok {
			return "", false
		}
		expanded = append(expanded, out)
	}
	return strings.Join(expanded, " || "), true
}

func expandNPMBranch(branch string) (string, bool) {
	if branch == "" {
		return "", false
	}
	lower := strings.ToLower(branch)
	if lower == "*" || lower == "x" {
		return ">=0.0.0", true
	}
	if strings.Contains(branch, " - ") {
		return "", false
	}
	fields := strings.Fields(branch)
	out := make([]string, 0, len(fields)*2)
	for _, field := range fields {
		expanded, ok := expandNPMComparator(field)
		if !ok {
			return "", false
		}
		out = append(out, expanded...)
	}
	if len(out) == 0 {
		return "", false
	}
	return strings.Join(out, " "), true
}

func expandNPMComparator(token string) ([]string, bool) {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, false
	}
	switch {
	case strings.HasPrefix(token, "^"):
		base := strings.TrimSpace(strings.TrimPrefix(token, "^"))
		return expandCaret(base)
	case strings.HasPrefix(token, "~"):
		base := strings.TrimSpace(strings.TrimPrefix(token, "~"))
		return expandTilde(base)
	case strings.HasPrefix(token, ">="),
		strings.HasPrefix(token, "<="),
		strings.HasPrefix(token, "=="),
		strings.HasPrefix(token, "!="):
		return []string{normalizeComparator(token, 2)}, true
	case strings.HasPrefix(token, ">"),
		strings.HasPrefix(token, "<"),
		strings.HasPrefix(token, "="):
		return []string{normalizeComparator(token, 1)}, true
	default:
		if _, ok := normalizeOSVSemver(token); !ok {
			return nil, false
		}
		return []string{"=" + token}, true
	}
}

func normalizeComparator(token string, operatorLen int) string {
	if len(token) < operatorLen {
		return token
	}
	return token[:operatorLen] + strings.TrimSpace(token[operatorLen:])
}

func expandCaret(base string) ([]string, bool) {
	major, minor, patch, ok := parseSemverParts(base)
	if !ok {
		return nil, false
	}
	switch {
	case major > 0:
		return []string{">=" + base, "<" + fmt.Sprintf("%d.0.0", major+1)}, true
	case minor > 0:
		return []string{">=" + base, "<" + fmt.Sprintf("0.%d.0", minor+1)}, true
	default:
		return []string{">=" + base, "<" + fmt.Sprintf("0.0.%d", patch+1)}, true
	}
}

func expandTilde(base string) ([]string, bool) {
	major, minor, _, ok := parseSemverParts(base)
	if !ok {
		return nil, false
	}
	return []string{">=" + base, "<" + fmt.Sprintf("%d.%d.0", major, minor+1)}, true
}

// parseSemverParts returns the major/minor/patch ints from a semver string,
// stripping any pre-release or build metadata. Caret and tilde expansion need
// only the numeric majors; pre-release ordering stays delegated to
// compareOSVSemver downstream.
func parseSemverParts(raw string) (int, int, int, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, 0, 0, false
	}
	if idx := strings.IndexAny(raw, "-+"); idx >= 0 {
		raw = raw[:idx]
	}
	parts := strings.Split(raw, ".")
	if len(parts) < 1 || len(parts) > 3 {
		return 0, 0, 0, false
	}
	major, ok := atoiNonNegative(parts[0])
	if !ok {
		return 0, 0, 0, false
	}
	minor := 0
	patch := 0
	if len(parts) >= 2 {
		minor, ok = atoiNonNegative(parts[1])
		if !ok {
			return 0, 0, 0, false
		}
	}
	if len(parts) == 3 {
		patch, ok = atoiNonNegative(parts[2])
		if !ok {
			return 0, 0, 0, false
		}
	}
	return major, minor, patch, true
}

func atoiNonNegative(token string) (int, bool) {
	if token == "" {
		return 0, false
	}
	value, err := strconv.Atoi(token)
	if err != nil || value < 0 {
		return 0, false
	}
	return value, true
}
