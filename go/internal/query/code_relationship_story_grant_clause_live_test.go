// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build live_nornicdb_relationship_story

// Live clause-attachment proof for POST /api/v0/code/relationships/story
// (#5167 batch 2b, question 1).
//
// relationshipStoryRepoPredicates already renders the caller's grant --
// "sourceRepo.id IN $relationship_repo_ids AND targetRepo.id IN
// $relationship_repo_ids" -- and both consumers, relationshipStoryGraphCypher
// and nornicDBRelationshipStoryGraphCypher, attach it to a WHERE that follows
// their OPTIONAL MATCH repository chains. A predicate in that position
// constrains the optional pattern, not the driving row set, so the theory to
// settle is whether that already-merged grant text filters anything at all.
//
// The tests named ...MustNotLeak... assert the behaviour the route needs. They
// are expected to FAIL against the builders as shipped -- that failure IS the
// measurement. The ...FixShape... tests run candidate rewrites against the same
// seeded graph and are expected to pass, so the pair is the red/green the fix
// commit inherits.
//
//	docker run -d --name nornic-5167-e1 -e NORNICDB_EMBEDDING_ENABLED=false \
//	  -e NORNICDB_NO_AUTH=true -p 17987:7687 \
//	  timothyswt/nornicdb-cpu-bge:v1.2.3@sha256:4dfa887d990bf0b536693830830e34351c036716b0fe6dc957e1a3680e9f3c74
//	cd go && go test ./internal/query -tags live_nornicdb_relationship_story \
//	  -run TestLiveNornicDBRelationshipStory -count=1 -v
package query

import (
	"context"
	"testing"
	"time"
)

// storyProbeRequest is the request every story probe shares: no repo_id (the
// gap the ledger row describes -- a scoped caller who names no repository), one
// relationship type, a limit wide enough that no row is dropped by paging.
func storyProbeRequest() relationshipStoryRequest {
	return relationshipStoryRequest{
		EntityID:         liveClauseAnchorUID,
		RelationshipType: "CALLS",
		Limit:            50,
	}
}

func newLiveStoryReader(ctx context.Context, t *testing.T) (*Neo4jReader, func()) {
	t.Helper()
	driver := openLiveClauseDriver(ctx, t)
	seedLiveClauseGraph(ctx, t, driver)
	return NewNeo4jReader(driver, "nornic"), func() { _ = driver.Close(context.Background()) }
}

// TestLiveNornicDBRelationshipStoryMustNotLeakUngrantedRows runs the exact
// statement nornicDBRelationshipStoryGraphCypher ships, in both directions,
// with a scoped filter granted one of the two seeded repositories.
func TestLiveNornicDBRelationshipStoryMustNotLeakUngrantedRows(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	reader, closeDriver := newLiveStoryReader(ctx, t)
	defer closeDriver()

	for _, tc := range []struct {
		direction string
		leaks     []string
		keep      string
		nameKeys  []string
	}{
		{
			direction: "outgoing",
			leaks:     []string{liveClauseUngrantedCallee, liveClauseOrphanCallee},
			keep:      liveClauseGrantedCallee,
			nameKeys:  []string{"target_name"},
		},
		{
			direction: "incoming",
			leaks:     []string{liveClauseUngrantedCaller},
			keep:      liveClauseGrantedCaller,
			nameKeys:  []string{"source_name"},
		},
	} {
		t.Run(tc.direction, func(t *testing.T) {
			cypher, params := nornicDBRelationshipStoryGraphCypher(
				storyProbeRequest(),
				liveClauseAnchorUID,
				"Function",
				"uid",
				tc.direction,
				liveClauseGrantedAccess(),
			)
			rows, err := reader.Run(ctx, cypher, params)
			if err != nil {
				t.Fatalf("run shipped story statement: %v", err)
			}
			names := liveClauseRowNames(rows, tc.nameKeys...)
			t.Logf("shipped %s statement returned %d rows: %v", tc.direction, len(rows), names)
			for _, row := range rows {
				t.Logf("  row source_repo=%v target_repo=%v", row["source_repo_fallback_id"], row["target_repo_fallback_id"])
			}
			if !liveClauseContainsName(names, tc.keep) {
				t.Fatalf("the granted neighbour %q is missing; the probe seeded the wrong graph: %v", tc.keep, names)
			}
			for _, leaked := range tc.leaks {
				if liveClauseContainsName(names, leaked) {
					t.Fatalf("the scoped %s read returned the out-of-grant row %q: %v", tc.direction, leaked, names)
				}
			}
		})
	}
}

// TestLiveNornicDBRelationshipStoryFixShapeExcludesUngrantedRows measures the
// candidate rewrite: the grant moves onto the anchoring MATCH's own WHERE and
// binds the entity node's own repo_id, the property the canonical node writer
// already persists. The OPTIONAL MATCH repository chains stay optional, so the
// projection keeps its repository columns and no traversal changes shape.
func TestLiveNornicDBRelationshipStoryFixShapeExcludesUngrantedRows(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	reader, closeDriver := newLiveStoryReader(ctx, t)
	defer closeDriver()

	params := map[string]any{
		"entity_id":             liveClauseAnchorUID,
		"limit":                 50,
		"offset":                0,
		"relationship_repo_ids": []string{codeGrantGrantedRepo},
	}
	rows, err := reader.Run(ctx, `
		MATCH (anchor:Function {uid: $entity_id})-[rel:CALLS]->(target)
		WHERE anchor.repo_id IN $relationship_repo_ids
		  AND target.repo_id IN $relationship_repo_ids
		OPTIONAL MATCH (anchor)<-[:CONTAINS]-(sourceFile:File)
		OPTIONAL MATCH (sourceRepo:Repository)-[:REPO_CONTAINS]->(sourceFile)
		OPTIONAL MATCH (target)<-[:CONTAINS]-(targetFile:File)
		OPTIONAL MATCH (targetRepo:Repository)-[:REPO_CONTAINS]->(targetFile)
		RETURN target.name as target_name,
		       target.repo_id as target_node_repo_id,
		       targetRepo.id as target_repo_fallback_id,
		       targetFile.relative_path as target_file_path
		ORDER BY target.name
		SKIP $offset
		LIMIT $limit
	`, params)
	if err != nil {
		t.Fatalf("run candidate story statement: %v", err)
	}
	names := liveClauseRowNames(rows, "target_name")
	t.Logf("candidate anchor-WHERE statement returned %d rows: %v", len(rows), names)
	for _, row := range rows {
		t.Logf("  row target_repo_fallback_id=%v target_file_path=%v", row["target_repo_fallback_id"], row["target_file_path"])
	}
	if !liveClauseContainsName(names, liveClauseGrantedCallee) {
		t.Fatalf("candidate statement dropped the granted neighbour %q: %v", liveClauseGrantedCallee, names)
	}
	for _, leaked := range []string{liveClauseUngrantedCallee, liveClauseOrphanCallee} {
		if liveClauseContainsName(names, leaked) {
			t.Fatalf("candidate statement returned the out-of-grant row %q: %v", leaked, names)
		}
	}
}

// TestLiveNornicDBRelationshipStoryRequiredRepositoryMatchFixShape measures the
// other candidate: promote both OPTIONAL MATCH repository chains to required
// MATCHes, the shape batch 1 landed for complexityListAnchor. It is stricter
// (an entity the graph cannot attribute to a repository is dropped) and it
// changes the plan's operator set, which is why the two candidates are measured
// side by side rather than one being assumed.
func TestLiveNornicDBRelationshipStoryRequiredRepositoryMatchFixShape(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	reader, closeDriver := newLiveStoryReader(ctx, t)
	defer closeDriver()

	params := map[string]any{
		"entity_id":             liveClauseAnchorUID,
		"limit":                 50,
		"offset":                0,
		"relationship_repo_ids": []string{codeGrantGrantedRepo},
	}
	rows, err := reader.Run(ctx, `
		MATCH (anchor:Function {uid: $entity_id})-[rel:CALLS]->(target)
		MATCH (anchor)<-[:CONTAINS]-(sourceFile:File)
		MATCH (sourceRepo:Repository)-[:REPO_CONTAINS]->(sourceFile)
		MATCH (target)<-[:CONTAINS]-(targetFile:File)
		MATCH (targetRepo:Repository)-[:REPO_CONTAINS]->(targetFile)
		WHERE sourceRepo.id IN $relationship_repo_ids
		  AND targetRepo.id IN $relationship_repo_ids
		RETURN target.name as target_name,
		       targetRepo.id as target_repo_fallback_id,
		       targetFile.relative_path as target_file_path
		ORDER BY target.name
		SKIP $offset
		LIMIT $limit
	`, params)
	if err != nil {
		t.Fatalf("run required-MATCH story statement: %v", err)
	}
	names := liveClauseRowNames(rows, "target_name")
	t.Logf("required-MATCH statement returned %d rows: %v", len(rows), names)
	for _, row := range rows {
		t.Logf("  row target_repo_fallback_id=%v target_file_path=%v", row["target_repo_fallback_id"], row["target_file_path"])
	}
	if !liveClauseContainsName(names, liveClauseGrantedCallee) {
		t.Fatalf("required-MATCH statement dropped the granted neighbour %q: %v", liveClauseGrantedCallee, names)
	}
	for _, leaked := range []string{liveClauseUngrantedCallee, liveClauseOrphanCallee} {
		if liveClauseContainsName(names, leaked) {
			t.Fatalf("required-MATCH statement returned the out-of-grant row %q: %v", leaked, names)
		}
	}
}

// TestLiveNornicDBRelationshipStoryCompatBuilderMustNotLeakUngrantedRows runs
// relationshipStoryGraphCypher, the Neo4j-compat sibling. Its anchor predicate
// sits in the SAME OPTIONAL MATCH-attached WHERE as the grant, so the probe
// also records whether the anchor itself still binds.
func TestLiveNornicDBRelationshipStoryCompatBuilderMustNotLeakUngrantedRows(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	reader, closeDriver := newLiveStoryReader(ctx, t)
	defer closeDriver()

	cypher, params := relationshipStoryGraphCypher(
		storyProbeRequest(),
		&EntityContent{EntityID: liveClauseAnchorUID},
		"outgoing",
		graphEntityIDPredicate,
		liveClauseGrantedAccess(),
	)
	rows, err := reader.Run(ctx, cypher, params)
	if err != nil {
		t.Fatalf("run shipped compat story statement: %v", err)
	}
	names := liveClauseRowNames(rows, "target_name")
	sources := liveClauseRowNames(rows, "source_name")
	t.Logf("compat outgoing statement returned %d rows; sources=%v targets=%v", len(rows), sources, names)
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

// TestLiveNornicDBRelationshipStoryClassMethodsHaveNoRepositoryBinding pins the
// second half of question 1: the class-methods and inheritance-depth builders
// carry no repository binding at all, in either backend's builder. The probe
// anchors on an out-of-grant class and records what comes back.
func TestLiveNornicDBRelationshipStoryClassMethodsHaveNoRepositoryBinding(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	reader, closeDriver := newLiveStoryReader(ctx, t)
	defer closeDriver()

	cypher, params := nornicDBRelationshipStoryClassMethodsCypher(
		storyProbeRequest(),
		liveClauseUngrantedOwnerUID,
		"uid",
	)
	rows, err := reader.Run(ctx, cypher, params)
	if err != nil {
		t.Fatalf("run shipped class-methods statement: %v", err)
	}
	names := liveClauseRowNames(rows, "method_name")
	t.Logf("class-methods statement anchored on an out-of-grant class returned %d rows: %v", len(rows), names)
	if liveClauseContainsName(names, liveClauseUngrantedMethod) {
		t.Fatalf("the class-methods read returned the out-of-grant method %q with no grant in the statement: %v",
			liveClauseUngrantedMethod, names)
	}
}

// TestLiveNornicDBRelationshipStoryInheritanceDepthHasNoRepositoryBinding
// anchors the inheritance walk on a granted class whose parent lives in the
// out-of-grant repository.
func TestLiveNornicDBRelationshipStoryInheritanceDepthHasNoRepositoryBinding(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	reader, closeDriver := newLiveStoryReader(ctx, t)
	defer closeDriver()

	cypher, params := nornicDBRelationshipStoryInheritanceDepthCypher(
		storyProbeRequest(),
		liveClauseAnchorClassUID,
		"outgoing",
		"uid",
	)
	rows, err := reader.Run(ctx, cypher, params)
	if err != nil {
		t.Fatalf("run shipped inheritance-depth statement: %v", err)
	}
	names := liveClauseRowNames(rows, "target_name")
	t.Logf("inheritance-depth statement returned %d rows: %v", len(rows), names)
	if liveClauseContainsName(names, liveClauseUngrantedParent) {
		t.Fatalf("the inheritance walk crossed into the out-of-grant repository and returned %q: %v",
			liveClauseUngrantedParent, names)
	}
}
