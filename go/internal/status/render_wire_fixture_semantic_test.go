// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package status

// fixtureSemanticExtractionStatus returns a fully populated
// SemanticExtractionStatus, including ProviderProfiles, Queue, Budget, and
// Audit, for maxRawSnapshot (render_wire_fixture_test.go).
func fixtureSemanticExtractionStatus() SemanticExtractionStatus {
	return SemanticExtractionStatus{
		State:                            SemanticExtractionAvailable,
		Reason:                           SemanticExtractionReasonProviderConfigured,
		Detail:                           "fixture semantic extraction provider is configured and healthy",
		ProviderConfigured:               true,
		DocumentationObservationsEnabled: true,
		CodeHintsEnabled:                 true,
		DeterministicPathsAffected:       false,
		UpdatedAt:                        fixtureTime(120),
		ProviderProfiles: []SemanticProviderProfileStatus{
			{
				ProfileID:              "profile-fixture-a",
				DisplayName:            "Fixture Provider A",
				ProviderKind:           "embedding",
				CredentialSourceKind:   "vault",
				CredentialConfigured:   true,
				ModelID:                "fixture-model-a",
				EmbeddingDimensions:    768,
				EndpointProfileID:      "endpoint-fixture-a",
				SourceClasses:          []string{"documentation", "code_hints"},
				SourcePolicyConfigured: true,
				State:                  SemanticProviderProfileConfigured,
				Reason:                 "provider_profile_configured",
				Detail:                 "fixture profile a detail",
				UpdatedAt:              fixtureTime(121),
			},
			{
				ProfileID:              "profile-fixture-b",
				DisplayName:            "Fixture Provider B",
				ProviderKind:           "completion",
				CredentialSourceKind:   "environment",
				CredentialConfigured:   true,
				ModelID:                "fixture-model-b",
				EmbeddingDimensions:    1536,
				EndpointProfileID:      "endpoint-fixture-b",
				SourceClasses:          []string{"documentation"},
				SourcePolicyConfigured: true,
				State:                  SemanticProviderProfileHealthy,
				Reason:                 "provider_profile_healthy",
				Detail:                 "fixture profile b detail",
				UpdatedAt:              fixtureTime(122),
			},
		},
		Queue: SemanticExtractionQueueSnapshot{
			Total:               50,
			Pending:             10,
			Claimed:             8,
			Retrying:            4,
			Succeeded:           20,
			DeadLetter:          2,
			Skipped:             1,
			NoProvider:          1,
			PolicyDenied:        1,
			BudgetExhausted:     1,
			Unsafe:              1,
			ProviderUnavailable: 1,
			Unchanged:           3,
			Stale:               2,
			StatusCounts: []NamedCount{
				{Name: "pending", Count: 10},
				{Name: "succeeded", Count: 20},
			},
			SourceClassCounts: []NamedCount{
				{Name: "documentation", Count: 30},
				{Name: "code_hints", Count: 20},
			},
			FailureClassCounts: []NamedCount{
				{Name: "provider_timeout", Count: 3},
				{Name: "rate_limited", Count: 1},
			},
			ProviderProfileCounts: []SemanticExtractionProviderProfileQueueCount{
				{ProviderKind: "embedding", ProviderProfileID: "profile-fixture-a", ProviderProfileClass: "primary", Count: 30},
				{ProviderKind: "completion", ProviderProfileID: "profile-fixture-b", ProviderProfileClass: "secondary", Count: 20},
			},
			PolicyDecisionCounts: []SemanticExtractionDecisionCount{
				{State: "allowed", Reason: "policy_allowed", Count: 40},
				{State: "denied", Reason: "policy_denied", Count: 10},
			},
			GuardDecisionCounts: []SemanticExtractionDecisionCount{
				{State: "allowed", Reason: "guard_allowed", Count: 42},
				{State: "denied", Reason: "unsafe_output", Count: 8},
			},
			UpdatedAt: fixtureTime(123),
		},
		Budget: SemanticExtractionBudgetSnapshot{
			EstimatedInputTokens:  100000,
			EstimatedOutputTokens: 20000,
			EstimatedCostMicros:   500000,
			ActualInputTokens:     95000,
			ActualOutputTokens:    18000,
			ActualCostMicros:      470000,
			RemainingTokens:       5000,
			RemainingCostMicros:   30000,
			Exhausted:             1,
			DecisionCounts: []SemanticExtractionBudgetDecisionCount{
				{State: "allowed", Reason: "within_budget", BudgetUnit: "tokens", Count: 90},
				{State: "denied", Reason: "budget_exhausted", BudgetUnit: "cost_micros", Count: 10},
			},
		},
		Audit: SemanticExtractionAuditSnapshot{
			ActorClassCounts: []NamedCount{
				{Name: "collector", Count: 25},
				{Name: "operator", Count: 5},
			},
			ACLStateCounts: []NamedCount{
				{Name: "allowed", Count: 27},
				{Name: "denied", Count: 3},
			},
			LastProcessedAt: fixtureTime(124),
		},
	}
}

// fixtureAnswerNarrationStatus returns a fully populated
// AnswerNarrationStatus for maxRawSnapshot (render_wire_fixture_test.go).
func fixtureAnswerNarrationStatus() AnswerNarrationStatus {
	return AnswerNarrationStatus{
		State:                          AnswerNarrationAvailable,
		Reason:                         AnswerNarrationReasonAvailable,
		Detail:                         "fixture answer narration is policy allowed",
		ProviderConfigured:             true,
		ProviderTrafficEnabled:         true,
		PolicyAllowed:                  true,
		BudgetAvailable:                true,
		PublishSafetyEnabled:           true,
		DeterministicFallbackAvailable: true,
		CanonicalTruthAffected:         false,
		RetentionPosture:               AnswerNarrationRetentionMetadataOnly,
		PolicyHash:                     "fixture-policy-hash-0123456789abcdef",
		UpdatedAt:                      fixtureTime(130),
	}
}
