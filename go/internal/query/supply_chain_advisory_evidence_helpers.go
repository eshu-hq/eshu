// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"sort"
	"strings"
)

func anyMapSliceVal(payload map[string]any, key string) []map[string]any {
	raw, ok := payload[key].([]any)
	if !ok || len(raw) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		if row, ok := item.(map[string]any); ok {
			out = append(out, row)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func addSet(values map[string]struct{}, value string) {
	if trimmed := strings.TrimSpace(value); trimmed != "" {
		values[trimmed] = struct{}{}
	}
}

func setToSortedSlice(values map[string]struct{}) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	for value := range values {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func sortedStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	for _, value := range values {
		addSet(seen, value)
	}
	return setToSortedSlice(seen)
}

func sourceConfidenceLabel(values map[string]struct{}) string {
	switch len(values) {
	case 0:
		return ""
	case 1:
		for value := range values {
			return value
		}
	}
	return "mixed"
}

func sortAdvisoryEvidence(row *AdvisoryEvidenceRow) {
	sort.Slice(row.Sources, func(i, j int) bool {
		if row.Sources[i].Source == row.Sources[j].Source {
			return row.Sources[i].AdvisoryID < row.Sources[j].AdvisoryID
		}
		return row.Sources[i].Source < row.Sources[j].Source
	})
	sort.Slice(row.AffectedPackages, func(i, j int) bool {
		if row.AffectedPackages[i].PackageID == row.AffectedPackages[j].PackageID {
			return row.AffectedPackages[i].Source < row.AffectedPackages[j].Source
		}
		return row.AffectedPackages[i].PackageID < row.AffectedPackages[j].PackageID
	})
	sort.Slice(row.AffectedProducts, func(i, j int) bool {
		return row.AffectedProducts[i].MatchCriteriaID < row.AffectedProducts[j].MatchCriteriaID
	})
	sort.Slice(row.EPSS, func(i, j int) bool {
		return row.EPSS[i].ScoreDate < row.EPSS[j].ScoreDate
	})
	sort.Slice(row.KEV, func(i, j int) bool {
		return row.KEV[i].DateAdded < row.KEV[j].DateAdded
	})
	sort.Slice(row.References, func(i, j int) bool {
		return row.References[i].URL < row.References[j].URL
	})
}
