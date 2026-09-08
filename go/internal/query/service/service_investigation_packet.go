// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package service

import (
	"fmt"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// This file hosts the pure service-investigation packet builders behind the
// service investigation route and the service-story overview (Issue #6060,
// lane B B4). They moved here from the query root (service_investigation.go),
// whose *EntityHandler route method must stay in package query: Go requires
// methods to live with their receiver type. The staying route method keeps
// serving the same packet through the exported BuildServiceInvestigationPacket
// home. Bodies are unchanged modulo package qualifiers and the export renames
// below.

// ServiceInvestigationOptions tunes the service investigation packet.
// Pinned by the staying investigation route (service_investigation.go) via
// the root alias, and by the service-story overview in this package.
type ServiceInvestigationOptions struct {
	Environment string
	Intent      string
	Question    string
}

// BuildServiceInvestigationPacket builds the service investigation packet
// for a resolved workload context. Pinned by the staying investigation
// route method.
func BuildServiceInvestigationPacket(
	serviceName string,
	workloadContext map[string]any,
	opts ServiceInvestigationOptions,
) map[string]any {
	return buildServiceInvestigationPacketWithContext(serviceName, newServiceStoryBuildContext(workloadContext), opts)
}

func buildServiceInvestigationPacketWithContext(
	serviceName string,
	buildCtx serviceStoryBuildContext,
	opts ServiceInvestigationOptions,
) map[string]any {
	workloadContext := buildCtx.workloadContext
	serviceName = canonicalServiceName(serviceName, workloadContext)
	evidenceFamilies := serviceInvestigationEvidenceFamiliesWithContext(buildCtx)
	repositories := serviceInvestigationRepositories(workloadContext, evidenceFamilies)
	withEvidence := repositoriesWithEvidence(repositories)
	findings := serviceInvestigationFindingsWithContext(buildCtx, evidenceFamilies)
	nextCalls := serviceInvestigationNextCalls(serviceName, workloadContext)
	coverage := serviceInvestigationCoverage(workloadContext, repositories, withEvidence, evidenceFamilies)

	packet := map[string]any{
		"service_name":               serviceName,
		"repositories_considered":    repositories,
		"repositories_with_evidence": withEvidence,
		"evidence_families_found":    evidenceFamilies,
		"coverage_summary":           coverage,
		"investigation_findings":     findings,
		"recommended_next_calls":     nextCalls,
		"service_story_path":         "/api/v0/services/" + serviceName + "/story",
		"service_context_path":       "/api/v0/services/" + serviceName + "/context",
	}
	if opts.Environment != "" {
		packet["environment"] = opts.Environment
	}
	if opts.Intent != "" {
		packet["intent"] = opts.Intent
	}
	if opts.Question != "" {
		packet["question"] = opts.Question
	}
	return packet
}

func serviceInvestigationEvidenceFamiliesWithContext(buildCtx serviceStoryBuildContext) []string {
	workloadContext := buildCtx.workloadContext
	families := make([]string, 0, 6)
	if buildCtx.hasAPISurface &&
		(querycontract.IntVal(buildCtx.apiSurface, "endpoint_count") > 0 || querycontract.IntVal(buildCtx.apiSurface, "spec_count") > 0 ||
			len(querycontract.MapSliceValue(buildCtx.apiSurface, "endpoints")) > 0) {
		families = append(families, "api_surface")
	}
	if len(querycontract.MapSliceValue(workloadContext, "instances")) > 0 || len(serviceDeploymentArtifacts(workloadContext)) > 0 {
		families = append(families, "deployment_lanes")
	}
	if len(querycontract.MapValue(workloadContext, "documentation_overview")) > 0 {
		families = append(families, "documentation")
	}
	if len(querycontract.MapSliceValue(workloadContext, "dependencies")) > 0 ||
		len(querycontract.MapSliceValue(workloadContext, "provisioning_source_chains")) > 0 ||
		len(serviceDeploymentArtifacts(workloadContext)) > 0 {
		families = append(families, "upstream_dependencies")
	}
	if len(querycontract.MapSliceValue(workloadContext, "dependents")) > 0 ||
		len(querycontract.MapSliceValue(workloadContext, "consumer_repositories")) > 0 {
		families = append(families, "downstream_consumers")
	}
	if len(querycontract.MapValue(workloadContext, "support_overview")) > 0 {
		families = append(families, "support")
	}
	sort.Strings(families)
	return families
}

func serviceInvestigationRepositories(workloadContext map[string]any, evidenceFamilies []string) []map[string]any {
	repos := map[string]map[string]any{}
	serviceRepoID := querycontract.SafeStr(workloadContext, "repo_id")
	addInvestigationRepo(repos, serviceRepoID, querycontract.SafeStr(workloadContext, "repo_name"), "service_owner", evidenceFamilies...)

	for _, artifact := range serviceDeploymentArtifacts(workloadContext) {
		addInvestigationRepo(
			repos,
			querycontract.StringVal(artifact, "source_repo_id"),
			querycontract.StringVal(artifact, "source_repo_name"),
			"deployment_source",
			querycontract.FirstNonEmptyString(querycontract.StringVal(artifact, "artifact_family"), querycontract.StringVal(artifact, "relationship_type")),
		)
		addInvestigationRepo(
			repos,
			querycontract.StringVal(artifact, "target_repo_id"),
			querycontract.StringVal(artifact, "target_repo_name"),
			"deployment_target",
			querycontract.FirstNonEmptyString(querycontract.StringVal(artifact, "artifact_family"), querycontract.StringVal(artifact, "relationship_type")),
		)
	}
	for _, dependent := range querycontract.MapSliceValue(workloadContext, "dependents") {
		addInvestigationRepo(repos, querycontract.StringVal(dependent, "repo_id"), querycontract.StringVal(dependent, "repository"), "graph_dependent", "downstream_consumers")
	}
	for _, consumer := range querycontract.MapSliceValue(workloadContext, "consumer_repositories") {
		addInvestigationRepo(repos, querycontract.StringVal(consumer, "repo_id"), querycontract.StringVal(consumer, "repository"), "content_consumer", "downstream_consumers")
	}
	for _, chain := range querycontract.MapSliceValue(workloadContext, "provisioning_source_chains") {
		addInvestigationRepo(repos, querycontract.StringVal(chain, "repo_id"), querycontract.StringVal(chain, "repository"), "provisioning_source", "upstream_dependencies")
	}

	rows := make([]map[string]any, 0, len(repos))
	for _, repo := range repos {
		sortStringFields(repo, "roles", "evidence_families")
		rows = append(rows, repo)
	}
	sort.Slice(rows, func(i, j int) bool {
		return querycontract.StringVal(rows[i], "repo_id") < querycontract.StringVal(rows[j], "repo_id")
	})
	capped, truncated := CapMapRows(rows, serviceStoryItemLimit)
	if truncated {
		capped = append(capped, map[string]any{
			"repo_id": serviceInvestigationTruncationMarkerID,
			"note":    fmt.Sprintf("repository list truncated at %d rows", serviceStoryItemLimit),
		})
	}
	return capped
}

const serviceInvestigationTruncationMarkerID = "__truncated__"

func addInvestigationRepo(repos map[string]map[string]any, repoID string, repoName string, role string, families ...string) {
	repoID = strings.TrimSpace(repoID)
	repoName = strings.TrimSpace(repoName)
	if repoID == "" && repoName == "" {
		return
	}
	key := repoID
	if key == "" {
		key = repoName
	}
	row := repos[key]
	if row == nil {
		row = map[string]any{
			"repo_id":           repoID,
			"repo_name":         repoName,
			"roles":             []string{},
			"evidence_families": []string{},
		}
		repos[key] = row
	}
	addUniqueStringField(row, "roles", role)
	for _, family := range families {
		addUniqueStringField(row, "evidence_families", family)
	}
	row["evidence_family_count"] = len(querycontract.StringSliceVal(row, "evidence_families"))
}

func repositoriesWithEvidence(repositories []map[string]any) []map[string]any {
	withEvidence := make([]map[string]any, 0, len(repositories))
	for _, repo := range repositories {
		if querycontract.StringVal(repo, "repo_id") == serviceInvestigationTruncationMarkerID {
			continue
		}
		if len(querycontract.StringSliceVal(repo, "evidence_families")) == 0 {
			continue
		}
		withEvidence = append(withEvidence, repo)
	}
	return withEvidence
}

func serviceInvestigationFindingsWithContext(buildCtx serviceStoryBuildContext, evidenceFamilies []string) []map[string]any {
	findings := make([]map[string]any, 0, len(evidenceFamilies))
	for _, family := range evidenceFamilies {
		findings = append(findings, map[string]any{
			"family":        family,
			"summary":       serviceInvestigationFamilySummaryWithContext(buildCtx, family),
			"evidence_path": serviceInvestigationEvidencePath(family),
		})
	}
	return findings
}

func serviceInvestigationFamilySummaryWithContext(buildCtx serviceStoryBuildContext, family string) string {
	workloadContext := buildCtx.workloadContext
	switch family {
	case "api_surface":
		return fmt.Sprintf("%d endpoint(s) across %d spec file(s)", querycontract.IntVal(buildCtx.apiSurface, "endpoint_count"), querycontract.IntVal(buildCtx.apiSurface, "spec_count"))
	case "deployment_lanes":
		return fmt.Sprintf("%d runtime instance(s), %d deployment artifact(s)", len(querycontract.MapSliceValue(workloadContext, "instances")), len(serviceDeploymentArtifacts(workloadContext)))
	case "documentation":
		return "indexed documentation metadata is available for the service repository"
	case "downstream_consumers":
		return serviceInvestigationBoundedSummary(
			fmt.Sprintf("%d graph dependent(s), %d content consumer repo(s)", len(querycontract.MapSliceValue(workloadContext, "dependents")), len(querycontract.MapSliceValue(workloadContext, "consumer_repositories"))),
			querycontract.BoolVal(workloadContext, "dependents_truncated") || querycontract.BoolVal(workloadContext, "consumer_repositories_truncated"),
		)
	case "upstream_dependencies":
		return serviceInvestigationBoundedSummary(
			fmt.Sprintf("%d dependency row(s), %d provisioning chain(s)", len(querycontract.MapSliceValue(workloadContext, "dependencies")), len(querycontract.MapSliceValue(workloadContext, "provisioning_source_chains"))),
			querycontract.BoolVal(workloadContext, "provisioning_source_chains_truncated"),
		)
	case "support":
		return "support metadata is available for the service"
	default:
		return "evidence family is present"
	}
}

// serviceInvestigationBoundedSummary marks a family summary whose counts came
// from a bounded read.
//
// #5720 round-8 P3-2: round 7 fixed only half of this. coverage_summary gained
// a truncated flag, but the human-readable findings[].summary next to it still
// rendered a 40-dependent service as "25 graph dependent(s), 0 content consumer
// repo(s)" -- a bare number that reads as the whole population. An operator
// scanning the findings list, which is the part written to be read rather than
// parsed, had nothing telling them the number was a ceiling.
//
// The flag is chosen per family rather than OR-ing all three signals: the
// downstream families and the upstream families are fed by different bounds,
// and consumer_repositories_truncated can fire from sources that never touch
// provisioning_source_chains. Marking a list that was not bounded would be its
// own false claim.
func serviceInvestigationBoundedSummary(summary string, truncated bool) string {
	if !truncated {
		return summary
	}
	return summary + " (bounded)"
}

func serviceInvestigationEvidencePath(family string) string {
	return map[string]string{
		"api_surface":           "api_surface",
		"deployment_lanes":      "deployment_evidence.artifacts",
		"documentation":         "documentation_overview",
		"downstream_consumers":  "dependents, consumer_repositories",
		"upstream_dependencies": "dependencies, provisioning_source_chains, deployment_evidence.artifacts",
		"support":               "support_overview",
	}[family]
}

func serviceInvestigationNextCalls(serviceName string, workloadContext map[string]any) []map[string]any {
	nextCalls := []map[string]any{
		{
			"tool":   "get_service_story",
			"reason": "retrieve the full one-call dossier for answer generation",
			"arguments": map[string]any{
				"workload_id": serviceName,
			},
		},
		{
			"tool":   "get_service_context",
			"reason": "drill into raw service context when the dossier is not enough",
			"arguments": map[string]any{
				"workload_id": serviceName,
			},
		},
		{
			"tool":   "trace_deployment_chain",
			"reason": "walk the deployment graph only when chain details are needed",
			"arguments": map[string]any{
				"service_name":                 serviceName,
				"include_related_module_usage": true,
			},
		},
	}
	for _, artifact := range serviceDeploymentArtifacts(workloadContext) {
		resolvedID := querycontract.StringVal(artifact, "resolved_id")
		if resolvedID == "" {
			continue
		}
		nextCalls = append(nextCalls, map[string]any{
			"tool":   "get_relationship_evidence",
			"reason": "dereference the durable source evidence for a relationship",
			"arguments": map[string]any{
				"resolved_id": resolvedID,
			},
		})
		if len(nextCalls) >= 8 {
			break
		}
	}
	return nextCalls
}

func serviceInvestigationCoverage(
	workloadContext map[string]any,
	repositories []map[string]any,
	withEvidence []map[string]any,
	evidenceFamilies []string,
) map[string]any {
	state := "unknown"
	reason := "no cross-repository evidence families were found"
	if len(evidenceFamilies) > 0 {
		state = "partial"
		reason = "evidence was found, but the index cannot prove exhaustive coverage across every related repository"
	}
	if len(querycontract.StringSliceVal(workloadContext, "limitations")) > 0 ||
		querycontract.StringVal(workloadContext, "materialization_status") == "identity_only" {
		state = "partial"
		reason = "service materialization reports limitations"
	}
	// #5720 round-7 P1-1: serviceInvestigationRepositoriesTruncated only fires
	// once the merged repository list exceeds serviceStoryItemLimit (50), but
	// the reads that feed dependents, consumer_repositories, and
	// provisioning_source_chains are bounded well below that by
	// DefaultIndirectEvidenceSearchLimit (25) -- so a 40-dependent service
	// reported "25 graph dependent(s)" with truncated: false, identical to the
	// pre-fix behavior. The three *_truncated signals
	// service/service_query_enrichment.go sets from
	// impacttrace.QueryProvisioningRepositoryCandidates are the only thing
	// that makes that bound observable on this route.
	upstreamTruncated := querycontract.BoolVal(workloadContext, "dependents_truncated") ||
		querycontract.BoolVal(workloadContext, "consumer_repositories_truncated") ||
		querycontract.BoolVal(workloadContext, "provisioning_source_chains_truncated")
	return map[string]any{
		"state":                            state,
		"reason":                           reason,
		"repository_count":                 len(repositories),
		"repositories_with_evidence_count": len(withEvidence),
		"evidence_family_count":            len(evidenceFamilies),
		"result_limit":                     serviceStoryItemLimit,
		"downstream_read_limit":            querycontract.BoundedTraceEnrichmentLimit(0),
		// PR #5933 review fix (Codex, service_story_dossier.go:308 sibling):
		// consumer_repositories_truncated (folded into upstreamTruncated
		// above) can fire purely because the service repository's own
		// indexed-file list hit serviceEvidenceFileLimit
		// (service_evidence_types.go), a bound with no relation to
		// downstream_read_limit. Naming it here keeps this route's coverage
		// summary honest about which bound fired, matching
		// buildServiceResultLimitsWithContext's evidence_file_read_limit.
		"evidence_file_read_limit": serviceEvidenceFileLimit,
		"truncated":                serviceInvestigationRepositoriesTruncated(repositories) || upstreamTruncated,
	}
}

func serviceInvestigationRepositoriesTruncated(repositories []map[string]any) bool {
	for _, repo := range repositories {
		if querycontract.StringVal(repo, "repo_id") == serviceInvestigationTruncationMarkerID {
			return true
		}
	}
	return false
}
