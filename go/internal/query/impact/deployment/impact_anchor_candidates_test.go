// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package deployment

import (
	"context"
	"strings"
	"testing"
)

type candidateRowsGraph struct {
	rows   []map[string]any
	cypher string
	params map[string]any
}

func (g *candidateRowsGraph) Run(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
	g.cypher, g.params = cypher, params
	return g.rows, nil
}

func (g *candidateRowsGraph) RunSingle(context.Context, string, map[string]any) (map[string]any, error) {
	return nil, nil
}

// The scoped candidate statement keeps ORDER BY and LIMIT on the RETURN clause
// (a WITH ... ORDER BY form drops the ORDER BY on the pinned NornicDB build),
// binds its cap, and the unscoped single-row statement is still LIMIT 1.
// Decoded candidates are sorted by (id, label) and deduplicated by id.
func TestResolveImpactAnchorCandidatesShapeAndOrder(t *testing.T) {
	t.Parallel()
	g := &candidateRowsGraph{rows: []map[string]any{
		{"label": "Function", "id": "b", "repo_id": "repo-b"},
		{"label": "Workload", "id": "a"},
		{"label": "Function", "id": "a"},
		{"label": "", "id": "c"},
	}}
	got, err := ResolveImpactAnchorCandidates(context.Background(), g, "start_id", "shared")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].ID != "a" || got[0].Label != "Function" || got[1].ID != "b" || got[1].RepoID != "repo-b" {
		t.Fatalf("candidates = %+v, want [a/Function b]", got)
	}
	if !strings.HasSuffix(g.cypher, "\nRETURN label, id, name, labels, uid, repo_id\nORDER BY id, label\nLIMIT $candidate_limit") ||
		strings.Contains(g.cypher, "WITH ") {
		t.Fatalf("candidate cypher tail = %q", g.cypher[max(0, len(g.cypher)-120):])
	}
	if g.params["candidate_limit"] != ImpactAnchorCandidateLimit || g.params["start_id"] != "shared" {
		t.Fatalf("params = %v", g.params)
	}
	if single := impactAnchorResolveCypher("start_id"); !strings.HasSuffix(single, "\nLIMIT 1") || strings.Contains(single, "ORDER BY") {
		t.Fatalf("unscoped resolve changed: %q", single[max(0, len(single)-80):])
	}
	g.rows = nil
	if got, err := ResolveImpactAnchorCandidates(context.Background(), g, "start_id", "none"); err != nil || got != nil {
		t.Fatalf("no rows = %+v, %v; want nil", got, err)
	}
}
