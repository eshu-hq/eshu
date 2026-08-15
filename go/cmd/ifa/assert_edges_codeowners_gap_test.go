// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/ifa"
	"github.com/eshu-hq/eshu/go/internal/ifa/graphdump"
)

// TestAssertMaterializedEdgesCannotIdentifyRealCodeownerTeamEndpoint is a RED
// test (#5992) proving a second, more severe, and currently BLOCKING gap for
// the codeowners_ownership_edges family: the live `assert-edges` gate cannot
// identify a real CodeownerTeam node's endpoint identity at all, so today it
// would report every single DECLARES_CODEOWNER edge as an "endpoint defect",
// no matter how correct the family's Odù, cassette, or expected-edge fixture
// is.
//
// endpointID (assert_edges.go) resolves an edge endpoint's identity by
// reading the node's "uid" property, falling back to "id". That covers every
// endpoint label documented there (Repository/Workload/WorkloadInstance/
// Platform are id-keyed; content entities are uid-keyed). CodeownerTeam is
// neither: its ONLY writer merges it as
// `MERGE (team:CodeownerTeam {ref: row.owner_ref})`
// (canonical_codeowners_edges.go) — no uid, no id, only "ref" — and the query
// side (internal/query/codeowners_ownership_cypher.go) reads team.ref as its
// established identity, so this is the real, deliberate production node
// shape, not a fixture oversight.
//
// This test constructs the edge exactly as the real writer and a real graph
// read would produce it (ToProps carrying only "ref") and proves
// assertMaterializedEdges reports an endpoint defect rather than a clean
// match — the SAME failure TestAssertMaterializedEdgesMissingEndpointIdentityFails
// asserts for a genuinely unmaterialized node, except here the node IS
// materialized correctly; the gate's identity resolution just does not know
// how to read it.
//
// endpointID and assertMaterializedEdges are shared across every #5543
// family (owned by the umbrella coordinator, not #5992 alone — a concurrent
// agent is proving #5998 rationale_edges against the same file), so the fix
// is out of this family's scope this phase. This test is committed as
// evidence for that report, not as a family fixture the codeowners guard
// depends on.
func TestAssertMaterializedEdgesCannotIdentifyRealCodeownerTeamEndpoint(t *testing.T) {
	t.Parallel()

	edgeTypes, err := ifa.MaterializedEdgeDomainEdgeTypes("codeowners_ownership_edges")
	if err != nil {
		t.Fatalf("MaterializedEdgeDomainEdgeTypes(codeowners_ownership_edges): %v", err)
	}

	expected := []ifa.ExpectedEdge{
		{RelationshipType: "DECLARES_CODEOWNER", SourceEntityID: "repo-x", TargetEntityID: "org/docs"},
	}
	// The real graph shape: Repository is id-keyed (existing endpointID
	// coverage), CodeownerTeam carries ONLY "ref" — no uid, no id.
	reader := fakeEdgeReader{edges: []graphdump.Edge{
		{
			Type:      "DECLARES_CODEOWNER",
			FromProps: map[string]any{"id": "repo-x"},
			ToProps:   map[string]any{"ref": "org/docs"},
		},
	}}

	err = assertMaterializedEdges(context.Background(), reader, "codeowners_ownership_edges", edgeTypes, nil, expected)

	// This is the RED assertion: a correctly-materialized DECLARES_CODEOWNER
	// edge to a correctly-materialized CodeownerTeam node MUST assert clean.
	// It fails today because endpointID cannot read "ref", so the edge is
	// reported as carrying an unidentified target endpoint even though the
	// endpoint is real and correct.
	if err == nil {
		t.Fatal("assertMaterializedEdges(real CodeownerTeam ref-only endpoint) = nil, want it to assert cleanly — if this passes, endpointID has grown ref-awareness and this gap is closed")
	}
	if !strings.Contains(err.Error(), "endpoint") {
		t.Fatalf("assertMaterializedEdges error = %q, want it to report an endpoint defect (confirming the diagnosed cause)", err)
	}
	t.Logf("confirmed gap: %v", err)
}
