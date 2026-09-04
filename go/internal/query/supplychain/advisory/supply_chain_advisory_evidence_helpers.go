// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package advisory

import (
	"fmt"
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

// SetToSortedSlice copies a set into a sorted slice, or nil when empty.
// Exported for the staying root work-item evidence state helper, which
// shares the rendering.
func SetToSortedSlice(values map[string]struct{}) []string {
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
	return SetToSortedSlice(seen)
}

// mapVal extracts a trimmed, non-empty map payload value. Copied from root
// package query's security_alert_reconciliation.go: the advisory model reads
// cvss_metrics and parsed_affected_range through it, and an unexported root
// symbol cannot be called across the package boundary. The names stay root's
// because they are neutral; only the location changes with the #6060 move.
func mapVal(payload map[string]any, key string) map[string]any {
	raw, ok := payload[key].(map[string]any)
	if !ok || len(raw) == 0 {
		return nil
	}
	out := make(map[string]any, len(raw))
	for key, value := range raw {
		if strings.TrimSpace(key) != "" && value != nil {
			out[key] = value
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// stringMapSliceVal extracts a sanitized []map[string]string payload value.
// Copied from root package query's security_alert_reconciliation.go for the
// same reason as mapVal: the advisory model reads severity through it.
func stringMapSliceVal(payload map[string]any, key string) []map[string]string {
	items, ok := payload[key].([]any)
	if !ok || len(items) == 0 {
		return nil
	}
	out := make([]map[string]string, 0, len(items))
	for _, item := range items {
		raw, ok := item.(map[string]any)
		if !ok {
			continue
		}
		row := make(map[string]string, len(raw))
		for key, value := range raw {
			text := strings.TrimSpace(fmt.Sprint(value))
			if strings.TrimSpace(key) != "" && text != "" && text != "<nil>" {
				row[key] = text
			}
		}
		if len(row) > 0 {
			out = append(out, row)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// derefString returns the value a *string points at, or "" when it is nil.
// Copied from root package query's workItemDerefString
// (factschema_decode_workitem.go): many root decode files call it, so the
// #6060 family move cannot take it, and an unexported root symbol cannot be
// called across a package boundary. Named for what it does here rather than
// the root file it came from: nothing in this package is work-item-shaped
// (same rationale as packagereg's derefString).
func derefString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

// derefFloat64 returns the value a *float64 points at, or 0 when it is nil,
// matching the pre-typing floatVal(0) behavior for a field this migration
// converts from a raw payload lookup to a typed pointer. Copied from the
// former root package query helper of the same shape (deleted when the
// advisory family moved in the #6060 lane-A PR1) for the same reason as
// derefString.
func derefFloat64(value *float64) float64 {
	if value == nil {
		return 0
	}
	return *value
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
