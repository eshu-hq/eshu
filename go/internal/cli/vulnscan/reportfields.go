// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package vulnscan

import (
	"fmt"
	"sort"
	"strings"
)

// evidenceHandlesFromFinding turns the finding's evidence_fact_ids into
// typed handles an operator can fetch. Every id becomes a `fact` handle;
// there is no other handle kind today.
func evidenceHandlesFromFinding(finding map[string]any) []EvidenceHandle {
	ids := stringSliceFromAny(finding["evidence_fact_ids"])
	handles := make([]EvidenceHandle, 0, len(ids))
	for _, id := range ids {
		handles = append(handles, EvidenceHandle{Kind: "fact", ID: id})
	}
	return handles
}

// RemediationFromFinding reads the reducer's remediation block into the map
// the report publishes. It stays a map rather than a struct because the
// reducer owns the key set and the CLI passes unknown keys through; the fixed
// list below is what the CLI promises to preserve, and `fixed_version` is
// lifted from the finding when remediation does not carry its own.
//
// Both the JSON report and the SARIF export read it, which is why it is
// exported rather than private to the report builder.
func RemediationFromFinding(finding map[string]any) map[string]any {
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

// priorityFromFinding reads the finding's priority fields, returning nil when
// none are populated so the report omits the block rather than publishing an
// empty bucket that reads as a real verdict.
func priorityFromFinding(finding map[string]any) *ReportPriorityContext {
	priority := ReportPriorityContext{
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

// findingsByStatus counts findings per reducer impact status for the report
// summary. Findings with no status count as `unknown` rather than being
// dropped, so the per-status counts always add up to the total.
func findingsByStatus(findings []map[string]any) map[string]int {
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

// highestPriority returns the most severe priority bucket present across the
// findings, or an empty string when no finding carries one. Buckets the CLI
// does not know are ignored rather than ranked.
func highestPriority(findings []map[string]any) string {
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

// evidenceFactsTotal reads the readiness counts block's evidence fact total,
// which the report surfaces so an operator can tell an empty answer backed by
// evidence from one backed by nothing.
func evidenceFactsTotal(counts map[string]any) int {
	if counts == nil {
		return 0
	}
	return intFromAny(counts["evidence_facts_total"])
}

// stringFromMap reads a trimmed string value, returning "" when the key is
// absent or holds another type.
func stringFromMap(values map[string]any, key string) string {
	if value, ok := values[key].(string); ok {
		return strings.TrimSpace(value)
	}
	return ""
}

// stringSliceFromAny reads a string list out of a JSON-decoded value, dropping
// non-string members and blanks. It returns nil for an empty result so the
// enclosing field is omitted rather than published as an empty array.
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

// compactStrings trims each value and drops the blanks, returning nil rather
// than an empty slice.
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

// mergeStringLists concatenates two string lists, trimming blanks and dropping
// duplicates while preserving first-seen order. The report uses it to merge
// the server's missing evidence with the CLI's own scope-plan reasons without
// reporting the same reason twice.
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

// mapSliceFromAny reads a list of objects out of a JSON-decoded value,
// dropping members that are not objects.
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

// intFromAny reads an integer out of a JSON-decoded value. encoding/json
// decodes numbers as float64 by default and as json.Number when the decoder
// is configured that way, so both are handled; anything else reads as 0.
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

// jsonNumber matches json.Number without importing encoding/json here, so
// intFromAny handles a decoder configured with UseNumber.
type jsonNumber interface {
	Int64() (int64, error)
}

// boolPtrFromAny returns a pointer to the bool value, or nil when the value is
// absent or another type. The report uses pointers where "not reported" and
// "reported false" must stay distinguishable.
func boolPtrFromAny(value any) *bool {
	typed, ok := value.(bool)
	if !ok {
		return nil
	}
	return &typed
}

// unsupportedTargetSummaries renders the readiness envelope's unsupported
// targets as sorted `kind/reason count=N` lines for the human summary. Sorting
// keeps the output stable across runs since the server does not promise an
// order.
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

// evidenceHandleIDs lists the non-blank handle ids for the human summary's
// evidence column.
func evidenceHandleIDs(handles []EvidenceHandle) []string {
	ids := make([]string, 0, len(handles))
	for _, handle := range handles {
		if strings.TrimSpace(handle.ID) != "" {
			ids = append(ids, handle.ID)
		}
	}
	return ids
}

// defaultString returns fallback when value is blank, so the human summary
// prints "unknown" or "-" instead of an empty column.
func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
