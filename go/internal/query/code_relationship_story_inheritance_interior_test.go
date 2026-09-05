// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"testing"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j/dbtype"
)

// The NornicDB inheritance walk binds its two path ENDPOINTS in Cypher and
// filters the classes between them in Go, because on the pinned build no
// all(node IN nodes(path) ...) predicate works in either direction: the list
// form never filters and the scalar form filters everything out, including
// chains where every node is granted. Measured for #6548; the table is in
// docs/public/reference/nornicdb-query-pitfalls.md.
//
// So the statement projects nodes(path) and this is the filter. An out-of-grant
// class between two granted ones must take the whole row with it -- otherwise
// the caller learns a depth that counts hops through a repository it cannot
// read -- and path_nodes must never reach the response.

// inheritanceInteriorGraph returns fixed rows, so the test drives the Go filter
// rather than a fake's idea of Cypher.
type inheritanceInteriorGraph struct {
	rows []map[string]any
	runs int
}

func (g *inheritanceInteriorGraph) Run(
	_ context.Context,
	_ string,
	_ map[string]any,
) ([]map[string]any, error) {
	g.runs++
	out := make([]map[string]any, 0, len(g.rows))
	for _, row := range g.rows {
		out = append(out, cloneGraphRow(row))
	}
	return out, nil
}

func (g *inheritanceInteriorGraph) RunSingle(
	ctx context.Context,
	cypher string,
	params map[string]any,
) (map[string]any, error) {
	rows, err := g.Run(ctx, cypher, params)
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	return rows[0], nil
}

func inheritancePathNode(repoID string) dbtype.Node {
	return dbtype.Node{Props: map[string]any{"repo_id": repoID}}
}

// TestNornicDBInheritanceWalkDropsAnOutOfGrantInteriorClass is the regression.
// Two rows come back from the backend: one whose path is wholly in grant, one
// that reaches a granted class only by passing through an ungranted one.
func TestNornicDBInheritanceWalkDropsAnOutOfGrantInteriorClass(t *testing.T) {
	t.Parallel()

	graph := &inheritanceInteriorGraph{rows: []map[string]any{
		{
			"direction": "outgoing", "source_uid": "class:a", "source_name": "GrantedA",
			"target_uid": "class:d", "target_name": "GrantedD", "depth": int64(1),
			"path_nodes": []any{
				inheritancePathNode(codeGrantGrantedRepo),
				inheritancePathNode(codeGrantGrantedRepo),
			},
		},
		{
			"direction": "outgoing", "source_uid": "class:a", "source_name": "GrantedA",
			"target_uid": "class:b", "target_name": "GrantedB", "depth": int64(2),
			"path_nodes": []any{
				inheritancePathNode(codeGrantGrantedRepo),
				inheritancePathNode(codeGrantOtherRepo),
				inheritancePathNode(codeGrantGrantedRepo),
			},
		},
	}}
	handler := &CodeHandler{Profile: ProfileLocalAuthoritative, GraphBackend: GraphBackendNornicDB, Neo4j: graph}
	ctx := ContextWithAuthContext(context.Background(), codeGrantScopedAuthContext([]string{codeGrantGrantedRepo}))

	rows, err := handler.nornicDBRelationshipStoryInheritanceDepthRows(ctx, relationshipStoryRequest{Limit: 50}, "class:a", "outgoing")
	if err != nil {
		t.Fatalf("inheritance rows: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1; the chain through the ungranted class was kept: %#v", len(rows), rows)
	}
	if got := StringVal(rows[0], "target_name"); got != "GrantedD" {
		t.Fatalf("surviving row = %q, want the wholly-granted GrantedD", got)
	}
	if _, leaked := rows[0]["path_nodes"]; leaked {
		t.Fatalf("path_nodes reached the response: %#v", rows[0])
	}
}

// TestNornicDBInheritanceWalkFailsClosedOnAnUnattributableHop pins the other
// half: a class the graph cannot attribute to a repository is not readable, so
// a path through it is dropped rather than admitted.
func TestNornicDBInheritanceWalkFailsClosedOnAnUnattributableHop(t *testing.T) {
	t.Parallel()

	for name, interior := range map[string]any{
		"empty_repo_id": inheritancePathNode(""),
		"absent_prop":   dbtype.Node{Props: map[string]any{}},
		"unknown_shape": "not-a-node",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			graph := &inheritanceInteriorGraph{rows: []map[string]any{{
				"direction": "outgoing", "source_uid": "class:a", "target_uid": "class:b",
				"target_name": "GrantedB", "depth": int64(2),
				"path_nodes": []any{
					inheritancePathNode(codeGrantGrantedRepo),
					interior,
					inheritancePathNode(codeGrantGrantedRepo),
				},
			}}}
			handler := &CodeHandler{Profile: ProfileLocalAuthoritative, GraphBackend: GraphBackendNornicDB, Neo4j: graph}
			ctx := ContextWithAuthContext(context.Background(), codeGrantScopedAuthContext([]string{codeGrantGrantedRepo}))
			rows, err := handler.nornicDBRelationshipStoryInheritanceDepthRows(ctx, relationshipStoryRequest{Limit: 50}, "class:a", "outgoing")
			if err != nil {
				t.Fatalf("inheritance rows: %v", err)
			}
			if len(rows) != 0 {
				t.Fatalf("an unattributable interior class was admitted: %#v", rows)
			}
		})
	}
}

// TestNornicDBInheritanceWalkLeavesAnUnscopedCallerAlone pins that the
// projection and the filter are both scoped-only: a shared-key caller's
// statement carries no nodes(path) and its rows are untouched.
func TestNornicDBInheritanceWalkLeavesAnUnscopedCallerAlone(t *testing.T) {
	t.Parallel()

	access := repositoryAccessFilter{AllScopes: true}
	cypher := firstOf(nornicDBRelationshipStoryInheritanceDepthCypher(
		relationshipStoryRequest{Limit: 50}, "class:a", "outgoing", "uid", access))
	if containsAny(cypher, "path_nodes", "nodes(path)") {
		t.Fatalf("an unscoped caller rendered the path projection:\n%s", cypher)
	}

	graph := &inheritanceInteriorGraph{rows: []map[string]any{
		{"direction": "outgoing", "target_uid": "class:b", "target_name": "GrantedB", "depth": int64(2)},
	}}
	handler := &CodeHandler{Profile: ProfileLocalAuthoritative, GraphBackend: GraphBackendNornicDB, Neo4j: graph}
	rows, err := handler.nornicDBRelationshipStoryInheritanceDepthRows(
		context.Background(), relationshipStoryRequest{Limit: 50}, "class:a", "outgoing")
	if err != nil {
		t.Fatalf("inheritance rows: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("an unscoped caller lost rows: %#v", rows)
	}
}

// TestNornicDBInheritanceWalkScopedStatementProjectsThePath pins the other side
// of that: a scoped caller's statement must carry the projection the filter
// reads, in both directions.
func TestNornicDBInheritanceWalkScopedStatementProjectsThePath(t *testing.T) {
	t.Parallel()

	access := repositoryAccessFilter{AllowedRepositoryIDs: []string{codeGrantGrantedRepo}}
	for _, direction := range []string{"outgoing", "incoming"} {
		cypher := firstOf(nornicDBRelationshipStoryInheritanceDepthCypher(
			relationshipStoryRequest{Limit: 50}, "class:a", direction, "uid", access))
		if !containsAny(cypher, "nodes(path) as path_nodes") {
			t.Fatalf("the scoped %s statement does not project the path the Go filter reads:\n%s", direction, cypher)
		}
	}
}

func containsAny(haystack string, needles ...string) bool {
	for _, needle := range needles {
		if len(needle) > 0 && len(haystack) >= len(needle) {
			for i := 0; i+len(needle) <= len(haystack); i++ {
				if haystack[i:i+len(needle)] == needle {
					return true
				}
			}
		}
	}
	return false
}
