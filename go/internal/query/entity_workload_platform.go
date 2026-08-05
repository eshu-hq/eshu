// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

// Split out of entity_workload_context.go (P3 review follow-up to #5764) to
// keep that file under the repository's 500-line cap: this half owns the
// direct-runtime WorkloadInstance-to-Platform edge lookup and the resulting
// per-instance platform attachment, which fetchWorkloadContextForOperation
// (entity_workload_context.go) and workload_runtime_topology.go call into but
// do not otherwise share state with.

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

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
