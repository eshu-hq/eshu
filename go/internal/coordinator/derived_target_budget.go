package coordinator

const derivedTargetSkipReasonBudgetExhausted = "derived_target_budget_exhausted"

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
