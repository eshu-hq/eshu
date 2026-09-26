// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codequery

import (
	"context"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"

	"github.com/eshu-hq/eshu/go/internal/query/testutil"
)

// relDispatchGraph answers just enough for a transitive CALLS request to reach
// its traversal: a label row, a metadata row for the anchor, and no neighbours.
// It records every statement so the test can see which traversal ran.
type relDispatchGraph struct {
	mu         sync.Mutex
	statements []string
	params     []map[string]any
}

func (g *relDispatchGraph) Run(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
	g.mu.Lock()
	g.statements = append(g.statements, cypher)
	g.params = append(g.params, params)
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

// relGrantCondition is the exact grant text a statement must carry on alias.
func relGrantCondition(alias string) string {
	return "(" + alias + ".repo_id IN $allowed_repository_ids OR " + alias + ".repo_id IN $allowed_scope_ids)"
}

// relGrantStatement returns the first recorded statement containing marker,
// normalized, with its parameters.
func relGrantStatement(t *testing.T, graph *relDispatchGraph, marker string) (string, map[string]any) {
	t.Helper()
	for i, stmt := range graph.statements {
		if strings.Contains(stmt, marker) {
			return querycontract.NormalizeCypherWhitespace(stmt), graph.params[i]
		}
	}
	t.Fatalf("no statement containing %q among %d", marker, len(graph.statements))
	return "", nil
}

func relGrantAssertBound(t *testing.T, name string, params map[string]any) {
	t.Helper()
	if !reflect.DeepEqual(params["allowed_repository_ids"], []string{codeGrantGrantedRepo}) {
		t.Fatalf("%s: $allowed_repository_ids = %#v, want [%s]", name, params["allowed_repository_ids"], codeGrantGrantedRepo)
	}
	if _, ok := params["allowed_scope_ids"]; !ok {
		t.Fatalf("%s: $allowed_scope_ids is not bound", name)
	}
}

// TestCodeRelationshipsGrantStatementsThroughTheHandler renders the grant-bound
// statements the handler actually sends (#5167) and checks their clause
// position and parameters: the NornicDB per-hop walk in both directions, and
// the Neo4j one-statement read for the entity-id, name and name+repo anchors.
// An unscoped caller's statements carry no grant.
func TestCodeRelationshipsGrantStatementsThroughTheHandler(t *testing.T) {
	t.Parallel()
	auth := testutil.CodeGrantScopedAuthContext([]string{codeGrantGrantedRepo})
	serve := func(backend GraphBackend, body map[string]any, a *AuthContext) *relDispatchGraph {
		graph := &relDispatchGraph{}
		handler := &CodeHandler{Profile: ProfileLocalAuthoritative, GraphBackend: backend, Neo4j: graph}
		relGrantFixture{handler: handler}.serve(t, body, a)
		return graph
	}

	for _, tc := range []struct{ direction, anchorAlias, neighbourAlias string }{
		{"outgoing", "source", "target"},
		{"incoming", "target", "source"},
	} {
		body := map[string]any{"entity_id": relGrantGrantedID, "direction": tc.direction, "relationship_type": "CALLS", "transitive": true, "max_depth": 2}
		stmt, params := relGrantStatement(t, serve(GraphBackendNornicDB, body, &auth), "MATCH (source)-[:CALLS]->(target)")
		clause := "MATCH (source)-[:CALLS]->(target) WHERE " + tc.anchorAlias + ".uid = $entity_id AND " + relGrantCondition(tc.neighbourAlias) + " RETURN"
		if !strings.Contains(stmt, clause) {
			t.Fatalf("nornicdb %s hop: want %q in:\n%s", tc.direction, clause, stmt)
		}
		relGrantAssertBound(t, "nornicdb "+tc.direction+" hop", params)
		if stmt, params := relGrantStatement(t, serve(GraphBackendNornicDB, body, nil), "MATCH (source)-[:CALLS]->(target)"); strings.Contains(stmt, "allowed_") || params["allowed_repository_ids"] != nil {
			t.Fatalf("nornicdb %s hop: unscoped statement carries a grant:\n%s", tc.direction, stmt)
		}
	}

	for _, body := range []map[string]any{
		{"entity_id": relGrantGrantedID},
		{"name": relGrantGrantedName},
		{"name": relGrantGrantedName, "repo_id": codeGrantGrantedRepo},
	} {
		stmt, params := relGrantStatement(t, serve(GraphBackendNeo4j, body, &auth), "OPTIONAL MATCH (e)-[outgoingRel]->(target)")
		limit := strings.Index(stmt, " LIMIT ")
		for _, clause := range []string{
			"WITH e WHERE " + relGrantCondition("e") + " OPTIONAL MATCH",
			"OPTIONAL MATCH (e)-[outgoingRel]->(target) WHERE " + relGrantCondition("target") + " OPTIONAL MATCH",
			"OPTIONAL MATCH (source)-[incomingRel]->(e) WHERE " + relGrantCondition("source") + " OPTIONAL MATCH",
		} {
			if at := strings.Index(stmt, clause); at < 0 || at > limit {
				t.Fatalf("neo4j %v: want %q before LIMIT in:\n%s", body, clause, stmt)
			}
		}
		relGrantAssertBound(t, fmt.Sprintf("neo4j %v", body), params)
		if stmt, params := relGrantStatement(t, serve(GraphBackendNeo4j, body, nil), "OPTIONAL MATCH (e)-[outgoingRel]->(target)"); strings.Contains(stmt, "allowed_") || params["allowed_repository_ids"] != nil {
			t.Fatalf("neo4j %v: unscoped statement carries a grant:\n%s", body, stmt)
		}
	}
}
