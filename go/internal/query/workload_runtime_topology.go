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
	platformResult, err := h.fetchWorkloadPlatformResult(
		ctx, repoID, StringVal(params, "workload_id"), runtimeResult.instances,
	)
	if err != nil {
		return workloadDeploymentTopologyResult{}, err
	}
	if len(platformResult.limits) == 0 {
		platformResult = emptyWorkloadPlatformResult()
	}
	attachDirectPlatforms(runtimeResult.instances, platformResult.rows)
	provisionedResult := emptyProvisionedPlatformResult()
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
	// WorkloadInstance and INSTANCE_OF relationships are global today and do
	// not carry repository ownership. A scoped caller cannot distinguish shared
	// workload evidence contributed by an authorized repository from evidence
	// contributed by another repository, so this path must fail closed. Omit the
	// limits as well: exact-empty metadata would falsely claim that the withheld
	// collection was completely observed.
	if access.scoped() {
		return workloadRuntimeTopologyResult{
			instances:     []map[string]any{},
			topologyEdges: []map[string]any{},
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
		MATCH (i:WorkloadInstance)-[instanceOf:INSTANCE_OF]->(w:Workload)<-[defines:DEFINES]-(repo:Repository)
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

	// Collect every distinct instance and every topology edge from the
	// bounded row set FIRST, with no length cap on either. Only after the
	// full distinct instance set is sorted into a deterministic order do we
	// truncate to contextStoryItemLimit and drop the edges that no longer
	// reference a retained instance. Capping instances mid-walk (the #5644
	// bug) let the 50 survivors depend on backend row order instead of
	// stable instance identity.
	instances := make([]map[string]any, 0, len(rows))
	topologyEdges := make([]map[string]any, 0, len(rows)*2)
	seenInstances := make(map[string]struct{}, len(rows))
	seenEdges := make(map[string]int, len(rows)*2)
	edgeInstances := make(map[string]map[string]struct{}, len(rows)*2)
	for _, row := range rows {
		instanceID := StringVal(row, "instance_id")
		if instanceID == "" {
			continue
		}
		if _, seen := seenInstances[instanceID]; !seen {
			seenInstances[instanceID] = struct{}{}
			instance, err := newWorkloadInstance(row)
			if err != nil {
				return workloadRuntimeTopologyResult{}, err
			}
			instances = append(instances, instance)
		}
		definesEdge := observedTopologyEdge(
			"DEFINES", StringVal(row, "repo_id"), StringVal(row, "repo_name"),
			StringVal(row, "workload_id"), StringVal(row, "workload_name"), mapValue(row, "defines_edge"),
		)
		if err := appendUniqueTopologyEdge(&topologyEdges, seenEdges, definesEdge); err != nil {
			return workloadRuntimeTopologyResult{}, fmt.Errorf("select DEFINES edge evidence: %w", err)
		}
		recordTopologyEdgeInstance(edgeInstances, definesEdge, instanceID)
		instanceOfEdge := observedTopologyEdge(
			"INSTANCE_OF", instanceID, "", StringVal(row, "workload_id"),
			StringVal(row, "workload_name"), mapValue(row, "instance_edge"),
		)
		if err := appendUniqueTopologyEdge(&topologyEdges, seenEdges, instanceOfEdge); err != nil {
			return workloadRuntimeTopologyResult{}, fmt.Errorf("select INSTANCE_OF edge evidence: %w", err)
		}
		recordTopologyEdgeInstance(edgeInstances, instanceOfEdge, instanceID)
	}
	sortWorkloadInstances(instances)
	distinctInstanceCount := len(instances)
	truncatedByCap := distinctInstanceCount > contextStoryItemLimit
	if truncatedByCap {
		instances = instances[:contextStoryItemLimit]
	}
	retainedInstances := make(map[string]struct{}, len(instances))
	for _, instance := range instances {
		retainedInstances[StringVal(instance, "instance_id")] = struct{}{}
	}
	topologyEdges = retainTopologyEdgesForInstances(topologyEdges, edgeInstances, retainedInstances)
	sortTopologyEdges(topologyEdges)
	truncated := len(rows) >= queryLimit || truncatedByCap
	return workloadRuntimeTopologyResult{
		instances:     instances,
		topologyEdges: topologyEdges,
		limits: boundedCollectionMetadata(
			contextStoryItemLimit, queryLimit, len(instances), len(rows), truncated,
			[]string{"environment", "instance_id"},
		),
	}, nil
}

// recordTopologyEdgeInstance tracks which instance_ids an observed topology
// edge is evidence for. An INSTANCE_OF edge is unique to one instance, but a
// DEFINES edge (repo -> workload) is shared by every instance of that
// workload from that repo, so it must survive truncation if ANY of those
// instances is retained.
func recordTopologyEdgeInstance(edgeInstances map[string]map[string]struct{}, edge map[string]any, instanceID string) {
	key := topologyEdgeKey(edge)
	if key == "" {
		return
	}
	instances, ok := edgeInstances[key]
	if !ok {
		instances = make(map[string]struct{}, 1)
		edgeInstances[key] = instances
	}
	instances[instanceID] = struct{}{}
}

// retainTopologyEdgesForInstances filters topology edges built from the full
// distinct instance set down to only those still backed by a retained
// (post-truncation) instance, so truncated instances do not leave dangling
// edges in the response.
func retainTopologyEdgesForInstances(
	edges []map[string]any,
	edgeInstances map[string]map[string]struct{},
	retainedInstances map[string]struct{},
) []map[string]any {
	retained := make([]map[string]any, 0, len(edges))
	for _, edge := range edges {
		for instanceID := range edgeInstances[topologyEdgeKey(edge)] {
			if _, ok := retainedInstances[instanceID]; ok {
				retained = append(retained, edge)
				break
			}
		}
	}
	return retained
}

func newWorkloadInstance(row map[string]any) (map[string]any, error) {
	instanceID := StringVal(row, "instance_id")
	materializationConfidence, err := finiteGraphFloat(
		row,
		"materialization_confidence",
		fmt.Sprintf("workload instance %q", instanceID),
	)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"instance_id":                instanceID,
		"platform_name":              "",
		"platform_kind":              "",
		"platforms":                  []map[string]any{},
		"environment":                StringVal(row, "environment"),
		"materialization_confidence": materializationConfidence,
		"materialization_provenance": StringSliceVal(row, "materialization_provenance"),
		"platform_confidence":        0.0,
		"platform_reason":            "",
	}, nil
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

func appendUniqueTopologyEdge(rows *[]map[string]any, seen map[string]int, edge map[string]any) error {
	key := topologyEdgeKey(edge)
	if key == "" {
		return nil
	}
	propertiesKey, err := stablePropertiesKey(mapValue(edge, "properties"))
	if err != nil {
		return err
	}
	if index, exists := seen[key]; exists {
		existingPropertiesKey, err := stablePropertiesKey(mapValue((*rows)[index], "properties"))
		if err != nil {
			return err
		}
		if propertiesKey < existingPropertiesKey {
			(*rows)[index] = edge
		}
		return nil
	}
	seen[key] = len(*rows)
	*rows = append(*rows, edge)
	return nil
}

func stablePropertiesKey(properties map[string]any) (string, error) {
	encoded, err := json.Marshal(properties)
	if err != nil {
		return "", fmt.Errorf("marshal graph relationship properties: %w", err)
	}
	return string(encoded), nil
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

// sortWorkloadInstances orders runtime instances by environment and then by
// stable instance identity. NornicDB can replay an identical Cypher ORDER BY
// as a different row set order across calls over unchanged retained data
// (see docs/internal/evidence/5272-service-story-runtime-topology.md and
// issue #5644), so relying on the backend's row order alone let repeated
// calls return the same instances in different array positions.
// instance_id is unique per WorkloadInstance, so this is a total order
// regardless of backend row order.
func sortWorkloadInstances(instances []map[string]any) {
	sort.Slice(instances, func(i, j int) bool {
		left, right := instances[i], instances[j]
		if leftEnv, rightEnv := StringVal(left, "environment"), StringVal(right, "environment"); leftEnv != rightEnv {
			return leftEnv < rightEnv
		}
		return StringVal(left, "instance_id") < StringVal(right, "instance_id")
	})
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

func emptyBoundedCollectionMetadata(limit int, ordering []string) map[string]any {
	return boundedCollectionMetadata(limit, limit+1, 0, 0, false, ordering)
}

func emptyWorkloadPlatformResult() workloadPlatformResult {
	return workloadPlatformResult{
		rows: []map[string]any{},
		limits: emptyBoundedCollectionMetadata(
			workloadPlatformEdgeLimit,
			[]string{"instance_id", "platform_name", "platform_id"},
		),
	}
}

func copyStringAnyMap(source map[string]any) map[string]any {
	copy := make(map[string]any, len(source))
	for key, value := range source {
		copy[key] = value
	}
	return copy
}
