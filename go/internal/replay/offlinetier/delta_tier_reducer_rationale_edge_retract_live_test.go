// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Reducer-materialized rationale EXPLAINS edge retract coverage (C-14 #4367
// retract axis).
//
// This is the regression test for the rationale sibling of #5116: the delta
// (by-file) EXPLAINS retract anchored its target on a node-label disjunction
// (MATCH (rationale:Rationale)-[rel:EXPLAINS]->(target:Function|Class|...|File)
// WHERE target.path IN ...). On the pinned NornicDB v1.1.11 that shape deletes
// NOTHING (probed: two EXPLAINS edges written by the production template
// survive the disjunction retract with count unchanged, while the same
// per-label statements delete both), so a changed file's stale EXPLAINS edges
// survived every delta reprojection. The write template's target disjunction
// with an inline {uid: ...} anchor inside UNWIND is NOT affected (probed:
// creates every edge), and the whole-repo retract anchors on the single
// Rationale label (probed: works); only the by-file retract carried the broken
// shape.
//
// The test drives the REAL production write and retract paths
// (cypher.EdgeWriter.WriteEdges / RetractEdges for
// reducer.DomainRationaleEdges). It writes EXPLAINS edges onto a Function and
// a File target in one changed file, plus an EXPLAINS edge in an unchanged
// file of the same repository, delta-retracts the changed file, and asserts
// the changed file's edges are gone (0), the unchanged file's edge survives
// (1, proving a file-scoped retract not a repo wipe), and every node survives.
// It then repo-retracts and asserts the remaining edge is gone, covering the
// whole-repo path the retractable_edge:EXPLAINS claim also rides on.
//
// Skills active: golang-engineering, eshu-golden-corpus-rigor,
// cypher-query-rigor, concurrency-deadlock-rigor.

package offlinetier_test

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

const (
	ratRepoID         = "replay-rationale-edge:repo"
	ratChangedPath    = "rationale-edge/changed/mod.py"
	ratUnchangedPath  = "rationale-edge/unchanged/mod.py"
	ratEvidenceSource = "reducer/rationale"

	ratFnTarget    = "rationale-edge:fn:changed"
	ratFileTarget  = "rationale-edge:file:changed"
	ratClsSurvivor = "rationale-edge:cls:unchanged"
	ratUIDFn       = "rationale-edge:rat:fn"
	ratUIDFile     = "rationale-edge:rat:file"
	ratUIDCls      = "rationale-edge:rat:cls"
)

// TestReducerRationaleEdgeRetractGraphTruth proves the rationale EXPLAINS
// delta retract deletes only the changed file's edges on a real NornicDB. It
// is the failing-then-green regression for the rationale sibling of #5116.
func TestReducerRationaleEdgeRetractGraphTruth(t *testing.T) {
	if !liveTierEnabled() {
		t.Skipf("set %s=1 (and ESHU_GRAPH_BACKEND/NEO4J_URI/ESHU_NEO4J_DATABASE) to run the rationale-edge retract tier against a real NornicDB", liveTierEnv)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	exec, _ := openDeltaLiveBackend(ctx, t)
	cleanupRationaleEdgeScope(ctx, t, exec)
	t.Cleanup(func() {
		cleanCtx, cleanCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanCancel()
		cleanupRationaleEdgeScope(cleanCtx, t, exec)
	})

	seedRationaleEdgeTargets(ctx, t, exec)

	writer := cypher.NewEdgeWriter(exec, 0)

	// Production write path: the rationale template MERGEs the Rationale node
	// inline and MATCHes the target by uid. Payload shapes mirror what the
	// reducer's rationale edge intents emit (rationale_edge_intents.go).
	writeRows := []reducer.SharedProjectionIntentRow{
		{IntentID: "rat-fn", RepositoryID: ratRepoID, Payload: map[string]any{
			"rationale_uid": ratUIDFn, "target_entity_id": ratFnTarget,
			"repo_id": ratRepoID, "comment_kind": "WHY", "excerpt_hash": "h-fn",
		}},
		{IntentID: "rat-file", RepositoryID: ratRepoID, Payload: map[string]any{
			"rationale_uid": ratUIDFile, "target_entity_id": ratFileTarget,
			"repo_id": ratRepoID, "comment_kind": "NOTE", "excerpt_hash": "h-file",
		}},
		{IntentID: "rat-cls", RepositoryID: ratRepoID, Payload: map[string]any{
			"rationale_uid": ratUIDCls, "target_entity_id": ratClsSurvivor,
			"repo_id": ratRepoID, "comment_kind": "HACK", "excerpt_hash": "h-cls",
		}},
	}
	if err := writer.WriteEdges(ctx, reducer.DomainRationaleEdges, writeRows, ratEvidenceSource); err != nil {
		t.Fatalf("WriteEdges: %v", err)
	}

	fnQ := "MATCH (:Rationale {uid: $r})-[e:EXPLAINS]->(:Function {uid: $t}) RETURN count(e)"
	fileQ := "MATCH (:Rationale {uid: $r})-[e:EXPLAINS]->(:File {uid: $t}) RETURN count(e)"
	clsQ := "MATCH (:Rationale {uid: $r})-[e:EXPLAINS]->(:Class {uid: $t}) RETURN count(e)"
	nodeQ := "MATCH (n {uid: $u}) RETURN count(n)"

	inFn := map[string]any{"r": ratUIDFn, "t": ratFnTarget}
	inFile := map[string]any{"r": ratUIDFile, "t": ratFileTarget}
	survivor := map[string]any{"r": ratUIDCls, "t": ratClsSurvivor}

	assertEdgeCount(ctx, t, exec, fnQ, inFn, 1, "write: changed-file Function EXPLAINS present")
	assertEdgeCount(ctx, t, exec, fileQ, inFile, 1, "write: changed-file File EXPLAINS present")
	assertEdgeCount(ctx, t, exec, clsQ, survivor, 1, "write: unchanged-file Class EXPLAINS present")

	// Production delta retract path: delta rows route to the by-file EXPLAINS
	// retract, which must delete only the changed file's edges.
	deltaRows := []reducer.SharedProjectionIntentRow{
		{IntentID: "retract-delta", RepositoryID: ratRepoID, Payload: map[string]any{
			"repo_id":          ratRepoID,
			"delta_projection": true,
			"delta_file_paths": []string{ratChangedPath},
		}},
	}
	if err := writer.RetractEdges(ctx, reducer.DomainRationaleEdges, deltaRows, ratEvidenceSource); err != nil {
		t.Fatalf("RetractEdges(delta): %v", err)
	}

	// Rationale sibling of #5116: the changed file's edges of every target
	// label must be gone.
	assertEdgeCount(ctx, t, exec, fnQ, inFn, 0, "delta retract: changed-file Function EXPLAINS gone")
	assertEdgeCount(ctx, t, exec, fileQ, inFile, 0, "delta retract: changed-file File EXPLAINS gone")
	// File-scoped retract, not a repo wipe: the unchanged file's edge survives.
	assertEdgeCount(ctx, t, exec, clsQ, survivor, 1, "delta retract: unchanged-file Class EXPLAINS survives")
	// Edge retract must never delete nodes.
	for _, uid := range []string{
		ratFnTarget, ratFileTarget, ratClsSurvivor,
		ratUIDFn, ratUIDFile, ratUIDCls,
	} {
		assertEdgeCount(ctx, t, exec, nodeQ, map[string]any{"u": uid}, 1, "node survives: "+uid)
	}

	// Whole-repo retract (single-label Rationale anchor, probed working):
	// clears the remaining edge, completing the retractable_edge:EXPLAINS claim.
	repoRows := []reducer.SharedProjectionIntentRow{
		{IntentID: "retract-repo", RepositoryID: ratRepoID},
	}
	if err := writer.RetractEdges(ctx, reducer.DomainRationaleEdges, repoRows, ratEvidenceSource); err != nil {
		t.Fatalf("RetractEdges(repo): %v", err)
	}
	assertEdgeCount(ctx, t, exec, clsQ, survivor, 0, "repo retract: unchanged-file Class EXPLAINS gone")
}

// seedRationaleEdgeTargets creates the code-entity target nodes the write path
// MATCHes by uid. The changed-file targets carry ratChangedPath so the by-file
// retract binds them; the survivor carries ratUnchangedPath in the same repo so
// it must outlive the delta retract.
func seedRationaleEdgeTargets(ctx context.Context, t *testing.T, exec liveExecutor) {
	t.Helper()
	stmt := cypher.Statement{
		Cypher: `CREATE (:Function {uid: $fn, repo_id: $repo, path: $changed}),
       (:File {uid: $file, repo_id: $repo, path: $changed}),
       (:Class {uid: $cls, repo_id: $repo, path: $unchanged})`,
		Parameters: map[string]any{
			"fn":        ratFnTarget,
			"file":      ratFileTarget,
			"cls":       ratClsSurvivor,
			"repo":      ratRepoID,
			"changed":   ratChangedPath,
			"unchanged": ratUnchangedPath,
		},
	}
	if err := exec.Execute(ctx, stmt); err != nil {
		t.Fatalf("seed rationale-edge targets: %v", err)
	}
}

// cleanupRationaleEdgeScope removes every node this test creates (targets and
// inline-MERGEd Rationale nodes) so a rerun starts clean.
func cleanupRationaleEdgeScope(ctx context.Context, t *testing.T, exec liveExecutor) {
	t.Helper()
	if err := exec.Execute(ctx, cypher.Statement{
		Cypher:     `MATCH (n) WHERE n.repo_id = $repo DETACH DELETE n`,
		Parameters: map[string]any{"repo": ratRepoID},
	}); err != nil {
		t.Fatalf("cleanup rationale-edge scope: %v", err)
	}
}
