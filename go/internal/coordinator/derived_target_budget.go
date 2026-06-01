package coordinator

import "sort"

const derivedTargetSkipReasonBudgetExhausted = "derived_target_budget_exhausted"

const (
	derivedTargetSkipReasonMissingPackageName    = "derived_target_missing_package_name"
	derivedTargetSkipReasonMissingSourceLocation = "derived_target_missing_source_location"
	derivedTargetSkipReasonNonExactVersion       = "derived_target_non_exact_version"
)

type derivedTargetSkipEvidence struct {
	CollectorKind string   `json:"collector_kind"`
	TargetClass   string   `json:"target_class"`
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
