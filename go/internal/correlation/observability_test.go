package correlation

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/correlation/engine"
	"github.com/eshu-hq/eshu/go/internal/correlation/model"
)

func TestBuildSummaryCountsOutcomes(t *testing.T) {
	t.Parallel()

	evaluation := engine.Evaluation{
		OrderedRuleNames: []string{"extract-name", "match-image", "admit-workload"},
		Results: []engine.Result{
			{
				Candidate: model.Candidate{
					ID:               "winner",
					Kind:             "deployable_unit",
					CorrelationKey:   "sample-service",
					State:            model.CandidateStateAdmitted,
					Confidence:       0.92,
					RejectionReasons: nil,
				},
			},
			{
				Candidate: model.Candidate{
					ID:               "low-confidence",
					Kind:             "deployable_unit",
					CorrelationKey:   "sample-service-util",
					State:            model.CandidateStateRejected,
					Confidence:       0.41,
					RejectionReasons: []model.RejectionReason{model.RejectionReasonLowConfidence},
				},
			},
			{
				Candidate: model.Candidate{
					ID:               "tie-break-loser",
					Kind:             "deployable_unit",
					CorrelationKey:   "sample-service",
					State:            model.CandidateStateRejected,
					Confidence:       0.91,
					RejectionReasons: []model.RejectionReason{model.RejectionReasonLostTieBreak},
				},
			},
		},
	}

	summary := BuildSummary(evaluation)
	if summary.EvaluatedRules != 3 {
		t.Fatalf("EvaluatedRules = %d, want 3", summary.EvaluatedRules)
	}
	if summary.AdmittedCandidates != 1 {
		t.Fatalf("AdmittedCandidates = %d, want 1", summary.AdmittedCandidates)
	}
	if summary.RejectedCandidates != 2 {
		t.Fatalf("RejectedCandidates = %d, want 2", summary.RejectedCandidates)
	}
	if summary.ConflictCount != 1 {
		t.Fatalf("ConflictCount = %d, want 1", summary.ConflictCount)
	}
	if summary.LowConfidenceCount != 1 {
		t.Fatalf("LowConfidenceCount = %d, want 1", summary.LowConfidenceCount)
	}
}
