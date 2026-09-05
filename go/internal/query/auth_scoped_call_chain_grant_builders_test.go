// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"strings"
	"testing"
)

// Shipped-text pin for the two shortestPath call-chain builders, split out of
// auth_scoped_call_chain_grant_test.go to keep both files under the file cap.

// TestShortestPathCallChainBuildersBindTheGrant is the shipped-text pin for both
// shortestPath builders, with a SCOPED filter -- the caller class every other
// call site of these two builders omits.
func TestShortestPathCallChainBuildersBindTheGrant(t *testing.T) {
	t.Parallel()

	access := repositoryAccessFilter{AllowedRepositoryIDs: []string{codeGrantGrantedRepo}}
	req := callChainRequest{
		StartEntityID: callChainGrantedStart,
		EndEntityID:   callChainGrantedEnd,
		MaxDepth:      3,
	}

	t.Run("neo4j_compat_binds_endpoints_and_every_hop", func(t *testing.T) {
		t.Parallel()
		cypher, params := buildCallChainCypher(req, GraphBackendNeo4j, access)
		endpoints, hops, parsed := callChainClausePredicates(cypher)
		if !parsed {
			t.Fatalf("the compat statement no longer has the shape this pin reads:\n%s", cypher)
		}
		for _, want := range []string{
			access.GraphConditionOnProperty("start", "repo_id"),
			access.GraphConditionOnProperty("end", "repo_id"),
		} {
			if !containsPredicate(endpoints, want) {
				t.Fatalf("the anchoring WHERE does not carry %q:\n%s", want, cypher)
			}
		}
		// The rendered condition, not a substring of it: the point of routing
		// this through the grant contract is that a change to how the contract
		// renders must move the hop predicate too, and a substring assertion
		// would not notice if it did not.
		if !containsPredicate(hops, access.GraphConditionOnProperty("node", "repo_id")) {
			t.Fatalf("no hop predicate binds the grant, so an interior hop is unbounded:\n%s", cypher)
		}
		if !graphParamContains(params, "allowed_repository_ids", codeGrantGrantedRepo) {
			t.Fatalf("params do not bind the grant array: %#v", params)
		}
	})

	t.Run("nornicdb_binds_endpoints", func(t *testing.T) {
		t.Parallel()
		cypher, params := buildNornicDBCallChainCypher(req, access)
		endpoints, hops, parsed := callChainClausePredicates(cypher)
		if !parsed {
			t.Fatalf("the NornicDB statement no longer has the shape this pin reads:\n%s", cypher)
		}
		for _, want := range []string{
			access.GraphConditionOnProperty("start", "repo_id"),
			access.GraphConditionOnProperty("end", "repo_id"),
		} {
			if !containsPredicate(endpoints, want) {
				t.Fatalf("the anchoring WHERE does not carry %q:\n%s", want, cypher)
			}
		}
		// Deliberately no hop predicate: a list-membership test inside
		// all(node IN nodes(path) ...) is not evaluated on the pinned NornicDB
		// build, so writing one here would be grant text that grants nothing.
		// The live NornicDB path bounds each hop as its traversal expands.
		if containsPredicate(hops, access.GraphConditionOnProperty("node", "repo_id")) {
			t.Fatalf("the NornicDB builder gained a hop predicate the backend does not evaluate:\n%s", cypher)
		}
		if !graphParamContains(params, "allowed_repository_ids", codeGrantGrantedRepo) {
			t.Fatalf("params do not bind the grant array: %#v", params)
		}
	})

	t.Run("unscoped_carries_no_grant", func(t *testing.T) {
		t.Parallel()
		for name, cypher := range map[string]string{
			"neo4j_compat": firstOf(buildCallChainCypher(req, GraphBackendNeo4j, repositoryAccessFilter{AllScopes: true})),
			"nornicdb":     firstOf(buildNornicDBCallChainCypher(req, repositoryAccessFilter{AllScopes: true})),
		} {
			if strings.Contains(cypher, "$allowed_repository_ids") || strings.Contains(cypher, "$allowed_scope_ids") {
				t.Fatalf("%s rendered a grant for an unscoped caller:\n%s", name, cypher)
			}
		}
	})
}
