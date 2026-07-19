// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
)

type workloadRuntimeTopologyResult struct {
	instances     []map[string]any
	topologyEdges []map[string]any
	limits        map[string]any
}

type workloadDeploymentTopologyResult struct {
	instances                 []map[string]any
	topologyEdges             []map[string]any
	provisionedPlatforms      []map[string]any
	instanceLimits            map[string]any
	platformLimits            map[string]any
	provisionedPlatformLimits map[string]any
}

func (h *EntityHandler) fetchWorkloadDeploymentTopology(
	ctx context.Context,
	whereClause string,
	params map[string]any,
	repoID string,
	includeProvisioning bool,
) (workloadDeploymentTopologyResult, error) {
	runtimeResult, err := fetchWorkloadRuntimeTopology(ctx, h.Neo4j, whereClause, params, repoID)
	if err != nil {
		return workloadDeploymentTopologyResult{}, err
	}
	platformResult, err := h.fetchWorkloadPlatformResult(ctx, runtimeResult.instances)
	if err != nil {
		return workloadDeploymentTopologyResult{}, err
	}
	attachDirectPlatforms(runtimeResult.instances, platformResult.rows)
	provisionedResult := provisionedPlatformResult{rows: []map[string]any{}}
	if includeProvisioning {
		provisionedResult, err = h.fetchProvisionedPlatformResult(ctx, repoID)
		if err != nil {
			return workloadDeploymentTopologyResult{}, err
		}
	}
	return workloadDeploymentTopologyResult{
		instances:                 runtimeResult.instances,
		topologyEdges:             runtimeResult.topologyEdges,
		provisionedPlatforms:      provisionedResult.rows,
		instanceLimits:            runtimeResult.limits,
		platformLimits:            platformResult.limits,
		provisionedPlatformLimits: provisionedResult.limits,
	}, nil
}

func fetchWorkloadRuntimeTopology(
	ctx context.Context,
	reader GraphQuery,
	whereClause string,
	params map[string]any,
	repoID string,
) (workloadRuntimeTopologyResult, error) {
	queryLimit := contextStoryItemLimit + 1
	access := repositoryAccessFilterFromContext(ctx)
	if access.empty() {
		return workloadRuntimeTopologyResult{
			instances:     []map[string]any{},
			topologyEdges: []map[string]any{},
			limits: boundedCollectionMetadata(
				contextStoryItemLimit, queryLimit, 0, 0, false,
				[]string{"environment", "instance_id"},
			),
		}, nil
	}
	params = copyStringAnyMap(params)
	params = access.graphParams(params)
	params["instance_limit"] = queryLimit
	if StringVal(params, "workload_id") != "" {
		whereClause = "i.workload_id = $workload_id AND (" + whereClause + ")"
	}
	whereClause += access.graphPredicate("repo")
	if repoID != "" {
		params["repo_id"] = repoID
		whereClause += " AND repo.id = $repo_id"
	}
	// WorkloadInstance has no canonical repository ownership property. Its
	// repository context comes from Repository-DEFINES-Workload-INSTANCE_OF;
	// binding that path to the selected repository keeps the scalar repository
	// and its observed DEFINES edge internally consistent.
	rows, err := reader.Run(ctx, fmt.Sprintf(`
		MATCH (repo:Repository)-[defines:DEFINES]->(w:Workload)<-[instanceOf:INSTANCE_OF]-(i:WorkloadInstance)
		WHERE %s
		RETURN repo.id as repo_id, repo.name as repo_name,
		       w.id as workload_id, w.name as workload_name,
		       i.id as instance_id, i.environment as environment,
		       i.materialization_confidence as materialization_confidence,
		       i.materialization_provenance as materialization_provenance,
		       properties(defines) as defines_edge,
		       properties(instanceOf) as instance_edge
		ORDER BY repo_id, workload_id, environment, instance_id
		LIMIT $instance_limit
	`, whereClause), params)
	if err != nil {
		return workloadRuntimeTopologyResult{}, err
	}

	instances := make([]map[string]any, 0, min(len(rows), contextStoryItemLimit))
	topologyEdges := make([]map[string]any, 0, len(rows)*2)
	seenInstances := make(map[string]struct{}, len(rows))
	seenEdges := make(map[string]int, len(rows)*2)
	for _, row := range rows {
		instanceID := StringVal(row, "instance_id")
		if instanceID == "" {
			continue
		}
		if _, seen := seenInstances[instanceID]; !seen && len(instances) < contextStoryItemLimit {
			seenInstances[instanceID] = struct{}{}
			instances = append(instances, newWorkloadInstance(row))
		}
		if _, visible := seenInstances[instanceID]; !visible {
			continue
		}
		appendUniqueTopologyEdge(&topologyEdges, seenEdges, observedTopologyEdge(
			"DEFINES", StringVal(row, "repo_id"), StringVal(row, "repo_name"),
			StringVal(row, "workload_id"), StringVal(row, "workload_name"), mapValue(row, "defines_edge"),
		))
		appendUniqueTopologyEdge(&topologyEdges, seenEdges, observedTopologyEdge(
			"INSTANCE_OF", instanceID, "", StringVal(row, "workload_id"),
			StringVal(row, "workload_name"), mapValue(row, "instance_edge"),
		))
	}
	sortTopologyEdges(topologyEdges)
	truncated := len(rows) >= queryLimit || len(seenInstances) > contextStoryItemLimit
	return workloadRuntimeTopologyResult{
		instances:     instances,
		topologyEdges: topologyEdges,
		limits: boundedCollectionMetadata(
			contextStoryItemLimit, queryLimit, len(instances), len(rows), truncated,
			[]string{"environment", "instance_id"},
		),
	}, nil
}

func newWorkloadInstance(row map[string]any) map[string]any {
	return map[string]any{
		"instance_id":                StringVal(row, "instance_id"),
		"platform_name":              "",
		"platform_kind":              "",
		"platforms":                  []map[string]any{},
		"environment":                StringVal(row, "environment"),
		"materialization_confidence": floatVal(row, "materialization_confidence"),
		"materialization_provenance": StringSliceVal(row, "materialization_provenance"),
		"platform_confidence":        0.0,
		"platform_reason":            "",
	}
}

func observedTopologyEdge(
	relationshipType, sourceID, sourceName, targetID, targetName string,
	properties map[string]any,
) map[string]any {
	edge := platformTopologyEdge(
		relationshipType, sourceID, sourceName, targetID, targetName,
		floatVal(properties, "confidence"), StringVal(properties, "reason"), properties,
	)
	edge["properties"] = copyStringAnyMap(properties)
	return edge
}

func appendUniqueTopologyEdge(rows *[]map[string]any, seen map[string]int, edge map[string]any) {
	key := topologyEdgeKey(edge)
	if key == "" {
		return
	}
	if index, exists := seen[key]; exists {
		if stablePropertiesKey(mapValue(edge, "properties")) < stablePropertiesKey(mapValue((*rows)[index], "properties")) {
			(*rows)[index] = edge
		}
		return
	}
	seen[key] = len(*rows)
	*rows = append(*rows, edge)
}

func stablePropertiesKey(properties map[string]any) string {
	encoded, err := json.Marshal(properties)
	if err != nil {
		return ""
	}
	return string(encoded)
}

func topologyEdgeKey(edge map[string]any) string {
	relationshipType := StringVal(edge, "relationship_type")
	sourceID := StringVal(edge, "source_id")
	targetID := StringVal(edge, "target_id")
	if relationshipType == "" || sourceID == "" || targetID == "" {
		return ""
	}
	return relationshipType + "\x00" + sourceID + "\x00" + targetID
}

func sortTopologyEdges(rows []map[string]any) {
	sort.Slice(rows, func(i, j int) bool { return topologyEdgeKey(rows[i]) < topologyEdgeKey(rows[j]) })
}

func boundedCollectionMetadata(limit, queryLimit, returned, observed int, truncated bool, ordering []string) map[string]any {
	return map[string]any{
		"limit":                         limit,
		"query_sentinel_limit":          queryLimit,
		"returned_count":                returned,
		"observed_count":                observed,
		"observed_count_is_lower_bound": truncated,
		"truncated":                     truncated,
		"ordering":                      ordering,
	}
}

func copyStringAnyMap(source map[string]any) map[string]any {
	copy := make(map[string]any, len(source))
	for key, value := range source {
		copy[key] = value
	}
	return copy
}
