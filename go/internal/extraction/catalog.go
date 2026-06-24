// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package extraction

import "github.com/eshu-hq/eshu/go/internal/scope"

// coreCriteria marks every criterion not-applicable: a correlation-critical core
// collector is not evaluated against the SDK/trust/runtime extraction criteria
// because it stays in tree by policy.
func coreCriteria() []CriterionResult {
	out := make([]CriterionResult, 0, len(orderedCriteria))
	for _, criterion := range orderedCriteria {
		out = append(out, CriterionResult{
			Criterion: criterion,
			State:     NotApplicable,
			Detail:    "core collector kept in tree; not evaluated for extraction",
		})
	}
	return out
}

// provenCriteria marks every criterion met, used for a family that has completed
// the full out-of-tree reference proof.
func provenCriteria(detail string) []CriterionResult {
	out := make([]CriterionResult, 0, len(orderedCriteria))
	for _, criterion := range orderedCriteria {
		out = append(out, CriterionResult{Criterion: criterion, State: Met, Detail: detail})
	}
	return out
}

// candidateNotStartedCriteria describes an in-tree vendor-API collector whose
// source-level criteria hold but whose out-of-tree execution criteria (trust
// boundary, hosted runtime, and proof surface) are unmet because no component
// package, hosted worker, or extraction proof exists for it yet.
func candidateNotStartedCriteria() []CriterionResult {
	const inTree = "in-tree vendor-API collector emitting documented source facts with durable scope and generation"
	const notStarted = "no out-of-tree component package, hosted component-extension worker, or extraction proof exists for this family yet"
	return []CriterionResult{
		{Criterion: SourceCoupling, State: Met, Detail: inTree},
		{Criterion: FactContract, State: Met, Detail: inTree},
		{Criterion: ScopeGeneration, State: Met, Detail: inTree},
		{Criterion: TrustBoundary, State: Unmet, Detail: notStarted},
		{Criterion: RuntimeBehavior, State: Unmet, Detail: notStarted},
		{Criterion: ReleaseCadence, State: Met, Detail: "vendor API churn is independent enough to benefit from a separate release cadence"},
		{Criterion: ProofSurface, State: Unmet, Detail: notStarted},
	}
}

// catalog is the evidence-based set of collector-family profiles. It mirrors the
// "Keep In Tree" and "Extraction Candidates" lists in
// docs/public/reference/collector-extraction-policy.md. Entries are authored
// from documented repo evidence, so the classifications stay reproducible and
// reviewable; the policy doc is the single source of truth for membership.
var catalog = []Profile{
	{Family: scope.CollectorGit, DisplayName: "Git", CorrelationCritical: true, Criteria: coreCriteria()},
	{Family: scope.CollectorTerraformState, DisplayName: "Terraform state", CorrelationCritical: true, Criteria: coreCriteria()},
	{Family: scope.CollectorAWS, DisplayName: "AWS cloud inventory", CorrelationCritical: true, Criteria: coreCriteria()},
	{Family: scope.CollectorGCP, DisplayName: "GCP cloud inventory", CorrelationCritical: true, Criteria: coreCriteria()},
	{Family: scope.CollectorAzure, DisplayName: "Azure cloud inventory", CorrelationCritical: true, Criteria: coreCriteria()},
	{Family: scope.CollectorKubernetesLive, DisplayName: "Kubernetes live", CorrelationCritical: true, Criteria: coreCriteria()},
	{
		Family:                scope.CollectorPagerDuty,
		DisplayName:           "PagerDuty incident routing",
		BoundaryProofComplete: true,
		Extracted:             false,
		Criteria:              provenCriteria("PagerDuty reference component-extension boundary proof is complete and tracked by tests, Compose proof, and proof scripts"),
		Rationale:             "The in-tree PagerDuty collector stays the production correlation path until the broader incident-routing surface lands; see the collector extraction policy.",
	},
	{Family: scope.CollectorJira, DisplayName: "Jira work items", Criteria: candidateNotStartedCriteria()},
	{Family: scope.CollectorDocumentation, DisplayName: "Confluence / documentation", Criteria: candidateNotStartedCriteria()},
	{Family: scope.CollectorGrafana, DisplayName: "Grafana observability", Criteria: candidateNotStartedCriteria()},
	{Family: scope.CollectorLoki, DisplayName: "Loki observability", Criteria: candidateNotStartedCriteria()},
	{Family: scope.CollectorTempo, DisplayName: "Tempo observability", Criteria: candidateNotStartedCriteria()},
	{Family: scope.CollectorPrometheusMimir, DisplayName: "Prometheus / Mimir observability", Criteria: candidateNotStartedCriteria()},
	{Family: scope.CollectorVulnerabilityIntelligence, DisplayName: "Vulnerability intelligence", Criteria: candidateNotStartedCriteria()},
}

// Catalog returns the advisory extraction-readiness verdict for every collector
// family the extraction policy tracks, sorted for stable presentation. The
// result is read-only and safe to mutate by the caller.
func Catalog() []Readiness {
	rows := make([]Readiness, 0, len(catalog))
	for _, profile := range catalog {
		rows = append(rows, Evaluate(profile))
	}
	SortReadiness(rows)
	return rows
}

// Lookup returns the advisory readiness for a single collector family. The
// boolean is false when the family is not tracked by the extraction policy.
func Lookup(family scope.CollectorKind) (Readiness, bool) {
	for _, profile := range catalog {
		if profile.Family == family {
			return Evaluate(profile), true
		}
	}
	return Readiness{}, false
}
