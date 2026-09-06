// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/impacttrace"
	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

// TestImpactPathDecodersDecodeBothBackendShapes guards the cross-backend decode:
// relationships(path) is a neo4j.Relationship on Neo4j but a map[string]any on
// NornicDB, and nodes(path) is a neo4j.Node on both. Both rel shapes and the node
// shape must decode to the same provenance/identity, or a hop is silently dropped
// on one backend.
//
// It lives in the query root (not impacttrace/) because the decoders are
// driver-aware and the driver may only be named from driver-owning root
// files. See #6060.
func TestImpactPathDecodersDecodeBothBackendShapes(t *testing.T) {
	t.Parallel()

	relCases := map[string]any{
		"neo4j.Relationship": []any{
			neo4jdriver.Relationship{Type: "DEPENDS_ON", Props: map[string]any{"confidence": 0.9, "reason": "import"}},
		},
		"nornicdb map": []any{
			map[string]any{"type": "DEPENDS_ON", "properties": map[string]any{"confidence": 0.9, "reason": "import"}},
		},
	}
	for name, raw := range relCases {
		got := impactRelProvenanceList(raw)
		if len(got) != 1 {
			t.Fatalf("%s: got %d rels, want 1", name, len(got))
		}
		if got[0].RelType != "DEPENDS_ON" || !got[0].HasConf || got[0].Confidence != 0.9 || got[0].Reason != "import" {
			t.Errorf("%s: decoded %#v, want DEPENDS_ON/0.9/import", name, got[0])
		}
	}

	nodeCases := map[string]any{
		"neo4j.Node": []any{neo4jdriver.Node{Props: map[string]any{"id": "z:1", "name": "one"}}, neo4jdriver.Node{Props: map[string]any{"id": "z:2", "name": "two"}}},
		"map":        []any{map[string]any{"properties": map[string]any{"id": "z:1", "name": "one"}}, map[string]any{"properties": map[string]any{"id": "z:2", "name": "two"}}},
	}
	for name, raw := range nodeCases {
		got := impactNodeIdentityList(raw)
		if len(got) != 2 || got[0].ID != "z:1" || got[0].Name != "one" || got[1].ID != "z:2" {
			t.Errorf("%s: decoded %#v, want [z:1/one, z:2/two]", name, got)
		}
	}

	// A zipped hop uses path-order endpoints (nodes[i] -> nodes[i+1]).
	hops := impacttrace.ImpactDependencyHops(
		impactNodeIdentityList(nodeCases["neo4j.Node"]),
		impactRelProvenanceList(relCases["neo4j.Relationship"]),
	)
	if len(hops) != 1 {
		t.Fatalf("hops = %#v, want 1", hops)
	}
	if hops[0]["from_id"] != "z:1" || hops[0]["to_id"] != "z:2" || hops[0]["type"] != "DEPENDS_ON" {
		t.Errorf("zipped hop = %#v, want z:1->z:2 DEPENDS_ON", hops[0])
	}
}
