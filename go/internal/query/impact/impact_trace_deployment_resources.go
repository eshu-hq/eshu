// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/impacttrace"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// CloudResourceResult is the bounded cloud-resource read for one repo: the
// admitted rows plus the limit block surfaced beside them. Fields stay
// exported because the query root's cloud-limit tests read them from outside
// this package. See #6060.
type CloudResourceResult struct {
	Rows   []map[string]any
	Limits map[string]any
}

// CloudResourceObservationLimit bounds the cloud-resource observation read
// (limit+1 over-fetch so the caller surfaces explicit truncation). Exported
// because the query root's cloud-limit tests pin it from outside this
// package. See #6060.
const CloudResourceObservationLimit = querycontract.ServiceStoryItemLimit * querycontract.ServiceStoryItemLimit

func (h *ImpactHandler) fetchCloudResourceResult(
	ctx context.Context,
	repoID string,
	workloadID string,
) (CloudResourceResult, error) {
	queryLimit := querycontract.ServiceStoryItemLimit + 1
	repoID = strings.TrimSpace(repoID)
	workloadID = strings.TrimSpace(workloadID)
	if h == nil || h.Neo4j == nil || repoID == "" || workloadID == "" {
		return boundedCloudResourceResult(nil, queryLimit), nil
	}
	access := querycontract.RepositoryAccessFilterFromContext(ctx)
	// WorkloadInstance and USES relationships are global today and do not
	// carry repository ownership. A repository-scoped token therefore cannot
	// prove that a reachable cloud observation belongs to its grant. Omit the
	// limits with the rows so consumers cannot misread withheld evidence as an
	// exact empty collection.
	if access.Scoped() {
		return CloudResourceResult{Rows: []map[string]any{}}, nil
	}
	params := access.GraphParams(map[string]any{
		"repo_id":                 repoID,
		"workload_id":             workloadID,
		"cloud_observation_limit": CloudResourceObservationLimit + 1,
	})
	rows, err := h.Neo4j.Run(ctx, fmt.Sprintf(`
		MATCH (repo:Repository)-[:DEFINES]->(w:Workload {id: $workload_id})<-[:INSTANCE_OF]-(i:WorkloadInstance)-[rel:USES]->(c:CloudResource)
		WHERE repo.id = $repo_id%s
		WITH c, i, rel,
		     c.name AS sort_name, c.id AS sort_id,
		     rel.confidence AS sort_confidence,
		     rel.stable_fact_key AS sort_stable_fact_key,
		     rel.source_fact_id AS sort_source_fact_id,
		     rel.source_system AS sort_source_system,
		     rel.source_record_id AS sort_source_record_id,
		     i.environment AS sort_instance_environment
		ORDER BY sort_name, sort_id, sort_confidence DESC,
		         sort_stable_fact_key, sort_source_fact_id,
		         sort_source_system, sort_source_record_id,
		         sort_instance_environment
		LIMIT $cloud_observation_limit
		RETURN c.id as id, c.name as name, c.kind as kind, c.provider as provider,
		       properties(rel) as observation,
		       i.environment as instance_environment,
		       c.environment as resource_environment
	`, access.GraphPredicate("repo")), params)
	if err != nil {
		return CloudResourceResult{}, err
	}
	observationCount := 0
	for _, row := range rows {
		if observations := querycontract.MapSliceValue(row, "observations"); len(observations) > 0 {
			observationCount += querycontract.IntVal(row, "observation_count")
			continue
		}
		observationCount++
	}
	resources, err := impacttrace.DeploymentTraceCloudResourcesFromRows(rows, "")
	if err != nil {
		return CloudResourceResult{}, err
	}
	return boundedCloudResourceResultWithObservationState(
		resources,
		queryLimit,
		observationCount,
		observationCount > CloudResourceObservationLimit ||
			len(rows) > CloudResourceObservationLimit || len(resources) >= queryLimit,
	), nil
}

func boundedCloudResourceResult(rows []map[string]any, queryLimit int) CloudResourceResult {
	return boundedCloudResourceResultWithObservationState(rows, queryLimit, 0, false)
}

func boundedCloudResourceResultWithObservationState(
	rows []map[string]any,
	queryLimit int,
	observationCount int,
	observationsTruncated bool,
) CloudResourceResult {
	returned, truncated := querycontract.CapMapRows(rows, querycontract.ServiceStoryItemLimit)
	truncated = truncated || observationsTruncated
	limits := querycontract.BoundedCollectionMetadata(
		querycontract.ServiceStoryItemLimit, queryLimit, len(returned), len(rows), truncated,
		[]string{"name", "id"},
	)
	limits["observation_limit"] = CloudResourceObservationLimit
	limits["observation_query_sentinel_limit"] = CloudResourceObservationLimit + 1
	limits["observation_count"] = observationCount
	limits["observation_count_is_lower_bound"] = observationsTruncated
	return CloudResourceResult{
		Rows:   returned,
		Limits: limits,
	}
}

type k8sResourceResult struct {
	rows              []map[string]any
	imageRefs         []string
	limits            map[string]any
	candidates        []map[string]any
	contentLowerBound bool
	// selectCandidatePoolTruncated is true when the directed SELECTS candidate
	// scan (ListRepoK8sSelectCandidates) hit the querycontract.RepositorySemanticEntityLimit
	// ceiling, so some selector-matching Services may be missing from the
	// surfaced pool. It drives the k8s_relationships_complete=false disclosure
	// (see boundedK8sResourceResult) and must be threaded back through any
	// re-merge (the handler and deployment-config-influence re-call
	// boundedK8sResourceResult with deployment-source rows).
	selectCandidatePoolTruncated bool
}

func (h *ImpactHandler) fetchK8sResources(
	ctx context.Context,
	repoID string,
	workloadName string,
) ([]map[string]any, []string, error) {
	result, err := h.FetchK8sResourceResult(ctx, repoID, workloadName)
	return result.rows, result.imageRefs, err
}

// FetchK8sResourceResult is the exported rename of fetchK8sResourceResult,
// which the deployment-config-influence family calls from outside the impact
// move set. See #6060.
func (h *ImpactHandler) FetchK8sResourceResult(
	ctx context.Context,
	repoID string,
	workloadName string,
) (k8sResourceResult, error) {
	if h == nil || h.Content == nil || repoID == "" || workloadName == "" {
		return boundedK8sResourceResult(nil, false, nil, false, false), nil
	}

	queryLimit := querycontract.ServiceStoryItemLimit + 1
	rows, err := h.Content.SearchEntitiesByName(ctx, repoID, "K8sResource", workloadName, queryLimit)
	if err != nil {
		return k8sResourceResult{}, err
	}

	// Phase 1: the name-anchored surfaced pool. This IS the historical wire
	// response (rows whose entity_name equals the traced workload's name), kept
	// unchanged. While building it, prepare a directed match target for each
	// anchored Deployment so the phase-2 candidate scan parses each workload's
	// pod-template labels exactly once.
	resources := make([]map[string]any, 0, len(rows))
	surfaced := make(map[string]struct{}, len(rows))
	targets := make([]anchoredDeploymentTarget, 0, 1)
	for _, row := range rows {
		if row.EntityName != workloadName {
			continue
		}
		resources = append(resources, k8sResourceWireRow(row))
		surfaced[row.EntityID] = struct{}{}
		if querycontract.IsK8sResourceKind(row, "Deployment") {
			targets = append(targets, anchoredDeploymentTarget{
				entityID: row.EntityID,
				target:   querycontract.NewK8sWorkloadMatchTarget(querycontract.K8sSelectMatchInputFromEntity(row)),
			})
		}
	}
	contentLowerBound := len(rows) >= queryLimit

	// Phase 2: the directed, matcher-only candidate scan. Only Services that
	// ACTUALLY selector-match an anchored Deployment are hydrated (by ID, wide
	// shape) and joined to the surfaced pool -- a differently-named Service is
	// discovered here (the #5363 under-linking fix) without any unmatched
	// candidate ever touching the wire.
	matchedIDs, candidatePoolTruncated, err := h.fetchK8sSelectMatchedServiceIDs(ctx, repoID, targets, surfaced)
	if err != nil {
		return k8sResourceResult{}, err
	}
	if len(matchedIDs) > 0 {
		hydrated, err := h.Content.ListRepoEntitiesByIDs(ctx, repoID, matchedIDs, len(matchedIDs))
		if err != nil {
			return k8sResourceResult{}, err
		}
		for _, row := range hydrated {
			if _, ok := surfaced[row.EntityID]; ok {
				continue
			}
			resources = append(resources, k8sResourceWireRow(row))
			surfaced[row.EntityID] = struct{}{}
		}
	}

	return boundedK8sResourceResult(resources, contentLowerBound, nil, false, candidatePoolTruncated), nil
}

func boundedK8sResourceResult(
	contentRows []map[string]any,
	contentLowerBound bool,
	deploymentSourceRows []map[string]any,
	deploymentSourceLowerBound bool,
	selectCandidatePoolTruncated bool,
) k8sResourceResult {
	merged := mergeDeploymentTraceRows(contentRows, deploymentSourceRows)
	impacttrace.SortDeploymentTraceMaps(merged)
	observedCount := len(merged)
	rows, mergedTruncated := querycontract.CapMapRows(merged, querycontract.ServiceStoryItemLimit)

	imageSet := make(map[string]struct{})
	for _, row := range rows {
		for _, image := range querycontract.StringSliceVal(row, "container_images") {
			imageSet[image] = struct{}{}
		}
	}
	imageRefs := make([]string, 0, len(imageSet))
	for image := range imageSet {
		imageRefs = append(imageRefs, image)
	}
	sort.Strings(imageRefs)
	observedCountIsLowerBound := contentLowerBound || deploymentSourceLowerBound
	limits := map[string]any{
		"limit":                                           querycontract.ServiceStoryItemLimit,
		"query_sentinel_limit":                            querycontract.ServiceStoryItemLimit + 1,
		"deployment_source_query_sentinel_limit":          querycontract.RepositorySemanticEntityLimit + 1,
		"returned_count":                                  len(rows),
		"observed_count":                                  observedCount,
		"observed_count_is_lower_bound":                   observedCountIsLowerBound,
		"content_observed_count":                          len(contentRows),
		"content_observed_count_is_lower_bound":           contentLowerBound,
		"deployment_source_observed_count":                len(deploymentSourceRows),
		"deployment_source_observed_count_is_lower_bound": deploymentSourceLowerBound,
		"truncated":                                       observedCountIsLowerBound || mergedTruncated,
		"ordering":                                        []string{"repo_id", "relative_path", "entity_id"},
		// Directed SELECTS candidate-scan completeness (#5363). These two keys
		// are always present so operators and clients can read the SELECTS
		// completeness of every response, not only the truncated ones; they are
		// additive (existing keys and their values are unchanged), so a repo
		// that surfaces no new match stays byte-identical on every pre-existing
		// key. When the candidate pool truncated at the ceiling, the
		// machine-readable reason is added and k8s_relationships_complete is
		// false.
		"k8s_select_candidate_sentinel_limit": querycontract.RepositorySemanticEntityLimit + 1,
		"k8s_relationships_complete":          !selectCandidatePoolTruncated,
	}
	if selectCandidatePoolTruncated {
		limits["k8s_relationships_incomplete_reason"] = k8sSelectCandidatePoolTruncationReason
	}
	return k8sResourceResult{
		rows:                         rows,
		imageRefs:                    imageRefs,
		candidates:                   merged,
		contentLowerBound:            contentLowerBound,
		selectCandidatePoolTruncated: selectCandidatePoolTruncated,
		limits:                       limits,
	}
}

func mergeDeploymentTraceRows(left []map[string]any, right []map[string]any) []map[string]any {
	if len(left) == 0 {
		return right
	}
	if len(right) == 0 {
		return left
	}
	seen := make(map[string]struct{}, len(left)+len(right))
	merged := make([]map[string]any, 0, len(left)+len(right))
	for _, row := range append(append([]map[string]any{}, left...), right...) {
		key := querycontract.StringVal(row, "entity_id")
		if key == "" {
			key = querycontract.StringVal(row, "qualified_name") + "|" + querycontract.StringVal(row, "relative_path")
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		merged = append(merged, row)
	}
	impacttrace.SortDeploymentTraceMaps(merged)
	return merged
}
