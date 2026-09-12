// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package playbook

import "github.com/eshu-hq/eshu/go/internal/query/querycontract"

func incidentContextEvidencePathPlaybook() Definition {
	return Definition{
		ID:           "incident_context_evidence_path",
		Name:         "Incident context with evidence path",
		Version:      "1.0.0",
		PromptFamily: "incident.context",
		Description:  "Build incident context with source evidence, missing-slot explanation, and bounded follow-up paths.",
		RequiredInputs: []Input{
			{Name: "incident_id", Type: InputIdentifier, Required: true, Description: "Incident, alert, or work-item identifier to investigate."},
			{Name: "service_name", Type: InputIdentifier, Required: false, Description: "Optional service or workload selector when the incident is not enough."},
		},
		Steps: []Step{
			{
				ID:   "incident_context",
				Tool: "get_incident_context",
				Params: []Param{
					InputParam("incident_id", "incident_id"),
					InputParam("service_name", "service_name"),
					LimitParam("limit", 10),
				},
				ExpectedTruth:    querycontract.AnswerTruthDerived,
				EvidenceExpected: "incident context with linked services, repositories, deployments, observability sources, and explicit missing slots",
				Drilldowns: []Drilldown{
					{Tool: "list_work_item_evidence", Reason: "inspect the work-item evidence when incident context has gaps"},
					{Tool: "build_evidence_citation_packet", Reason: "hydrate returned evidence handles before citing the answer"},
				},
			},
			{
				ID:               "service_story_drilldown",
				Tool:             "get_service_story",
				Params:           []Param{InputParam("workload_id", "service_name"), LimitParam("limit", 10)},
				ExpectedTruth:    querycontract.AnswerTruthDeterministic,
				EvidenceExpected: "service dossier for the impacted workload when the incident identifies one",
				Drilldowns:       []Drilldown{{Tool: "trace_deployment_chain", Reason: "follow deployment evidence for the impacted service"}},
			},
		},
		FailureModes: secondWaveFailureModes("incident context"),
	}
}

func supplyChainImpactExplanationPlaybook() Definition {
	return Definition{
		ID:           "supply_chain_impact_explanation",
		Name:         "Supply-chain impact explanation",
		Version:      "1.0.0",
		PromptFamily: "supply-chain.impact",
		Description:  "Explain vulnerability or package impact while separating provider observations from Eshu-derived state.",
		RequiredInputs: []Input{
			{Name: "finding_id", Type: InputIdentifier, Required: true, Description: "Supply-chain finding, advisory, or package identifier."},
			{Name: "repo_id", Type: InputIdentifier, Required: false, Description: "Optional repository scope."},
		},
		Steps: []Step{
			{
				ID:   "impact_explanation",
				Tool: "explain_supply_chain_impact",
				Params: []Param{
					InputParam("finding_id", "finding_id"),
					InputParam("repo_id", "repo_id"),
					LimitParam("limit", 10),
				},
				ExpectedTruth:    querycontract.AnswerTruthDerived,
				EvidenceExpected: "impact explanation with provider evidence separated from Eshu package, image, repository, and service correlations",
				Drilldowns: []Drilldown{
					{Tool: "list_supply_chain_impact_findings", Reason: "list neighboring findings when the selected finding is ambiguous"},
					{Tool: "list_advisory_evidence", Reason: "inspect provider advisory evidence before making a claim"},
				},
			},
			{
				ID:               "citation_packet",
				Tool:             "build_evidence_citation_packet",
				Params:           []Param{LimitParam("limit", 10)},
				ExpectedTruth:    querycontract.AnswerTruthCodeHint,
				EvidenceExpected: "bounded citation packet for package, advisory, image, repository, and service evidence handles",
				Drilldowns:       []Drilldown{{Tool: "get_vulnerability_scanner_read_contract", Reason: "explain provider/Eshu state boundaries when confidence is unclear"}},
			},
		},
		FailureModes: secondWaveFailureModes("supply-chain impact"),
	}
}

func secretsIAMTrustChainPosturePlaybook() Definition {
	return Definition{
		ID:           "secrets_iam_trust_chain_posture",
		Name:         "Secrets/IAM trust-chain posture",
		Version:      "1.0.0",
		PromptFamily: "secrets-iam.posture",
		Description:  "Inspect identity trust chains, access paths, and posture gaps with exact, partial, and permission-hidden handling.",
		RequiredInputs: []Input{
			{Name: "identity_id", Type: InputIdentifier, Required: true, Description: "Identity, role, service account, or principal selector."},
			{Name: "scope_id", Type: InputIdentifier, Required: false, Description: "Optional account, tenant, repository, or workload scope."},
		},
		Steps: []Step{
			{
				ID:   "trust_chains",
				Tool: "list_secrets_iam_identity_trust_chains",
				Params: []Param{
					InputParam("identity_id", "identity_id"),
					InputParam("scope_id", "scope_id"),
					LimitParam("limit", 10),
				},
				ExpectedTruth:    querycontract.AnswerTruthDeterministic,
				EvidenceExpected: "trust chains with explicit exact, partial, or permission-hidden posture for the selected identity",
				Drilldowns: []Drilldown{
					{Tool: "list_secrets_iam_secret_access_paths", Reason: "inspect secret access paths for the selected identity"},
					{Tool: "list_secrets_iam_posture_gaps", Reason: "list posture gaps when the trust chain is incomplete"},
				},
			},
			{
				ID:               "posture_count",
				Tool:             "count_secrets_iam_posture",
				Params:           []Param{InputParam("identity_id", "identity_id"), LimitParam("limit", 10)},
				ExpectedTruth:    querycontract.AnswerTruthDerived,
				EvidenceExpected: "bounded posture counts that summarize trust-chain and secret-access coverage",
				Drilldowns:       []Drilldown{{Tool: "list_secrets_iam_privilege_posture_observations", Reason: "inspect privilege observations when counts look incomplete"}},
			},
		},
		FailureModes: secondWaveFailureModes("secrets/IAM posture"),
	}
}

func incrementalFreshnessReadinessPlaybook() Definition {
	return Definition{
		ID:           "incremental_freshness_readiness",
		Name:         "Incremental freshness and readiness investigation",
		Version:      "1.0.0",
		PromptFamily: "freshness.readiness",
		Description:  "Investigate stale or building answers with generation lifecycle, changed-since, index, and semantic readiness checks.",
		RequiredInputs: []Input{
			{Name: "scope_id", Type: InputIdentifier, Required: true, Description: "Repository, workload, service, or deployment scope to check for freshness."},
			{Name: "since", Type: InputString, Required: false, Description: "Optional timestamp or generation marker to compare against."},
		},
		Steps: []Step{
			{
				ID:   "generation_lifecycle",
				Tool: "get_generation_lifecycle",
				Params: []Param{
					InputParam("scope_id", "scope_id"),
					LimitParam("limit", 10),
				},
				ExpectedTruth:    querycontract.AnswerTruthDeterministic,
				EvidenceExpected: "generation lifecycle state showing fresh, stale, building, failed, or partial readiness",
				Drilldowns: []Drilldown{
					{Tool: "get_changed_since", Reason: "list changed entities when freshness is stale"},
					{Tool: "get_index_status", Reason: "inspect index convergence before trusting the answer"},
				},
			},
			{
				ID:               "semantic_readiness",
				Tool:             "get_semantic_capability_status",
				Params:           []Param{InputParam("scope_id", "scope_id"), LimitParam("limit", 10)},
				ExpectedTruth:    querycontract.AnswerTruthDerived,
				EvidenceExpected: "semantic capability status and next checks for stale-answer remediation",
				Drilldowns:       []Drilldown{{Tool: "get_service_changed_since", Reason: "narrow freshness checks to a single service when scope is broad"}},
			},
		},
		FailureModes: secondWaveFailureModes("freshness readiness"),
	}
}

func hostedOnboardingGovernanceStatusPlaybook() Definition {
	return Definition{
		ID:           "hosted_onboarding_governance_status",
		Name:         "Hosted onboarding governance status",
		Version:      "1.0.0",
		PromptFamily: "hosted.governance",
		Description:  "Summarize hosted onboarding readiness, auth scope, collector health, and governance caveats without exposing secrets.",
		RequiredInputs: []Input{
			{Name: "tenant_id", Type: InputIdentifier, Required: true, Description: "Tenant, team, or workspace identifier to check."},
			{Name: "scope_id", Type: InputIdentifier, Required: false, Description: "Optional repository or workload scope."},
		},
		Steps: []Step{
			{
				ID:   "index_readiness",
				Tool: "get_index_status",
				Params: []Param{
					InputParam("tenant_id", "tenant_id"),
					InputParam("scope_id", "scope_id"),
					LimitParam("limit", 10),
				},
				ExpectedTruth:    querycontract.AnswerTruthDeterministic,
				EvidenceExpected: "hosted index readiness, queue convergence, and explicit auth or scope caveats",
				Drilldowns: []Drilldown{
					{Tool: "list_collectors", Reason: "inspect collector availability for the tenant"},
					{Tool: "list_ingesters", Reason: "inspect ingestion readiness for hosted onboarding"},
				},
			},
			{
				ID:               "extension_diagnostics",
				Tool:             "get_component_extension_diagnostics",
				Params:           []Param{InputParam("scope_id", "scope_id"), LimitParam("limit", 10)},
				ExpectedTruth:    querycontract.AnswerTruthDerived,
				EvidenceExpected: "component extension diagnostics with hosted policy, auth, and scope caveats",
				Drilldowns:       []Drilldown{{Tool: "get_ingester_status", Reason: "drill into ingester status when onboarding is building or stale"}},
			},
		},
		FailureModes: secondWaveFailureModes("hosted governance readiness"),
	}
}

func changeSurfaceSourceInvestigationPlaybook() Definition {
	return Definition{
		ID:           "change_surface_source_investigation",
		Name:         "Change-surface source investigation",
		Version:      "1.0.0",
		PromptFamily: "change.surface",
		Description:  "Find a change surface, investigate ranked source drilldowns, and cite the exact source or relationship evidence.",
		RequiredInputs: []Input{
			{Name: "change_id", Type: InputIdentifier, Required: true, Description: "Pull request, commit, issue, or change identifier."},
			{Name: "repo_id", Type: InputIdentifier, Required: false, Description: "Optional repository scope."},
		},
		Steps: []Step{
			{
				ID:   "surface_search",
				Tool: "find_change_surface",
				Params: []Param{
					InputParam("change_id", "change_id"),
					InputParam("repo_id", "repo_id"),
					LimitParam("limit", 25),
				},
				ExpectedTruth:    querycontract.AnswerTruthCodeHint,
				EvidenceExpected: "ranked affected files, entities, relationships, and source handles for the change",
				Drilldowns: []Drilldown{
					{Tool: "investigate_change_surface", Reason: "expand the ranked surface into ownership and dependency evidence"},
					{Tool: "get_file_lines", Reason: "read exact source lines for a cited file handle"},
				},
			},
			{
				ID:               "relationship_evidence",
				Tool:             "get_relationship_evidence",
				Params:           []Param{LimitParam("limit", 10)},
				ExpectedTruth:    querycontract.AnswerTruthDeterministic,
				EvidenceExpected: "relationship evidence behind the change surface for citation or reviewer follow-up",
				Drilldowns:       []Drilldown{{Tool: "build_evidence_citation_packet", Reason: "hydrate selected handles into a bounded citation packet"}},
			},
		},
		FailureModes: secondWaveFailureModes("change surface"),
	}
}

func secondWaveFailureModes(scope string) []FailureMode {
	return []FailureMode{
		{
			Condition: "unsupported capability",
			Meaning:   scope + " requires a capability the current runtime does not advertise",
			Fallback:  "return an unsupported answer and call get_semantic_capability_status or get_index_status to identify the missing capability",
		},
		{
			Condition: "missing evidence",
			Meaning:   scope + " matched an entity but returned no addressable evidence handles",
			Fallback:  "present a partial answer and use the declared drilldowns to find source or relationship evidence",
		},
		{
			Condition: "stale freshness",
			Meaning:   scope + " evidence exists but may not reflect the current generation",
			Fallback:  "call get_generation_lifecycle or get_changed_since before citing the answer",
		},
		{
			Condition: "building freshness",
			Meaning:   scope + " is still building and the answer may be incomplete",
			Fallback:  "surface the building state and retry only after the readiness or lifecycle tool reports convergence",
		},
		{
			Condition: "truncated result set",
			Meaning:   scope + " returned more rows or handles than the bounded limit allowed",
			Fallback:  "increase the bounded limit or page through the next result window before drawing a final conclusion",
		},
		{
			Condition: "ambiguous selector",
			Meaning:   scope + " input matched multiple candidate entities",
			Fallback:  "ask for a narrower selector or use the list/search drilldown to choose an exact identifier",
		},
	}
}
