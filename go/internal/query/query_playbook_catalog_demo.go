// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

// Demo first-five-questions catalog entries (issue #4745).
//
// specs/demo-first-answers.v1.yaml is the acceptance oracle: it pins each of
// the five demo questions to a bounded call on an existing read surface. Two
// questions reuse an already-shipped playbook verbatim rather than forking its
// semantics: q1_code_to_deployment resolves through serviceStoryCitationPlaybook
// (service_story_citation) and q3_incident_to_service resolves through
// incidentContextEvidencePathPlaybook (incident_context_evidence_path), both
// declared elsewhere in this package. The three playbooks in this file give the
// remaining questions a catalog identity so `eshu playbooks resolve <id>` and
// the MCP/API surfaces can list and resolve them the same way as any other
// playbook; each wraps exactly one existing, already-registered MCP tool with
// the bounded arguments the manifest requires and introduces no new query
// capability. They are registered in PlaybookCatalog (query_playbook_catalog.go)
// alongside every other wave.

// demoDeploymentToCloudResourcePlaybook answers demo question
// q2_deployment_to_cloud_resource: "which cloud-managed image does the api-svc
// Kubernetes workload run, and what correlated it there?" It resolves through
// list_kubernetes_correlations scoped to a cluster, per
// specs/demo-first-answers.v1.yaml notes: list_kubernetes_correlations is
// pinned over trace_deployment_chain because the golden-corpus-gate run
// returned an empty cloud_resources for trace_deployment_chain while this tool
// returned the digest-joined workload -> image correlation (rc-4).
func demoDeploymentToCloudResourcePlaybook() QueryPlaybook {
	return QueryPlaybook{
		ID:           "demo_deployment_to_cloud_resource",
		Name:         "Demo: deployment to cloud resource",
		Version:      "1.0.0",
		PromptFamily: "demo.deployment_to_cloud_resource",
		Description: "Answer the demo deployment-to-cloud-resource question by listing the " +
			"reducer-owned Kubernetes workload -> image correlations for a cluster.",
		RequiredInputs: []PlaybookInput{
			{
				Name:        "cluster_id",
				Type:        PlaybookInputIdentifier,
				Required:    true,
				Description: "Cluster ID to anchor the Kubernetes correlation lookup, e.g. supply-chain-demo.",
			},
		},
		Steps: []PlaybookStep{
			{
				ID:   "kubernetes_correlations",
				Tool: "list_kubernetes_correlations",
				Params: []PlaybookParam{
					inputParam("cluster_id", "cluster_id"),
					limitParam("limit", 50),
				},
				ExpectedTruth:    AnswerTruthDerived,
				EvidenceExpected: "digest-joined Kubernetes workload -> OCI image correlations for the cluster, with outcome and join_mode per row",
				Drilldowns: []PlaybookDrilldown{
					{Tool: "get_service_story", Reason: "drill into the owning service dossier when a workload correlation is selected"},
				},
			},
		},
		FailureModes: demoFailureModes("deployment-to-cloud-resource correlation"),
	}
}

// demoDependencyCrossRepoPlaybook answers demo question
// q4_dependency_cross_repo: "which repository depends on
// github.com/acme/lib-common, and how was that dependency declared and
// resolved?" It resolves through list_package_registry_correlations scoped to
// the package, per specs/demo-first-answers.v1.yaml notes: this question was
// evaluated as dependency -> vulnerability and rejected because the corpus
// carries advisory evidence with no CVE-to-component impact finding, so it is
// pinned to the cross-repo dependency correlation the corpus actually proves
// (rc-3).
func demoDependencyCrossRepoPlaybook() QueryPlaybook {
	return QueryPlaybook{
		ID:           "demo_dependency_cross_repo",
		Name:         "Demo: cross-repo dependency",
		Version:      "1.0.0",
		PromptFamily: "demo.dependency_cross_repo",
		Description: "Answer the demo cross-repo dependency question by listing the reducer-owned " +
			"package consumption correlations for a package.",
		RequiredInputs: []PlaybookInput{
			{
				Name:        "package_id",
				Type:        PlaybookInputIdentifier,
				Required:    true,
				Description: "Package.uid to anchor the package registry correlation lookup, e.g. github.com/acme/lib-common.",
			},
		},
		Steps: []PlaybookStep{
			{
				ID:   "package_registry_correlations",
				Tool: "list_package_registry_correlations",
				Params: []PlaybookParam{
					inputParam("package_id", "package_id"),
					limitParam("limit", 50),
				},
				ExpectedTruth:    AnswerTruthDerived,
				EvidenceExpected: "manifest-backed consumption correlations naming the repository that depends on the package, with relationship_kind and outcome per row",
				Drilldowns: []PlaybookDrilldown{
					{Tool: "list_package_registry_dependencies", Reason: "inspect the underlying package-native dependency edges when consumption alone is not enough"},
				},
			},
		},
		FailureModes: demoFailureModes("cross-repo dependency correlation"),
	}
}

// demoObservabilityToWorkloadPlaybook answers demo question
// q5_observability_to_workload: "which workload does the tempo trace coverage
// in the demo org correlate to, and how fresh is that coverage?" It resolves
// through list_observability_coverage_correlations scoped to the tempo
// provider, matching the HTTP route
// GET /api/v0/observability/coverage/correlations?provider=tempo&limit=50 that
// specs/demo-first-answers.v1.yaml pins for this question.
func demoObservabilityToWorkloadPlaybook() QueryPlaybook {
	return QueryPlaybook{
		ID:           "demo_observability_to_workload",
		Name:         "Demo: observability to workload",
		Version:      "1.0.0",
		PromptFamily: "demo.observability_to_workload",
		Description: "Answer the demo observability-to-workload question by listing the " +
			"reducer-owned observability coverage correlations for a provider.",
		RequiredInputs: []PlaybookInput{
			{
				Name:        "provider",
				Type:        PlaybookInputString,
				Required:    true,
				Description: "Observability provider to scope coverage correlation lookup, e.g. tempo.",
			},
		},
		Steps: []PlaybookStep{
			{
				ID:   "observability_coverage_correlations",
				Tool: "list_observability_coverage_correlations",
				Params: []PlaybookParam{
					inputParam("provider", "provider"),
					limitParam("limit", 50),
				},
				ExpectedTruth:    AnswerTruthDerived,
				EvidenceExpected: "observability coverage correlations naming the covered workload or resource, with coverage_status and freshness per row",
				Drilldowns: []PlaybookDrilldown{
					{Tool: "get_service_story", Reason: "drill into the owning service dossier when a covered workload is selected"},
				},
			},
		},
		FailureModes: demoFailureModes("observability-to-workload correlation"),
	}
}

// demoFailureModes declares the shared failure modes for the demo single-step
// catalog entries: each wraps exactly one bounded list call, so the failure
// surface is empty result, truncation, or an unready reducer generation.
func demoFailureModes(scope string) []PlaybookFailureMode {
	return []PlaybookFailureMode{
		{
			Condition: "no correlations returned",
			Meaning:   scope + " matched no rows in scope; the answer is unsupported, not an empty fact",
			Fallback:  "confirm the selector (cluster_id, package_id, or provider) matches the demo corpus and retry with a broader scope",
		},
		{
			Condition: "result truncated",
			Meaning:   scope + " returned more rows than the bounded limit allowed",
			Fallback:  "page with after_correlation_id or raise the bounded limit before drawing a final conclusion",
		},
		{
			Condition: "reducer generation not yet converged",
			Meaning:   scope + " may be incomplete while the reducer is still materializing this correlation family",
			Fallback:  "check get_generation_lifecycle before citing the answer as final",
		},
	}
}
