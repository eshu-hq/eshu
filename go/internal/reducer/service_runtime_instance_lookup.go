// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// GraphServiceRuntimeInstanceLoader loads the materialized runtime instances of a
// service's workload from the canonical graph, scoped by repository. It is the
// graph-backed implementation of RepositoryScopedRuntimeInstanceLoader that the
// service catalog correlation handler uses to source the runtime evidence family
// (#1986, part of #1943/#1797). It reads only durable platform/environment/
// workload identity off the WorkloadInstance and Platform nodes, never a
// resolution or materialization generation id, so the runtime
// service_evidence_key stays generation-stable across re-materializations.
type GraphServiceRuntimeInstanceLoader struct {
	// Graph runs the bounded read-only runtime-instance queries. It is required;
	// a nil graph is treated as a wiring error rather than an empty result so the
	// runtime family never silently disappears when the loader is misconfigured.
	Graph GraphQueryRunner
}

// GetRuntimeInstancesForRepos returns the materialized runtime instances grouped
// by repository id. Each repository is read with two bounded scalar queries that
// mirror the query surface's WorkloadInstance reads (entity_workload_context.go)
// so the loader truth and the API truth come from the same node reads: first the
// WorkloadInstance nodes anchored on the workload_instance_repo_id index, then
// their RUNS_ON platforms anchored on the indexed WorkloadInstance ids. Scalar
// queries (no OPTIONAL MATCH or map projection) keep the read NornicDB-portable,
// matching the optional-projection-safety contract the query layer enforces.
//
// A WorkloadInstance with multiple inferred platforms yields one row per platform
// because each platform kind is a distinct runtime identity; an instance with no
// inferred platform still surfaces (with empty platform fields) so it can key on
// its durable environment identity. Instances without a durable WorkloadInstance
// id are skipped because they cannot be keyed into a generation-stable runtime
// row.
func (l GraphServiceRuntimeInstanceLoader) GetRuntimeInstancesForRepos(
	ctx context.Context,
	repoIDs []string,
) (map[string][]ServiceRuntimeInstance, error) {
	if len(repoIDs) == 0 {
		return nil, nil
	}
	if l.Graph == nil {
		return nil, fmt.Errorf("graph service runtime instance loader requires graph query runner")
	}

	result := make(map[string][]ServiceRuntimeInstance, len(repoIDs))
	for _, repoID := range repoIDs {
		repoID = strings.TrimSpace(repoID)
		if repoID == "" {
			continue
		}
		instances, err := l.loadRepoRuntimeInstances(ctx, repoID)
		if err != nil {
			return nil, err
		}
		if len(instances) > 0 {
			result[repoID] = instances
		}
	}
	return result, nil
}

// loadRepoRuntimeInstances reads the durable runtime instances for one repository
// and joins each WorkloadInstance to its RUNS_ON platforms in process.
func (l GraphServiceRuntimeInstanceLoader) loadRepoRuntimeInstances(
	ctx context.Context,
	repoID string,
) ([]ServiceRuntimeInstance, error) {
	instanceRows, err := l.Graph.Run(ctx, serviceRuntimeWorkloadInstancesByRepoCypher, map[string]any{
		"repo_id": repoID,
	})
	if err != nil {
		return nil, fmt.Errorf("load runtime workload instances for repo %q: %w", repoID, err)
	}

	type instanceFields struct {
		workloadName string
		environment  string
		confidence   float64
	}
	byID := make(map[string]instanceFields, len(instanceRows))
	instanceIDs := make([]string, 0, len(instanceRows))
	for _, row := range instanceRows {
		instanceID := anyToString(row["instance_id"])
		if instanceID == "" {
			continue
		}
		if _, seen := byID[instanceID]; seen {
			continue
		}
		byID[instanceID] = instanceFields{
			workloadName: anyToString(row["workload_name"]),
			environment:  anyToString(row["environment"]),
			confidence:   anyToFloat(row["materialization_confidence"]),
		}
		instanceIDs = append(instanceIDs, instanceID)
	}
	if len(instanceIDs) == 0 {
		return nil, nil
	}

	platformsByInstance, err := l.loadRuntimePlatforms(ctx, repoID, instanceIDs)
	if err != nil {
		return nil, err
	}

	instances := make([]ServiceRuntimeInstance, 0, len(instanceIDs))
	for _, instanceID := range instanceIDs {
		fields := byID[instanceID]
		platforms := platformsByInstance[instanceID]
		if len(platforms) == 0 {
			// No inferred runtime platform: the instance still carries a durable
			// environment identity, so surface it with empty platform fields.
			instances = append(instances, ServiceRuntimeInstance{
				Environment:  fields.environment,
				WorkloadRef:  instanceID,
				WorkloadName: fields.workloadName,
				Confidence:   fields.confidence,
			})
			continue
		}
		for _, platform := range platforms {
			instances = append(instances, ServiceRuntimeInstance{
				PlatformKind: platform.kind,
				Environment:  fields.environment,
				WorkloadRef:  instanceID,
				PlatformName: platform.name,
				WorkloadName: fields.workloadName,
				Confidence:   fields.confidence,
			})
		}
	}
	return instances, nil
}

// runtimePlatform is a single durable RUNS_ON platform of a WorkloadInstance.
type runtimePlatform struct {
	kind string
	name string
}

// loadRuntimePlatforms reads the RUNS_ON platforms for the given WorkloadInstance
// ids, grouped by instance id. The lookup is anchored on the indexed
// WorkloadInstance ids so it is one bounded read rather than a round trip per
// instance, mirroring fetchWorkloadPlatformRows on the query surface.
func (l GraphServiceRuntimeInstanceLoader) loadRuntimePlatforms(
	ctx context.Context,
	repoID string,
	instanceIDs []string,
) (map[string][]runtimePlatform, error) {
	rows, err := l.Graph.Run(ctx, serviceRuntimePlatformsByInstanceCypher, map[string]any{
		"instance_ids": instanceIDs,
	})
	if err != nil {
		return nil, fmt.Errorf("load runtime platforms for repo %q: %w", repoID, err)
	}
	platformsByInstance := make(map[string][]runtimePlatform, len(instanceIDs))
	for _, row := range rows {
		instanceID := anyToString(row["instance_id"])
		kind := anyToString(row["platform_kind"])
		name := anyToString(row["platform_name"])
		if instanceID == "" || (kind == "" && name == "") {
			continue
		}
		platformsByInstance[instanceID] = append(platformsByInstance[instanceID], runtimePlatform{
			kind: kind,
			name: name,
		})
	}
	for instanceID := range platformsByInstance {
		platforms := platformsByInstance[instanceID]
		sort.Slice(platforms, func(i, j int) bool {
			if platforms[i].kind != platforms[j].kind {
				return platforms[i].kind < platforms[j].kind
			}
			return platforms[i].name < platforms[j].name
		})
		platformsByInstance[instanceID] = platforms
	}
	return platformsByInstance, nil
}

// serviceRuntimeWorkloadInstancesByRepoCypher reads the durable WorkloadInstance
// nodes for one repository. It anchors on the workload_instance_repo_id index
// (graph/schema.go: CREATE INDEX workload_instance_repo_id ... FOR
// (i:WorkloadInstance) ON (i.repo_id)) so the read is a bounded indexed lookup
// rather than a label scan. The projection is scalar (no OPTIONAL MATCH or map
// projection) and carries no generation-bearing field: i.id is the durable
// instance identity (workload-instance:<workload_name>:<environment>).
const serviceRuntimeWorkloadInstancesByRepoCypher = `MATCH (i:WorkloadInstance {repo_id: $repo_id})
RETURN i.repo_id AS repo_id,
       i.id AS instance_id,
       i.name AS workload_name,
       i.environment AS environment,
       i.materialization_confidence AS materialization_confidence
ORDER BY instance_id`

// serviceRuntimePlatformsByInstanceCypher reads the RUNS_ON platforms for an
// exact batch of WorkloadInstance ids. Anchoring on i.id IN $instance_ids lets
// the backend use the indexed WorkloadInstance ids (workload_instance_id_lookup)
// for one bounded read instead of one Bolt round trip per instance, and the
// scalar p.kind/p.name projection stays NornicDB-portable. It carries no
// generation-bearing field.
const serviceRuntimePlatformsByInstanceCypher = `MATCH (i:WorkloadInstance)-[:RUNS_ON]->(p:Platform)
WHERE i.id IN $instance_ids
RETURN i.id AS instance_id,
       p.kind AS platform_kind,
       p.name AS platform_name
ORDER BY instance_id, platform_kind`

// anyToFloat extracts a float64 from a graph row value, tolerating the integer
// and float numeric kinds Bolt drivers can return. A nil or non-numeric value
// yields 0 so a missing confidence does not fail the read.
func anyToFloat(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case float32:
		return float64(n)
	case int64:
		return float64(n)
	case int:
		return float64(n)
	default:
		return 0
	}
}
