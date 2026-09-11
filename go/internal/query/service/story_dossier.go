// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package service

import (
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/repository"
)

// serviceStoryItemLimit bounds relationship and instance fan-out attached to
// service workload context/story payloads. The implementation moved to
// querycontract for #6060 so the impact handler-family subpackages can share
// it without importing this package; this alias keeps root callers unchanged.
const serviceStoryItemLimit = querycontract.ServiceStoryItemLimit

func enrichServiceStoryDossierResponseWithContext(response map[string]any, buildCtx serviceStoryBuildContext) {
	workloadContext := buildCtx.workloadContext
	response["service_identity"] = buildServiceIdentity(workloadContext)
	response["api_surface"] = buildCtx.dossierAPISurface()
	response["deployment_lanes"] = buildServiceDeploymentLanes(workloadContext)
	response["upstream_dependencies"] = buildServiceUpstreamDependencies(workloadContext)
	response["downstream_consumers"] = BuildServiceDownstreamConsumers(workloadContext)
	response["evidence_graph"] = buildServiceEvidenceGraph(workloadContext)
	response["result_limits"] = buildServiceResultLimitsWithContext(buildCtx)
	rawContextLimits := map[string]any{}
	for _, key := range []string{
		"hostnames",
		"entrypoint_candidates",
		"entrypoints",
		"network_paths",
		"observed_config_environments",
		"dependents",
		"consumer_repositories",
		"provisioning_source_chains",
		"cloud_resources",
		"uncorrelated_cloud_resources",
		"deployment_evidence",
		"limitations",
	} {
		if value, ok := workloadContext[key]; ok && value != nil {
			bounded, limit := boundedServiceStoryRawValue(value)
			response[key] = bounded
			if len(limit) > 0 {
				rawContextLimits[key] = limit
			}
		}
	}
	if len(rawContextLimits) > 0 {
		response["raw_context_limits"] = rawContextLimits
	}
	// Surface the uncorrelated cloud-resource truncation flag directly on the
	// dossier response. The whitelist loop above copies the row slice but not
	// the boolean, so callers would silently receive a capped list with no
	// signal. Copy it only when true to keep the schema sparse.
	if truncated, ok := workloadContext["uncorrelated_cloud_resources_truncated"]; ok && truncated == true {
		response["uncorrelated_cloud_resources_truncated"] = true
	}
}

func buildServiceIdentity(workloadContext map[string]any) map[string]any {
	identity := map[string]any{
		"service_id":   querycontract.SafeStr(workloadContext, "id"),
		"service_name": querycontract.SafeStr(workloadContext, "name"),
		"kind":         querycontract.SafeStr(workloadContext, "kind"),
		"repo_id":      querycontract.SafeStr(workloadContext, "repo_id"),
		"repo_name":    querycontract.SafeStr(workloadContext, "repo_name"),
	}
	for _, key := range []string{"materialization_status", "query_basis"} {
		if value := querycontract.SafeStr(workloadContext, key); value != "" {
			identity[key] = value
		}
	}
	if limitations := querycontract.StringSliceVal(workloadContext, "limitations"); len(limitations) > 0 {
		identity["limitations"] = limitations
	}
	return identity
}

func buildServiceDossierAPISurface(workloadContext map[string]any) map[string]any {
	apiSurface := copyMap(querycontract.MapValue(workloadContext, "api_surface"))
	if len(apiSurface) == 0 {
		return emptyServiceDossierAPISurface()
	}
	endpoints, truncated := CapMapRows(querycontract.MapSliceValue(apiSurface, "endpoints"), serviceStoryItemLimit)
	apiSurface["endpoints"] = endpoints
	apiSurface["truncated"] = truncated
	if _, ok := apiSurface["endpoint_count"]; !ok {
		apiSurface["endpoint_count"] = len(endpoints)
	}
	if _, ok := apiSurface["method_count"]; !ok {
		apiSurface["method_count"] = 0
	}
	apiSurface["spec_count"] = serviceAPISpecCount(apiSurface)
	return apiSurface
}

func normalizedServiceAPISurface(workloadContext map[string]any) (map[string]any, bool) {
	if len(querycontract.MapValue(workloadContext, "api_surface")) == 0 {
		return nil, false
	}
	return buildServiceDossierAPISurface(workloadContext), true
}

func emptyServiceDossierAPISurface() map[string]any {
	return map[string]any{
		"endpoint_count": 0,
		"method_count":   0,
		"spec_count":     0,
		"endpoints":      []map[string]any{},
		"truncated":      false,
	}
}

// serviceAPISpecCount keeps every service-story section on the same bounded API
// surface evidence contract, including graph surfaces that expose spec_paths
// without a separate aggregate spec_count field.
func serviceAPISpecCount(apiSurface map[string]any) int {
	if count := querycontract.FirstPositiveInt(apiSurface, "spec_count", "spec_files_count", "spec_path_count"); count > 0 {
		return count
	}
	if count := len(querycontract.StringSliceVal(apiSurface, "spec_files")); count > 0 {
		return count
	}
	return len(querycontract.StringSliceVal(apiSurface, "spec_paths"))
}

func buildServiceDeploymentLanes(workloadContext map[string]any) []map[string]any {
	lanes := map[string]map[string]any{}
	for _, instance := range querycontract.MapSliceValue(workloadContext, "instances") {
		laneType := serviceLaneType(querycontract.StringVal(instance, "platform_kind"), "")
		if laneType == "" {
			continue
		}
		lane := serviceLane(lanes, laneType)
		addUniqueStringField(lane, "environments", querycontract.StringVal(instance, "environment"))
		addUniqueStringField(lane, "runtime_platforms", querycontract.StringVal(instance, "platform_name"))
		addUniqueStringField(lane, "platform_kinds", querycontract.StringVal(instance, "platform_kind"))
	}
	for _, artifact := range serviceDeploymentArtifacts(workloadContext) {
		laneType := serviceLaneType(querycontract.StringVal(artifact, "runtime_platform_kind"), querycontract.StringVal(artifact, "artifact_family"))
		if laneType == "" {
			continue
		}
		lane := serviceLane(lanes, laneType)
		addUniqueStringField(lane, "environments", querycontract.StringVal(artifact, "environment"))
		addUniqueStringField(lane, "source_repositories", querycontract.StringVal(artifact, "source_repo_name"))
		addUniqueStringField(lane, "relationship_types", querycontract.StringVal(artifact, "relationship_type"))
		addUniqueStringField(lane, "resolved_ids", querycontract.StringVal(artifact, "resolved_id"))
		if confidence := querycontract.FloatVal(artifact, "confidence"); confidence > 0 {
			lane["max_confidence"] = maxFloat(querycontract.FloatVal(lane, "max_confidence"), confidence)
		}
	}
	result := make([]map[string]any, 0, len(lanes))
	for _, lane := range lanes {
		sortStringFields(lane, "environments", "runtime_platforms", "platform_kinds", "source_repositories", "relationship_types", "resolved_ids")
		result = append(result, lane)
	}
	sort.Slice(result, func(i, j int) bool {
		return querycontract.StringVal(result[i], "lane_type") < querycontract.StringVal(result[j], "lane_type")
	})
	return result
}

func serviceLane(lanes map[string]map[string]any, laneType string) map[string]any {
	lane := lanes[laneType]
	if lane == nil {
		lane = map[string]any{"lane_type": laneType}
		lanes[laneType] = lane
	}
	return lane
}

func serviceLaneType(platformKind string, artifactFamily string) string {
	joined := strings.ToLower(strings.TrimSpace(platformKind + " " + artifactFamily))
	switch {
	case strings.Contains(joined, "ecs") || strings.Contains(joined, "terraform"):
		return "ecs_terraform"
	case strings.Contains(joined, "argocd") ||
		strings.Contains(joined, "k8s") ||
		strings.Contains(joined, "kubernetes") ||
		strings.Contains(joined, "helm") ||
		strings.Contains(joined, "kustomize"):
		return "k8s_gitops"
	default:
		return ""
	}
}

func buildServiceUpstreamDependencies(workloadContext map[string]any) []map[string]any {
	rows := serviceUpstreamDependencyRows(workloadContext)
	sort.Slice(rows, func(i, j int) bool {
		if querycontract.StringVal(rows[i], "relationship_type") != querycontract.StringVal(rows[j], "relationship_type") {
			return querycontract.StringVal(rows[i], "relationship_type") < querycontract.StringVal(rows[j], "relationship_type")
		}
		return querycontract.StringVal(rows[i], "source") < querycontract.StringVal(rows[j], "source")
	})
	// #5720 round-7 P3-2: the discarded bool here is recovered, not lost --
	// buildServiceResultLimitsWithContext recomputes the same
	// serviceUpstreamDependencyRows length and compares it against
	// serviceStoryItemLimit, so an upstream list trimmed by this cap always
	// reports result_limits.truncated: true. Unlike the downstream lists, the
	// upstream rows have no bound below serviceStoryItemLimit, so that
	// count-vs-limit comparison is sufficient here.
	capped, _ := CapMapRows(rows, serviceStoryItemLimit)
	return capped
}

func serviceUpstreamDependencyRows(workloadContext map[string]any) []map[string]any {
	rows := make([]map[string]any, 0)
	seenArtifacts := map[string]map[string]any{}
	for _, artifact := range serviceDeploymentArtifacts(workloadContext) {
		if querycontract.StringVal(artifact, "direction") == "outgoing" {
			continue
		}
		row := map[string]any{
			"source":                   querycontract.StringVal(artifact, "source_repo_name"),
			"source_repo_id":           querycontract.StringVal(artifact, "source_repo_id"),
			"source_repo_canonical_id": querycontract.StringVal(artifact, "source_repo_canonical_id"),
			"source_repo_scope_key":    querycontract.StringVal(artifact, "source_repo_scope_key"),
			"target":                   querycontract.StringVal(artifact, "target_repo_name"),
			"target_repo_id":           querycontract.StringVal(artifact, "target_repo_id"),
			"relationship_type":        querycontract.StringVal(artifact, "relationship_type"),
			"resolved_id":              querycontract.StringVal(artifact, "resolved_id"),
			"confidence":               querycontract.FloatVal(artifact, "confidence"),
			"evidence_count":           querycontract.FirstPositiveInt(artifact, "evidence_count"),
			"rationale":                querycontract.StringVal(artifact, "rationale"),
		}
		key := serviceRelationshipKey(row)
		if existing := seenArtifacts[key]; existing != nil {
			mergeServiceRelationshipRow(existing, row)
			continue
		}
		seenArtifacts[key] = row
		rows = append(rows, row)
	}
	for _, dependency := range querycontract.MapSliceValue(workloadContext, "dependencies") {
		rows = append(rows, map[string]any{
			"source":            querycontract.SafeStr(workloadContext, "name"),
			"target":            querycontract.FirstNonEmptyString(querycontract.StringVal(dependency, "target_name"), querycontract.StringVal(dependency, "name")),
			"target_id":         querycontract.StringVal(dependency, "target_id"),
			"relationship_type": querycontract.FirstNonEmptyString(querycontract.StringVal(dependency, "type"), querycontract.StringVal(dependency, "relationship_type")),
			"confidence":        querycontract.FloatVal(dependency, "confidence"),
		})
	}
	for _, chain := range querycontract.MapSliceValue(workloadContext, "provisioning_source_chains") {
		rows = append(rows, map[string]any{
			"source":            querycontract.StringVal(chain, "repository"),
			"source_repo_id":    querycontract.StringVal(chain, "repo_id"),
			"relationship_type": "PROVISIONING_SOURCE_CHAIN",
			"modules":           querycontract.StringSliceVal(chain, "modules"),
		})
	}
	return rows
}

func BuildServiceDownstreamConsumers(workloadContext map[string]any) map[string]any {
	dependents, graphTruncated := CapMapRows(querycontract.MapSliceValue(workloadContext, "dependents"), serviceStoryItemLimit)
	consumers, contentTruncated := CapMapRows(querycontract.MapSliceValue(workloadContext, "consumer_repositories"), serviceStoryItemLimit)
	// #5720 round-2 P1-1: graphTruncated/contentTruncated only fire once the
	// row count exceeds serviceStoryItemLimit (50), but the underlying
	// provisioning-candidate read is bounded well below that by default
	// (defaultIndirectEvidenceSearchLimit, 25) -- so a genuinely truncated
	// read could report truncated: false here every time. dependents_truncated
	// closes that gap from impacttrace.QueryProvisioningRepositoryCandidates's own
	// truncated bool. PR #5933 review fix (Copilot): consumer_repositories_truncated
	// is no longer that same bool -- since round 9 it is the merged
	// consumersTruncated impacttrace.LoadConsumerRepositoryEnrichmentFromCandidates
	// returns, which also folds in the evidence-file, hostname, and
	// content-search bounds (see buildServiceResultLimitsWithContext's
	// evidence_file_read_limit below for the largest of those).
	upstreamTruncated := querycontract.BoolVal(workloadContext, "dependents_truncated") || querycontract.BoolVal(workloadContext, "consumer_repositories_truncated")
	return map[string]any{
		"graph_dependent_count":  len(querycontract.MapSliceValue(workloadContext, "dependents")),
		"content_consumer_count": len(querycontract.MapSliceValue(workloadContext, "consumer_repositories")),
		"graph_dependents":       dependents,
		"content_consumers":      consumers,
		"truncated":              graphTruncated || contentTruncated || upstreamTruncated,
	}
}

func buildServiceResultLimitsWithContext(buildCtx serviceStoryBuildContext) map[string]any {
	workloadContext := buildCtx.workloadContext
	apiSurface := buildCtx.dossierAPISurface()
	endpointCount := querycontract.IntVal(apiSurface, "endpoint_count")
	if endpointCount == 0 {
		endpointCount = len(querycontract.MapSliceValue(apiSurface, "endpoints"))
	}
	upstreamCount := len(serviceUpstreamDependencyRows(workloadContext))
	dependentCount := len(querycontract.MapSliceValue(workloadContext, "dependents"))
	contentConsumerCount := len(querycontract.MapSliceValue(workloadContext, "consumer_repositories"))
	consumerCount := len(querycontract.MapSliceValue(workloadContext, "dependents")) +
		len(querycontract.MapSliceValue(workloadContext, "consumer_repositories"))
	// Fold the infrastructure panel's own truncation into this block's
	// "truncated" field (round-11 review follow-up to #5764, PR #5936,
	// mirroring the P3 fix already applied to getRepositoryStory in
	// repository/handler.go): fetchWorkloadContextForOperation
	// (entity_workload_context.go) appends repository.InfrastructureTruncatedReason to
	// workloadContext["limitations"] when the infrastructure read lands past
	// repositoryInfrastructureEntityLimit, but that reason previously reached
	// only answer_metadata.partial_reasons -- never this "truncated" field,
	// so BuildAnswerMetadata (which falls back to result_limits.truncated
	// when no top-level "truncated"/"coverage" key exists) and
	// serviceStoryAnswerData (which reads result_limits.truncated directly)
	// both reported answer_metadata.truncated/answer_packet.truncated as
	// false for a service whose infrastructure evidence was clipped.
	infrastructureTruncated := querycontract.ContainsString(querycontract.StringSliceVal(workloadContext, "limitations"), repository.InfrastructureTruncatedReason)
	// #5720 round-2 P1-1: same disclosure gap as BuildServiceDownstreamConsumers
	// above -- the count-vs-serviceStoryItemLimit comparisons below can never
	// fire on the default (25-row) indirect-evidence search limit, so the
	// upstream *_truncated signals from query_enrichment.go are
	// required to make a genuinely truncated read observable here.
	upstreamTruncated := querycontract.BoolVal(workloadContext, "dependents_truncated") ||
		querycontract.BoolVal(workloadContext, "consumer_repositories_truncated") ||
		querycontract.BoolVal(workloadContext, "provisioning_source_chains_truncated")
	return map[string]any{
		"limit": serviceStoryItemLimit,
		// #5720 round-7 P2-5: `limit` is the 50-row serviceStoryItemLimit that
		// caps each rendered list, but the bound that actually fires on the
		// downstream lists is the indirect-evidence search limit underneath
		// them. Reporting only limit: 50 next to downstream_count: 25 and
		// truncated: true told a caller "more than 50 existed" when the truth
		// was "more than 25". Every route that emits this block (service
		// story, service context, workload context/story, service
		// investigation) enriches with MaxDepth unset, so
		// querycontract.BoundedTraceEnrichmentLimit(0) is the bound that fired; deriving it
		// from that function rather than restating the constant keeps the two
		// from drifting. A future route that both passes a non-zero MaxDepth
		// and renders this block would have to thread its own bound here.
		"downstream_read_limit": querycontract.BoundedTraceEnrichmentLimit(0),
		// PR #5933 review fix (Codex): downstream_read_limit alone implied
		// truncated: true always means "more than 25 downstream rows
		// existed". That is true for dependents_truncated/
		// provisioning_source_chains_truncated (pure candidatesTruncated),
		// but consumer_repositories_truncated can now fire purely because the
		// service repository's own indexed-file list hit
		// serviceEvidenceFileLimit (query_evidence_types.go) -- a
		// 5,000-file bound with no relation to the 25-row fan-out, and one
		// that can trip while graph_dependent_count/content_consumer_count
		// both sit far under downstream_read_limit. Naming this second bound
		// lets a caller tell the two apart instead of inferring the wrong
		// downstream cardinality from downstream_read_limit alone.
		"evidence_file_read_limit": serviceEvidenceFileLimit,
		"ordering":                 "deterministic",
		"endpoint_count":           endpointCount,
		"upstream_count":           upstreamCount,
		"downstream_count":         consumerCount,
		"truncated":                endpointCount > serviceStoryItemLimit || upstreamCount > serviceStoryItemLimit || dependentCount > serviceStoryItemLimit || contentConsumerCount > serviceStoryItemLimit || infrastructureTruncated || upstreamTruncated,
		"drilldown_basis":          "resolved_id",
		"relationship_tool":        "get_relationship_evidence",
		"service_context_path":     "/api/v0/services/" + querycontract.SafeStr(workloadContext, "name") + "/context",
	}
}

func serviceDeploymentArtifacts(workloadContext map[string]any) []map[string]any {
	return querycontract.MapSliceValue(querycontract.MapValue(workloadContext, "deployment_evidence"), "artifacts")
}

// CapMapRows truncates rows to limit. The implementation moved to
// querycontract for #6060; this wrapper keeps root callers unchanged.
func CapMapRows(rows []map[string]any, limit int) ([]map[string]any, bool) {
	return querycontract.CapMapRows(rows, limit)
}

// copyMap returns a shallow copy of the input map. The implementation moved
// to querycontract for #6060; this wrapper keeps root callers unchanged.
func copyMap(input map[string]any) map[string]any {
	return querycontract.CopyMap(input)
}

// addUniqueStringField appends value to the row's key slice. The
// implementation moved to querycontract for #6060; this wrapper keeps root
// callers unchanged.
func addUniqueStringField(row map[string]any, key string, value string) {
	querycontract.AddUniqueStringField(row, key, value)
}

// sortStringFields sorts the row's string-slice columns in place. The
// implementation moved to querycontract for #6060; this wrapper keeps root
// callers unchanged.
func sortStringFields(row map[string]any, keys ...string) {
	querycontract.SortStringFields(row, keys...)
}

func maxFloat(left float64, right float64) float64 {
	if left >= right {
		return left
	}
	return right
}
