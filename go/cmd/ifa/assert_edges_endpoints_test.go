// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/ifa"
	"github.com/eshu-hq/eshu/go/internal/ifa/graphdump"
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
		labeledEdge("DEPENDS_ON", "Workload", "wl-a", "Workload", "wl-b"),
	}}

	repoTypes, err := ifa.MaterializedEdgeDomainEdgeTypes("repo_dependency")
	if err != nil {
		t.Fatalf("MaterializedEdgeDomainEdgeTypes(repo_dependency): %v", err)
	}
	repoEndpoints, ok := cypher.MaterializedEdgeEndpointLabels("repo_dependency")
	if !ok {
		t.Fatal("repo_dependency has no endpoint constraints; it shares DEPENDS_ON and needs them")
	}
	repoExpected := []ifa.ExpectedEdge{
		{RelationshipType: "DEPENDS_ON", SourceEntityID: "repo-a", TargetEntityID: "repo-b"},
	}
	if err := assertMaterializedEdges(context.Background(), graph, "repo_dependency", repoTypes, repoEndpoints, repoExpected); err != nil {
		t.Errorf("repo_dependency assertion = %v, want nil; the Workload->Workload edge leaked into this family", err)
	}

	workloadTypes, err := ifa.MaterializedEdgeDomainEdgeTypes("workload_dependency")
	if err != nil {
		t.Fatalf("MaterializedEdgeDomainEdgeTypes(workload_dependency): %v", err)
	}
	workloadEndpoints, ok := cypher.MaterializedEdgeEndpointLabels("workload_dependency")
	if !ok {
		t.Fatal("workload_dependency has no endpoint constraints; it shares DEPENDS_ON and needs them")
	}
	workloadExpected := []ifa.ExpectedEdge{
		{RelationshipType: "DEPENDS_ON", SourceEntityID: "wl-a", TargetEntityID: "wl-b"},
	}
	if err := assertMaterializedEdges(context.Background(), graph, "workload_dependency", workloadTypes, workloadEndpoints, workloadExpected); err != nil {
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
	repoTypes, err := ifa.MaterializedEdgeDomainEdgeTypes("repo_dependency")
	if err != nil {
		t.Fatalf("MaterializedEdgeDomainEdgeTypes(repo_dependency): %v", err)
	}
	repoExpected := []ifa.ExpectedEdge{
		{RelationshipType: "DEPENDS_ON", SourceEntityID: "repo-a", TargetEntityID: "repo-b"},
	}

	err = assertMaterializedEdges(context.Background(), graph, "repo_dependency", repoTypes, nil, repoExpected)
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
// everything out, eleven of the thirteen families would assert an empty
// population and pass any graph — a false green far worse than the collision the
// constraints exist to fix.
func TestUnconstrainedFamilyMatchesByTypeAlone(t *testing.T) {
	t.Parallel()

	graph := fakeEdgeReader{edges: []graphdump.Edge{
		uidEdge("RUNS_IN", "Function", "fn-a", "Workload", "wl-a"),
	}}
	types, err := ifa.MaterializedEdgeDomainEdgeTypes("runs_in")
	if err != nil {
		t.Fatalf("MaterializedEdgeDomainEdgeTypes(runs_in): %v", err)
	}
	endpoints, constrained := cypher.MaterializedEdgeEndpointLabels("runs_in")
	if constrained {
		t.Fatalf("runs_in unexpectedly carries endpoint constraints %+v", endpoints)
	}
	expected := []ifa.ExpectedEdge{
		{RelationshipType: "RUNS_IN", SourceEntityID: "fn-a", TargetEntityID: "wl-a"},
	}
	if err := assertMaterializedEdges(context.Background(), graph, "runs_in", types, endpoints, expected); err != nil {
		t.Fatalf("unconstrained family assertion = %v, want nil; a nil constraint map must not filter the family's edges away", err)
	}
}
