// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package workloadinstance

import (
	"context"
	"strings"
	"testing"
)

type stubGraph struct {
	cypher string
	params map[string]any
	rows   []map[string]any
}

func (g *stubGraph) Run(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
	g.cypher, g.params = cypher, params
	return g.rows, nil
}

var (
	prod  = Anchor{WorkloadID: "workload:app", Environment: "prod"}
	stage = Anchor{WorkloadID: "workload:app", Environment: "stage"}
)

// TestGraphExistenceLookupMirrorsWriterMatch pins the lookup to the USES
// writer's endpoint MATCH shape, so "ready" means exactly "the writer's MATCH
// will bind", and proves it maps returned rows back to anchors.
func TestGraphExistenceLookupMirrorsWriterMatch(t *testing.T) {
	t.Parallel()
	graph := &stubGraph{rows: []map[string]any{
		{"ready_workload_id": "workload:app", "ready_environment": "prod"},
		{"ready_workload_id": "", "ready_environment": "stage"},
	}}
	existing, err := GraphExistenceLookup{Graph: graph}.ExistingAnchors(context.Background(), []Anchor{prod, stage})
	if err != nil {
		t.Fatalf("ExistingAnchors() error = %v", err)
	}
	for _, fragment := range []string{
		"MATCH (workload:Workload {id: anchor.workload_id})<-[:INSTANCE_OF]-(instance:WorkloadInstance)",
		"WHERE instance.environment = anchor.environment",
	} {
		if !strings.Contains(graph.cypher, fragment) {
			t.Fatalf("cypher missing writer-parity fragment %q:\n%s", fragment, graph.cypher)
		}
	}
	if anchors, _ := graph.params["anchors"].([]any); len(anchors) != 2 {
		t.Fatalf("anchors param = %v, want 2 entries", graph.params["anchors"])
	}
	if _, ok := existing[prod]; !ok || len(existing) != 1 {
		t.Fatalf("existing = %v, want only prod (a row with no workload id is ignored)", existing)
	}
}

// TestGraphExistenceLookupRequiresGraph proves a missing graph is a wiring
// error, never "nothing exists".
func TestGraphExistenceLookupRequiresGraph(t *testing.T) {
	t.Parallel()
	if _, err := (GraphExistenceLookup{}).ExistingAnchors(context.Background(), []Anchor{prod}); err == nil {
		t.Fatal("ExistingAnchors() error = nil, want a wiring error")
	}
}
