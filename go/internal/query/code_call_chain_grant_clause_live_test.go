// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build live_nornicdb_call_chain

// Live clause-attachment proof for POST /api/v0/code/call-chain
// (#5167 batch 2b, question 2).
//
// nornicDBCallChainOneHopRows writes its repository predicate --
// "coalesce(target.repo_id, targetRepo.id, ”) IN $traversal_repo_ids" -- after
// two OPTIONAL MATCH clauses. If a predicate in that position does not decide
// row membership, then the existing cross_repo traversal bound is already open
// on origin/main, and pushing a caller's grant into the same clause would be
// grant text that grants nothing.
//
// The ...MustNotLeak... test asserts the behaviour the traversal needs and is
// expected to FAIL against the builder as shipped; that failure is the
// measurement. The ...FixShape... test runs the candidate rewrite on the same
// seeded graph and is expected to pass.
//
//	docker run -d --name nornic-5167-e1 -e NORNICDB_EMBEDDING_ENABLED=false \
//	  -e NORNICDB_NO_AUTH=true -p 17987:7687 \
//	  timothyswt/nornicdb-cpu-bge:v1.2.3@sha256:4dfa887d990bf0b536693830830e34351c036716b0fe6dc957e1a3680e9f3c74
//	cd go && go test ./internal/query -tags live_nornicdb_call_chain \
//	  -run TestLiveNornicDBCallChain -count=1 -v
package query

import (
	"context"
	"testing"
	"time"
)

func newLiveCallChainHandler(ctx context.Context, t *testing.T) (*CodeHandler, func()) {
	t.Helper()
	driver := openLiveClauseDriver(ctx, t)
	seedLiveClauseGraph(ctx, t, driver)
	handler := &CodeHandler{
		Profile:      ProfileLocalAuthoritative,
		GraphBackend: GraphBackendNornicDB,
		Neo4j:        NewNeo4jReader(driver, "nornic"),
	}
	return handler, func() { _ = driver.Close(context.Background()) }
}

// TestLiveNornicDBCallChainOneHopMustNotLeakUngrantedTargets runs the exact
// statement nornicDBCallChainOneHopRows ships, with the traversal restricted to
// the one granted repository.
func TestLiveNornicDBCallChainOneHopMustNotLeakUngrantedTargets(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	handler, closeDriver := newLiveCallChainHandler(ctx, t)
	defer closeDriver()

	rows, err := handler.nornicDBCallChainOneHopRows(
		ctx,
		liveClauseAnchorUID,
		"Function",
		[]string{codeGrantGrantedRepo},
	)
	if err != nil {
		t.Fatalf("run shipped one-hop statement: %v", err)
	}
	names := liveClauseRowNames(rows, "name")
	t.Logf("shipped one-hop statement returned %d rows: %v", len(rows), names)
	for _, row := range rows {
		t.Logf("  row id=%v repo_id=%v", row["id"], row["repo_id"])
	}
	if !liveClauseContainsName(names, liveClauseGrantedCallee) {
		t.Fatalf("the in-repository callee %q is missing; the probe seeded the wrong graph: %v",
			liveClauseGrantedCallee, names)
	}
	for _, leaked := range []string{liveClauseUngrantedCallee, liveClauseOrphanCallee} {
		if liveClauseContainsName(names, leaked) {
			t.Fatalf("the bounded one-hop traversal returned the out-of-bound target %q: %v", leaked, names)
		}
	}
}

// TestLiveNornicDBCallChainOneHopFixShapeExcludesUngrantedTargets measures the
// candidate rewrite: the repository condition moves onto the anchoring MATCH's
// own WHERE and binds the target node's own repo_id, the property the canonical
// node writer already persists on every entity. The OPTIONAL MATCH file and
// repository hops stay optional, so the projection keeps its fallback columns.
//
// A target the graph cannot attribute to a repository is dropped, which is the
// fail-closed half batch 1 landed for complexityListAnchor.
func TestLiveNornicDBCallChainOneHopFixShapeExcludesUngrantedTargets(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	handler, closeDriver := newLiveCallChainHandler(ctx, t)
	defer closeDriver()

	rows, err := handler.Neo4j.Run(ctx, `
		MATCH (source:Function {uid: $source_id})-[:CALLS]->(target)
		WHERE coalesce(target.repo_id, '') IN $traversal_repo_ids
		OPTIONAL MATCH (target)<-[:CONTAINS]-(targetFile:File)
		OPTIONAL MATCH (targetRepo:Repository)-[:REPO_CONTAINS]->(targetFile)
		RETURN coalesce(target.id, target.uid) as id,
		       target.name as name,
		       labels(target) as labels,
		       coalesce(target.repo_id, targetRepo.id) as repo_id,
		       coalesce(target.language, target.lang) as language
	`, map[string]any{
		"source_id":          liveClauseAnchorUID,
		"traversal_repo_ids": []string{codeGrantGrantedRepo},
	})
	if err != nil {
		t.Fatalf("run candidate one-hop statement: %v", err)
	}
	names := liveClauseRowNames(rows, "name")
	t.Logf("candidate one-hop statement returned %d rows: %v", len(rows), names)
	for _, row := range rows {
		t.Logf("  row id=%v repo_id=%v", row["id"], row["repo_id"])
	}
	if !liveClauseContainsName(names, liveClauseGrantedCallee) {
		t.Fatalf("candidate statement dropped the in-repository callee %q: %v", liveClauseGrantedCallee, names)
	}
	for _, leaked := range []string{liveClauseUngrantedCallee, liveClauseOrphanCallee} {
		if liveClauseContainsName(names, leaked) {
			t.Fatalf("candidate statement returned the out-of-bound target %q: %v", leaked, names)
		}
	}
}

// TestLiveNornicDBCallChainOneHopUnscopedIsUnchanged pins the other direction:
// with no traversal bound the statement must still return every callee,
// including the orphan the fail-closed rewrite drops for a bounded caller.
func TestLiveNornicDBCallChainOneHopUnscopedIsUnchanged(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	handler, closeDriver := newLiveCallChainHandler(ctx, t)
	defer closeDriver()

	rows, err := handler.nornicDBCallChainOneHopRows(ctx, liveClauseAnchorUID, "Function", nil)
	if err != nil {
		t.Fatalf("run unbounded one-hop statement: %v", err)
	}
	names := liveClauseRowNames(rows, "name")
	t.Logf("unbounded one-hop statement returned %d rows: %v", len(rows), names)
	for _, want := range []string{liveClauseGrantedCallee, liveClauseUngrantedCallee, liveClauseOrphanCallee} {
		if !liveClauseContainsName(names, want) {
			t.Fatalf("the unbounded traversal lost %q: %v", want, names)
		}
	}
}

// TestLiveNornicDBRelationshipMetadataRowAnchors confirms the shared seed
// lookup routes 2, 4 and 5 all use. Its predicate sits on a plain anchoring
// MATCH pair, and both required MATCH clauses mean an entity the graph cannot
// attribute to a repository never resolves.
func TestLiveNornicDBRelationshipMetadataRowAnchors(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	handler, closeDriver := newLiveCallChainHandler(ctx, t)
	defer closeDriver()

	for _, tc := range []struct {
		name      string
		entity    string
		repoID    string
		wantNames []string
		wantCount int
	}{
		{
			name:      "in_repository_entity_resolves",
			entity:    liveClauseUngrantedCalleeUID,
			repoID:    codeGrantOtherRepo,
			wantNames: []string{liveClauseUngrantedCallee},
			wantCount: 1,
		},
		{
			name:      "wrong_repository_resolves_nothing",
			entity:    liveClauseUngrantedCalleeUID,
			repoID:    codeGrantGrantedRepo,
			wantCount: 0,
		},
		{
			name:      "entity_with_no_repository_path_resolves_nothing",
			entity:    "fn:live-clause-orphan",
			repoID:    "",
			wantCount: 0,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			predicate, params := nornicDBRelationshipMetadataPredicate("", tc.repoID)
			params["entity_id"] = tc.entity
			rows, err := handler.Neo4j.Run(
				ctx,
				nornicDBRelationshipMetadataCypher(predicate, "Function", "uid"),
				params,
			)
			if err != nil {
				t.Fatalf("run metadata statement: %v", err)
			}
			names := liveClauseRowNames(rows, "name")
			t.Logf("%s returned %d rows: %v", tc.name, len(rows), names)
			if len(rows) != tc.wantCount {
				t.Fatalf("row count = %d, want %d: %v", len(rows), tc.wantCount, names)
			}
			for _, want := range tc.wantNames {
				if !liveClauseContainsName(names, want) {
					t.Fatalf("metadata row lost %q: %v", want, names)
				}
			}
		})
	}
}
