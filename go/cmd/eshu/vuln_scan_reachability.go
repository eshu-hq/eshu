// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"strings"

	exportspkg "github.com/eshu-hq/eshu/go/internal/exports"
)

func reachabilityFromFinding(finding map[string]any) *vulnScanReportReachability {
	raw, ok := finding["reachability"].(map[string]any)
	if !ok {
		return nil
	}
	state := strings.TrimSpace(stringFromMap(raw, "state"))
	if state == "" {
		return nil
	}
	return &vulnScanReportReachability{
		State:            state,
		Confidence:       strings.TrimSpace(stringFromMap(raw, "confidence")),
		Source:           strings.TrimSpace(stringFromMap(raw, "source")),
		Evidence:         strings.TrimSpace(stringFromMap(raw, "evidence")),
		Reason:           strings.TrimSpace(stringFromMap(raw, "reason")),
		LanguageMaturity: strings.TrimSpace(stringFromMap(raw, "language_maturity")),
		MissingEvidence:  stringSliceFromAny(raw["missing_evidence"]),
	}
}

func vulnScanSARIFReachability(finding map[string]any) *exportspkg.Reachability {
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
