// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package materializededges

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/ifa"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/relationships"
)

// workloadDependencyEdgesFamily is the materialized-edge family key this
// guard asserts. It MUST equal "workload_dependency" -- the domain key
// reducer.DomainWorkloadDependency and cypher's singleTypeMaterializedEdgeFamilies
// both key on (materialized_edge_families.go).
const workloadDependencyEdgesFamily = "workload_dependency"

// workloadDependencyRelationshipType is the family's one relationship type.
// It is spelled out as a literal rather than importing
// go/internal/graph/edgetype solely for one string constant this file uses
// twice, matching repoDependencyRunsOnType's precedent in the sibling file.
const workloadDependencyRelationshipType = "DEPENDS_ON"

// workloadDependencyGuardClock is the fixed, deterministic wall-clock reading
// this guard hands to reducer.ExtractRepoDependencyIntentRows for every repo-
// edge row's CreatedAt. go/internal/ifa/AGENTS.md forbids wall-clock time
// inside Ifá derivation; CreatedAt is never part of the edge-identity
// comparison this guard performs (mirrors repoDependencyGuardClock's role
// exactly -- this guard reuses the same repo-to-repo extraction seam
// repo_dependency's guard runs, see resolveWorkloadDependencyMaterializedEdges's
// doc comment).
var workloadDependencyGuardClock = time.Date(2026, 8, 19, 0, 0, 0, 0, time.UTC)

// workloadDependencyGuardSourceRunID is the fixed source_run_id this guard
// hands to reducer.ExtractRepoDependencyIntentRows. Never part of the edge-
// identity comparison this guard performs (mirrors
// repoDependencyGuardSourceRunID).
const workloadDependencyGuardSourceRunID = "ifa-workload-dependency-guard-run"

// workloadDependencyExpectedEdgesRelPath is where the family's hand-derived
// expected-edge-set fixture lives. Under go/internal/ifa/testdata/, not
// testdata/cassettes/, for the same reason the sibling families' fixtures
// are: the offline cassette validator globs every testdata/cassettes/*/*.json
// as a replay cassette, and this file is a gate ASSERTION, not a cassette.
const workloadDependencyExpectedEdgesRelPath = "go/internal/ifa/testdata/workloaddependency/ifa-workload-dependency-family-expected-edges.json"

// workloadDependencyFamilyExpectedEdgesPath joins repoRoot onto the expected-
// edge fixture.
func workloadDependencyFamilyExpectedEdgesPath(repoRoot string) string {
	return filepath.Join(repoRoot, workloadDependencyExpectedEdgesRelPath)
}

// workloadDependencyFamilyGraphLookup implements
// reducer.WorkloadDependencyGraphLookup entirely from this Odù's own
// production-derived data plus a small, explicitly-named set of PERSISTED
// entries the fixture's doc comment (workload_dependency_family_catalog.go)
// names -- simulating what a live graph backend's ListRepoWorkloads would
// report for a repo whose workload record predates this generation. Both list
// methods filter on repoIDs exactly as the production Neo4j adapter does;
// returning unrelated rows would let this guard certify an input production
// can never supply.
// RepoEdges comes from the SAME evidence/resolution seam repo_dependency's
// guard runs (resolveWorkloadDependencyMaterializedEdges's doc comment); it
// is never hand-authored. ListWorkloadDependencyEdges always returns empty:
// this is a first-generation Odù, so the retract path
// ReconcileWorkloadDependencyEdges also exercises is provably nil here --
// nothing this guard asserts depends on retraction.
type workloadDependencyFamilyGraphLookup struct {
	repoEdges          []reducer.RepoDependencyEdge
	persistedWorkloads []reducer.RepoWorkload
}

// ListRepoDependencyEdges returns derived repo-to-repo DEPENDS_ON edges whose
// source or target is one of repoIDs, matching neo4jWorkloadDependencyLookup's
// two anchored query branches.
func (l workloadDependencyFamilyGraphLookup) ListRepoDependencyEdges(_ context.Context, repoIDs []string) ([]reducer.RepoDependencyEdge, error) {
	requested := workloadDependencyRepoIDSet(repoIDs)
	edges := make([]reducer.RepoDependencyEdge, 0, len(l.repoEdges))
	for _, edge := range l.repoEdges {
		_, sourceRequested := requested[edge.SourceRepoID]
		_, targetRequested := requested[edge.TargetRepoID]
		if sourceRequested || targetRequested {
			edges = append(edges, edge)
		}
	}
	return edges, nil
}

// ListRepoWorkloads returns persisted workloads owned by repoIDs, matching the
// production adapter's repo.id IN $repo_ids predicate.
func (l workloadDependencyFamilyGraphLookup) ListRepoWorkloads(_ context.Context, repoIDs []string) ([]reducer.RepoWorkload, error) {
	requested := workloadDependencyRepoIDSet(repoIDs)
	workloads := make([]reducer.RepoWorkload, 0, len(l.persistedWorkloads))
	for _, workload := range l.persistedWorkloads {
		if _, ok := requested[workload.RepoID]; ok {
			workloads = append(workloads, workload)
		}
	}
	return workloads, nil
}

// ListWorkloadDependencyEdges always returns empty: see this type's doc
// comment.
func (l workloadDependencyFamilyGraphLookup) ListWorkloadDependencyEdges(_ context.Context, _ []string, _ string) ([]reducer.ExistingWorkloadDependencyEdge, error) {
	return nil, nil
}

// resolveWorkloadDependencyMaterializedEdges is workload_dependency's named
// vacuity guard (#6003), mirroring resolveRepoDependencyMaterializedEdges's
// role for its own family.
//
// Unlike every other single-type family's guard, this one derives its input
// through TWO independent production seams rather than one, because
// reducer.ReconcileWorkloadDependencyEdges itself takes two independently
// derived inputs:
//
//  1. The repo-to-repo DEPENDS_ON edge set (reducer.RepoDependencyEdge),
//     derived through the EXACT SAME backend-free seam
//     resolveRepoDependencyMaterializedEdges already runs over this Odù's own
//     facts -- DiscoveredEvidence -> relationships.Resolve -> the resolved
//     rows reducer.ExtractRepoDependencyIntentRows routes to relationship_type
//     "DEPENDS_ON". A guard that hand-authored a reducer.RepoDependencyEdge
//     instead would certify a repo pair no live evidence-resolution path
//     could actually produce, the same false-green class the #5351 live-proof
//     finding warns about for endpoint identity.
//  2. The current-generation repo-to-workload map (reducer.RepoDescriptor),
//     derived through reducer.ExtractWorkloadCandidates ->
//     reducer.BuildProjectionRowsWithInfrastructurePlatforms -- the exact pure
//     seam production's workload materialization handler runs immediately
//     before calling reducer.ReconcileWorkloadDependencyEdges
//     (workload_materialization_handler.go:305-308).
//
// Both seams' outputs then drive the REAL reducer.ReconcileWorkloadDependencyEdges
// and reducer.BuildWorkloadDependencyIntentRowsFromEdges against
// workloadDependencyFamilyGraphLookup, an in-memory
// reducer.WorkloadDependencyGraphLookup built from those two seams' outputs
// plus a small, explicitly-named persisted-workload set (this file's
// workloadDependencyFamilyGraphLookup doc comment). The multi-workload pair
// proves ReconcileWorkloadDependencyEdges' ambiguity drop actually fires. The
// orphan pair proves the lookup stays production-anchored: because neither
// endpoint is current, the live query cannot return it and this fake must not
// smuggle it into reconciliation.
//
// What this guard DOES catch: a regression in Docker Compose depends_on
// evidence discovery or resolution; a regression in
// reducer.ExtractRepoDependencyIntentRows' DEPENDS_ON routing; a regression in
// reducer.ExtractWorkloadCandidates'/BuildProjectionRowsWithInfrastructurePlatforms'
// Kubernetes-Deployment-to-workload-candidate admission; and a regression in
// either of ReconcileWorkloadDependencyEdges' two drop conditions. What it
// does NOT catch: anything downstream of the live Cypher writer (that is
// `eshu-ifa assert-edges`' job once a coverage-manifest row and live
// determinism/fault-injection cells exist for this family), and the retract
// path (this Odù is first-generation, so ListWorkloadDependencyEdges always
// returns empty here).
func resolveWorkloadDependencyMaterializedEdges(odu ifa.Odu, expectedEdgesPath string) (bool, string) {
	expected, err := LoadExpectedEdges(expectedEdgesPath, workloadDependencyEdgesFamily)
	if err != nil {
		return false, err.Error()
	}
	if len(expected) == 0 {
		return false, fmt.Sprintf("odù %q: workload-dependency expected edges %s declares no edges; an empty expected set would make the gate vacuous", odu.Name, expectedEdgesPath)
	}
	registry, err := MaterializedEdgeDomainEdgeTypes(workloadDependencyEdgesFamily)
	if err != nil {
		return false, err.Error()
	}
	if missing := missingWorkloadDependencyExpectedTypes(expected, registry); len(missing) > 0 {
		return false, fmt.Sprintf("odù %q: expected-edge set does not cover all registry types, missing: %v", odu.Name, missing)
	}

	if len(odu.Facts) == 0 {
		return false, fmt.Sprintf("odù %q: carries no facts", odu.Name)
	}
	first := odu.Facts[0]

	// Seam 1: repo-to-repo DEPENDS_ON edges.
	evidence := ifa.DiscoveredEvidence(odu)
	if len(evidence) == 0 {
		return false, fmt.Sprintf("odù %q: DiscoveredEvidence produced zero evidence facts from this Odù's own Docker Compose depends_on content facts", odu.Name)
	}
	_, resolved := relationships.Resolve(evidence, nil, relationships.DefaultConfidenceThreshold)
	if len(resolved) == 0 {
		return false, fmt.Sprintf("odù %q: the production evidence/resolution seams (DiscoveredEvidence -> relationships.Resolve) found zero resolved relationships in its own facts; workload_dependency cannot admit any edge without at least one resolved repo-to-repo DEPENDS_ON relationship", odu.Name)
	}
	depRows, _ := reducer.ExtractRepoDependencyIntentRows(resolved, first.ScopeID, workloadDependencyGuardSourceRunID, first.GenerationID, workloadDependencyGuardClock)
	repoEdges := workloadDependencyRepoEdgesFromRows(depRows)
	if len(repoEdges) != 3 {
		return false, fmt.Sprintf("odù %q: evidence resolution produced %d repo-to-repo DEPENDS_ON edge(s), want exactly 3 (positive, multi-workload, and an orphan pair that the production-shaped lookup must filter out)", odu.Name, len(repoEdges))
	}

	// Seam 2: current-generation repo-to-workload map.
	candidates, deploymentEnvs := reducer.ExtractWorkloadCandidates(odu.Facts)
	if len(candidates) == 0 {
		return false, fmt.Sprintf("odù %q: ExtractWorkloadCandidates admitted zero workload candidates from this Odù's own Kubernetes Deployment file facts", odu.Name)
	}
	projection := reducer.BuildProjectionRowsWithInfrastructurePlatforms(candidates, deploymentEnvs, nil)
	if projection == nil || len(projection.RepoDescriptors) != 4 {
		got := 0
		if projection != nil {
			got = len(projection.RepoDescriptors)
		}
		return false, fmt.Sprintf("odù %q: BuildProjectionRowsWithInfrastructurePlatforms admitted %d RepoDescriptor(s), want exactly 4 (positive source/target, multi-workload source/target); the orphan pair must NOT admit a descriptor", odu.Name, got)
	}

	lookup := workloadDependencyFamilyGraphLookup{
		repoEdges: repoEdges,
		persistedWorkloads: []reducer.RepoWorkload{
			{RepoID: ifa.WorkloadDependencyFamilyMultiTargetRepoID, WorkloadID: ifa.WorkloadDependencyFamilyMultiTargetPhantomWorkloadID},
			{RepoID: ifa.WorkloadDependencyFamilyOrphanSourceRepoID, WorkloadID: ifa.WorkloadDependencyFamilyOrphanSourcePersistedWorkloadID},
			{RepoID: ifa.WorkloadDependencyFamilyOrphanTargetRepoID, WorkloadID: ifa.WorkloadDependencyFamilyOrphanTargetPersistedWorkloadID},
		},
	}

	admitted, _, err := reducer.ReconcileWorkloadDependencyEdges(context.Background(), projection.RepoDescriptors, lookup)
	if err != nil {
		return false, fmt.Sprintf("odù %q: ReconcileWorkloadDependencyEdges: %v", odu.Name, err)
	}
	if detail := workloadDependencyAssertSelectiveAdmission(odu.Name, admitted); detail != "" {
		return false, detail
	}

	writeRows := reducer.BuildWorkloadDependencyIntentRowsFromEdges(admitted)
	if len(writeRows) != len(admitted) {
		return false, fmt.Sprintf("odù %q: BuildWorkloadDependencyIntentRowsFromEdges dropped %d of %d admitted row(s)", odu.Name, len(admitted)-len(writeRows), len(admitted))
	}

	actual := make([]ExpectedEdge, 0, len(writeRows))
	for _, row := range writeRows {
		actual = append(actual, ExpectedEdge{
			RelationshipType: workloadDependencyRelationshipType,
			SourceEntityID:   anyToStringValue(row.Payload["workload_id"]),
			TargetEntityID:   anyToStringValue(row.Payload["target_workload_id"]),
		})
	}
	if mismatch := compareWorkloadDependencyExpectedEdges(odu.Name, expected, actual); mismatch != "" {
		return false, mismatch
	}

	return true, fmt.Sprintf(
		"odù %q: DiscoveredEvidence -> relationships.Resolve -> ExtractRepoDependencyIntentRows (repo-to-repo seam) plus ExtractWorkloadCandidates -> BuildProjectionRowsWithInfrastructurePlatforms (repo-to-workload seam) feed the production-anchored lookup and real ReconcileWorkloadDependencyEdges to reproduce the expected %d-edge set exactly; the multi-workload ambiguity is dropped and the neither-current orphan pair remains unreachable through the live lookup contract",
		odu.Name, len(expected),
	)
}

func workloadDependencyRepoIDSet(repoIDs []string) map[string]struct{} {
	result := make(map[string]struct{}, len(repoIDs))
	for _, repoID := range repoIDs {
		if repoID = strings.TrimSpace(repoID); repoID != "" {
			result[repoID] = struct{}{}
		}
	}
	return result
}

// workloadDependencyRepoEdgesFromRows narrows
// reducer.ExtractRepoDependencyIntentRows' output to the rows this family
// cares about (relationship_type "DEPENDS_ON"), projecting them onto
// reducer.RepoDependencyEdge -- the exact input shape
// reducer.WorkloadDependencyGraphLookup.ListRepoDependencyEdges returns.
func workloadDependencyRepoEdgesFromRows(rows []reducer.SharedProjectionIntentRow) []reducer.RepoDependencyEdge {
	var edges []reducer.RepoDependencyEdge
	for _, row := range rows {
		if anyToStringValue(row.Payload["relationship_type"]) != workloadDependencyRelationshipType {
			continue
		}
		sourceRepoID := anyToStringValue(row.Payload["repo_id"])
		targetRepoID := anyToStringValue(row.Payload["target_repo_id"])
		if sourceRepoID == "" || targetRepoID == "" {
			continue
		}
		edges = append(edges, reducer.RepoDependencyEdge{SourceRepoID: sourceRepoID, TargetRepoID: targetRepoID})
	}
	return edges
}

// workloadDependencyAssertSelectiveAdmission proves the positive pair admits,
// the ambiguous pair is dropped by reconciliation, and the neither-current
// orphan pair is not smuggled past the production lookup's anchoring contract.
func workloadDependencyAssertSelectiveAdmission(oduName string, admitted []reducer.WorkloadDependencyEdgeRow) string {
	admittedPairs := make(map[string]struct{}, len(admitted))
	for _, row := range admitted {
		admittedPairs[row.RepoID+"->"+row.TargetRepoID] = struct{}{}
	}

	multiPair := ifa.WorkloadDependencyFamilyMultiSourceRepoID + "->" + ifa.WorkloadDependencyFamilyMultiTargetRepoID
	if _, ok := admittedPairs[multiPair]; ok {
		return fmt.Sprintf("odù %q: multi-workload pair %q leaked into the admitted set; the fixture no longer proves ReconcileWorkloadDependencyEdges' len(targetWorkloads)!=1 drop (workload_dependency_reconciliation.go:138)", oduName, multiPair)
	}

	orphanPair := ifa.WorkloadDependencyFamilyOrphanSourceRepoID + "->" + ifa.WorkloadDependencyFamilyOrphanTargetRepoID
	if _, ok := admittedPairs[orphanPair]; ok {
		return fmt.Sprintf("odù %q: neither-current orphan pair %q leaked into the admitted set; the fixture lookup no longer matches production's source-or-target anchoring query", oduName, orphanPair)
	}

	positivePair := ifa.WorkloadDependencyFamilySourceRepoID + "->" + ifa.WorkloadDependencyFamilyTargetRepoID
	if _, ok := admittedPairs[positivePair]; !ok {
		return fmt.Sprintf("odù %q: positive pair %q did not admit; nothing would distinguish the two selectivity checks above from dropping every resolved pair", oduName, positivePair)
	}
	if len(admitted) != 1 {
		return fmt.Sprintf("odù %q: expected exactly 1 admitted workload dependency row, got %d", oduName, len(admitted))
	}

	return ""
}

func missingWorkloadDependencyExpectedTypes(expected []ExpectedEdge, registry map[string]struct{}) []string {
	present := make(map[string]struct{}, len(expected))
	for _, edge := range expected {
		present[edge.RelationshipType] = struct{}{}
	}
	var missing []string
	for edgeType := range registry {
		if _, ok := present[edgeType]; !ok {
			missing = append(missing, edgeType)
		}
	}
	sort.Strings(missing)
	return missing
}

func compareWorkloadDependencyExpectedEdges(oduName string, expected, actual []ExpectedEdge) string {
	want := make(map[string]int, len(expected))
	got := make(map[string]int, len(actual))
	for _, edge := range expected {
		want[edge.Key()]++
	}
	for _, edge := range actual {
		got[edge.Key()]++
	}
	var missing, extra []string
	for key, count := range want {
		if got[key] < count {
			missing = append(missing, key)
		}
	}
	for key, count := range got {
		if count > want[key] {
			extra = append(extra, key)
		}
	}
	sort.Strings(missing)
	sort.Strings(extra)
	if len(missing) == 0 && len(extra) == 0 {
		return ""
	}
	return fmt.Sprintf("odù %q: workload-dependency edge set does not match %d hand-derived edge(s); MISSING: %s; EXTRA: %s", oduName, len(expected), strings.Join(missing, ", "), strings.Join(extra, ", "))
}
