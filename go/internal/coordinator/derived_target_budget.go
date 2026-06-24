// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

import "sort"

const derivedTargetSkipReasonBudgetExhausted = "derived_target_budget_exhausted"

const (
	derivedTargetSkipReasonMissingPackageName    = "derived_target_missing_package_name"
	derivedTargetSkipReasonMissingPURL           = "derived_target_missing_purl"
	derivedTargetSkipReasonMissingSource         = "derived_target_missing_source"
	derivedTargetSkipReasonMissingSourceLocation = "derived_target_missing_source_location"
	derivedTargetSkipReasonMissingSubject        = "derived_target_missing_subject"
	derivedTargetSkipReasonMissingVersion        = "derived_target_missing_version"
	derivedTargetSkipReasonNonExactVersion       = "derived_target_non_exact_version"
	derivedTargetSkipReasonConflictingVersion    = "derived_target_conflicting_version"
)

type derivedTargetSkipEvidence struct {
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

func derivedTargetSkipEvidenceByReason(
	collectorKind string,
	targetLimit int,
	selectedCount int,
	skippedCounts map[string]map[string]int,
	sources []string,
) []derivedTargetSkipEvidence {
	if len(skippedCounts) == 0 {
		return nil
	}
	reasons := sortedMapKeys(skippedCounts)
	out := make([]derivedTargetSkipEvidence, 0, len(skippedCounts))
	for _, reason := range reasons {
		ecosystemCounts := skippedCounts[reason]
		for _, ecosystem := range sortedMapKeys(ecosystemCounts) {
			count := ecosystemCounts[ecosystem]
			if count <= 0 {
				continue
			}
			out = append(out, derivedTargetSkipEvidence{
				CollectorKind: collectorKind,
				TargetClass:   targetClassOwnedPackage,
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

func derivedTargetSkipEvidenceByReasonForClass(
	collectorKind string,
	targetClass string,
	sourceFamily string,
	targetLimit int,
	selectedCount int,
	skippedCounts map[string]map[string]int,
	sources []string,
) []derivedTargetSkipEvidence {
	out := derivedTargetSkipEvidenceByReason(collectorKind, targetLimit, selectedCount, skippedCounts, sources)
	for i := range out {
		out[i].TargetClass = targetClass
		out[i].SourceFamily = sourceFamily
	}
	return out
}

func recordDerivedTargetSkip(skippedCounts map[string]map[string]int, reason string, ecosystem string) {
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

func derivedTargetBudgetSkipEvidence(
	collectorKind string,
	targetLimit int,
	selectedCount int,
	skippedCount int,
	ecosystems []string,
	sources []string,
) []derivedTargetSkipEvidence {
	if skippedCount <= 0 {
		return nil
	}
	return []derivedTargetSkipEvidence{{
		CollectorKind: collectorKind,
		TargetClass:   targetClassOwnedPackage,
		Reason:        derivedTargetSkipReasonBudgetExhausted,
		TargetLimit:   targetLimit,
		SelectedCount: selectedCount,
		SkippedCount:  skippedCount,
		Ecosystems:    ecosystems,
		Sources:       sources,
	}}
}
