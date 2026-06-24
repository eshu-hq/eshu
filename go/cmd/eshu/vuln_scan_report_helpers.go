// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"fmt"
	"sort"
	"strings"
)

func evidenceHandlesFromFinding(finding map[string]any) []vulnScanEvidenceHandle {
	ids := stringSliceFromAny(finding["evidence_fact_ids"])
	handles := make([]vulnScanEvidenceHandle, 0, len(ids))
	for _, id := range ids {
		handles = append(handles, vulnScanEvidenceHandle{Kind: "fact", ID: id})
	}
	return handles
}

func remediationFromFinding(finding map[string]any) map[string]any {
	remediation, _ := finding["remediation"].(map[string]any)
	out := map[string]any{}
	for _, key := range []string{
		"ecosystem",
		"current_version",
		"vulnerable_range",
		"fixed_version_source",
		"match_reason",
		"first_patched_version",
		"manifest_range",
		"manifest_allows_fix",
		"parent_package",
		"confidence",
		"reason",
	} {
		if value := stringFromMap(remediation, key); value != "" {
			out[key] = value
		}
	}
	if direct, ok := remediation["direct"].(bool); ok {
		out["direct"] = direct
	}
	if missing := stringSliceFromAny(remediation["missing_evidence"]); len(missing) > 0 {
		out["missing_evidence"] = missing
	}
	if branches := mapSliceFromAny(remediation["patched_version_branches"]); len(branches) > 0 {
		out["patched_version_branches"] = branches
	}
	if fixed := stringFromMap(finding, "fixed_version"); fixed != "" {
		out["fixed_version"] = fixed
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func priorityFromFinding(finding map[string]any) *vulnScanReportPriorityContext {
	priority := vulnScanReportPriorityContext{
		Bucket:      stringFromMap(finding, "priority_bucket"),
		Score:       intFromAny(finding["priority_score"]),
		Reason:      stringFromMap(finding, "priority_reason"),
		ReasonCodes: stringSliceFromAny(finding["priority_reason_codes"]),
	}
	if priority.Bucket == "" && priority.Score == 0 && priority.Reason == "" && len(priority.ReasonCodes) == 0 {
		return nil
	}
	return &priority
}

func vulnScanFindingsByStatus(findings []map[string]any) map[string]int {
	counts := map[string]int{}
	for _, finding := range findings {
		status := stringFromMap(finding, "impact_status")
		if status == "" {
			status = "unknown"
		}
		counts[status]++
	}
	if len(counts) == 0 {
		return nil
	}
	return counts
}

func highestVulnScanPriority(findings []map[string]any) string {
	rank := map[string]int{"critical": 0, "high": 1, "medium": 2, "low": 3, "informational": 4}
	best := ""
	bestRank := len(rank) + 1
	for _, finding := range findings {
		bucket := stringFromMap(finding, "priority_bucket")
		if bucket == "" {
			continue
		}
		if currentRank, ok := rank[bucket]; ok && currentRank < bestRank {
			best = bucket
			bestRank = currentRank
		}
	}
	return best
}

func evidenceFactsTotal(counts map[string]any) int {
	if counts == nil {
		return 0
	}
	return intFromAny(counts["evidence_facts_total"])
}

func stringFromMap(values map[string]any, key string) string {
	if value, ok := values[key].(string); ok {
		return strings.TrimSpace(value)
	}
	return ""
}

func stringSliceFromAny(value any) []string {
	switch typed := value.(type) {
	case []string:
		return compactStrings(typed)
	case []any:
		values := make([]string, 0, len(typed))
		for _, item := range typed {
			if text, ok := item.(string); ok {
				values = append(values, text)
			}
		}
		return compactStrings(values)
	default:
		return nil
	}
}

func compactStrings(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			out = append(out, value)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func mergeStringLists(first []string, second []string) []string {
	if len(first) == 0 {
		return compactStrings(second)
	}
	if len(second) == 0 {
		return compactStrings(first)
	}
	seen := make(map[string]struct{}, len(first)+len(second))
	out := make([]string, 0, len(first)+len(second))
	for _, values := range [][]string{first, second} {
		for _, value := range values {
			value = strings.TrimSpace(value)
			if value == "" {
				continue
			}
			if _, ok := seen[value]; ok {
				continue
			}
			seen[value] = struct{}{}
			out = append(out, value)
		}
	}
	return out
}

func mapSliceFromAny(value any) []map[string]any {
	switch typed := value.(type) {
	case []map[string]any:
		return typed
	case []any:
		values := make([]map[string]any, 0, len(typed))
		for _, item := range typed {
			if entry, ok := item.(map[string]any); ok {
				values = append(values, entry)
			}
		}
		if len(values) == 0 {
			return nil
		}
		return values
	default:
		return nil
	}
}

func intFromAny(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case jsonNumber:
		n, err := typed.Int64()
		if err == nil {
			return int(n)
		}
	}
	return 0
}

type jsonNumber interface {
	Int64() (int64, error)
}

func boolPtrFromAny(value any) *bool {
	typed, ok := value.(bool)
	if !ok {
		return nil
	}
	return &typed
}

func unsupportedTargetSummaries(targets []map[string]any) []string {
	if len(targets) == 0 {
		return nil
	}
	summaries := make([]string, 0, len(targets))
	for _, target := range targets {
		kind := stringFromMap(target, "target_kind")
		reason := stringFromMap(target, "reason")
		count := intFromAny(target["count"])
		if kind == "" && reason == "" {
			continue
		}
		summaries = append(summaries, fmt.Sprintf("%s/%s count=%d", kind, reason, count))
	}
	sort.Strings(summaries)
	return summaries
}

func evidenceHandleIDs(handles []vulnScanEvidenceHandle) []string {
	ids := make([]string, 0, len(handles))
	for _, handle := range handles {
		if strings.TrimSpace(handle.ID) != "" {
			ids = append(ids, handle.ID)
		}
	}
	return ids
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
