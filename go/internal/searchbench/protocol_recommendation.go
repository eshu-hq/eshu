package searchbench

import (
	"fmt"
	"strings"
)

// ValidateProtocolRecommendation checks guardrails for a protocol decision gate.
func ValidateProtocolRecommendation(recommendation ProtocolRecommendation) error {
	var problems []string
	problems = append(problems, validateRerankBaselineEvidence(recommendation.BaselineHybridEvidence)...)
	if !validProtocolCandidate(recommendation.CandidateProtocol) {
		problems = append(problems, "candidate_protocol is invalid")
	}
	if !validProtocolDecision(recommendation.Decision) {
		problems = append(problems, "decision is invalid")
	}
	if recommendation.Decision == ProtocolDecisionAddProtocol &&
		(recommendation.CandidateProtocol == ProtocolCandidateCurrentAPIMCP ||
			recommendation.CandidateProtocol == ProtocolCandidateDeferred) {
		problems = append(problems, "candidate_protocol must be an expansion candidate for add_protocol")
	}
	if recommendation.CandidateProtocol == ProtocolCandidateDeferred &&
		recommendation.Decision != ProtocolDecisionDeferExpansion {
		problems = append(problems, "deferred candidate requires defer_protocol_expansion decision")
	}
	if strings.TrimSpace(recommendation.Rationale) == "" {
		problems = append(problems, "rationale is required")
	}
	problems = append(
		problems,
		validateProtocolAssessment("migration_risk", recommendation.MigrationRisk)...,
	)
	problems = append(
		problems,
		validateProtocolAssessment("security_risk", recommendation.SecurityRisk)...,
	)
	problems = append(
		problems,
		validateProtocolAssessment("operator_burden", recommendation.OperatorBurden)...,
	)
	if strings.TrimSpace(recommendation.FallbackBehavior) == "" {
		problems = append(problems, "fallback_behavior is required")
	}
	if !recommendation.APIMCPAuthorizationPreserved {
		problems = append(problems, "api_mcp_authorization_preserved must be true")
	}
	problems = append(problems, validateProtocolValueEvidence(recommendation.ExpectedUserValue)...)
	problems = append(problems, validateProtocolImpact("latency_impact", recommendation.LatencyImpact)...)
	problems = append(problems, validateProtocolImpact("cost_impact", recommendation.CostImpact)...)
	return joinedValidationError(problems)
}

func validateProtocolValueEvidence(values []ProtocolValueEvidence) []string {
	if len(values) == 0 {
		return []string{"expected_user_value is required"}
	}
	var problems []string
	for i, value := range values {
		prefix := fmt.Sprintf("expected_user_value[%d]", i)
		if !validProtocolUserValue(value.Value) {
			problems = append(problems, prefix+".value is invalid")
		}
		if strings.TrimSpace(value.Evidence) == "" && strings.TrimSpace(value.DeferredReason) == "" {
			problems = append(problems, prefix+".evidence or deferred_reason is required")
		}
	}
	return problems
}

func validateProtocolImpact(prefix string, impact ProtocolImpact) []string {
	var problems []string
	if !validProtocolImpactDirection(impact.Direction) {
		problems = append(problems, prefix+".direction is invalid")
	}
	if strings.TrimSpace(impact.Evidence) == "" && strings.TrimSpace(impact.DeferredReason) == "" {
		problems = append(problems, prefix+".evidence or deferred_reason is required")
	}
	return problems
}

func validateProtocolAssessment(
	field string,
	category ProtocolAssessmentCategory,
) []string {
	if strings.TrimSpace(string(category)) == "" {
		return []string{field + " is required"}
	}
	if !validProtocolAssessmentCategory(category) {
		return []string{field + " is invalid"}
	}
	return nil
}

func validProtocolCandidate(candidate ProtocolCandidate) bool {
	switch candidate {
	case ProtocolCandidateCurrentAPIMCP,
		ProtocolCandidateGraphQL,
		ProtocolCandidateGRPC,
		ProtocolCandidateQdrantGRPC,
		ProtocolCandidateNornicNative,
		ProtocolCandidateDeferred:
		return true
	default:
		return false
	}
}

func validProtocolAssessmentCategory(category ProtocolAssessmentCategory) bool {
	switch category {
	case ProtocolAssessmentNone,
		ProtocolAssessmentLow,
		ProtocolAssessmentMedium,
		ProtocolAssessmentHigh,
		ProtocolAssessmentUnknown:
		return true
	default:
		return false
	}
}

func validProtocolDecision(decision ProtocolDecision) bool {
	switch decision {
	case ProtocolDecisionKeepCurrentPath,
		ProtocolDecisionAddProtocol,
		ProtocolDecisionDeferExpansion:
		return true
	default:
		return false
	}
}

func validProtocolUserValue(value ProtocolUserValue) bool {
	switch value {
	case ProtocolUserValueLatency,
		ProtocolUserValueCost,
		ProtocolUserValueOperability,
		ProtocolUserValueSecurity,
		ProtocolUserValueIncidentDebug:
		return true
	default:
		return false
	}
}

func validProtocolImpactDirection(direction ProtocolImpactDirection) bool {
	switch direction {
	case ProtocolImpactImproved,
		ProtocolImpactRegressed,
		ProtocolImpactNeutral,
		ProtocolImpactUnknown:
		return true
	default:
		return false
	}
}
