// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer/code/semantic"
)

// DSN-gated live proof for the #8359 dedicated CONTAINS edge retract.
//
// Gate: ESHU_CYPHER_BOLT_DSN must be set (e.g. neo4j://127.0.0.1:7687).
// When unset the test skips; the hermetic shape tests in
// semantic_entity_edge_retract_test.go carry the contract in normal CI.
//
// The old retract DETACH DELETEd the shared semantic nodes, so every
// foreign edge below died with them. Under the dedicated retract only this
// writer's CONTAINS edges are deleted, a node with a surviving foreign
// edge is not reaped, and an edgeless stale node is.

func TestLiveSemanticContainmentRetractSparesForeignEdges(t *testing.T) {
	t.Parallel()

	runner := openBoltTestRunner(t)
	defer runner.close(context.Background())
	ctx := context.Background()

	const (
		repoID      = "8359-repo"
		otherRepoID = "8359-other-repo"
		source      = "parser/semantic-entities"
		canonical   = "projector/canonical"

		filePath  = "8359/repo/main.go"
		edgeFnUID = "8359-fn-edged" // owned CONTAINS + foreign CALLS: node survives
		staleUID  = "8359-fn-stale" // owned CONTAINS only: reaped once edgeless
		varUID    = "8359-var"      // legacy unstamped CONTAINS: reaped via node scope
		calleeUID = "8359-fn-callee"
		otherUID  = "8359-fn-other-repo" // stamped CONTAINS, foreign repo: survives
	)

	seed := func(cypher string, params map[string]any) {
		t.Helper()
		if err := boltWriteStatement(ctx, runner, cypher, params); err != nil {
			t.Fatalf("seed %q: %v", cypher, err)
		}
	}
	cleanup := func() {
		_ = boltWriteStatement(ctx, runner,
			`MATCH (n) WHERE n.uid IN $uids OR n.path = $path DETACH DELETE n`,
			map[string]any{"uids": []string{edgeFnUID, staleUID, varUID, calleeUID, otherUID}, "path": filePath})
	}
	cleanup()
	defer cleanup()

	seed(`MERGE (:File {path: $path})`, map[string]any{"path": filePath})
	for _, node := range []struct {
		label, uid, repo string
	}{
		{"Function", edgeFnUID, repoID},
		{"Function", staleUID, repoID},
		{"Variable", varUID, repoID},
		{"Function", calleeUID, repoID},
		{"Function", otherUID, otherRepoID},
	} {
		seed(`MERGE (n:`+node.label+` {uid: $uid}) SET n.repo_id = $repo, n.path = $path, n.evidence_source = $source`,
			map[string]any{"uid": node.uid, "repo": node.repo, "path": filePath, "source": source})
	}
	seed(`MERGE (:Class {uid: '8359-class'})`, nil)
	defer func() {
		_ = boltWriteStatement(ctx, runner, `MATCH (n:Class {uid: '8359-class'}) DETACH DELETE n`, nil)
	}()

	// Owned stamped containment.
	for _, uid := range []string{edgeFnUID, staleUID, otherUID} {
		seed(`MATCH (f:File {path: $path}) MATCH (n {uid: $uid}) MERGE (f)-[rel:CONTAINS]->(n) SET rel.evidence_source = $source`,
			map[string]any{"path": filePath, "uid": uid, "source": source})
	}
	// Legacy unstamped containment into a semantic-owned node.
	seed(`MATCH (f:File {path: $path}) MATCH (n {uid: $uid}) MERGE (f)-[:CONTAINS]->(n)`,
		map[string]any{"path": filePath, "uid": varUID})
	// Foreign edges on the edged function: a foreign type, and a canonical
	// CONTAINS from a label this writer never sources from.
	seed(`MATCH (a {uid: $a}) MATCH (b {uid: $b}) MERGE (a)-[rel:CALLS]->(b) SET rel.evidence_source = $source`,
		map[string]any{"a": edgeFnUID, "b": calleeUID, "source": source})
	seed(`MATCH (c:Class {uid: '8359-class'}) MATCH (n {uid: $uid}) MERGE (c)-[rel:CONTAINS]->(n) SET rel.evidence_source = $canonical`,
		map[string]any{"uid": edgeFnUID, "canonical": canonical})

	writer := NewSemanticEntityWriter(&boltTestExecutor{runner: runner}, 0)
	if _, err := writer.WriteSemanticEntities(ctx, semantic.EntityWrite{RepoIDs: []string{repoID}}); err != nil {
		t.Fatalf("WriteSemanticEntities retract: %v", err)
	}

	count := func(cypher string, params map[string]any) int64 {
		t.Helper()
		got, err := boltCount(ctx, runner, cypher, params)
		if err != nil {
			t.Fatalf("count %q: %v", cypher, err)
		}
		return got
	}
	edgeCount := func(uid, relType string) int64 {
		return count(`MATCH ()-[rel:`+relType+`]->(n {uid: $uid}) RETURN count(rel) AS count`,
			map[string]any{"uid": uid})
	}
	nodeCount := func(uid string) int64 {
		return count(`MATCH (n {uid: $uid}) RETURN count(n) AS count`, map[string]any{"uid": uid})
	}

	if got := edgeCount(edgeFnUID, "CONTAINS"); got != 1 {
		t.Errorf("edged function CONTAINS = %d, want 1 (only the canonical Class edge survives)", got)
	}
	if got := count(`MATCH (a {uid: $a})-[rel:CALLS]->() RETURN count(rel) AS count`, map[string]any{"a": edgeFnUID}); got != 1 {
		t.Errorf("foreign CALLS edges = %d, want 1 (foreign type survives)", got)
	}
	if got := nodeCount(edgeFnUID); got != 1 {
		t.Errorf("edged function node = %d, want 1 (not reaped while foreign edges remain)", got)
	}
	if got := nodeCount(staleUID); got != 0 {
		t.Errorf("stale function node = %d, want 0 (reaped once edgeless)", got)
	}
	if got := nodeCount(varUID); got != 0 {
		t.Errorf("legacy variable node = %d, want 0 (unstamped edge reaped via node scope)", got)
	}
	if got := edgeCount(otherUID, "CONTAINS"); got != 1 {
		t.Errorf("other-repo CONTAINS = %d, want 1 (repo scope fence)", got)
	}
	if got := nodeCount(otherUID); got != 1 {
		t.Errorf("other-repo node = %d, want 1", got)
	}
}

// TestLiveSemanticContainmentCanonicalRetractSparesForeignEdges is the
// canonical-node-mode counterpart — the mode production wires for
// NornicDB, where a label alternation inside a relationship pattern
// matches nothing (measured on the pinned fix-500 image 2026-10-17), so
// the dispatch must use the per-label single-source statements. Canonical
// mode differs from the legacy mode in one expectation: Function nodes
// are canonical-owned, so the semantic retract clears their semantic
// properties but never reaps them (the canonical writer's generation
// retract owns their lifecycle); Variable stays semantic-owned and is
// reaped once edgeless.
func TestLiveSemanticContainmentCanonicalRetractSparesForeignEdges(t *testing.T) {
	t.Parallel()

	runner := openBoltTestRunner(t)
	defer runner.close(context.Background())
	ctx := context.Background()

	const (
		repoID    = "8359c-repo"
		source    = "parser/semantic-entities"
		canonical = "projector/canonical"
		filePath  = "8359c/repo/main.go"
		fnUID     = "8359c-fn"
		varUID    = "8359c-var"
		calleeUID = "8359c-callee"
	)

	seed := func(cypher string, params map[string]any) {
		t.Helper()
		if err := boltWriteStatement(ctx, runner, cypher, params); err != nil {
			t.Fatalf("seed %q: %v", cypher, err)
		}
	}
	cleanup := func() {
		_ = boltWriteStatement(ctx, runner,
			`MATCH (n) WHERE n.uid IN $uids OR n.path = $path DETACH DELETE n`,
			map[string]any{"uids": []string{fnUID, varUID, calleeUID, "8359c-class"}, "path": filePath})
	}
	cleanup()
	defer cleanup()

	seed(`MERGE (:File {path: $path})`, map[string]any{"path": filePath})
	seed(`MERGE (:Class {uid: '8359c-class'})`, nil)
	for _, node := range []struct{ label, uid string }{
		{"Function", fnUID}, {"Variable", varUID}, {"Function", calleeUID},
	} {
		seed(`MERGE (n:`+node.label+` {uid: $uid}) SET n.repo_id = $repo, n.path = $path, n.evidence_source = $source`,
			map[string]any{"uid": node.uid, "repo": repoID, "path": filePath, "source": source})
	}
	for _, uid := range []string{fnUID, varUID} {
		seed(`MATCH (f:File {path: $path}) MATCH (n {uid: $uid}) MERGE (f)-[rel:CONTAINS]->(n) SET rel.evidence_source = $source`,
			map[string]any{"path": filePath, "uid": uid, "source": source})
	}
	seed(`MATCH (a {uid: $a}) MATCH (b {uid: $b}) MERGE (a)-[rel:CALLS]->(b) SET rel.evidence_source = $source`,
		map[string]any{"a": fnUID, "b": calleeUID, "source": source})
	seed(`MATCH (c:Class {uid: '8359c-class'}) MATCH (n {uid: $uid}) MERGE (c)-[rel:CONTAINS]->(n) SET rel.evidence_source = $canonical`,
		map[string]any{"uid": fnUID, "canonical": canonical})

	writer := NewSemanticEntityWriterWithCanonicalNodeRows(&boltTestExecutor{runner: runner}, 0).WithLabelScopedRetract()
	if _, err := writer.WriteSemanticEntities(ctx, semantic.EntityWrite{RepoIDs: []string{repoID}}); err != nil {
		t.Fatalf("WriteSemanticEntities canonical retract: %v", err)
	}

	count := func(cypher string, params map[string]any) int64 {
		t.Helper()
		got, err := boltCount(ctx, runner, cypher, params)
		if err != nil {
			t.Fatalf("count %q: %v", cypher, err)
		}
		return got
	}
	if got := count(`MATCH ()-[rel:CONTAINS]->(n {uid: $uid}) RETURN count(rel) AS count`, map[string]any{"uid": fnUID}); got != 1 {
		t.Errorf("function CONTAINS = %d, want 1 (only canonical Class edge survives)", got)
	}
	if got := count(`MATCH (a {uid: $a})-[rel:CALLS]->() RETURN count(rel) AS count`, map[string]any{"a": fnUID}); got != 1 {
		t.Errorf("foreign CALLS = %d, want 1", got)
	}
	if got := count(`MATCH (n {uid: $uid}) RETURN count(n) AS count`, map[string]any{"uid": fnUID}); got != 1 {
		t.Errorf("function node = %d, want 1 (canonical-owned, never reaped here)", got)
	}
	if got := count(`MATCH (n {uid: $uid}) RETURN count(n) AS count`, map[string]any{"uid": varUID}); got != 0 {
		t.Errorf("variable node = %d, want 0 (semantic-owned, reaped once edgeless)", got)
	}
}
