// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package vulnscan

import (
	"strings"

	exportspkg "github.com/eshu-hq/eshu/go/internal/exports"
)

// ReachabilityFromFinding reads the finding's reachability block into the
// report's shape, or returns nil when the block is absent or carries no state.
// A finding with no state is reported as having no reachability verdict rather
// than as an empty one, because an empty state would read as "not reachable".
//
// It is exported because the report builder and the reachability regression
// tests both call it directly.
func ReachabilityFromFinding(finding map[string]any) *ReportReachability {
	raw, ok := finding["reachability"].(map[string]any)
	if !ok {
		return nil
	}
	state := strings.TrimSpace(stringFromMap(raw, "state"))
	if state == "" {
		return nil
	}
	return &ReportReachability{
		State:            state,
		Confidence:       strings.TrimSpace(stringFromMap(raw, "confidence")),
		Source:           strings.TrimSpace(stringFromMap(raw, "source")),
		Evidence:         strings.TrimSpace(stringFromMap(raw, "evidence")),
		Reason:           strings.TrimSpace(stringFromMap(raw, "reason")),
		LanguageMaturity: strings.TrimSpace(stringFromMap(raw, "language_maturity")),
		MissingEvidence:  stringSliceFromAny(raw["missing_evidence"]),
	}
}

// sarifReachability is the SARIF export's copy of the same read. It differs
// from ReachabilityFromFinding in one way that matters: the missing-evidence
// list is sorted, because SARIF output must be byte-stable across runs while
// the JSON report preserves the server's order.
func sarifReachability(finding map[string]any) *exportspkg.Reachability {
	raw, ok := finding["reachability"].(map[string]any)
	if !ok {
		return nil
	}
	state := strings.TrimSpace(stringFromMap(raw, "state"))
	if state == "" {
		return nil
	}
	return &exportspkg.Reachability{
		State:            state,
		Confidence:       strings.TrimSpace(stringFromMap(raw, "confidence")),
		Source:           strings.TrimSpace(stringFromMap(raw, "source")),
		Evidence:         strings.TrimSpace(stringFromMap(raw, "evidence")),
		Reason:           strings.TrimSpace(stringFromMap(raw, "reason")),
		LanguageMaturity: strings.TrimSpace(stringFromMap(raw, "language_maturity")),
		MissingEvidence:  cloneAndSortStrings(stringSliceFromAny(raw["missing_evidence"])),
	}
}
