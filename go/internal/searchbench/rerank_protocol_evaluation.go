// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package searchbench

import "strings"

// RerankProtocolEvaluationVersion is the first issue #421 evaluation schema.
const RerankProtocolEvaluationVersion = "rerank-protocol-evaluation/v1"

// RerankProtocolEvaluation records the issue #421 phase-five decision gate.
type RerankProtocolEvaluation struct {
	Version                string                 `json:"version"`
	BaselineHybridEvidence RerankBaselineEvidence `json:"baseline_hybrid_evidence"`
	RerankEvaluation       RerankEvaluation       `json:"rerank_evaluation"`
	ProtocolRecommendation ProtocolRecommendation `json:"protocol_recommendation"`
	AcceptedStopReason     string                 `json:"accepted_stop_reason"`
}

// ValidateRerankProtocolEvaluation checks the issue #421 rerank/protocol gate.
func ValidateRerankProtocolEvaluation(evaluation RerankProtocolEvaluation) error {
	var problems []string
	if evaluation.Version != RerankProtocolEvaluationVersion {
		problems = append(problems, "version must be "+RerankProtocolEvaluationVersion)
	}

	stopReason := strings.TrimSpace(evaluation.AcceptedStopReason)
	baselinePresent := rerankBaselineEvidencePresent(evaluation.BaselineHybridEvidence)
	rerankPresent := rerankEvaluationPresent(evaluation.RerankEvaluation)
	protocolPresent := protocolRecommendationPresent(evaluation.ProtocolRecommendation)
	if stopReason != "" {
		if !strings.Contains(strings.ToLower(stopReason), "#421") {
			problems = append(problems, "accepted_stop_reason must reference issue #421")
		}
		if !strings.Contains(strings.ToLower(stopReason), "#417") {
			problems = append(problems, "accepted_stop_reason must reference issue #417")
		}
		if baselinePresent || rerankPresent || protocolPresent {
			problems = append(
				problems,
				"accepted_stop_reason cannot be set when baseline, rerank, or protocol evidence is present",
			)
		}
		return joinedValidationError(problems)
	}

	problems = append(problems, validateRerankBaselineEvidence(evaluation.BaselineHybridEvidence)...)
	if rerankPresent {
		if err := ValidateRerankEvaluation(evaluation.RerankEvaluation); err != nil {
			problems = append(problems, "rerank_evaluation is invalid: "+err.Error())
		}
		if baselinePresent &&
			!sameRerankBaselineEvidence(
				evaluation.BaselineHybridEvidence,
				evaluation.RerankEvaluation.BaselineHybridEvidence,
			) {
			problems = append(
				problems,
				"rerank_evaluation.baseline_hybrid_evidence must match baseline_hybrid_evidence",
			)
		}
	} else {
		problems = append(problems, "rerank_evaluation is required unless accepted_stop_reason is set")
	}

	if protocolPresent {
		if err := ValidateProtocolRecommendation(evaluation.ProtocolRecommendation); err != nil {
			problems = append(problems, "protocol_recommendation is invalid: "+err.Error())
		}
		if baselinePresent &&
			!sameRerankBaselineEvidence(
				evaluation.BaselineHybridEvidence,
				evaluation.ProtocolRecommendation.BaselineHybridEvidence,
			) {
			problems = append(
				problems,
				"protocol_recommendation.baseline_hybrid_evidence must match baseline_hybrid_evidence",
			)
		}
	} else {
		problems = append(problems, "protocol_recommendation is required unless accepted_stop_reason is set")
	}
	return joinedValidationError(problems)
}

func rerankBaselineEvidencePresent(evidence RerankBaselineEvidence) bool {
	return strings.TrimSpace(evidence.EvidenceID) != "" ||
		strings.TrimSpace(string(evidence.Backend)) != "" ||
		strings.TrimSpace(string(evidence.Mode)) != ""
}

func rerankEvaluationPresent(evaluation RerankEvaluation) bool {
	return strings.TrimSpace(evaluation.QueryID) != "" ||
		rerankBaselineEvidencePresent(evaluation.BaselineHybridEvidence) ||
		metricsPresent(evaluation.BaselineMetrics) ||
		metricsPresent(evaluation.RerankedMetrics) ||
		evaluation.RecallDelta != 0 ||
		evaluation.PrecisionDelta != 0 ||
		evaluation.NDCGDelta != 0 ||
		evaluation.FalseCanonicalClaimDelta != 0 ||
		evaluation.FalseCanonicalCandidateCount != 0 ||
		evaluation.BaselineLatency != 0 ||
		evaluation.RerankedLatency != 0 ||
		evaluation.LatencyDelta != 0 ||
		evaluation.BaselineCostMicrosUSD != 0 ||
		evaluation.RerankedCostMicrosUSD != 0 ||
		evaluation.CostDeltaMicrosUSD != 0 ||
		len(evaluation.Results) != 0
}

func protocolRecommendationPresent(recommendation ProtocolRecommendation) bool {
	return rerankBaselineEvidencePresent(recommendation.BaselineHybridEvidence) ||
		recommendation.CandidateProtocol != "" ||
		recommendation.Decision != "" ||
		strings.TrimSpace(recommendation.Rationale) != "" ||
		len(recommendation.ExpectedUserValue) != 0 ||
		recommendation.MigrationRisk != "" ||
		recommendation.SecurityRisk != "" ||
		recommendation.OperatorBurden != "" ||
		protocolImpactPresent(recommendation.LatencyImpact) ||
		protocolImpactPresent(recommendation.CostImpact) ||
		strings.TrimSpace(recommendation.FallbackBehavior) != "" ||
		recommendation.APIMCPAuthorizationPreserved
}

func metricsPresent(metrics RetrievalMetrics) bool {
	return metrics.Recall != 0 ||
		metrics.Precision != 0 ||
		metrics.NDCG != 0 ||
		metrics.FalseCanonicalClaimCount != nil
}

func protocolImpactPresent(impact ProtocolImpact) bool {
	return impact.Direction != "" ||
		strings.TrimSpace(impact.Evidence) != "" ||
		strings.TrimSpace(impact.DeferredReason) != ""
}

func sameRerankBaselineEvidence(left RerankBaselineEvidence, right RerankBaselineEvidence) bool {
	return strings.TrimSpace(left.EvidenceID) == strings.TrimSpace(right.EvidenceID) &&
		strings.TrimSpace(string(left.Backend)) == strings.TrimSpace(string(right.Backend)) &&
		strings.TrimSpace(string(left.Mode)) == strings.TrimSpace(string(right.Mode))
}
