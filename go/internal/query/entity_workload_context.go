// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
)

// fetchWorkloadContext queries graph-backed workload context with a custom
// WHERE clause and enriches linked repositories with local context evidence.
func (h *EntityHandler) fetchWorkloadContext(ctx context.Context, whereClause string, params map[string]any) (map[string]any, error) {
	return h.fetchWorkloadContextForOperation(ctx, whereClause, params, "workload_context")
}

// fetchServiceWorkloadContext avoids a backend-sensitive OR predicate by
// trying exact service-name lookup before exact workload-id lookup.
func (h *EntityHandler) fetchServiceWorkloadContext(ctx context.Context, serviceName string, operation string) (map[string]any, error) {
	serviceName = strings.TrimSpace(serviceName)
	if serviceName == "" {
		return nil, nil
	}
	result, err := h.fetchWorkloadContextForOperation(
		ctx,
		"w.name = $service_name",
		map[string]any{"service_name": serviceName},
		operation,
	)
	if err != nil || result != nil {
		return result, err
	}
	result, err = h.fetchWorkloadContextForOperation(
		ctx,
		"w.id = $service_name",
		map[string]any{"service_name": serviceName},
		operation,
	)
	if err != nil || result != nil {
		return result, err
	}
	return h.fetchServiceReadModelWorkloadContext(ctx, serviceName)
}

// fetchWorkloadContextForOperation queries workload context and tags timing
// logs with the caller operation that will render the context.
func (h *EntityHandler) fetchWorkloadContextForOperation(ctx context.Context, whereClause string, params map[string]any, operation string) (map[string]any, error) {
	access := repositoryAccessFilterFromContext(ctx)
	if access.empty() {
		return nil, nil
	}
	serviceName := StringVal(params, "service_name")
	if serviceName == "" {
		serviceName = StringVal(params, "workload_id")
	}
	if operation == "" {
		operation = "workload_context"
	}
	timer := startServiceQueryStage(ctx, h.Logger, operation, serviceName, "", "workload_lookup")
	params = access.graphParams(params)
	whereClause = scopedWorkloadWhereClause(whereClause, access)
	baseCypher := fmt.Sprintf(`
		MATCH (w:Workload) WHERE %s
		RETURN w.id as id, w.name as name, w.kind as kind, w.repo_id as repo_id
		LIMIT 1
	`, whereClause)

	row, err := h.Neo4j.RunSingle(ctx, baseCypher, params)
	timer.Done(ctx, slog.Bool("found", row != nil))
	if err != nil {
		return nil, err
	}

	if row == nil {
		return nil, nil
	}

	workloadID := StringVal(row, "id")
	followupWhereClause := whereClause
	followupParams := params
	if workloadID != "" {
		followupWhereClause = "w.id = $workload_id" // #nosec G101 -- Cypher parameterised query template, not a hardcoded credential
		followupParams = map[string]any{"workload_id": workloadID}
	}

	preferredRepoID := StringVal(row, "repo_id")
	if !access.allowsRepositoryID(preferredRepoID) {
		preferredRepoID = ""
	}
	timer = startServiceQueryStage(ctx, h.Logger, operation, StringVal(row, "name"), preferredRepoID, "repository_lookup")
	repoID, repoName, err := h.fetchWorkloadRepositoryForAccess(
		ctx, workloadID, access, preferredRepoID,
	)
	timer.Done(ctx, slog.String("resolved_repo_id", repoID))
	if err != nil {
		return nil, err
	}
	if repoName == "" {
		repoName = StringVal(row, "repo_name")
	}

	timer = startServiceQueryStage(ctx, h.Logger, operation, StringVal(row, "name"), repoID, "instance_lookup")
	topology, err := h.fetchWorkloadDeploymentTopology(
		ctx, followupWhereClause, followupParams, repoID, operation == "deployment_trace",
	)
	timer.Done(ctx, slog.Int("row_count", len(topology.instances)))
	if err != nil {
		return nil, err
	}
	instances := topology.instances
	if len(instances) == 0 {
		instances = extractInstances(row)
	}

	result := map[string]any{
		"id":                    StringVal(row, "id"),
		"name":                  StringVal(row, "name"),
		"kind":                  StringVal(row, "kind"),
		"repo_id":               repoID,
		"repo_name":             repoName,
		"instances":             instances,
		"topology_edges":        topology.topologyEdges,
		"provisioned_platforms": topology.provisionedPlatforms,
		"runtime_topology_limits": map[string]any{
			"instances":             topology.instanceLimits,
			"platform_edges":        topology.platformLimits,
			"provisioned_platforms": topology.provisionedPlatformLimits,
		},
	}
	if deploymentEvidence := mapValue(row, "deployment_evidence"); len(deploymentEvidence) > 0 {
		result["deployment_evidence"] = deploymentEvidence
	}

	if repoID != "" {
		repoParams := map[string]any{"repo_id": repoID}
		timer = startServiceQueryStage(ctx, h.Logger, operation, StringVal(row, "name"), repoID, "repo_dependencies")
		result["dependencies"] = queryRepoDependencies(ctx, h.Neo4j, repoParams)
		timer.Done(ctx, slog.Int("row_count", len(mapSliceValue(result, "dependencies"))))
		timer = startServiceQueryStage(ctx, h.Logger, operation, StringVal(row, "name"), repoID, "repo_infrastructure")
		result["infrastructure"] = queryRepoInfrastructure(ctx, h.Neo4j, h.Content, repoParams)
		timer.Done(ctx, slog.Int("row_count", len(mapSliceValue(result, "infrastructure"))))
	}

	return result, nil
}

// fetchServiceReadModelWorkloadContext exposes repositories with workload
// identity facts even when no graph Workload node has been materialized yet.
func (h *EntityHandler) fetchServiceReadModelWorkloadContext(ctx context.Context, serviceName string) (map[string]any, error) {
	if h.Content == nil {
		return nil, nil
	}
	access := repositoryAccessFilterFromContext(ctx)
	if access.empty() {
		return nil, nil
	}
	repo, err := h.Content.ResolveRepository(ctx, serviceName)
	if err != nil || repo == nil {
		return nil, err
	}
	if !access.allowsRepositoryID(repo.ID) {
		return nil, nil
	}

	summary := loadRepositoryReadModelSummary(ctx, h.Content, repo.ID)
	if summary == nil {
		return nil, nil
	}
	workloadName := matchingRepositoryWorkloadIdentity(serviceName, *repo, summary.WorkloadNames)
	if workloadName == "" {
		return nil, nil
	}

	repoParams := map[string]any{"repo_id": repo.ID}
	infrastructure := queryRepoInfrastructureFromContent(ctx, h.Content, repo.ID)
	if len(infrastructure) == 0 && h.Neo4j != nil {
		infrastructure = queryRepoInfrastructureFromGraph(ctx, h.Neo4j, repoParams)
	}
	dependencies := []map[string]any{}
	if h.Neo4j != nil {
		dependencies = queryRepoDependencies(ctx, h.Neo4j, repoParams)
	}
	return map[string]any{
		"id":                     "workload:" + workloadName,
		"name":                   workloadName,
		"kind":                   "service",
		"repo_id":                repo.ID,
		"repo_name":              repo.Name,
		"instances":              []map[string]any{},
		"dependencies":           dependencies,
		"infrastructure":         infrastructure,
		"materialization_status": "identity_only",
		"query_basis":            "repository_read_model",
		"limitations":            []string{"workload_identity_not_materialized"},
	}, nil
}

func matchingRepositoryWorkloadIdentity(serviceName string, repo RepositoryCatalogEntry, workloadNames []string) string {
	selector := strings.TrimSpace(serviceName)
	if selector == "" {
		return ""
	}
	plainSelector := strings.TrimPrefix(selector, "workload:")
	for _, workloadName := range workloadNames {
		normalized := strings.TrimSpace(workloadName)
		if normalized == "" {
			continue
		}
		if selector == normalized || plainSelector == normalized || selector == "workload:"+normalized {
			return normalized
		}
	}
	if selector != repo.Name && plainSelector != repo.Name {
		return ""
	}
	if len(workloadNames) != 1 {
		return ""
	}
	return strings.TrimSpace(workloadNames[0])
}

const workloadRepositoryCandidateLimit = contextStoryItemLimit

// fetchWorkloadRepositoryForAccess resolves a bounded repository candidate set
// from one exact Workload anchor while preserving scoped authorization. It
// sorts the complete bounded set in Go because NornicDB can re-plan backend
// ORDER BY/CASE relationship reads as global scans. A stored workload repo_id
// is preferred only after the DEFINES relationship proves it is a candidate.
func (h *EntityHandler) fetchWorkloadRepositoryForAccess(
	ctx context.Context,
	workloadID string,
	access repositoryAccessFilter,
	preferredRepoID string,
) (string, string, error) {
	if strings.TrimSpace(workloadID) == "" {
		return "", "", nil
	}
	queryLimit := workloadRepositoryCandidateLimit + 1
	params := access.graphParams(map[string]any{
		"workload_id":      workloadID,
		"repository_limit": queryLimit,
	})
	cypher := fmt.Sprintf(`
		MATCH (w:Workload {id: $workload_id})<-[:DEFINES]-(r:Repository)
		%s
		RETURN DISTINCT r.id as repo_id, r.name as repo_name
		LIMIT $repository_limit
	`, access.graphWhereClause("r"))
	rows, err := h.Neo4j.Run(ctx, cypher, params)
	if err != nil {
		return "", "", err
	}
	if len(rows) > workloadRepositoryCandidateLimit {
		return "", "", fmt.Errorf(
			"workload repository candidates exceed bound: returned %d, limit %d",
			len(rows), workloadRepositoryCandidateLimit,
		)
	}
	candidates := make([]map[string]any, 0, len(rows))
	seen := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		repoID := StringVal(row, "repo_id")
		if repoID == "" {
			continue
		}
		if _, exists := seen[repoID]; exists {
			continue
		}
		seen[repoID] = struct{}{}
		candidates = append(candidates, row)
	}
	if len(candidates) == 0 {
		return "", "", nil
	}
	sort.Slice(candidates, func(i, j int) bool {
		return StringVal(candidates[i], "repo_id") < StringVal(candidates[j], "repo_id")
	})
	selected := candidates[0]
	for _, candidate := range candidates {
		if StringVal(candidate, "repo_id") == preferredRepoID {
			selected = candidate
			break
		}
	}
	return StringVal(selected, "repo_id"), StringVal(selected, "repo_name"), nil
}

func scopedWorkloadWhereClause(whereClause string, access repositoryAccessFilter) string {
	if !access.scoped() {
		return whereClause
	}
	return whereClause + `
			AND (
				w.repo_id IN $allowed_repository_ids
				OR w.repo_id IN $allowed_scope_ids
				OR EXISTS {
					MATCH (scopeRepo:Repository)-[:DEFINES]->(w)
					WHERE ` + access.graphCondition("scopeRepo") + `
				}
			)`
}

const workloadPlatformEdgeLimit = contextStoryItemLimit * contextStoryItemLimit

type workloadPlatformResult struct {
	rows   []map[string]any
	limits map[string]any
}

// fetchWorkloadPlatformRows anchors platform lookup through the selected
// repository and workload before batching exact instance ids.
func (h *EntityHandler) fetchWorkloadPlatformRows(
	ctx context.Context,
	repoID string,
	workloadID string,
	instances []map[string]any,
) ([]map[string]any, error) {
	result, err := h.fetchWorkloadPlatformResult(ctx, repoID, workloadID, instances)
	return result.rows, err
}

func (h *EntityHandler) fetchWorkloadPlatformResult(
	ctx context.Context,
	repoID string,
	workloadID string,
	instances []map[string]any,
) (workloadPlatformResult, error) {
	repoID = strings.TrimSpace(repoID)
	workloadID = strings.TrimSpace(workloadID)
	if h == nil || h.Neo4j == nil || repoID == "" || workloadID == "" || len(instances) == 0 {
		return emptyWorkloadPlatformResult(), nil
	}
	access := repositoryAccessFilterFromContext(ctx)
	// WorkloadInstance and RUNS_ON relationships are global today and do not
	// carry repository ownership, so scoped callers cannot safely consume them.
	if access.scoped() {
		return emptyWorkloadPlatformResult(), nil
	}
	instanceIDs := make([]string, 0, len(instances))
	for _, instance := range instances {
		if instanceID := StringVal(instance, "instance_id"); instanceID != "" {
			instanceIDs = append(instanceIDs, instanceID)
		}
	}
	instanceIDs = sortedUniqueStrings(instanceIDs)
	if len(instanceIDs) == 0 {
		return emptyWorkloadPlatformResult(), nil
	}
	queryLimit := workloadPlatformEdgeLimit + 1
	platformCypher := fmt.Sprintf(`
		MATCH (repo:Repository)-[:DEFINES]->(w:Workload)<-[:INSTANCE_OF]-(i:WorkloadInstance)-[runsOn:RUNS_ON]->(p:Platform)
		WHERE repo.id = $repo_id AND w.id = $workload_id AND i.id IN $instance_ids%s
		RETURN i.id as instance_id, p.id as platform_id, p.name as platform_name, p.kind as platform_kind,
		       collect(DISTINCT properties(runsOn)) as platform_edges
		ORDER BY instance_id, platform_name, platform_id
		LIMIT $platform_edge_limit
	`, access.graphPredicate("repo"))
	params := access.graphParams(map[string]any{
		"instance_ids":        instanceIDs,
		"platform_edge_limit": queryLimit,
		"repo_id":             repoID,
		"workload_id":         workloadID,
	})
	rows, err := h.Neo4j.Run(ctx, platformCypher, params)
	if err != nil {
		return workloadPlatformResult{}, err
	}
	sortWorkloadPlatformRows(rows)
	returned, truncated := capMapRows(rows, workloadPlatformEdgeLimit)
	for _, row := range returned {
		if len(mapValue(row, "platform_edge")) == 0 {
			properties, err := deterministicEvidenceProperties(row, "platform_edges")
			if err != nil {
				return workloadPlatformResult{}, fmt.Errorf("select RUNS_ON edge evidence: %w", err)
			}
			row["platform_edge"] = properties
		}
	}
	return workloadPlatformResult{
		rows: returned,
		limits: boundedCollectionMetadata(
			workloadPlatformEdgeLimit, queryLimit, len(returned), len(rows), truncated,
			[]string{"instance_id", "platform_name", "platform_id"},
		),
	}, nil
}

// sortWorkloadPlatformRows orders direct-runtime platform rows by instance,
// then by stable platform identity. The production query already declares
// this order (ORDER BY instance_id, platform_name, platform_id), but that
// ORDER BY is not guaranteed to replay identically across NornicDB
// executions (see docs/internal/evidence/5272-service-story-runtime-topology.md
// and issue #5644), so relying on the backend row order alone let repeated
// service-story calls over unchanged retained data attach the same
// instance's platforms in a different order. This Go-level sort makes
// attachDirectPlatforms deterministic regardless of backend row order.
//
// The production Cypher aggregates by (instance_id, platform_id,
// platform_name, platform_kind), so two rows can still tie on
// (instance_id, platform_name, platform_id) when platform_id is empty but
// platform_kind differs. platform_kind is the final tiebreaker so every
// distinct aggregation key resolves to a unique, deterministic position.
func sortWorkloadPlatformRows(rows []map[string]any) {
	sort.Slice(rows, func(i, j int) bool {
		left, right := rows[i], rows[j]
		if leftInstance, rightInstance := StringVal(left, "instance_id"), StringVal(right, "instance_id"); leftInstance != rightInstance {
			return leftInstance < rightInstance
		}
		if leftName, rightName := StringVal(left, "platform_name"), StringVal(right, "platform_name"); leftName != rightName {
			return leftName < rightName
		}
		if leftID, rightID := StringVal(left, "platform_id"), StringVal(right, "platform_id"); leftID != rightID {
			return leftID < rightID
		}
		return StringVal(left, "platform_kind") < StringVal(right, "platform_kind")
	})
}

func attachDirectPlatforms(instances []map[string]any, platformRows []map[string]any) {
	byID := make(map[string]map[string]any, len(instances))
	for _, instance := range instances {
		byID[StringVal(instance, "instance_id")] = instance
	}
	for _, row := range platformRows {
		instance := byID[StringVal(row, "instance_id")]
		if instance == nil {
			continue
		}
		platform := map[string]any{
			"platform_id":         StringVal(row, "platform_id"),
			"platform_name":       StringVal(row, "platform_name"),
			"platform_kind":       StringVal(row, "platform_kind"),
			"platform_confidence": platformEdgeConfidence(row),
			"platform_reason":     platformEdgeReason(row),
			"topology_basis":      "direct_runtime",
			"topology_edges":      []map[string]any{directPlatformTopologyEdge(row)},
		}
		instance["platforms"] = append(platformTargets(instance), platform)
		if StringVal(instance, "platform_name") == "" {
			instance["platform_name"] = platform["platform_name"]
			instance["platform_kind"] = platform["platform_kind"]
			instance["platform_confidence"] = platform["platform_confidence"]
			instance["platform_reason"] = platform["platform_reason"]
		}
	}
}

// platformEdgeConfidence preserves edge confidence when a backend can return
// relationship properties but not the scalar relationship-property projection.
func platformEdgeConfidence(row map[string]any) float64 {
	if confidence := floatVal(row, "platform_confidence"); confidence != 0 {
		return confidence
	}
	return floatVal(mapValue(row, "platform_edge"), "confidence")
}

// platformEdgeReason preserves edge rationale through the same relationship
// properties fallback used for confidence.
func platformEdgeReason(row map[string]any) string {
	if reason := StringVal(row, "platform_reason"); reason != "" {
		return reason
	}
	return StringVal(mapValue(row, "platform_edge"), "reason")
}
