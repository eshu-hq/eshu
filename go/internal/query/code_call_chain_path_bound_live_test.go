// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build live_nornicdb_call_chain

// Path-wide bound probe for #5167 batch 2b, question 2.
//
// buildNornicDBCallChainCypher and its Neo4j-compat sibling bound the whole
// traversal with
//
//	WHERE all(node IN nodes(path) WHERE coalesce(node.repo_id, '') = $repo_id)
//
// attached to the MATCH that declares the shortestPath. That is a required
// MATCH, so clause position is not the question here; the question is whether
// the pinned build evaluates an all() list predicate over nodes(path) at all.
// It matters because that clause is the only thing stopping a chain between two
// in-repository endpoints from being routed through an intermediate hop in
// another repository -- and the intermediate hop's identity ships in the
// response's chain array.
package query

import (
	"context"
	"testing"
	"time"
)

// TestLiveNornicDBCallChainShippedNornicDBBuilderParses runs the exact
// statement buildNornicDBCallChainCypher ships for a repo-scoped request whose
// only route between two in-repository endpoints crosses the other repository.
//
// The builder is not on the live NornicDB path -- handleCallChain sends a
// NornicDB backend to nornicDBCallChainRows and only a non-NornicDB backend
// reaches buildCallChainCypher -- so this records what the statement does if
// anything ever routes to it.
func TestLiveNornicDBCallChainShippedNornicDBBuilderParses(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	handler, closeDriver := newLiveCallChainHandler(ctx, t)
	defer closeDriver()

	cypher, params := buildNornicDBCallChainCypher(callChainRequest{
		StartEntityID: liveClauseChainStartUID,
		EndEntityID:   liveClauseChainEndUID,
		RepoID:        codeGrantGrantedRepo,
		MaxDepth:      5,
	})
	t.Logf("shipped call-chain statement:\n%s", cypher)
	rows, err := handler.Neo4j.Run(ctx, cypher, params)
	if err != nil {
		t.Fatalf("the shipped NornicDB call-chain statement did not run: %v", err)
	}
	t.Logf("shipped call-chain statement returned %d rows", len(rows))
	for _, row := range rows {
		t.Logf("  depth=%v chain=%v", row["depth"], normalizeCallChainNodes(row["chain"]))
	}
	if len(rows) != 0 {
		t.Fatalf("the repo-scoped path bound admitted a chain routed through %q: %v",
			liveClauseChainBridge, rows)
	}
}

// TestLiveNornicDBPathListPredicateBehaviour records what the pinned build
// actually does with each way of writing "every hop on this path is inside the
// caller's grant", on a shape the build does run: a labelled inline-property
// anchor on both endpoints, over one in-repository chain whose only route
// crosses the other repository.
//
// wantRows is the MEASURED result, not the wanted one. They agree for the
// single scalar equality and disagree for every list form, and pinning the
// measurement is the point: a later NornicDB build that changes any of these
// numbers should be seen rather than silently absorbed. Each case says which
// kind of number it is.
func TestLiveNornicDBPathListPredicateBehaviour(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	handler, closeDriver := newLiveCallChainHandler(ctx, t)
	defer closeDriver()

	for _, tc := range []struct {
		name      string
		where     string
		params    map[string]any
		wantRows  int
		explained string
	}{
		{
			name:      "no_predicate_control",
			where:     "",
			params:    map[string]any{},
			wantRows:  1,
			explained: "MEASURED, right: the chain exists, so every later case starts from one row",
		},
		{
			name:      "unsatisfiable_all_over_nodes",
			where:     `WHERE all(node IN nodes(path) WHERE coalesce(node.repo_id, '') = $repo_id)`,
			params:    map[string]any{"repo_id": "repo://nobody/owns-this"},
			wantRows:  0,
			explained: "MEASURED, right: a single scalar equality inside all() IS evaluated",
		},
		{
			name:      "shipped_repo_scoped_all_over_nodes",
			where:     `WHERE all(node IN nodes(path) WHERE coalesce(node.repo_id, '') = $repo_id)`,
			params:    map[string]any{"repo_id": codeGrantGrantedRepo},
			wantRows:  0,
			explained: "MEASURED, right: the shipped repo-scoped scalar form does bound the path",
		},
		{
			name:      "shipped_repo_scoped_all_over_nodes_list_form",
			where:     `WHERE all(node IN nodes(path) WHERE coalesce(node.repo_id, '') IN $traversal_repo_ids)`,
			params:    map[string]any{"traversal_repo_ids": []string{codeGrantGrantedRepo}},
			wantRows:  1,
			explained: "MEASURED, WRONG: the list form is what a grant renders, and it admits the out-of-grant bridge hop",
		},
		{
			name:      "all_over_nodes_list_form_without_coalesce",
			where:     `WHERE all(node IN nodes(path) WHERE node.repo_id IN $traversal_repo_ids)`,
			params:    map[string]any{"traversal_repo_ids": []string{codeGrantGrantedRepo}},
			wantRows:  1,
			explained: "MEASURED, WRONG: the same list form without the coalesce wrapper",
		},
		{
			name:      "all_over_nodes_list_form_with_leading_conjunct",
			where:     `WHERE start.repo_id IN $traversal_repo_ids AND all(node IN nodes(path) WHERE node.repo_id IN $traversal_repo_ids)`,
			params:    map[string]any{"traversal_repo_ids": []string{codeGrantGrantedRepo}},
			wantRows:  1,
			explained: "MEASURED, WRONG: a satisfied conjunct ahead of the all() does not rescue the list form",
		},
		{
			name:      "none_over_nodes_negated_list_form",
			where:     `WHERE none(node IN nodes(path) WHERE NOT (coalesce(node.repo_id, '') IN $traversal_repo_ids))`,
			params:    map[string]any{"traversal_repo_ids": []string{codeGrantGrantedRepo}},
			wantRows:  1,
			explained: "MEASURED, WRONG: none()/NOT over the same list membership test is inert too",
		},
		{
			name:      "size_of_out_of_grant_hops_is_zero",
			where:     `WHERE size([node IN nodes(path) WHERE NOT (coalesce(node.repo_id, '') IN $traversal_repo_ids)]) = 0`,
			params:    map[string]any{"traversal_repo_ids": []string{codeGrantGrantedRepo}},
			wantRows:  1,
			explained: "MEASURED, WRONG: counting the out-of-grant hops in a comprehension is inert too",
		},
		{
			name:      "all_over_nodes_inline_literal_list",
			where:     `WHERE all(node IN nodes(path) WHERE coalesce(node.repo_id, '') IN ["` + codeGrantGrantedRepo + `"])`,
			params:    map[string]any{},
			wantRows:  1,
			explained: "MEASURED, WRONG: an inline literal list fails the same way, so IN is the offender and not parameter binding",
		},
		{
			name:      "all_over_nodes_scalar_equality_disjunction",
			where:     `WHERE all(node IN nodes(path) WHERE coalesce(node.repo_id, '') = $grant_0 OR coalesce(node.repo_id, '') = $grant_1)`,
			params:    map[string]any{"grant_0": codeGrantGrantedRepo, "grant_1": "repo://nobody/owns-this"},
			wantRows:  0,
			explained: "MEASURED, right answer for the wrong reason -- see the next case",
		},
		{
			name:      "all_over_nodes_scalar_equality_disjunction_admits_in_grant_chain",
			where:     `WHERE all(node IN nodes(path) WHERE coalesce(node.repo_id, '') = $grant_0 OR coalesce(node.repo_id, '') = $grant_1)`,
			params:    map[string]any{"grant_0": codeGrantGrantedRepo, "grant_1": codeGrantOtherRepo},
			wantRows:  0,
			explained: "MEASURED, WRONG: the OR-ed disjunction drops the chain even when both repositories are granted, so the previous zero is over-filtering, not filtering",
		},
		{
			name:      "unsatisfiable_endpoint_predicate",
			where:     `WHERE coalesce(end.repo_id, '') = $repo_id`,
			params:    map[string]any{"repo_id": "repo://nobody/owns-this"},
			wantRows:  0,
			explained: "MEASURED, right: a plain endpoint predicate in the same clause position is the control",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			params := map[string]any{
				"start_entity_id": liveClauseChainStartUID,
				"end_entity_id":   liveClauseChainEndUID,
			}
			for key, value := range tc.params {
				params[key] = value
			}
			rows, err := handler.Neo4j.Run(ctx, `
				MATCH path = (start:Function {uid: $start_entity_id})-[:CALLS*1..5]->(end:Function {uid: $end_entity_id})
				`+tc.where+`
				RETURN nodes(path) as chain,
				       length(path) as depth
				LIMIT 5
			`, params)
			if err != nil {
				t.Fatalf("run %s: %v", tc.name, err)
			}
			t.Logf("%s returned %d rows (%s)", tc.name, len(rows), tc.explained)
			if len(rows) != tc.wantRows {
				t.Fatalf("row count = %d, want the pinned measurement %d: %s", len(rows), tc.wantRows, tc.explained)
			}
		})
	}
}
