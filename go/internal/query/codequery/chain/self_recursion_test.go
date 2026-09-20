// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package chain

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// TestBuildCallChainCypherNeo4jHandlesSelfRecursion is the regression for the
// B-7 Neo4j run's find_function_call_chain failure (#6782).
//
// The golden corpus asks for the chain from recursionFib to recursionFib, a
// self-recursive call. Neo4j's legacy shortestPath() raises
// Neo.DatabaseError.Statement.ExecutionFailed when a row reaches it with the
// start node equal to the end node, so the whole request failed with HTTP 500.
// The NornicDB route (a Go-side breadth-first walk) returns the cycle, which is
// the contract the snapshot asserts. The compat statement must use a shortest
// search that accepts a cycle, and bound every hop during that search.
func TestBuildCallChainCypherNeo4jHandlesSelfRecursion(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		req    Request
		access querycontract.RepositoryAccessFilter
		depth  string
	}{
		"unscoped": {
			req:    Request{Start: "recursionFib", End: "recursionFib", MaxDepth: 2},
			access: querycontract.RepositoryAccessFilter{AllScopes: true},
			depth:  "{1,2}",
		},
		"repo_scoped": {
			req:    Request{Start: "recursionFib", End: "recursionFib", RepoID: "repo:a", MaxDepth: 2},
			access: querycontract.RepositoryAccessFilter{AllScopes: true},
			depth:  "{1,2}",
		},
		"cross_repo_granted": {
			req: Request{
				Start: "recursionFib", End: "recursionFib", CrossRepo: true,
				StartRepoID: "repo:a", EndRepoID: "repo:b", MaxDepth: 4,
			},
			access: querycontract.RepositoryAccessFilter{AllowedRepositoryIDs: []string{"repo:a", "repo:b"}},
			depth:  "{1,4}",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			cypher, _ := BuildCallChainCypher(tc.req, querycontract.GraphBackendNeo4j, tc.access)
			normalized := querycontract.NormalizeCypherWhitespace(cypher)

			if strings.Contains(normalized, "shortestPath(") {
				t.Fatalf("the compat statement still runs legacy shortestPath(), which raises on start = end rows:\n%s", cypher)
			}
			call := strings.Index(normalized, "CALL (start, end) {")
			shortest := strings.Index(normalized, "MATCH path = SHORTEST 1 (start)")
			if call < 0 || shortest < call {
				t.Fatalf("the SHORTEST search does not run in a scoped CALL subquery "+
					"(written inline, the planner scans both anchor indexes):\n%s", cypher)
			}
			search := normalized[shortest:]
			if end := strings.Index(search, " RETURN path"); end >= 0 {
				search = search[:end]
			}
			if !strings.Contains(search, "(()-[:CALLS]->(node)") {
				t.Fatalf("the search does not traverse CALLS hops:\n%s", search)
			}
			// A trailing WHERE on a SHORTEST path filters AFTER the shortest
			// path is chosen (measured on neo4j:2026-community: 0 rows where
			// a longer in-bound cycle exists), so every hop bound must sit
			// inside the quantified pattern, where the search applies it.
			for _, hop := range PathHopPredicates(tc.req, tc.access) {
				if !strings.Contains(search, hop) {
					t.Fatalf("the search does not bound each hop with %q:\n%s", hop, search)
				}
			}
			if strings.Contains(search, "nodes(path)") {
				t.Fatalf("the search post-filters the chosen path instead of bounding it:\n%s", search)
			}
			if !strings.Contains(search, tc.depth) {
				t.Fatalf("the search does not carry the request depth bound %s:\n%s", tc.depth, search)
			}

			endpoints, _, parsed := ClausePredicates(cypher)
			if !parsed {
				t.Fatalf("the grant fake can no longer read the compat statement:\n%s", cypher)
			}
			for _, predicate := range endpoints {
				if strings.Contains(predicate, "CALL") {
					t.Fatalf("endpoint predicates absorbed subquery text %q:\n%s", predicate, cypher)
				}
			}
		})
	}
}
