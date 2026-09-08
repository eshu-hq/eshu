// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package entity

// Split out of entity_workload_context.go (P3 review follow-up to #5764) to
// keep that file under the repository's 500-line cap: this half owns the
// direct-runtime WorkloadInstance-to-Platform edge lookup and the resulting
// per-instance platform attachment, which FetchWorkloadContextForOperation
// (entity_workload_context.go) and workload_runtime_topology.go call into but
// do not otherwise share state with.

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// WorkloadPlatformEdgeLimit caps attached-platform edge selection per
// workload read. Exported for the staying live determinism test; see #6060.
const WorkloadPlatformEdgeLimit = querycontract.ContextStoryItemLimit * querycontract.ContextStoryItemLimit

type workloadPlatformResult struct {
	rows   []map[string]any
	limits map[string]any
}

// Rows returns the selected attached-platform rows. Exported as an accessor
// (rather than exporting the struct) for the staying live determinism test
// that pins backend row selection; see #6060.
func (r workloadPlatformResult) Rows() []map[string]any {
	return r.rows
}

// FetchWorkloadPlatformRows reads the attached-platform rows for one
// workload's instances, anchoring platform lookup through the selected
// repository and workload before batching exact instance ids. Exported for
// the staying live determinism test that pins backend row selection;
// see #6060.
func (h *EntityHandler) FetchWorkloadPlatformRows(
	ctx context.Context,
	repoID string,
	workloadID string,
	instances []map[string]any,
) ([]map[string]any, error) {
	result, err := h.FetchWorkloadPlatformResult(ctx, repoID, workloadID, instances)
	return result.rows, err
}

// FetchWorkloadPlatformResult selects the attached-platform rows plus limit disclosure for one workload read. Exported for the staying live determinism test that pins backend row selection; see #6060.
func (h *EntityHandler) FetchWorkloadPlatformResult(
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
	access := querycontract.RepositoryAccessFilterFromContext(ctx)
	// WorkloadInstance and RUNS_ON relationships are global today and do not
	// carry repository ownership, so scoped callers cannot safely consume them.
	if access.Scoped() {
		return emptyWorkloadPlatformResult(), nil
	}
	instanceIDs := make([]string, 0, len(instances))
	for _, instance := range instances {
		if instanceID := querycontract.StringVal(instance, "instance_id"); instanceID != "" {
			instanceIDs = append(instanceIDs, instanceID)
		}
	}
	instanceIDs = querycontract.UniqueSortedStrings(instanceIDs)
	if len(instanceIDs) == 0 {
		return emptyWorkloadPlatformResult(), nil
	}
	queryLimit := WorkloadPlatformEdgeLimit + 1
	platformCypher := fmt.Sprintf(`
		MATCH (repo:Repository)-[:DEFINES]->(w:Workload)<-[:INSTANCE_OF]-(i:WorkloadInstance)-[runsOn:RUNS_ON]->(p:Platform)
		WHERE repo.id = $repo_id AND w.id = $workload_id AND i.id IN $instance_ids%s
		RETURN i.id as instance_id, p.id as platform_id, p.name as platform_name, p.kind as platform_kind,
		       collect(DISTINCT properties(runsOn)) as platform_edges
		ORDER BY instance_id, platform_name, platform_id
		LIMIT $platform_edge_limit
	`, access.GraphPredicate("repo"))
	params := access.GraphParams(map[string]any{
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
	returned, truncated := querycontract.CapMapRows(rows, WorkloadPlatformEdgeLimit)
	for _, row := range returned {
		if len(querycontract.MapValue(row, "platform_edge")) == 0 {
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
			WorkloadPlatformEdgeLimit, queryLimit, len(returned), len(rows), truncated,
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
		if leftInstance, rightInstance := querycontract.StringVal(left, "instance_id"), querycontract.StringVal(right, "instance_id"); leftInstance != rightInstance {
			return leftInstance < rightInstance
		}
		if leftName, rightName := querycontract.StringVal(left, "platform_name"), querycontract.StringVal(right, "platform_name"); leftName != rightName {
			return leftName < rightName
		}
		if leftID, rightID := querycontract.StringVal(left, "platform_id"), querycontract.StringVal(right, "platform_id"); leftID != rightID {
			return leftID < rightID
		}
		return querycontract.StringVal(left, "platform_kind") < querycontract.StringVal(right, "platform_kind")
	})
}

func attachDirectPlatforms(instances []map[string]any, platformRows []map[string]any) {
	byID := make(map[string]map[string]any, len(instances))
	for _, instance := range instances {
		byID[querycontract.StringVal(instance, "instance_id")] = instance
	}
	for _, row := range platformRows {
		instance := byID[querycontract.StringVal(row, "instance_id")]
		if instance == nil {
			continue
		}
		platform := map[string]any{
			"platform_id":         querycontract.StringVal(row, "platform_id"),
			"platform_name":       querycontract.StringVal(row, "platform_name"),
			"platform_kind":       querycontract.StringVal(row, "platform_kind"),
			"platform_confidence": platformEdgeConfidence(row),
			"platform_reason":     platformEdgeReason(row),
			"topology_basis":      "direct_runtime",
			"topology_edges":      []map[string]any{directPlatformTopologyEdge(row)},
		}
		instance["platforms"] = append(platformTargets(instance), platform)
		if querycontract.StringVal(instance, "platform_name") == "" {
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
	if confidence := querycontract.FloatVal(row, "platform_confidence"); confidence != 0 {
		return confidence
	}
	return querycontract.FloatVal(querycontract.MapValue(row, "platform_edge"), "confidence")
}

// platformEdgeReason preserves edge rationale through the same relationship
// properties fallback used for confidence.
func platformEdgeReason(row map[string]any) string {
	if reason := querycontract.StringVal(row, "platform_reason"); reason != "" {
		return reason
	}
	return querycontract.StringVal(querycontract.MapValue(row, "platform_edge"), "reason")
}
