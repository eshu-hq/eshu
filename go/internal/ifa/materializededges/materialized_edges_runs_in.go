// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package materializededges

import (
	"fmt"
	"path/filepath"
	"sort"

	"github.com/eshu-hq/eshu/go/internal/ifa"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// runsInFamily is the materialized-edge family key this guard asserts
// (#6000), matching the domain key MaterializedEdgeDomainEdgeTypes switches
// on and the registry entry cypher.SingleTypeMaterializedEdgeTypes resolves
// it through.
const runsInFamily = "runs_in"

// runsInExpectedEdgesPath joins repoRoot onto the family's hand-derived
// expected-edge-set fixture.
func runsInExpectedEdgesPath(repoRoot string) string {
	return filepath.Join(repoRoot, "go/internal/ifa/testdata/runsin/ifa-runs-in-family-expected-edges.json")
}

// resolveRunsInMaterializedEdges is runs_in's named vacuity guard (#6000).
//
// RUNS_IN's crux (see materialized_edges_symbol_runtime_shared.go): the
// intent row carries only (function_id, repo_id) -- it has no visibility
// into how many Workloads the repository DEFINES, because that admission
// runs in a wholly separate handler over different facts
// (workload_materialization, not code_call_materialization). The live
// Cypher's MATCH is (Repository)-[:DEFINES]->(Workload) with NO LIMIT, so ONE
// intent row fans OUT to N graph edges for N Workloads. A guard that
// converted intent rows to ExpectedEdge one-for-one would silently miss
// every edge past the first Workload. This guard instead derives the fan-out
// offline: it runs the SAME reducer.ExtractWorkloadCandidates ->
// reducer.BuildProjectionRows seam the live workload-materialization handler
// runs over the Odù's own facts, reads the resulting WorkloadRows (an
// exported field), and emits one ExpectedEdge per (function_id,
// workload_id) pair -- exactly the cross product a live MATCH with no LIMIT
// produces.
func resolveRunsInMaterializedEdges(odu ifa.Odu, expectedEdgesPath string) (bool, string) {
	expected, err := LoadExpectedEdges(expectedEdgesPath, runsInFamily)
	if err != nil {
		return false, err.Error()
	}
	registry, err := MaterializedEdgeDomainEdgeTypes(runsInFamily)
	if err != nil {
		return false, err.Error()
	}
	if missing := missingSymbolRuntimeExpectedTypes(expected, registry); len(missing) > 0 {
		return false, fmt.Sprintf("odù %q: expected-edge set does not cover all registry types, missing: %v", odu.Name, missing)
	}
	relType, err := singleRegistryEdgeType(runsInFamily, registry)
	if err != nil {
		return false, err.Error()
	}

	if len(odu.Facts) == 0 {
		return false, fmt.Sprintf("odù %q: carries no facts", odu.Name)
	}
	rows := symbolRuntimeUpsertRows(odu.Facts, reducer.DomainRunsIn)
	if len(rows) == 0 {
		return false, fmt.Sprintf("odù %q: ExtractSymbolRuntimeIntentRows produced zero RUNS_IN upsert rows; this fixture cannot prove anything", odu.Name)
	}

	candidates, deploymentEnvironments := reducer.ExtractWorkloadCandidates(odu.Facts)
	projection := reducer.BuildProjectionRows(candidates, deploymentEnvironments)
	workloadIDs := runsInWorkloadIDsByRepo(projection.WorkloadRows)

	actual, unresolved := runsInRowsToExpectedEdges(rows, workloadIDs, relType)
	if len(unresolved) > 0 {
		return false, fmt.Sprintf("odù %q: repository/ies %v define no Workload in the workload-materialization projection; RUNS_IN cannot fan out without at least one Workload", odu.Name, unresolved)
	}

	if mismatch := compareSymbolRuntimeExpectedEdges(odu.Name, "runs in", expected, actual); mismatch != "" {
		return false, mismatch
	}
	return true, fmt.Sprintf(
		"odù %q: ExtractSymbolRuntimeIntentRows + ExtractWorkloadCandidates/BuildProjectionRows reproduces the expected %d-edge RUNS_IN set exactly, fanned out from %d intent row(s) across %d fact envelope(s)",
		odu.Name, len(expected), len(rows), len(odu.Facts),
	)
}

// runsInWorkloadIDsByRepo indexes a workload projection's WorkloadRows by
// repo_id, preserving every Workload a repository DEFINES so
// runsInRowsToExpectedEdges can reproduce the live Cypher's unbounded
// fan-out exactly.
func runsInWorkloadIDsByRepo(rows []reducer.WorkloadRow) map[string][]string {
	out := make(map[string][]string, len(rows))
	for _, row := range rows {
		out[row.RepoID] = append(out[row.RepoID], row.WorkloadID)
	}
	for repoID := range out {
		sort.Strings(out[repoID])
	}
	return out
}

// runsInRowsToExpectedEdges fans each RUNS_IN upsert row out against every
// Workload its repository DEFINES, reproducing the cross product the live
// (Repository)-[:DEFINES]->(Workload) MATCH with no LIMIT performs. It
// returns the sorted list of repo_ids it could not resolve at least one
// Workload for, so the caller can report every gap at once.
//
// This is the one function a synthetic unit test drives directly to prove
// the N-Workload fan-out deterministically (one row, two workload ids for
// its repo, must yield exactly two ExpectedEdges), without needing a live
// backend or a second-workload fixture.
func runsInRowsToExpectedEdges(
	rows []reducer.SharedProjectionIntentRow,
	workloadIDs map[string][]string,
	relationshipType string,
) (edges []ExpectedEdge, unresolved []string) {
	unresolvedSet := make(map[string]struct{})
	for _, row := range rows {
		functionID := anyToStringValue(row.Payload["function_id"])
		repoID := anyToStringValue(row.Payload["repo_id"])
		ids, ok := workloadIDs[repoID]
		if !ok || len(ids) == 0 {
			unresolvedSet[repoID] = struct{}{}
			continue
		}
		for _, workloadID := range ids {
			edges = append(edges, ExpectedEdge{
				RelationshipType: relationshipType,
				SourceEntityID:   functionID,
				TargetEntityID:   workloadID,
			})
		}
	}
	for repoID := range unresolvedSet {
		unresolved = append(unresolved, repoID)
	}
	sort.Strings(unresolved)
	return edges, unresolved
}
