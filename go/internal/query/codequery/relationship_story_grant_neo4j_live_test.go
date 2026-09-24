// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build live_neo4j_relationship_story

// Live Neo4j grant-leak proof for POST /api/v0/code/relationships/story.
//
// relationshipStoryGraphCypher is the Neo4j-compat story builder. Since #7057
// it anchors the entity through codemodel.Neo4jEntityIDAnchor, a
// CALL () { ... UNION ... } subquery NornicDB cannot run, and the handler
// never sends it there (story_reads.go branches on the backend first). This
// test was therefore moved out of the NornicDB probe file
// (relationship_story_grant_clause_live_test.go), where it ran the Neo4j
// builder against NornicDB and failed on the CALL subquery, and now runs it
// against the backend that actually serves it.
//
// It reuses the two-tenant fixture from
// grant_clause_attachment_live_seed_test.go, seeded into the "neo4j" database.
// The builder anchors on the seeded function's uid with the scoped caller
// granted exactly one repository, and must return the granted callee, none of
// the out-of-grant rows, and only the anchor as a source.
//
//	docker run -d --name eshu-7057-neo4j -p 127.0.0.1:59474:7687 \
//	  -e NEO4J_AUTH=none neo4j:2026-community@sha256:eabfbb042bdaca2fd5e1950db1329b22c794eee80f0eacc4e7a729d44b2e863f
//	cd go && ESHU_NEO4J_URI=bolt://127.0.0.1:59474 \
//	  go test ./internal/query/codequery -tags live_neo4j_relationship_story \
//	  -run TestLiveNeo4jRelationshipStory -count=1 -v
package codequery

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/codemodel"
)

// TestLiveNeo4jRelationshipStoryCompatBuilderMustNotLeakUngrantedRows runs
// relationshipStoryGraphCypher on a live Neo4j. The granted callee must be
// present, so an empty result cannot pass by accident; the out-of-grant and
// orphan callees must be absent; and no caller may appear as a source, which
// proves the anchor predicate bound to the requested entity.
func TestLiveNeo4jRelationshipStoryCompatBuilderMustNotLeakUngrantedRows(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	driver := openLiveClauseDriver(ctx, t)
	defer func() { _ = driver.Close(context.Background()) }()
	const database = "neo4j"
	seedLiveClauseGraph(ctx, t, driver, database)
	reader := newLiveNornicDBReader(driver, database)

	cypher, params := relationshipStoryGraphCypher(
		codemodel.RelationshipStoryRequest{
			EntityID:         liveClauseAnchorUID,
			RelationshipType: "CALLS",
			Limit:            50,
		},
		&EntityContent{EntityID: liveClauseAnchorUID},
		"outgoing",
		liveClauseGrantedAccess(),
	)
	rows, err := reader.Run(ctx, cypher, params)
	if err != nil {
		t.Fatalf("run shipped compat story statement: %v", err)
	}
	names := liveClauseRowNames(rows, "target_name")
	sources := liveClauseRowNames(rows, "source_name")
	t.Logf("compat outgoing statement returned %d rows; sources=%v targets=%v", len(rows), sources, names)
	if !liveClauseContainsName(names, liveClauseGrantedCallee) {
		t.Fatalf("the scoped compat read dropped the granted neighbour %q: %v", liveClauseGrantedCallee, names)
	}
	for _, leaked := range []string{liveClauseUngrantedCallee, liveClauseOrphanCallee} {
		if liveClauseContainsName(names, leaked) {
			t.Fatalf("the scoped compat read returned the out-of-grant row %q: %v", leaked, names)
		}
	}
	for _, unrelated := range []string{liveClauseUngrantedCaller, liveClauseGrantedCaller} {
		if liveClauseContainsName(sources, unrelated) {
			t.Fatalf("the compat anchor predicate did not bind; %q appeared as a source: %v", unrelated, sources)
		}
	}
}
