// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import "strings"

func pypiVersionMajor(raw string) (int, bool) {
	parsed, ok := parsePyPIVersion(raw)
	if !ok || len(parsed.release) == 0 {
		return 0, false
	}
	return parsed.release[0], true
}

func mavenVersionMajor(raw string) (int, bool) {
	tokens, ok := mavenVersionTokens(raw)
	if !ok {
		return 0, false
	}
	for _, token := range tokens {
		if token.numeric {
			major, ok := atoiNonNegative(token.value)
			return major, ok
		}
	}
	return 0, false
}

func nugetVersionMajor(raw string) (int, bool) {
	parsed, ok := parseNuGetVersion(raw)
	if !ok {
		return 0, false
	}
	return parsed.numeric[0], true
}

func composerVersionMajor(raw string) (int, bool) {
	parts, ok := composerNumericParts(raw)
	if !ok || len(parts) == 0 {
		return 0, false
	}
	return parts[0], true
}

func rubyGemsVersionMajor(raw string) (int, bool) {
	segments, ok := parseRubyGemsVersion(raw)
	if !ok {
		return 0, false
	}
	for _, segment := range segments {
		if segment.numeric {
			major, ok := atoiNonNegative(segment.text)
			return major, ok
		}
	}
	return 0, false
}

func evaluateComparatorManifestAllowsFix(
	manifestRange string,
	candidate string,
	contains func(string, string) (bool, bool),
) (string, string) {
	manifestRange = strings.TrimSpace(manifestRange)
	candidate = strings.TrimSpace(candidate)
	if candidate == "" {
		return SupplyChainRemediationManifestUnknown, SupplyChainRemediationMissingFixedVersion
	}
	if manifestRange == "" {
		return SupplyChainRemediationManifestUnknown, SupplyChainRemediationMissingManifestRange
	}
	allows, valid := contains(manifestRange, candidate)
	if !valid {
		return SupplyChainRemediationManifestUnknown, SupplyChainRemediationMissingManifestRangeMalformed
	}
	if allows {
		return SupplyChainRemediationManifestAllowed, ""
	}
	return SupplyChainRemediationManifestBlocked, ""
}

func evaluateGoModuleManifestAllowsFix(manifestRange string, candidate string) (string, string) {
	manifestRange = strings.TrimSpace(manifestRange)
	candidate = strings.TrimSpace(candidate)
	if candidate == "" {
		return SupplyChainRemediationManifestUnknown, SupplyChainRemediationMissingFixedVersion
	}
	if manifestRange == "" {
		return SupplyChainRemediationManifestUnknown, SupplyChainRemediationMissingManifestRange
	}
	if nonVersionDependencyPrefix(strings.ToLower(manifestRange)) {
		return SupplyChainRemediationManifestUnknown, SupplyChainRemediationMissingManifestRangeMalformed
	}
	if validSupplyChainSemver(manifestRange) {
		cmp, valid := compareOSVSemver(candidate, manifestRange)
		if !valid {
			return SupplyChainRemediationManifestUnknown, SupplyChainRemediationMissingManifestRangeMalformed
		}
		if cmp == 0 {
			return SupplyChainRemediationManifestAllowed, ""
		}
		return SupplyChainRemediationManifestBlocked, ""
	}
	return evaluateComparatorManifestAllowsFix(manifestRange, candidate, goModuleComparatorRangeContains)
}

func evaluatePyPIManifestAllowsFix(manifestRange string, candidate string) (string, string) {
	return evaluateComparatorManifestAllowsFix(manifestRange, candidate, pypiSpecifierSetContains)
}

func evaluateMavenManifestAllowsFix(manifestRange string, candidate string) (string, string) {
	return evaluateComparatorManifestAllowsFix(manifestRange, candidate, mavenRangeContains)
}

func evaluateNuGetManifestAllowsFix(manifestRange string, candidate string) (string, string) {
	return evaluateComparatorManifestAllowsFix(manifestRange, candidate, nugetAffectedRangeRawContains)
}

func evaluateComposerManifestAllowsFix(manifestRange string, candidate string) (string, string) {
	return evaluateComparatorManifestAllowsFix(manifestRange, candidate, composerConstraintContains)
}

func evaluateRubyGemsManifestAllowsFix(manifestRange string, candidate string) (string, string) {
	return evaluateComparatorManifestAllowsFix(manifestRange, candidate, rubyGemsRequirementContains)
}

func evaluateCargoManifestAllowsFix(manifestRange string, candidate string) (string, string) {
	manifestRange = strings.TrimSpace(manifestRange)
	candidate = strings.TrimSpace(candidate)
	if candidate == "" {
		return SupplyChainRemediationManifestUnknown, SupplyChainRemediationMissingFixedVersion
	}
	if manifestRange == "" {
		return SupplyChainRemediationManifestUnknown, SupplyChainRemediationMissingManifestRange
	}
	expanded, ok := expandCargoRange(manifestRange)
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

func expandCargoRange(rangeExpr string) (string, bool) {
	rangeExpr = strings.TrimSpace(rangeExpr)
	if rangeExpr == "" {
		return "", false
	}
	lower := strings.ToLower(rangeExpr)
	if lower == "*" || lower == "x" {
		return ">=0.0.0", true
	}
	if lower == "latest" || nonVersionDependencyPrefix(lower) {
		return "", false
	}
	branches := strings.Split(rangeExpr, "||")
	expanded := make([]string, 0, len(branches))
	for _, branch := range branches {
		out, ok := expandCargoBranch(strings.TrimSpace(branch))
		if !ok {
			return "", false
		}
		expanded = append(expanded, out)
	}
	return strings.Join(expanded, " || "), true
}

func expandCargoBranch(branch string) (string, bool) {
	if branch == "" {
		return "", false
	}
	fields := strings.Fields(strings.ReplaceAll(branch, ",", " "))
	if len(fields) == 0 {
		return "", false
	}
	out := make([]string, 0, len(fields)*2)
	for _, field := range fields {
		expanded, ok := expandCargoComparator(field)
		if !ok {
			return "", false
		}
		out = append(out, expanded...)
	}
	return strings.Join(out, " "), true
}

func expandCargoComparator(token string) ([]string, bool) {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, false
	}
	switch {
	case strings.HasPrefix(token, "^"):
		return expandCaret(strings.TrimSpace(strings.TrimPrefix(token, "^")))
	case strings.HasPrefix(token, "~"):
		return expandTilde(strings.TrimSpace(strings.TrimPrefix(token, "~")))
	case strings.HasPrefix(token, ">="),
		strings.HasPrefix(token, "<="),
		strings.HasPrefix(token, "=="),
		strings.HasPrefix(token, "!="):
		return []string{normalizeComparator(token, 2)}, true
	case strings.HasPrefix(token, ">"),
		strings.HasPrefix(token, "<"):
		return []string{normalizeComparator(token, 1)}, true
	case strings.HasPrefix(token, "="):
		return []string{"=" + strings.TrimSpace(strings.TrimPrefix(token, "="))}, true
	default:
		if _, ok := normalizeOSVSemver(token); !ok {
			return nil, false
		}
		return expandCaret(token)
	}
}
