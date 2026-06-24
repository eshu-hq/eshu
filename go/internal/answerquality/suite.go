// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package answerquality

// DefaultSuite returns the representative answer families required by issue
// #1935. The prompts use redacted placeholders so scorecard evidence can be
// committed or pasted into tickets after live values are removed.
func DefaultSuite() Suite {
	return Suite{Prompts: []PromptSpec{
		{
			ID:                 "service-story",
			Family:             PromptFamilyServiceStory,
			Prompt:             "Build the service story for <service> and cite the evidence.",
			ExpectedTruthClass: "deterministic",
			RequiredSurfaces:   []Surface{SurfaceAPI, SurfaceMCP},
			RequiredNextCalls:  []string{"build_evidence_citation_packet"},
		},
		{
			ID:                 "code-topic",
			Family:             PromptFamilyCodeTopic,
			Prompt:             "Investigate <topic> in <repo> and read the relationship story.",
			ExpectedTruthClass: "code_hint",
			RequiredSurfaces:   []Surface{SurfaceAPI, SurfaceMCP},
			RequiredNextCalls:  []string{"get_code_relationship_story"},
		},
		{
			ID:                 "incident-context",
			Family:             PromptFamilyIncidentContext,
			Prompt:             "Summarize incident context for <service> with source evidence.",
			ExpectedTruthClass: "semantic_observation",
			RequiredSurfaces:   []Surface{SurfaceAPI, SurfaceMCP},
			RequiredNextCalls:  []string{"get_incident_context"},
		},
		{
			ID:                 "supply-chain",
			Family:             PromptFamilySupplyChainImpact,
			Prompt:             "Explain supply-chain impact for <repo> and cite findings.",
			ExpectedTruthClass: "deterministic",
			RequiredSurfaces:   []Surface{SurfaceAPI, SurfaceCLI},
			RequiredNextCalls:  []string{"vuln-scan repo"},
		},
		{
			ID:                 "documentation-truth",
			Family:             PromptFamilyDocumentationTruth,
			Prompt:             "Resolve a documentation finding and confirm it is current.",
			ExpectedTruthClass: "semantic_observation",
			RequiredSurfaces:   []Surface{SurfaceAPI, SurfaceMCP},
			RequiredNextCalls:  []string{"check_documentation_evidence_packet_freshness"},
		},
		{
			ID:                 "freshness-readiness",
			Family:             PromptFamilyFreshnessReadiness,
			Prompt:             "Explain freshness and readiness for <repo> before acting.",
			ExpectedTruthClass: "deterministic",
			RequiredSurfaces:   []Surface{SurfaceAPI, SurfaceMCP},
			RequiredNextCalls:  []string{"get_index_status"},
		},
		{
			ID:                 "hosted-governance",
			Family:             PromptFamilyHostedGovernance,
			Prompt:             "Explain hosted onboarding status and governance caveats.",
			ExpectedTruthClass: "deterministic",
			RequiredSurfaces:   []Surface{SurfaceHosted, SurfaceCLI},
			RequiredNextCalls:  []string{"hosted-onboard"},
		},
	}}
}
