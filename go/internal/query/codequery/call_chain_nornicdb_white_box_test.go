// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codequery

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querytestutil"
)

// TestCallChainOneHopBindsTheGrantInTheAnchoringMatch moved here from
// auth_scoped_call_chain_grant_test.go (#6060 lane A code L3 PR2): it pins
// nornicDBCallChainOneHopRows' shipped Cypher text directly rather than
// through POST /api/v0/code/call-chain, so it stays a white-box test in
// package query instead of exporting the method the route already covers at
// the response level.
func TestCallChainOneHopBindsTheGrantInTheAnchoringMatch(t *testing.T) {
	t.Parallel()

	graph := &callChainGrantGraph{entities: callChainGrantEntities()}
	handler := &CodeHandler{GraphBackend: GraphBackendNornicDB, Neo4j: graph}
	access := repositoryAccessFilter{AllowedRepositoryIDs: []string{codeGrantGrantedRepo}}
	ctx := ContextWithAuthContext(t.Context(), querytestutil.CodeGrantScopedAuthContext([]string{codeGrantGrantedRepo}))
	if _, err := handler.nornicDBCallChainOneHopRows(
		ctx, callChainGrantedStart, "Function", []string{codeGrantGrantedRepo},
	); err != nil {
		t.Fatalf("nornicDBCallChainOneHopRows() error = %v", err)
	}
	if len(graph.statements) != 1 {
		t.Fatalf("statements = %d, want 1", len(graph.statements))
	}
	anchoring, stranded := storyClausePredicates(graph.statements[0])
	for _, want := range []string{
		"coalesce(target.repo_id, '') IN $traversal_repo_ids",
		access.GraphConditionOnProperty("target", "repo_id"),
	} {
		if !containsPredicate(anchoring, want) {
			t.Fatalf("the anchoring MATCH does not carry %q:\n%s", want, graph.statements[0])
		}
		if containsPredicate(stranded, want) {
			t.Fatalf("%q sits after an OPTIONAL MATCH, where it filters nothing:\n%s", want, graph.statements[0])
		}
	}
}
