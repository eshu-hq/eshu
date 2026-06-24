// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"sort"
	"strings"
)

// selectFirstPatchedVersion picks the lowest source-reported fixed version
// Eshu can defend for one ecosystem. When fixed-version branches span
// multiple majors and an installed version is known, the selector keeps
// branches inside the observed major so the caller is not forced into a major
// bump unless the only fix lives across a major boundary. The returned
// branchCount is the number of unique parseable fixed versions Eshu observed
// across all sources, which lets the remediation layer detect "multiple
// patched branches" without redoing the parse.
func selectFirstPatchedVersion(
	observed string,
	primaryFixed string,
	branches []FixedVersionBranch,
	ops remediationVersionOperations,
) (string, int, bool) {
	uniqueVersions := uniqueParseableVersions(branches, primaryFixed, ops.valid)
	branchCount := len(uniqueVersions)
	if branchCount == 0 {
		return "", 0, false
	}
	if ops.major != nil {
		observedMajor, observedValid := ops.major(observed)
		if observedValid {
			sameMajor := versionsInMajor(uniqueVersions, observedMajor, ops.major)
			if len(sameMajor) > 0 {
				return lowestVersion(sameMajor, ops.compare), branchCount, true
			}
		}
	}
	return lowestVersion(uniqueVersions, ops.compare), branchCount, true
}

func uniqueParseableVersions(
	branches []FixedVersionBranch,
	primary string,
	valid func(string) bool,
) []string {
	seen := make(map[string]struct{})
	out := make([]string, 0, len(branches)+1)
	add := func(raw string) {
		raw = strings.TrimSpace(raw)
		if raw == "" || !valid(raw) {
			return
		}
		if _, dup := seen[raw]; dup {
			return
		}
		seen[raw] = struct{}{}
		out = append(out, raw)
	}
	add(primary)
	for _, branch := range branches {
		add(branch.Version)
	}
	return out
}

func semverMajor(raw string) (int, bool) {
	normalized, ok := normalizeOSVSemver(raw)
	if !ok {
		return 0, false
	}
	trimmed := strings.TrimPrefix(normalized, "v")
	major, _, _, ok := parseSemverParts(trimmed)
	if !ok {
		return 0, false
	}
	return major, true
}

func versionsInMajor(
	versions []string,
	major int,
	majorFunc func(string) (int, bool),
) []string {
	out := make([]string, 0, len(versions))
	for _, version := range versions {
		m, ok := majorFunc(version)
		if !ok {
			continue
		}
		if m == major {
			out = append(out, version)
		}
	}
	return out
}

func lowestVersion(versions []string, compare versionCompareFunc) string {
	if len(versions) == 0 {
		return ""
	}
	sorted := append([]string(nil), versions...)
	sort.SliceStable(sorted, func(i, j int) bool {
		cmp, ok := compare(sorted[i], sorted[j])
		if !ok {
			return sorted[i] < sorted[j]
		}
		return cmp < 0
	})
	return sorted[0]
}
