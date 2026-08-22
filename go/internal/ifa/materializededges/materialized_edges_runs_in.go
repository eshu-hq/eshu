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
// Cypher's MATCH is (Repository)-[:DEFINES]->(Workload) with NO LIMIT
// (go/internal/storage/cypher/canonical_runs_in_edges.go:26), so the WRITE
// TEMPLATE imposes no cap on how many Workloads one row could bind to. This
// guard therefore derives its fan-out offline rather than assuming 1-to-1: it
// runs the SAME reducer.ExtractWorkloadCandidates -> reducer.BuildProjectionRows
// seam the live workload-materialization handler runs over the Odù's own
// facts, reads the resulting WorkloadRows (an exported field), and emits one
// ExpectedEdge per (function_id, workload_id) pair actually present in that
// projection -- never a hardcoded count.
//
// What the write template PERMITS and what today's reducer candidate path
// CAN PRODUCE for one repository are two different claims, and this guard
// only exercises the second one live. ExtractWorkloadCandidates aggregates
// every workload signal into one repoSignals entry keyed by repo_id alone
// (go/internal/reducer/candidate_loader.go:37,50,69), and BuildProjectionRows
// emits exactly one WorkloadRow per WorkloadCandidate
// (go/internal/reducer/projection.go:259-301, one loop iteration per
// candidate, deduped by workloadID) -- so a single repository yields AT MOST
// ONE Workload through today's candidate path. The N>1-Workload cross
// product the no-LIMIT template permits is real and DEFENDED here (the
// fan-out is derived from the actual projection, never assumed 1-to-1), but
// it is not EXERCISED end to end by any live fixture, because the reducer's
// own candidate extraction cannot currently hand one repository more than one
// Workload to fan out against.
//
// The fixture carries two distinct route-bound handlers (HandleWidgets and
// HandleHealth, on distinct paths) so this is not a one-edge assertion that
// could not distinguish "processed every route-bound function" from
// "processed only the first one": a regression that stopped after the
// first resolved handler would drop the second edge, and the exact-set
// comparison below would report it as MISSING by name. The repository
// DEFINES exactly one Workload in this fixture -- the only shape today's
// candidate path can produce for one repo -- so the fan-out per row stays
// 1-to-1 here (2 rows, 2 edges); the N>1 Workload cross product this guard
// would perform if the projection ever contained one is proven only by a
// synthetic offline unit test that hand-builds a two-entry workloadIDs map
// under one repo key (materialized_edges_runs_in_test.go), never by a live
// fixture -- no live fixture can produce that shape today.
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
// backend or a second-workload fixture -- and it has to be synthetic: no live
// fixture can currently produce a two-Workload repository, because
// reducer.ExtractWorkloadCandidates/BuildProjectionRows cap one repository at
// one Workload today (see resolveRunsInMaterializedEdges's doc comment). The
// write template this function's cross product mirrors has no such cap, so
// the synthetic case defends a real capability of the MERGE, not a
// hypothetical one.
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
