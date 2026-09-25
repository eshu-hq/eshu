// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codequery

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/testutil"
)

// relDispatchGraph answers just enough for a transitive CALLS request to reach
// its traversal: a label row, a metadata row for the anchor, and no neighbours.
// It records every statement so the test can see which traversal ran.
type relDispatchGraph struct {
	mu         sync.Mutex
	statements []string
}

func (g *relDispatchGraph) Run(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
	g.mu.Lock()
	g.statements = append(g.statements, cypher)
	g.mu.Unlock()
	row := map[string]any{
		"id": relGrantGrantedID, "uid": relGrantGrantedID, "name": relGrantGrantedName,
		"labels": []any{"Function"}, "repo_id": codeGrantGrantedRepo,
	}
	switch {
	case strings.Contains(cypher, "CALL {"), strings.Contains(cypher, "REPO_CONTAINS]->(f)"):
		return []map[string]any{row}, nil
	default:
		return nil, nil
	}
}

func (g *relDispatchGraph) RunSingle(ctx context.Context, cypher string, params map[string]any) (map[string]any, error) {
	rows, err := g.Run(ctx, cypher, params)
	if len(rows) == 0 {
		// The Neo4j metadata read is a RunSingle over the one-statement row.
		return map[string]any{"id": relGrantGrantedID, "name": relGrantGrantedName, "repo_id": codeGrantGrantedRepo}, err
	}
	return rows[0], err
}

// TestCodeRelationshipsTransitiveDispatchIsPinnedPerBackend pins the backend
// split for transitive CALLS (#5167).
//
// codemodel.BuildTransitiveRelationshipRowsCypher binds the grant with
// all(node IN nodes(path) WHERE ...). Measured on neo4j:2026-community, that
// excludes every path through an ungranted node. Measured on the pinned
// NornicDB, the same predicate filters NOTHING, so NornicDB must answer
// transitive CALLS only through the per-hop Go breadth-first walk
// (nornicDBTransitiveOneHopRows), whose hop WHERE does bind. This test fails if
// a refactor ever routes NornicDB through the variable-length statement, and
// if the Neo4j statement ever loses its path-wide grant.
func TestCodeRelationshipsTransitiveDispatchIsPinnedPerBackend(t *testing.T) {
	t.Parallel()
	auth := testutil.CodeGrantScopedAuthContext([]string{codeGrantGrantedRepo})
	body := map[string]any{"entity_id": relGrantGrantedID, "direction": "outgoing", "relationship_type": "CALLS", "transitive": true, "max_depth": 3}

	for _, tc := range []struct {
		backend     GraphBackend
		wantHop     bool
		wantVarPath bool
	}{
		{backend: GraphBackendNornicDB, wantHop: true, wantVarPath: false},
		{backend: GraphBackendNeo4j, wantHop: false, wantVarPath: true},
	} {
		t.Run(string(tc.backend), func(t *testing.T) {
			t.Parallel()
			graph := &relDispatchGraph{}
			handler := &CodeHandler{Profile: ProfileLocalAuthoritative, GraphBackend: tc.backend, Neo4j: graph}
			rec := relGrantFixture{handler: handler}.serve(t, body, &auth)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
			}
			var sawHop, sawVarPath, varPathBound bool
			for _, stmt := range graph.statements {
				if strings.Contains(stmt, "MATCH (source)-[:CALLS]->(target)") {
					sawHop = true
				}
				if strings.Contains(stmt, "[:CALLS*1..") {
					sawVarPath = true
					varPathBound = strings.Contains(stmt, "all(node IN nodes(path) WHERE (node.repo_id IN $allowed_repository_ids")
				}
			}
			if sawHop != tc.wantHop || sawVarPath != tc.wantVarPath {
				t.Fatalf("%s: per-hop walk ran = %v (want %v), variable-length statement ran = %v (want %v)",
					tc.backend, sawHop, tc.wantHop, sawVarPath, tc.wantVarPath)
			}
			if sawVarPath && !varPathBound {
				t.Fatalf("%s: the variable-length statement carries no path-wide grant", tc.backend)
			}
		})
	}
}
