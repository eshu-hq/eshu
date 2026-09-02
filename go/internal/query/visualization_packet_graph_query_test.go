// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"strings"
	"testing"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

// graphQueryTestNode builds a driver node with a stable element id so a test can
// assert the packet node ID derived from it.
func graphQueryTestNode(elementID string, labels []string, props map[string]any) neo4jdriver.Node {
	return neo4jdriver.Node{ElementId: elementID, Labels: labels, Props: props}
}

// graphQueryTestRelationship builds a driver relationship between two element
// ids, which may or may not correspond to nodes the same query returned.
func graphQueryTestRelationship(elementID, start, end, relType string) neo4jdriver.Relationship {
	return neo4jdriver.Relationship{
		ElementId:      elementID,
		StartElementId: start,
		EndElementId:   end,
		Type:           relType,
	}
}

// hasLimitationContaining reports whether any limitation mentions substr, so a
// test asserts that the packet explains itself without pinning exact wording.
func hasLimitationContaining(limitations []string, substr string) bool {
	for _, limitation := range limitations {
		if strings.Contains(limitation, substr) {
			return true
		}
	}
	return false
}

// A RETURN of bare properties is not renderable as a graph. The builder must say
// so explicitly rather than emit a supported packet with an empty subgraph.
func TestBuildGraphQueryVisualizationPacketScalarRowsAreUnsupported(t *testing.T) {
	t.Parallel()

	rows := []map[string]any{
		{"name": "checkout", "count": int64(3)},
	}

	packet := BuildGraphQueryVisualizationPacket(rows, freshTruth())

	if packet.Supported {
		t.Fatalf("scalar-only rows produced a supported packet: %+v", packet)
	}
	if packet.View != VisualizationViewUnsupported {
		t.Fatalf("packet view = %q, want %q", packet.View, VisualizationViewUnsupported)
	}
	if len(packet.Nodes) != 0 || len(packet.Edges) != 0 {
		t.Fatalf("unsupported packet carried a subgraph: %d nodes, %d edges", len(packet.Nodes), len(packet.Edges))
	}
	if len(packet.Limitations) == 0 {
		t.Fatal("unsupported packet carried no limitation explaining why")
	}
}

// A relationships-only RETURN reaches finalize with edges but no nodes, so every
// edge is dangling and gets dropped. The packet must not come back as a silently
// empty subgraph: the truncation block and a limitation have to record the drop.
func TestBuildGraphQueryVisualizationPacketRelationshipOnlyRowsRecordDroppedEdges(t *testing.T) {
	t.Parallel()

	rows := []map[string]any{
		{"r": graphQueryTestRelationship("rel-1", "node-a", "node-b", "CALLS")},
	}

	packet := BuildGraphQueryVisualizationPacket(rows, freshTruth())

	if len(packet.Nodes) != 0 {
		t.Fatalf("relationship-only rows produced %d nodes, want 0", len(packet.Nodes))
	}
	if len(packet.Edges) != 0 {
		t.Fatalf("edge survived with neither endpoint returned as a node: %+v", packet.Edges)
	}
	if !packet.Truncation.Truncated {
		t.Fatal("dropped dangling edge was not recorded as truncation")
	}
	if packet.Truncation.DroppedEdgeCount != 1 {
		t.Fatalf("dropped edge count = %d, want 1", packet.Truncation.DroppedEdgeCount)
	}
	if len(packet.Limitations) == 0 {
		t.Fatal("empty subgraph carried no limitation; the result would read as a real empty answer")
	}
}

// An edge whose endpoints were not themselves returned must be dropped rather
// than inventing the missing nodes, and the drop must be visible to the caller.
func TestBuildGraphQueryVisualizationPacketDropsEdgeWithMissingEndpoint(t *testing.T) {
	t.Parallel()

	rows := []map[string]any{
		{
			"n": graphQueryTestNode("node-a", []string{"Service"}, map[string]any{"name": "checkout"}),
			"r": graphQueryTestRelationship("rel-1", "node-a", "node-missing", "CALLS"),
		},
	}

	packet := BuildGraphQueryVisualizationPacket(rows, freshTruth())

	if len(packet.Nodes) != 1 {
		t.Fatalf("node count = %d, want 1 (the returned node only)", len(packet.Nodes))
	}
	if len(packet.Edges) != 0 {
		t.Fatalf("edge to an unreturned endpoint survived: %+v", packet.Edges)
	}
	if packet.Truncation.DroppedEdgeCount != 1 {
		t.Fatalf("dropped edge count = %d, want 1", packet.Truncation.DroppedEdgeCount)
	}
	if !hasLimitationContaining(packet.Limitations, "truncated") {
		t.Fatalf("limitations = %v, want one naming the truncation", packet.Limitations)
	}
}

// Nodes with no relationships at all take the other branch: nothing is dropped,
// so the packet must explain the missing edges instead of reporting truncation.
func TestBuildGraphQueryVisualizationPacketNodesWithoutRelationships(t *testing.T) {
	t.Parallel()

	rows := []map[string]any{
		{"n": graphQueryTestNode("node-a", []string{"Service"}, map[string]any{"name": "checkout"})},
		{"n": graphQueryTestNode("node-b", []string{"Service"}, map[string]any{"name": "payments"})},
	}

	packet := BuildGraphQueryVisualizationPacket(rows, freshTruth())

	if !packet.Supported {
		t.Fatalf("node-only rows produced an unsupported packet: %+v", packet)
	}
	if len(packet.Nodes) != 2 {
		t.Fatalf("node count = %d, want 2", len(packet.Nodes))
	}
	if packet.Truncation.Truncated {
		t.Fatal("nothing was dropped, so the packet must not report truncation")
	}
	if !hasLimitationContaining(packet.Limitations, "no relationships") {
		t.Fatalf("limitations = %v, want one naming the absent relationships", packet.Limitations)
	}
}

// The ordinary case: both endpoints returned, so the edge survives finalize and
// the node label comes from the first non-empty label key.
func TestBuildGraphQueryVisualizationPacketRetainsConnectedEdge(t *testing.T) {
	t.Parallel()

	rows := []map[string]any{
		{
			"a": graphQueryTestNode("node-a", []string{"Service"}, map[string]any{"name": "checkout"}),
			"b": graphQueryTestNode("node-b", []string{"Service"}, map[string]any{"name": "payments"}),
			"r": graphQueryTestRelationship("rel-1", "node-a", "node-b", "CALLS"),
		},
	}

	packet := BuildGraphQueryVisualizationPacket(rows, freshTruth())

	if !packet.Supported {
		t.Fatalf("connected subgraph produced an unsupported packet: %+v", packet)
	}
	if len(packet.Nodes) != 2 {
		t.Fatalf("node count = %d, want 2", len(packet.Nodes))
	}
	if len(packet.Edges) != 1 {
		t.Fatalf("edge count = %d, want 1", len(packet.Edges))
	}
	if packet.Edges[0].Relationship != "CALLS" {
		t.Fatalf("edge relationship = %q, want %q", packet.Edges[0].Relationship, "CALLS")
	}
	if packet.Truncation.Truncated {
		t.Fatalf("nothing should have been dropped: %+v", packet.Truncation)
	}
	labels := make([]string, 0, len(packet.Nodes))
	for _, node := range packet.Nodes {
		labels = append(labels, node.Label)
	}
	for _, want := range []string{"checkout", "payments"} {
		found := false
		for _, label := range labels {
			if label == want {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("node labels = %v, want one equal to %q", labels, want)
		}
	}
}

// A path carries both endpoints for every relationship, so its edges always
// survive finalize even though the same relationship alone would be dangling.
func TestBuildGraphQueryVisualizationPacketProjectsPathEdges(t *testing.T) {
	t.Parallel()

	path := neo4jdriver.Path{
		Nodes: []neo4jdriver.Node{
			graphQueryTestNode("node-a", []string{"Service"}, map[string]any{"name": "checkout"}),
			graphQueryTestNode("node-b", []string{"Service"}, map[string]any{"name": "payments"}),
		},
		Relationships: []neo4jdriver.Relationship{
			graphQueryTestRelationship("rel-1", "node-a", "node-b", "CALLS"),
		},
	}

	packet := BuildGraphQueryVisualizationPacket([]map[string]any{{"p": path}}, freshTruth())

	if len(packet.Nodes) != 2 {
		t.Fatalf("node count = %d, want 2", len(packet.Nodes))
	}
	if len(packet.Edges) != 1 {
		t.Fatalf("path edge was dropped: %+v", packet)
	}
	if packet.Truncation.Truncated {
		t.Fatalf("path edges have present endpoints and must not be dropped: %+v", packet.Truncation)
	}
}

// Projection must not depend on Go map iteration order: the same rows in a
// different column order yield the same node and edge IDs.
func TestBuildGraphQueryVisualizationPacketIsOrderStable(t *testing.T) {
	t.Parallel()

	nodeA := graphQueryTestNode("node-a", []string{"Service"}, map[string]any{"name": "checkout"})
	nodeB := graphQueryTestNode("node-b", []string{"Service"}, map[string]any{"name": "payments"})
	rel := graphQueryTestRelationship("rel-1", "node-a", "node-b", "CALLS")

	first := BuildGraphQueryVisualizationPacket([]map[string]any{
		{"a": nodeA, "b": nodeB, "r": rel},
	}, freshTruth())
	second := BuildGraphQueryVisualizationPacket([]map[string]any{
		{"r": rel, "b": nodeB, "a": nodeA},
	}, freshTruth())

	assertSameGraphQueryPacketShape(t, first, second)
}

// The load-bearing half of the ordering contract. A node reached through two
// columns in the same row must resolve to one packet node whose presentation
// does not depend on which column the driver handed over first: the builder
// picks by role priority and then by presentation key, never by arrival. Sorting
// the row keys alone would not prove this, because Finalize re-sorts the output
// by ID and would hide an arrival-order-dependent Label behind a stable slice
// order.
func TestBuildGraphQueryVisualizationPacketMergesDuplicateNodeIndependentOfColumnOrder(t *testing.T) {
	t.Parallel()

	// Same element id under two columns, with labels that sort in a known
	// direction, so an arrival-order-dependent merge would visibly flip.
	asAlpha := graphQueryTestNode("node-a", []string{"Service"}, map[string]any{"name": "alpha"})
	asOmega := graphQueryTestNode("node-a", []string{"Service"}, map[string]any{"name": "omega"})

	first := BuildGraphQueryVisualizationPacket([]map[string]any{
		{"a": asAlpha, "z": asOmega},
	}, freshTruth())
	second := BuildGraphQueryVisualizationPacket([]map[string]any{
		{"a": asOmega, "z": asAlpha},
	}, freshTruth())

	if len(first.Nodes) != 1 {
		t.Fatalf("node count = %d, want 1 (both columns are the same graph node)", len(first.Nodes))
	}
	assertSameGraphQueryPacketShape(t, first, second)
	if first.Nodes[0].Label != second.Nodes[0].Label {
		t.Fatalf("duplicate node label depended on column order: %q vs %q",
			first.Nodes[0].Label, second.Nodes[0].Label)
	}
}

// assertSameGraphQueryPacketShape fails unless two packets agree on node and
// edge count and on every rendered identity and label, which is what a caller
// re-running the same query relies on.
func assertSameGraphQueryPacketShape(t *testing.T, first, second VisualizationPacket) {
	t.Helper()

	if len(first.Nodes) != len(second.Nodes) || len(first.Edges) != len(second.Edges) {
		t.Fatalf("packet shape differed: %d/%d vs %d/%d",
			len(first.Nodes), len(first.Edges), len(second.Nodes), len(second.Edges))
	}
	for i := range first.Nodes {
		if first.Nodes[i].ID != second.Nodes[i].ID {
			t.Fatalf("node %d ID differed: %q vs %q", i, first.Nodes[i].ID, second.Nodes[i].ID)
		}
		if first.Nodes[i].Label != second.Nodes[i].Label {
			t.Fatalf("node %d label differed: %q vs %q", i, first.Nodes[i].Label, second.Nodes[i].Label)
		}
		if first.Nodes[i].Type != second.Nodes[i].Type {
			t.Fatalf("node %d type differed: %q vs %q", i, first.Nodes[i].Type, second.Nodes[i].Type)
		}
	}
	for i := range first.Edges {
		if first.Edges[i].ID != second.Edges[i].ID {
			t.Fatalf("edge %d ID differed: %q vs %q", i, first.Edges[i].ID, second.Edges[i].ID)
		}
		if first.Edges[i].Relationship != second.Edges[i].Relationship {
			t.Fatalf("edge %d relationship differed: %q vs %q",
				i, first.Edges[i].Relationship, second.Edges[i].Relationship)
		}
	}
}
