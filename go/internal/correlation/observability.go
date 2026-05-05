package correlation

import (
	"github.com/eshu-hq/eshu/go/internal/correlation/engine"
	"github.com/eshu-hq/eshu/go/internal/correlation/model"
)

// Summary captures lightweight counters for correlation execution reporting.
type Summary struct {
	EvaluatedRules     int
	AdmittedCandidates int
	RejectedCandidates int
	ConflictCount      int
	LowConfidenceCount int
}

// BuildSummary reduces one evaluation into operator-facing counters.
func BuildSummary(evaluation engine.Evaluation) Summary {
	summary := Summary{
		EvaluatedRules: len(evaluation.OrderedRuleNames),
	}
	for _, result := range evaluation.Results {
		switch result.Candidate.State {
		case model.CandidateStateAdmitted:
			summary.AdmittedCandidates++
		case model.CandidateStateRejected:
			summary.RejectedCandidates++
		}
		for _, reason := range result.Candidate.RejectionReasons {
			switch reason {
			case model.RejectionReasonLowConfidence:
				summary.LowConfidenceCount++
			case model.RejectionReasonLostTieBreak:
				summary.ConflictCount++
			}
		}
	}
	return summary
}
