// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package schedule

import "sort"

// DerivedTargetSkipReasonBudgetExhausted marks targets dropped because the
// instance's target budget filled before they were reached.
const DerivedTargetSkipReasonBudgetExhausted = "derived_target_budget_exhausted"

// Derived-target skip reasons name why one candidate target was dropped
// before planning. They are recorded per reason and ecosystem so an operator
// can tell a configuration gap from a data gap.
const (
	DerivedTargetSkipReasonMissingPackageName    = "derived_target_missing_package_name"
	DerivedTargetSkipReasonMissingPURL           = "derived_target_missing_purl"
	DerivedTargetSkipReasonMissingSource         = "derived_target_missing_source"
	DerivedTargetSkipReasonMissingSourceLocation = "derived_target_missing_source_location"
	DerivedTargetSkipReasonMissingSubject        = "derived_target_missing_subject"
	DerivedTargetSkipReasonMissingVersion        = "derived_target_missing_version"
	DerivedTargetSkipReasonNonExactVersion       = "derived_target_non_exact_version"
	DerivedTargetSkipReasonConflictingVersion    = "derived_target_conflicting_version"
)

// DerivedTargetSkipEvidence is the operator-facing record of one reason a
// derived-target selection dropped candidates, carried on the plan so the
// skip is visible without re-running the derivation.
type DerivedTargetSkipEvidence struct {
	CollectorKind string   `json:"collector_kind"`
	TargetClass   string   `json:"target_class"`
	SourceFamily  string   `json:"source_family,omitempty"`
	Reason        string   `json:"reason"`
	TargetLimit   int      `json:"target_limit"`
	SelectedCount int      `json:"selected_count"`
	SkippedCount  int      `json:"skipped_count"`
	Ecosystems    []string `json:"ecosystems,omitempty"`
	Sources       []string `json:"sources,omitempty"`
}

// DerivedTargetSkipEvidenceByReason renders one evidence row per (reason,
// ecosystem) pair with a non-zero count, in a stable sorted order.
func DerivedTargetSkipEvidenceByReason(
	collectorKind string,
	targetLimit int,
	selectedCount int,
	skippedCounts map[string]map[string]int,
	sources []string,
) []DerivedTargetSkipEvidence {
	if len(skippedCounts) == 0 {
		return nil
	}
	reasons := sortedMapKeys(skippedCounts)
	out := make([]DerivedTargetSkipEvidence, 0, len(skippedCounts))
	for _, reason := range reasons {
		ecosystemCounts := skippedCounts[reason]
		for _, ecosystem := range sortedMapKeys(ecosystemCounts) {
			count := ecosystemCounts[ecosystem]
			if count <= 0 {
				continue
			}
			out = append(out, DerivedTargetSkipEvidence{
				CollectorKind: collectorKind,
				TargetClass:   TargetClassOwnedPackage,
				Reason:        reason,
				TargetLimit:   targetLimit,
				SelectedCount: selectedCount,
				SkippedCount:  count,
				Ecosystems:    []string{ecosystem},
				Sources:       sources,
			})
		}
	}
	return out
}

// DerivedTargetSkipEvidenceByReasonForClass is
// [DerivedTargetSkipEvidenceByReason] with every row attributed to one target
// class and source family.
func DerivedTargetSkipEvidenceByReasonForClass(
	collectorKind string,
	targetClass string,
	sourceFamily string,
	targetLimit int,
	selectedCount int,
	skippedCounts map[string]map[string]int,
	sources []string,
) []DerivedTargetSkipEvidence {
	out := DerivedTargetSkipEvidenceByReason(collectorKind, targetLimit, selectedCount, skippedCounts, sources)
	for i := range out {
		out[i].TargetClass = targetClass
		out[i].SourceFamily = sourceFamily
	}
	return out
}

// RecordDerivedTargetSkip counts one skipped candidate under its reason and
// ecosystem. Blank reasons or ecosystems are ignored rather than bucketed
// under an empty key.
func RecordDerivedTargetSkip(skippedCounts map[string]map[string]int, reason string, ecosystem string) {
	if reason == "" || ecosystem == "" {
		return
	}
	if skippedCounts[reason] == nil {
		skippedCounts[reason] = map[string]int{}
	}
	skippedCounts[reason][ecosystem]++
}

func sortedMapKeys[V any](values map[string]V) []string {
	out := make([]string, 0, len(values))
	for key := range values {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

// DerivedTargetBudgetSkipEvidence renders the single evidence row for targets
// dropped by budget exhaustion, or nil when none were.
func DerivedTargetBudgetSkipEvidence(
	collectorKind string,
	targetLimit int,
	selectedCount int,
	skippedCount int,
	ecosystems []string,
	sources []string,
) []DerivedTargetSkipEvidence {
	if skippedCount <= 0 {
		return nil
	}
	return []DerivedTargetSkipEvidence{{
		CollectorKind: collectorKind,
		TargetClass:   TargetClassOwnedPackage,
		Reason:        DerivedTargetSkipReasonBudgetExhausted,
		TargetLimit:   targetLimit,
		SelectedCount: selectedCount,
		SkippedCount:  skippedCount,
		Ecosystems:    ecosystems,
		Sources:       sources,
	}}
}
