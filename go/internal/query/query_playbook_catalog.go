// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

// PlaybookCatalog returns the versioned, deterministic catalog of query
// playbooks. The catalog is the single source of truth for starter-prompt and
// cookbook workflows expressed as machine-readable, bounded tool sequences. The
// returned slice is freshly built on each call, so callers may not mutate shared
// state; ordering is stable and pinned by TestCatalogStabilityGolden.
//
// Every step references a first-class MCP tool (validated against the registry
// by the mcp package cross-check test), declares bounded parameters with default
// limits, and states an expected truth class from the AnswerPacket taxonomy. No
// step exposes raw Cypher. Each playbook declares the failure modes a caller
// must handle and the recommended fallback for each.
func PlaybookCatalog() []QueryPlaybook {
	return []QueryPlaybook{
		serviceStoryCitationPlaybook(),
		repositoryCodeTopicInvestigationPlaybook(),
		documentationTruthCitationPlaybook(),
		incidentContextEvidencePathPlaybook(),
		supplyChainImpactExplanationPlaybook(),
		secretsIAMTrustChainPosturePlaybook(),
		incrementalFreshnessReadinessPlaybook(),
		hostedOnboardingGovernanceStatusPlaybook(),
		changeSurfaceSourceInvestigationPlaybook(),
		queryToServiceContextPlaybook(),
		queryToCodeTopicContextPlaybook(),
		queryToIncidentContextPlaybook(),
		queryToSupplyChainContextPlaybook(),
		demoDeploymentToCloudResourcePlaybook(),
		demoDependencyCrossRepoPlaybook(),
		demoObservabilityToWorkloadPlaybook(),
	}
}

// serviceStoryCitationPlaybook answers "tell me the story of this service and
// cite the evidence". It pulls the one-call service dossier, then hydrates the
// returned evidence handles into a bounded citation packet.
func serviceStoryCitationPlaybook() QueryPlaybook {
	return QueryPlaybook{
		ID:           "service_story_citation",
		Name:         "Service story with citation packet",
		Version:      "1.0.0",
		PromptFamily: "service.story",
		Description: "Answer a service story prompt and back it with a bounded citation " +
			"packet built from the evidence handles the dossier returns.",
		RequiredInputs: []PlaybookInput{
			{
				Name:        "service_name",
				Type:        PlaybookInputIdentifier,
				Required:    true,
				Description: "Service name or canonical workload identifier to tell the story for.",
			},
			{
				Name:        "environment",
				Type:        PlaybookInputString,
				Required:    false,
				Description: "Optional environment context such as prod or staging.",
			},
		},
		Steps: []PlaybookStep{
			{
				ID:               "service_dossier",
				Tool:             "get_service_story",
				Params:           serviceStoryParams(),
				ExpectedTruth:    AnswerTruthDeterministic,
				EvidenceExpected: "one-call service dossier: identity, API surface, deployment lanes, dependencies, consumers, and addressable evidence handles",
				Drilldowns: []PlaybookDrilldown{
					{Tool: "get_service_context", Reason: "drill into raw service context when the dossier is not enough"},
					{Tool: "trace_deployment_chain", Reason: "walk the deployment graph when chain details are needed"},
				},
			},
			{
				ID:               "evidence_citations",
				Tool:             "build_evidence_citation_packet",
				Params:           []PlaybookParam{limitParam("limit", 10)},
				ExpectedTruth:    AnswerTruthCodeHint,
				EvidenceExpected: "ranked source, docs, manifest, and deployment citations hydrated from the dossier evidence handles",
				Drilldowns: []PlaybookDrilldown{
					{Tool: "get_relationship_evidence", Reason: "dereference durable source evidence for a specific relationship"},
				},
			},
		},
		FailureModes: serviceStoryFailureModes(),
	}
}

func serviceStoryParams() []PlaybookParam {
	return []PlaybookParam{
		inputParam("workload_id", "service_name"),
		inputParam("environment", "environment"),
	}
}

func serviceStoryFailureModes() []PlaybookFailureMode {
	return []PlaybookFailureMode{
		{
			Condition: "service not found",
			Meaning:   "no workload matched the service_name selector; the answer is unsupported",
			Fallback:  "call investigate_service to plan discovery across related repositories and deployment sources",
		},
		{
			Condition: "dossier returns no evidence handles",
			Meaning:   "the service exists but has no addressable evidence yet; citations cannot be hydrated",
			Fallback:  "present the dossier as a partial answer and call get_repository_coverage to check completeness",
		},
		{
			Condition: "citation packet truncated",
			Meaning:   "more handles existed than the limit allowed; the citation set is partial",
			Fallback:  "raise the limit (bounded by the tool maximum) or send the next handle batch to build_evidence_citation_packet",
		},
	}
}

// repositoryCodeTopicInvestigationPlaybook answers "how does this repository
// handle X" with ranked evidence and a relationship-story drilldown.
func repositoryCodeTopicInvestigationPlaybook() QueryPlaybook {
	return QueryPlaybook{
		ID:           "repository_code_topic_investigation",
		Name:         "Repository code-topic investigation with drilldown",
		Version:      "1.0.0",
		PromptFamily: "code.topic",
		Description: "Investigate a code topic within a repository, return ranked files and " +
			"symbols, then read the source behind the top evidence.",
		RequiredInputs: []PlaybookInput{
			{
				Name:        "topic",
				Type:        PlaybookInputString,
				Required:    true,
				Description: "Natural-language topic or behavior to investigate.",
			},
			{
				Name:        "repo_id",
				Type:        PlaybookInputIdentifier,
				Required:    false,
				Description: "Optional canonical repository identifier to scope the investigation.",
			},
		},
		Steps: []PlaybookStep{
			{
				ID:   "topic_investigation",
				Tool: "investigate_code_topic",
				Params: []PlaybookParam{
					inputParam("topic", "topic"),
					inputParam("repo_id", "repo_id"),
					constStringParam("intent", "explain_flow"),
					limitParam("limit", 25),
				},
				ExpectedTruth:    AnswerTruthCodeHint,
				EvidenceExpected: "ranked files and symbols with coverage metadata, truncation flag, and next-call handles for source reads",
				Drilldowns: []PlaybookDrilldown{
					{Tool: "find_symbol", Reason: "resolve a specific symbol surfaced in the ranked evidence"},
					{Tool: "search_file_content", Reason: "widen the search when ranked evidence is thin"},
				},
			},
			{
				ID:               "relationship_story",
				Tool:             "get_code_relationship_story",
				Params:           []PlaybookParam{limitParam("limit", 25)},
				ExpectedTruth:    AnswerTruthDeterministic,
				EvidenceExpected: "graph-backed relationship story for the top entity, explaining callers, callees, and ownership",
				Drilldowns: []PlaybookDrilldown{
					{Tool: "get_file_lines", Reason: "read the exact source lines behind a cited entity"},
				},
			},
		},
		FailureModes: codeTopicFailureModes(),
	}
}

func codeTopicFailureModes() []PlaybookFailureMode {
	return []PlaybookFailureMode{
		{
			Condition: "no evidence groups returned",
			Meaning:   "the topic matched nothing in scope; the answer is unsupported, not empty fact",
			Fallback:  "broaden by dropping repo_id or call search_file_content with a looser query",
		},
		{
			Condition: "investigation truncated",
			Meaning:   "more evidence existed than the limit allowed; ranking is partial",
			Fallback:  "page with the offset argument or raise the bounded limit before drilling down",
		},
		{
			Condition: "relationship story unavailable for the top entity",
			Meaning:   "the entity has content-index evidence but no graph relationships yet",
			Fallback:  "present the code-hint evidence as partial and call get_file_lines to read the source directly",
		},
	}
}

// documentationTruthCitationPlaybook answers "what do the docs say about X and
// is it still true" with a bounded documentation evidence packet plus a
// freshness check.
func documentationTruthCitationPlaybook() QueryPlaybook {
	return QueryPlaybook{
		ID:           "documentation_truth_citation",
		Name:         "Documentation truth with citation",
		Version:      "1.0.0",
		PromptFamily: "documentation.truth",
		Description: "Resolve a documentation finding into a bounded evidence packet and " +
			"confirm the packet is still current before citing it.",
		RequiredInputs: []PlaybookInput{
			{
				Name:        "finding_id",
				Type:        PlaybookInputIdentifier,
				Required:    true,
				Description: "Documentation finding identifier to cite.",
			},
		},
		Steps: []PlaybookStep{
			{
				ID:               "evidence_packet",
				Tool:             "get_documentation_evidence_packet",
				Params:           []PlaybookParam{inputParam("finding_id", "finding_id")},
				ExpectedTruth:    AnswerTruthSemanticObservation,
				EvidenceExpected: "bounded documentation evidence packet for the finding, with packet identifier and version",
				Drilldowns: []PlaybookDrilldown{
					{Tool: "list_documentation_findings", Reason: "list neighboring findings when the packet alone is insufficient"},
				},
			},
			{
				ID:               "freshness_check",
				Tool:             "check_documentation_evidence_packet_freshness",
				Params:           []PlaybookParam{},
				ExpectedTruth:    AnswerTruthDeterministic,
				EvidenceExpected: "current-or-stale verdict for the saved packet version so the citation is not stale",
				Drilldowns:       nil,
			},
		},
		FailureModes: documentationFailureModes(),
	}
}

func documentationFailureModes() []PlaybookFailureMode {
	return []PlaybookFailureMode{
		{
			Condition: "finding not found",
			Meaning:   "no documentation finding matched finding_id; the answer is unsupported",
			Fallback:  "call list_documentation_findings to rediscover the correct finding identifier",
		},
		{
			Condition: "packet stale on freshness check",
			Meaning:   "the cited packet version no longer reflects current documentation truth",
			Fallback:  "re-fetch get_documentation_evidence_packet and cite the refreshed packet version",
		},
	}
}
