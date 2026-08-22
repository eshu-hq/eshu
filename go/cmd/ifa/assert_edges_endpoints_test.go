// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/ifa/graphdump"
	"github.com/eshu-hq/eshu/go/internal/ifa/materializededges"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

// labeledEdge builds a graph edge carrying endpoint labels, which sqlEdge omits
// because the SQL family needs no endpoint scoping.
func labeledEdge(edgeType, fromLabel, fromID, toLabel, toID string) graphdump.Edge {
	// PRODUCTION SHAPE, deliberately: Repository, Workload, WorkloadInstance and
	// Platform are MERGEd `{id: ...}` and carry no uid at all
	// (canonical_node_cypher.go:98, canonical.go:24/36/50). An earlier version of
	// this helper put the value under "uid", which no writer emits for these
	// labels — so the partition test passed against a graph shape production
	// cannot produce, while the real live cell would have rejected every edge.
	return graphdump.Edge{
		Type:       edgeType,
		FromLabels: []string{fromLabel},
		FromProps:  map[string]any{"id": fromID},
		ToLabels:   []string{toLabel},
		ToProps:    map[string]any{"id": toID},
	}
}

func labeledOwnedEdge(edgeType, fromLabel, fromID, toLabel, toID, evidenceSource string) graphdump.Edge {
	edge := labeledEdge(edgeType, fromLabel, fromID, toLabel, toID)
	edge.Props = map[string]any{"evidence_source": evidenceSource}
	return edge
}

// uidEdge builds the other identity convention: content entities (Function,
// Class, File) ARE uid-keyed, and both must resolve.
func uidEdge(edgeType, fromLabel, fromUID, toLabel, toUID string) graphdump.Edge {
	return graphdump.Edge{
		Type:       edgeType,
		FromLabels: []string{fromLabel},
		FromProps:  map[string]any{"uid": fromUID},
		ToLabels:   []string{toLabel},
		ToProps:    map[string]any{"uid": toUID},
	}
}

// TestEndpointScopingPartitionsASharedEdgeType is the proof that the endpoint
// filter does the job it was added for (#5543).
//
// DEPENDS_ON is written by two families. With a type-only filter, asserting
// repo_dependency against a graph that also holds workload_dependency's edges
// reports the workload edge as a spurious extra — which is exactly what a single
// batched live cell would produce, since the plan proves both families in one
// stack bring-up.
//
// Both directions are asserted from the SAME graph: repo_dependency must see
// only the Repository->Repository edge and workload_dependency only the
// Workload->Workload one. Checking one family alone would pass with a filter
// that simply dropped everything.
func TestEndpointScopingPartitionsASharedEdgeType(t *testing.T) {
	t.Parallel()

	graph := fakeEdgeReader{edges: []graphdump.Edge{
		labeledEdge("DEPENDS_ON", "Repository", "repo-a", "Repository", "repo-b"),
		labeledOwnedEdge("DEPENDS_ON", "Workload", "wl-a", "Workload", "wl-b", reducer.EvidenceSourceWorkloads),
		labeledOwnedEdge("DEPENDS_ON", "Workload", "wl-other", "Workload", "wl-target", "another/writer"),
	}}

	repoTypes, err := materializededges.MaterializedEdgeDomainEdgeTypes("repo_dependency")
	if err != nil {
		t.Fatalf("MaterializedEdgeDomainEdgeTypes(repo_dependency): %v", err)
	}
	repoEndpoints, ok := cypher.MaterializedEdgeEndpointLabels("repo_dependency")
	if !ok {
		t.Fatal("repo_dependency has no endpoint constraints; it shares DEPENDS_ON and needs them")
	}
	repoExpected := []materializededges.ExpectedEdge{
		{RelationshipType: "DEPENDS_ON", SourceEntityID: "repo-a", TargetEntityID: "repo-b"},
	}
	if err := assertMaterializedEdges(context.Background(), graph, "repo_dependency", repoTypes, repoEndpoints, nil, repoExpected); err != nil {
		t.Errorf("repo_dependency assertion = %v, want nil; the Workload->Workload edge leaked into this family", err)
	}

	workloadTypes, err := materializededges.MaterializedEdgeDomainEdgeTypes("workload_dependency")
	if err != nil {
		t.Fatalf("MaterializedEdgeDomainEdgeTypes(workload_dependency): %v", err)
	}
	workloadEndpoints, ok := cypher.MaterializedEdgeEndpointLabels("workload_dependency")
	if !ok {
		t.Fatal("workload_dependency has no endpoint constraints; it shares DEPENDS_ON and needs them")
	}
	workloadExpected := []materializededges.ExpectedEdge{
		{RelationshipType: "DEPENDS_ON", SourceEntityID: "wl-a", TargetEntityID: "wl-b"},
	}
	if err := assertMaterializedEdges(context.Background(), graph, "workload_dependency", workloadTypes, workloadEndpoints, nil, workloadExpected); err != nil {
		t.Errorf("workload_dependency assertion = %v, want nil; the Repository->Repository edge leaked into this family", err)
	}
}

// TestTypeOnlyFilteringWouldConflateTheSharedType pins WHY the constraint is
// required, so removing it fails loudly here instead of only in a live gate run
// that costs a stack bring-up to discover.
//
// Same graph, same expected set, constraints omitted: the other family's
// DEPENDS_ON edge must surface as an extra. A filter change that silently
// stopped partitioning would turn this test green, which is the signal.
func TestTypeOnlyFilteringWouldConflateTheSharedType(t *testing.T) {
	t.Parallel()

	graph := fakeEdgeReader{edges: []graphdump.Edge{
		labeledEdge("DEPENDS_ON", "Repository", "repo-a", "Repository", "repo-b"),
		labeledEdge("DEPENDS_ON", "Workload", "wl-a", "Workload", "wl-b"),
	}}
	repoTypes, err := materializededges.MaterializedEdgeDomainEdgeTypes("repo_dependency")
	if err != nil {
		t.Fatalf("MaterializedEdgeDomainEdgeTypes(repo_dependency): %v", err)
	}
	repoExpected := []materializededges.ExpectedEdge{
		{RelationshipType: "DEPENDS_ON", SourceEntityID: "repo-a", TargetEntityID: "repo-b"},
	}

	err = assertMaterializedEdges(context.Background(), graph, "repo_dependency", repoTypes, nil, nil, repoExpected)
	if err == nil {
		t.Fatal("type-only filtering accepted a graph holding another family's DEPENDS_ON; endpoint scoping would then be unnecessary and this test is no longer meaningful")
	}
	if !strings.Contains(err.Error(), "wl-a") {
		t.Errorf("failure %q does not name the leaked workload edge; the diagnosis would not point at the conflated family", err)
	}
}

// TestUnconstrainedFamilyMatchesByTypeAlone proves absent constraints mean
// "match everything of these types", not "match nothing".
//
// The nil-map path is what every unconstrained family uses, so if it filtered
// everything out, twelve of the fourteen families would assert an empty
// population and pass any graph — a false green far worse than the collision the
// constraints exist to fix.
func TestUnconstrainedFamilyMatchesByTypeAlone(t *testing.T) {
	t.Parallel()

	graph := fakeEdgeReader{edges: []graphdump.Edge{
		uidEdge("RUNS_IN", "Function", "fn-a", "Workload", "wl-a"),
	}}
	types, err := materializededges.MaterializedEdgeDomainEdgeTypes("runs_in")
	if err != nil {
		t.Fatalf("MaterializedEdgeDomainEdgeTypes(runs_in): %v", err)
	}
	endpoints, constrained := cypher.MaterializedEdgeEndpointLabels("runs_in")
	if constrained {
		t.Fatalf("runs_in unexpectedly carries endpoint constraints %+v", endpoints)
	}
	expected := []materializededges.ExpectedEdge{
		{RelationshipType: "RUNS_IN", SourceEntityID: "fn-a", TargetEntityID: "wl-a"},
	}
	if err := assertMaterializedEdges(context.Background(), graph, "runs_in", types, endpoints, nil, expected); err != nil {
		t.Fatalf("unconstrained family assertion = %v, want nil; a nil constraint map must not filter the family's edges away", err)
	}
}

// TestUidWinsWhenAnEndpointCarriesBoth pins the precedence the identity fallback
// depends on.
//
// The fallback exists because Repository/Workload/WorkloadInstance/Platform are
// id-keyed, but content-entity nodes carry BOTH: canonicalEntityProperties sets
// props["id"] = entity.EntityID while the node is MERGEd on {uid: row.entity_id}.
// Today those two strings are equal, so swapping the lookup order changes no
// existing fixture and the whole suite stays green on a mutation that silently
// reverses identity precedence. That latency is exactly what makes it survive
// review, so this test gives the two properties DIFFERENT values and requires
// uid to win.
func TestUidWinsWhenAnEndpointCarriesBoth(t *testing.T) {
	t.Parallel()

	both := graphdump.Edge{
		Type:       "RUNS_IN",
		FromLabels: []string{"Function"},
		FromProps:  map[string]any{"uid": "u-from", "id": "i-from"},
		ToLabels:   []string{"Workload"},
		ToProps:    map[string]any{"uid": "u-to", "id": "i-to"},
	}
	types, err := materializededges.MaterializedEdgeDomainEdgeTypes("runs_in")
	if err != nil {
		t.Fatalf("MaterializedEdgeDomainEdgeTypes(runs_in): %v", err)
	}

	byUID := []materializededges.ExpectedEdge{{RelationshipType: "RUNS_IN", SourceEntityID: "u-from", TargetEntityID: "u-to"}}
	if err := assertMaterializedEdges(context.Background(), fakeEdgeReader{edges: []graphdump.Edge{both}}, "runs_in", types, nil, nil, byUID); err != nil {
		t.Errorf("expected set naming the uids = %v, want nil; uid must win when both properties are present", err)
	}

	byID := []materializededges.ExpectedEdge{{RelationshipType: "RUNS_IN", SourceEntityID: "i-from", TargetEntityID: "i-to"}}
	if err := assertMaterializedEdges(context.Background(), fakeEdgeReader{edges: []graphdump.Edge{both}}, "runs_in", types, nil, nil, byID); err == nil {
		t.Error("expected set naming the ids passed; id must NOT win over uid, or the fallback silently reverses identity for every content entity")
	}
}

// TestRefFallbackResolvesCodeownerTeamEndpoint proves the third endpointID
// fallback: a CodeownerTeam endpoint, MERGEd `{ref: row.owner_ref}`
// (canonical_codeowners_edges.go) and carrying neither uid nor id, still
// resolves by its ref rather than reporting as an unmaterialized endpoint —
// the exact shape a live DECLARES_CODEOWNER edge's target carries. ToLabels
// is set to CodeownerTeam because the fallback is scoped to that label (see
// TestRefFallbackIsScopedToCodeownerTeam) — a real live edge always carries
// its endpoint's labels, so this is the realistic shape, not a simplification
// the scoping happens to tolerate.
func TestRefFallbackResolvesCodeownerTeamEndpoint(t *testing.T) {
	t.Parallel()

	types, err := materializededges.MaterializedEdgeDomainEdgeTypes("codeowners_ownership_edges")
	if err != nil {
		t.Fatalf("MaterializedEdgeDomainEdgeTypes(codeowners_ownership_edges): %v", err)
	}
	graph := fakeEdgeReader{edges: []graphdump.Edge{{
		Type:      "DECLARES_CODEOWNER",
		FromProps: map[string]any{"id": "repo-1"},
		ToLabels:  []string{"CodeownerTeam"},
		ToProps:   map[string]any{"ref": "team-a"}, // ref-only: no uid, no id.
	}}}
	expected := []materializededges.ExpectedEdge{
		{RelationshipType: "DECLARES_CODEOWNER", SourceEntityID: "repo-1", TargetEntityID: "team-a"},
	}

	if err := assertMaterializedEdges(context.Background(), graph, "codeowners_ownership_edges", types, nil, nil, expected); err != nil {
		t.Fatalf("assertMaterializedEdges(ref-only CodeownerTeam target) = %v, want nil; ref must resolve the endpoint", err)
	}
}

// TestRefFallbackIsScopedToCodeownerTeam proves the ref fallback does NOT
// apply globally: an endpoint of some OTHER label that lost its real
// identity (no uid, no id) but happens to carry an incidental "ref" property
// must still report as unmaterialized, not silently resolve by that ref. An
// unscoped fallback would mask exactly this failure mode — a node in a
// uid/id-keyed family that lost its real identity would read as
// "identified" instead of "unmaterialized".
func TestRefFallbackIsScopedToCodeownerTeam(t *testing.T) {
	t.Parallel()

	types, err := materializededges.MaterializedEdgeDomainEdgeTypes("codeowners_ownership_edges")
	if err != nil {
		t.Fatalf("MaterializedEdgeDomainEdgeTypes(codeowners_ownership_edges): %v", err)
	}
	graph := fakeEdgeReader{edges: []graphdump.Edge{{
		Type:      "DECLARES_CODEOWNER",
		FromProps: map[string]any{"id": "repo-1"},
		ToLabels:  []string{"SomeOtherLabel"},
		ToProps:   map[string]any{"ref": "incidental"}, // no uid, no id -- must NOT resolve.
	}}}
	expected := []materializededges.ExpectedEdge{
		{RelationshipType: "DECLARES_CODEOWNER", SourceEntityID: "repo-1", TargetEntityID: "incidental"},
	}

	err = assertMaterializedEdges(context.Background(), graph, "codeowners_ownership_edges", types, nil, nil, expected)
	if err == nil {
		t.Fatal("assertMaterializedEdges(non-CodeownerTeam endpoint with only an incidental ref) = nil, want an endpoint-defect failure")
	}
	if !strings.Contains(err.Error(), "endpoint") {
		t.Errorf("error %q does not report the endpoint defect", err)
	}
}

// TestUidAndIDWinOverRef pins the fallback precedence: an endpoint carrying
// uid or id must resolve by that property, never by a stale or unrelated ref
// it also happens to carry.
func TestUidAndIDWinOverRef(t *testing.T) {
	t.Parallel()

	types, err := materializededges.MaterializedEdgeDomainEdgeTypes("runs_in")
	if err != nil {
		t.Fatalf("MaterializedEdgeDomainEdgeTypes(runs_in): %v", err)
	}

	uidWins := graphdump.Edge{
		Type:       "RUNS_IN",
		FromLabels: []string{"Function"},
		FromProps:  map[string]any{"uid": "u-from", "ref": "ref-from"},
		ToLabels:   []string{"Workload"},
		ToProps:    map[string]any{"uid": "u-to", "ref": "ref-to"},
	}
	byUID := []materializededges.ExpectedEdge{{RelationshipType: "RUNS_IN", SourceEntityID: "u-from", TargetEntityID: "u-to"}}
	if err := assertMaterializedEdges(context.Background(), fakeEdgeReader{edges: []graphdump.Edge{uidWins}}, "runs_in", types, nil, nil, byUID); err != nil {
		t.Errorf("expected set naming the uids = %v, want nil; uid must win over ref", err)
	}

	idWins := graphdump.Edge{
		Type:       "RUNS_IN",
		FromLabels: []string{"Function"},
		FromProps:  map[string]any{"id": "i-from", "ref": "ref-from"},
		ToLabels:   []string{"Workload"},
		ToProps:    map[string]any{"id": "i-to", "ref": "ref-to"},
	}
	byID := []materializededges.ExpectedEdge{{RelationshipType: "RUNS_IN", SourceEntityID: "i-from", TargetEntityID: "i-to"}}
	if err := assertMaterializedEdges(context.Background(), fakeEdgeReader{edges: []graphdump.Edge{idWins}}, "runs_in", types, nil, nil, byID); err != nil {
		t.Errorf("expected set naming the ids = %v, want nil; id must win over ref", err)
	}
}

// TestEndpointDefectNamesWhichSideIsUnidentified pins the diagnostic wording.
//
// The endpoint check fires when EITHER side lacks an identity, so a message
// asserting both are missing sends an operator to inspect a node that is
// materialized correctly. At 3 AM that misdirection costs more than the missing
// edge does.
func TestEndpointDefectNamesWhichSideIsUnidentified(t *testing.T) {
	t.Parallel()

	types, err := materializededges.MaterializedEdgeDomainEdgeTypes("runs_in")
	if err != nil {
		t.Fatalf("MaterializedEdgeDomainEdgeTypes(runs_in): %v", err)
	}
	expected := []materializededges.ExpectedEdge{{RelationshipType: "RUNS_IN", SourceEntityID: "fn-a", TargetEntityID: "wl-a"}}

	for _, tc := range []struct {
		name       string
		from, to   map[string]any
		wantPhrase string
	}{
		{"source only", map[string]any{}, map[string]any{"uid": "wl-a"}, "source endpoint"},
		{"target only", map[string]any{"uid": "fn-a"}, map[string]any{}, "target endpoint"},
		{"both", map[string]any{}, map[string]any{}, "source and target endpoint"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			graph := fakeEdgeReader{edges: []graphdump.Edge{{
				Type: "RUNS_IN", FromLabels: []string{"Function"}, FromProps: tc.from,
				ToLabels: []string{"Workload"}, ToProps: tc.to,
			}}}
			err := assertMaterializedEdges(context.Background(), graph, "runs_in", types, nil, nil, expected)
			if err == nil {
				t.Fatal("an endpoint with no identity was accepted")
			}
			if !strings.Contains(err.Error(), tc.wantPhrase) {
				t.Errorf("error %q does not say %q; the operator is pointed at the wrong node", err, tc.wantPhrase)
			}
		})
	}
}

// TestProvenancePartitionsRunsOnBetweenTwoLiveWriters is the guard for the
// collision endpoint LABELS cannot resolve.
//
// RUNS_ON is written with the identical (WorkloadInstance)->(Platform) shape by
// the cross-repo resolver (repo_dependency) and by workload materialization.
// Type and labels are the same on both, so before provenance the family's exact
// set counted the other writer's edge as a spurious extra on any graph where
// workload materialization had run — which is every realistic graph.
//
// Both directions from the SAME graph: the resolver's edge must count, the
// materializer's must be skipped, not reported as extra. Stripping the
// evidence_source filter turns this red.
func TestProvenancePartitionsRunsOnBetweenTwoLiveWriters(t *testing.T) {
	t.Parallel()

	runsOn := func(instance, platform, evidenceSource string) graphdump.Edge {
		return graphdump.Edge{
			Type:       "RUNS_ON",
			FromLabels: []string{"WorkloadInstance"},
			FromProps:  map[string]any{"id": instance},
			ToLabels:   []string{"Platform"},
			ToProps:    map[string]any{"id": platform},
			Props:      map[string]any{"evidence_source": evidenceSource},
		}
	}
	graph := fakeEdgeReader{edges: []graphdump.Edge{
		runsOn("inst-resolver", "plat-a", reducer.CrossRepoEvidenceSource),
		runsOn("inst-workload", "plat-b", reducer.EvidenceSourceWorkloads),
	}}

	types, err := materializededges.MaterializedEdgeDomainEdgeTypes("repo_dependency")
	if err != nil {
		t.Fatalf("MaterializedEdgeDomainEdgeTypes(repo_dependency): %v", err)
	}
	endpoints, ok := cypher.MaterializedEdgeEndpointLabels("repo_dependency")
	if !ok {
		t.Fatal("repo_dependency carries no endpoint constraints")
	}
	if endpoints["RUNS_ON"].EvidenceSource == "" {
		t.Fatal("RUNS_ON carries no evidence_source constraint; labels alone cannot partition it from workload materialization")
	}

	// Only the resolver's edge belongs to this family.
	expected := []materializededges.ExpectedEdge{
		{RelationshipType: "RUNS_ON", SourceEntityID: "inst-resolver", TargetEntityID: "plat-a"},
	}
	if err := assertMaterializedEdges(context.Background(), graph, "repo_dependency", types, endpoints, nil, expected); err != nil {
		t.Errorf("repo_dependency assertion = %v, want nil; the workload-materialized RUNS_ON edge leaked into this family", err)
	}

	// And the materializer's edge must not be silently adoptable either: naming
	// it in the expected set has to fail, or the filter would be matching on
	// nothing rather than on provenance.
	wrong := []materializededges.ExpectedEdge{
		{RelationshipType: "RUNS_ON", SourceEntityID: "inst-workload", TargetEntityID: "plat-b"},
	}
	if err := assertMaterializedEdges(context.Background(), graph, "repo_dependency", types, endpoints, nil, wrong); err == nil {
		t.Error("an expected set naming the workload-materialized edge passed; the family must not be able to claim another writer's RUNS_ON")
	}
}

// TestEvidenceSourceConstraintTracksTheWriterConstant stops the partition drifting.
//
// The constraint references reducer.CrossRepoEvidenceSource rather than a copied
// literal precisely so a change to what the writer stamps cannot leave the gate
// asserting a stale value. This pins that wiring.
func TestEvidenceSourceConstraintTracksTheWriterConstant(t *testing.T) {
	t.Parallel()

	endpoints, ok := cypher.MaterializedEdgeEndpointLabels("repo_dependency")
	if !ok {
		t.Fatal("repo_dependency carries no endpoint constraints")
	}
	if got := endpoints["RUNS_ON"].EvidenceSource; got != reducer.CrossRepoEvidenceSource {
		t.Errorf("RUNS_ON evidence_source = %q, want the writer's own constant %q", got, reducer.CrossRepoEvidenceSource)
	}
	if reducer.CrossRepoEvidenceSource == reducer.EvidenceSourceWorkloads {
		t.Fatal("the two writers stamp the same evidence_source; provenance can no longer partition RUNS_ON")
	}
}

// TestEndpointDefectMessageNamesTheRefFallback pins the diagnostic, not the
// behavior. endpointID has three fallbacks, but the failure message it feeds
// only named the first two, so an operator debugging a DECLARES_CODEOWNER
// regression was told to go look for a uid or an id on a node keyed by
// neither. codeowners_ownership_edges is the first family that can reach this
// branch on a real target, so its operator is the one who reads it.
func TestEndpointDefectMessageNamesTheRefFallback(t *testing.T) {
	t.Parallel()

	types, err := materializededges.MaterializedEdgeDomainEdgeTypes("codeowners_ownership_edges")
	if err != nil {
		t.Fatalf("MaterializedEdgeDomainEdgeTypes(codeowners_ownership_edges): %v", err)
	}
	graph := fakeEdgeReader{edges: []graphdump.Edge{{
		Type:      "DECLARES_CODEOWNER",
		FromProps: map[string]any{"id": "repo-1"},
		ToLabels:  []string{"CodeownerTeam"},
		ToProps:   map[string]any{}, // genuinely unmaterialized: no uid, no id, no ref.
	}}}
	expected := []materializededges.ExpectedEdge{
		{RelationshipType: "DECLARES_CODEOWNER", SourceEntityID: "repo-1", TargetEntityID: "team-a"},
	}

	err = assertMaterializedEdges(context.Background(), graph, "codeowners_ownership_edges", types, nil, nil, expected)
	if err == nil {
		t.Fatal("assertMaterializedEdges(endpoint with no identity property at all) = nil, want an endpoint-defect failure")
	}
	const want = "carries neither uid, id, nor (for a CodeownerTeam endpoint) ref"
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("error = %q, want it to contain %q so the message names every identity endpointID actually tries", err, want)
	}
}
