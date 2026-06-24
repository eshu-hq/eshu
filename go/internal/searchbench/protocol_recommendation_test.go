// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package searchbench

import (
	"strings"
	"testing"
)

func TestValidateProtocolRecommendationAcceptsKeepCurrentPath(t *testing.T) {
	t.Parallel()

	if err := ValidateProtocolRecommendation(validProtocolRecommendation()); err != nil {
		t.Fatalf("ValidateProtocolRecommendation() error = %v, want nil", err)
	}
}

func TestValidateProtocolRecommendationRejectsMissingBaselineHybridEvidence(t *testing.T) {
	t.Parallel()

	recommendation := validProtocolRecommendation()
	recommendation.BaselineHybridEvidence = RerankBaselineEvidence{}

	err := ValidateProtocolRecommendation(recommendation)
	if err == nil {
		t.Fatal("ValidateProtocolRecommendation() error = nil, want missing baseline evidence failure")
	}
	if want := "baseline hybrid evidence is required"; !strings.Contains(err.Error(), want) {
		t.Fatalf("ValidateProtocolRecommendation() error = %q, want substring %q", err, want)
	}
}

func TestValidateProtocolRecommendationRejectsAPIMCPAuthorizationBypass(t *testing.T) {
	t.Parallel()

	recommendation := validProtocolRecommendation()
	recommendation.APIMCPAuthorizationPreserved = false

	err := ValidateProtocolRecommendation(recommendation)
	if err == nil {
		t.Fatal("ValidateProtocolRecommendation() error = nil, want authorization failure")
	}
	if want := "api_mcp_authorization_preserved must be true"; !strings.Contains(err.Error(), want) {
		t.Fatalf("ValidateProtocolRecommendation() error = %q, want substring %q", err, want)
	}
}

func TestValidateProtocolRecommendationRejectsMissingFallbackBehavior(t *testing.T) {
	t.Parallel()

	recommendation := validProtocolRecommendation()
	recommendation.FallbackBehavior = ""

	err := ValidateProtocolRecommendation(recommendation)
	if err == nil {
		t.Fatal("ValidateProtocolRecommendation() error = nil, want fallback failure")
	}
	if want := "fallback_behavior is required"; !strings.Contains(err.Error(), want) {
		t.Fatalf("ValidateProtocolRecommendation() error = %q, want substring %q", err, want)
	}
}

func TestValidateProtocolRecommendationRejectsUnsupportedRiskCategories(t *testing.T) {
	t.Parallel()

	recommendation := validProtocolRecommendation()
	recommendation.MigrationRisk = ProtocolAssessmentCategory("sometimes")
	recommendation.SecurityRisk = ProtocolAssessmentCategory("maybe")
	recommendation.OperatorBurden = ProtocolAssessmentCategory("large")

	err := ValidateProtocolRecommendation(recommendation)
	if err == nil {
		t.Fatal("ValidateProtocolRecommendation() error = nil, want risk category failures")
	}
	for _, want := range []string{
		"migration_risk is invalid",
		"security_risk is invalid",
		"operator_burden is invalid",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("ValidateProtocolRecommendation() error = %q, want substring %q", err, want)
		}
	}
}

func TestValidateProtocolRecommendationRejectsUnsupportedCandidateProtocol(t *testing.T) {
	t.Parallel()

	recommendation := validProtocolRecommendation()
	recommendation.CandidateProtocol = ProtocolCandidate("raw_backend_socket")

	err := ValidateProtocolRecommendation(recommendation)
	if err == nil {
		t.Fatal("ValidateProtocolRecommendation() error = nil, want candidate failure")
	}
	if want := "candidate_protocol is invalid"; !strings.Contains(err.Error(), want) {
		t.Fatalf("ValidateProtocolRecommendation() error = %q, want substring %q", err, want)
	}
}

func TestValidateProtocolRecommendationRejectsUnsupportedUserValueClaims(t *testing.T) {
	t.Parallel()

	recommendation := validProtocolRecommendation()
	recommendation.ExpectedUserValue = []ProtocolValueEvidence{
		{
			Value: ProtocolUserValue("team_preference"),
		},
	}

	err := ValidateProtocolRecommendation(recommendation)
	if err == nil {
		t.Fatal("ValidateProtocolRecommendation() error = nil, want unsupported value failure")
	}
	for _, want := range []string{
		"expected_user_value[0].value is invalid",
		"expected_user_value[0].evidence or deferred_reason is required",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("ValidateProtocolRecommendation() error = %q, want substring %q", err, want)
		}
	}
}

func TestValidateProtocolRecommendationRejectsMissingImpactEvidence(t *testing.T) {
	t.Parallel()

	recommendation := validProtocolRecommendation()
	recommendation.LatencyImpact = ProtocolImpact{Direction: ProtocolImpactImproved}
	recommendation.CostImpact = ProtocolImpact{Direction: ProtocolImpactUnknown}

	err := ValidateProtocolRecommendation(recommendation)
	if err == nil {
		t.Fatal("ValidateProtocolRecommendation() error = nil, want impact evidence failure")
	}
	for _, want := range []string{
		"latency_impact.evidence or deferred_reason is required",
		"cost_impact.evidence or deferred_reason is required",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("ValidateProtocolRecommendation() error = %q, want substring %q", err, want)
		}
	}
}

func validProtocolRecommendation() ProtocolRecommendation {
	return ProtocolRecommendation{
		BaselineHybridEvidence: RerankBaselineEvidence{
			EvidenceID: "search-benchmark:hybrid:v1",
			Backend:    BackendNornicDBHybrid,
			Mode:       ModeHybrid,
		},
		CandidateProtocol:            ProtocolCandidateCurrentAPIMCP,
		Decision:                     ProtocolDecisionKeepCurrentPath,
		Rationale:                    "Current API/MCP path keeps authorization and operator debugging stable.",
		MigrationRisk:                ProtocolAssessmentNone,
		SecurityRisk:                 ProtocolAssessmentNone,
		OperatorBurden:               ProtocolAssessmentNone,
		FallbackBehavior:             "Keep current API/MCP path.",
		APIMCPAuthorizationPreserved: true,
		ExpectedUserValue: []ProtocolValueEvidence{
			{
				Value:    ProtocolUserValueOperability,
				Evidence: "Existing API/MCP path already has bounded auth and debugging behavior.",
			},
		},
		LatencyImpact: ProtocolImpact{
			Direction: ProtocolImpactNeutral,
			Evidence:  "No new network hop is introduced.",
		},
		CostImpact: ProtocolImpact{
			Direction: ProtocolImpactNeutral,
			Evidence:  "No additional service or client runtime is introduced.",
		},
	}
}
