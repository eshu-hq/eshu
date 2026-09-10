// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package chain_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/codequery"
	"github.com/eshu-hq/eshu/go/internal/query/codequery/chain"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/querytestutil"
)

// TestCallChainOneHopBindsTheGrantInTheAnchoringMatch moved here from
// auth_scoped_call_chain_grant_test.go (#6060 lane A code L3 PR2): it pins
// the shipped one-hop Cypher text rather than the response, so it stays a
// white-box test. It used to call the handler method directly; the method is
// unexported and unreachable from this leaf's external test package, so the
// proof now drives the same read through the route on a NornicDB backend and
// judges the recorded statement: both the traversal bound and the caller
// grant must sit in the anchoring MATCH's own WHERE, never after an OPTIONAL
// MATCH where they would filter nothing.
func TestCallChainOneHopBindsTheGrantInTheAnchoringMatch(t *testing.T) {
	t.Parallel()

	graph := &chain.GrantGraph{Entities: callChainGrantEntities()}
	handler := &codequery.CodeHandler{
		Profile:      querycontract.ProfileLocalAuthoritative,
		GraphBackend: querycontract.GraphBackendNornicDB,
		Neo4j:        graph,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	auth := querytestutil.CodeGrantScopedAuthContext([]string{codeGrantGrantedRepo})
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, newChainRouteRequest(t, map[string]any{
		"start_entity_id": callChainGrantedStart,
		"end_entity_id":   callChainGrantedEnd,
		"repo_id":         codeGrantGrantedRepo,
		"max_depth":       3,
	}, &auth))
	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, rec.Body.String())
	}
	if body := rec.Body.String(); !strings.Contains(body, callChainGrantedName) {
		t.Fatalf("the bounded traversal lost its in-grant endpoint: %s", body)
	}

	statement := ""
	for _, recorded := range graph.Statements {
		if strings.Contains(recorded, "-[:CALLS]->(target)") {
			statement = recorded
		}
	}
	if statement == "" {
		t.Fatalf("the route issued no one-hop read: %v", graph.Statements)
	}
	access := querycontract.RepositoryAccessFilter{AllowedRepositoryIDs: []string{codeGrantGrantedRepo}}
	anchoring, stranded := chain.StoryClausePredicates(statement)
	for _, want := range []string{
		"coalesce(target.repo_id, '') IN $traversal_repo_ids",
		access.GraphConditionOnProperty("target", "repo_id"),
	} {
		if !containsPredicate(anchoring, want) {
			t.Fatalf("the anchoring MATCH does not carry %q:\n%s", want, statement)
		}
		if containsPredicate(stranded, want) {
			t.Fatalf("%q sits after an OPTIONAL MATCH, where it filters nothing:\n%s", want, statement)
		}
	}
}
