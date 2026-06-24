// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

// Third-wave "query-to-context" playbooks start from semantic search as
// read-only context discovery and then bridge into bounded readbacks (service
// story, citation, impact, evidence). The search step opts into
// graph-neighborhood reranking so its recommended_next_calls drive the readback
// steps; the playbook never infers graph truth from retrieval alone.

// queryToContextInputs declares the inputs shared by every query-to-context
// playbook: a required repository scope and query, plus optional anchors the
// search step uses to bound and rerank results.
func queryToContextInputs() []PlaybookInput {
	return []PlaybookInput{
		{
			Name:        "repo_id",
			Type:        PlaybookInputIdentifier,
			Required:    true,
			Description: "Repository id that bounds the searchable corpus.",
		},
		{
			Name:        "query",
			Type:        PlaybookInputString,
			Required:    true,
			Description: "Natural-language question to discover context for.",
		},
		{
			Name:        "service_id",
			Type:        PlaybookInputIdentifier,
			Required:    false,
			Description: "Optional service anchor to bound and rerank the search around.",
		},
		{
			Name:        "workload_id",
			Type:        PlaybookInputIdentifier,
			Required:    false,
			Description: "Optional workload anchor inside the repository corpus.",
		},
		{
			Name:        "environment",
			Type:        PlaybookInputString,
			Required:    false,
			Description: "Optional environment anchor such as prod or staging.",
		},
	}
}

// queryToContextSearchStep is the shared first step: bounded, reranked,
// read-only semantic discovery whose recommended_next_calls parametrize the
// following readbacks.
func queryToContextSearchStep() PlaybookStep {
	return PlaybookStep{
		ID:   "semantic_search",
		Tool: "search_semantic_context",
		Params: []PlaybookParam{
			inputParam("repo_id", "repo_id"),
			inputParam("query", "query"),
			inputParam("service_id", "service_id"),
			inputParam("workload_id", "workload_id"),
			inputParam("environment", "environment"),
			constStringParam("mode", "hybrid"),
			limitParam("limit", 10),
			limitParam("timeout_ms", 2000),
			boolParam("rerank", true),
		},
		ExpectedTruth: AnswerTruthDerived,
		EvidenceExpected: "ranked curated context with graph handles, per-result ranking basis, and " +
			"recommended_next_calls; read-only discovery that never asserts graph truth",
		Drilldowns: []PlaybookDrilldown{
			{Tool: "get_semantic_capability_status", Reason: "confirm semantic readiness when results are empty or degraded"},
		},
	}
}

// queryToContextFailureModes declares the failure modes a query-to-context
// caller must handle: missing search readiness, no hits, stale vectors, an
// ambiguous readback target, and truncation.
func queryToContextFailureModes() []PlaybookFailureMode {
	return []PlaybookFailureMode{
		{
			Condition: "semantic search not ready (semantic_unavailable or index_unready)",
			Meaning:   "no governed embedder or vector index answered; only keyword discovery is available",
			Fallback:  "retry the search step in keyword mode and treat the result as lexical-only discovery",
		},
		{
			Condition: "no search hits in scope",
			Meaning:   "the query matched nothing in the bounded corpus; there is no context to hand off, not an empty fact",
			Fallback:  "broaden the query, drop source_kinds, or widen the repository scope before any readback",
		},
		{
			Condition: "stale vectors (vector_index_stale)",
			Meaning:   "the vector index is behind the active generation; semantic ranking may be outdated",
			Fallback:  "use keyword mode or wait for reprojection, and check get_semantic_capability_status",
		},
		{
			Condition: "ambiguous target across multiple candidates",
			Meaning:   "the top results point at more than one service or workload, so the readback target is not unique",
			Fallback:  "narrow with service_id or pick a single recommended_next_call before the readback step",
		},
		{
			Condition: "search or readback result truncated",
			Meaning:   "more results existed than the limit allowed; ranking and handoff are partial",
			Fallback:  "raise the bounded limit or page before drilling into a readback",
		},
		{
			Condition: "readback target not yet resolved from search",
			Meaning:   "the readback step is a template: its target comes from the search step's recommended_next_calls or a supplied anchor, not from the playbook alone",
			Fallback:  "run the semantic_search step first and parametrize the readback from its recommended_next_calls, or supply service_id/workload_id up front",
		},
	}
}

// queryToServiceContextPlaybook answers "find the service behind this question
// and tell its story" by searching, then resolving the top service handle into
// a dossier and a citation packet.
func queryToServiceContextPlaybook() QueryPlaybook {
	return QueryPlaybook{
		ID:           "query_to_service_context",
		Name:         "Query to service context",
		Version:      "1.0.0",
		PromptFamily: "query.service_context",
		Description: "Discover context with semantic search, then resolve the top service the " +
			"search recommends into a dossier and cite the evidence.",
		RequiredInputs: queryToContextInputs(),
		Steps: []PlaybookStep{
			queryToContextSearchStep(),
			{
				ID:   "service_story",
				Tool: "get_service_story",
				Params: []PlaybookParam{
					inputParam("workload_id", "workload_id"),
					inputParam("environment", "environment"),
				},
				ExpectedTruth: AnswerTruthDeterministic,
				EvidenceExpected: "service dossier for the service in the search step's recommended_next_calls: " +
					"identity, deployment lanes, dependencies, and evidence handles",
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
				EvidenceExpected: "ranked citations hydrated from the dossier evidence handles",
				Drilldowns: []PlaybookDrilldown{
					{Tool: "get_relationship_evidence", Reason: "dereference durable source evidence for a specific relationship"},
				},
			},
		},
		FailureModes: queryToContextFailureModes(),
	}
}

// queryToCodeTopicContextPlaybook answers "how does this repository handle X"
// by searching, then ranking the code topic and reading the relationship story.
func queryToCodeTopicContextPlaybook() QueryPlaybook {
	return QueryPlaybook{
		ID:           "query_to_code_topic_context",
		Name:         "Query to code-topic context",
		Version:      "1.0.0",
		PromptFamily: "query.code_topic_context",
		Description: "Discover context with semantic search, then rank the code topic and read " +
			"the graph-backed relationship story behind the top entity.",
		RequiredInputs: queryToContextInputs(),
		Steps: []PlaybookStep{
			queryToContextSearchStep(),
			{
				ID:   "topic_investigation",
				Tool: "investigate_code_topic",
				Params: []PlaybookParam{
					inputParam("topic", "query"),
					inputParam("repo_id", "repo_id"),
					constStringParam("intent", "explain_flow"),
					limitParam("limit", 25),
				},
				ExpectedTruth:    AnswerTruthCodeHint,
				EvidenceExpected: "ranked files and symbols for the query, with coverage and next-call handles",
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
				EvidenceExpected: "graph-backed relationship story for the top entity: callers, callees, and ownership",
				Drilldowns: []PlaybookDrilldown{
					{Tool: "get_file_lines", Reason: "read the exact source lines behind a cited entity"},
				},
			},
		},
		FailureModes: queryToContextFailureModes(),
	}
}

// queryToIncidentContextPlaybook answers "what incident does this question
// relate to" by searching, then resolving the incident handle into context and
// a citation packet.
func queryToIncidentContextPlaybook() QueryPlaybook {
	return QueryPlaybook{
		ID:           "query_to_incident_context",
		Name:         "Query to incident context",
		Version:      "1.0.0",
		PromptFamily: "query.incident_context",
		Description: "Discover context with semantic search, then resolve the incident the search " +
			"recommends into bounded incident context and cite the evidence.",
		RequiredInputs: queryToContextInputs(),
		Steps: []PlaybookStep{
			queryToContextSearchStep(),
			{
				ID:               "incident_context",
				Tool:             "get_incident_context",
				Params:           []PlaybookParam{limitParam("limit", 25)},
				ExpectedTruth:    AnswerTruthDerived,
				EvidenceExpected: "bounded incident context for the incident in the search step's recommended_next_calls, with linked evidence",
				Drilldowns: []PlaybookDrilldown{
					{Tool: "get_service_story", Reason: "drill into the impacted service when one is selected"},
				},
			},
			{
				ID:               "evidence_citations",
				Tool:             "build_evidence_citation_packet",
				Params:           []PlaybookParam{limitParam("limit", 10)},
				ExpectedTruth:    AnswerTruthCodeHint,
				EvidenceExpected: "ranked citations hydrated from the incident-context evidence handles",
				Drilldowns:       nil,
			},
		},
		FailureModes: queryToContextFailureModes(),
	}
}

// queryToSupplyChainContextPlaybook answers "what is the supply-chain impact
// behind this question" by searching, then explaining the package or image
// impact and citing the evidence.
func queryToSupplyChainContextPlaybook() QueryPlaybook {
	return QueryPlaybook{
		ID:           "query_to_supply_chain_context",
		Name:         "Query to supply-chain context",
		Version:      "1.0.0",
		PromptFamily: "query.supply_chain_context",
		Description: "Discover context with semantic search, then explain the supply-chain impact " +
			"for the package or image the search recommends and cite the evidence.",
		RequiredInputs: queryToContextInputs(),
		Steps: []PlaybookStep{
			queryToContextSearchStep(),
			{
				ID:            "supply_chain_impact",
				Tool:          "explain_supply_chain_impact",
				Params:        []PlaybookParam{limitParam("limit", 25)},
				ExpectedTruth: AnswerTruthDerived,
				EvidenceExpected: "supply-chain impact for the package or image in the search step's recommended_next_calls, " +
					"separating provider observations from Eshu-derived state",
				Drilldowns: []PlaybookDrilldown{
					{Tool: "list_supply_chain_impact_findings", Reason: "list neighboring findings when the target is ambiguous"},
				},
			},
			{
				ID:               "evidence_citations",
				Tool:             "build_evidence_citation_packet",
				Params:           []PlaybookParam{limitParam("limit", 10)},
				ExpectedTruth:    AnswerTruthCodeHint,
				EvidenceExpected: "ranked citations hydrated from the supply-chain impact evidence handles",
				Drilldowns:       nil,
			},
		},
		FailureModes: queryToContextFailureModes(),
	}
}
