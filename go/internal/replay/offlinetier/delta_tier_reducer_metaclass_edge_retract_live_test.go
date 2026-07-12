// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Reducer-materialized USES_METACLASS edge retract coverage (C-14 #4367
// retract axis).
//
// USES_METACLASS rides the per-source-label code-call retract fixed in #5116:
// under the parser/python-metaclass evidence source the retract fans out to
// one statement per source label (Function/Class/File) with the
// USES_METACLASS relationship type, run sequentially. The write template
// (batchCanonicalMetaclassUpsertCypher) is the UNWIND + label disjunction +
// inline {uid} anchor shape, which matches correctly on NornicDB v1.1.11
// (probed while fixing the rationale retract; see nornicdb-pitfalls.md).
//
// The test drives the REAL production write and retract paths
// (cypher.EdgeWriter.WriteEdges / RetractEdges for reducer.DomainCodeCalls
// with the parser/python-metaclass evidence source). It writes a
// Class->Class USES_METACLASS edge in one repo scope plus an out-of-scope
// edge, retracts the in-scope repo, and asserts the in-scope edge is gone,
// the out-of-scope edge survives, and every endpoint node survives.
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
	metaEdgeInScopeRepoID  = "replay-metaclass-edge:inscope"
	metaEdgeOutScopeRepoID = "replay-metaclass-edge:outscope"
	metaEdgeInScopePath    = "metaclass-edge/in/mod.py"
	metaEdgeOutScopePath   = "metaclass-edge/out/mod.py"
	metaEdgeEvidenceSource = "parser/python-metaclass"

	metaClsUser    = "metaclass-edge:cls:user"
	metaClsMeta    = "metaclass-edge:cls:meta"
	metaClsUserOut = "metaclass-edge:cls:user-out"
	metaClsMetaOut = "metaclass-edge:cls:meta-out"
)

// TestReducerMetaclassEdgeRetractGraphTruth proves the USES_METACLASS retract
// path deletes only the in-scope edges on a real NornicDB, riding the
// per-source-label code-call retract (#5116).
func TestReducerMetaclassEdgeRetractGraphTruth(t *testing.T) {
	if !liveTierEnabled() {
		t.Skipf("set %s=1 (and ESHU_GRAPH_BACKEND/NEO4J_URI/ESHU_NEO4J_DATABASE) to run the metaclass-edge retract tier against a real NornicDB", liveTierEnv)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	exec, _ := openDeltaLiveBackend(ctx, t)
	cleanupMetaclassEdgeScope(ctx, t, exec)
	t.Cleanup(func() {
		cleanCtx, cleanCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanCancel()
		cleanupMetaclassEdgeScope(cleanCtx, t, exec)
	})

	seedMetaclassEdgeNodes(ctx, t, exec)

	writer := cypher.NewEdgeWriter(exec, 0)

	// Production write path: the metaclass template is the UNWIND + inline
	// {uid} anchor shape, which matches on v1.1.11. Payload keys mirror the
	// metaclass branch of buildCodeCallRowMap.
	writeRows := []reducer.SharedProjectionIntentRow{
		{IntentID: "meta-in", RepositoryID: metaEdgeInScopeRepoID, Payload: map[string]any{
			"relationship_type": "USES_METACLASS",
			"source_entity_id":  metaClsUser,
			"target_entity_id":  metaClsMeta,
		}},
		{IntentID: "meta-out", RepositoryID: metaEdgeOutScopeRepoID, Payload: map[string]any{
			"relationship_type": "USES_METACLASS",
			"source_entity_id":  metaClsUserOut,
			"target_entity_id":  metaClsMetaOut,
		}},
	}
	if err := writer.WriteEdges(ctx, reducer.DomainCodeCalls, writeRows, metaEdgeEvidenceSource); err != nil {
		t.Fatalf("WriteEdges: %v", err)
	}

	metaQ := "MATCH (:Class {uid: $s})-[r:USES_METACLASS]->(:Class {uid: $t}) RETURN count(r)"
	nodeQ := "MATCH (n {uid: $u}) RETURN count(n)"
	inMeta := map[string]any{"s": metaClsUser, "t": metaClsMeta}
	outMeta := map[string]any{"s": metaClsUserOut, "t": metaClsMetaOut}

	assertEdgeCount(ctx, t, exec, metaQ, inMeta, 1, "write: in-scope USES_METACLASS present")
	assertEdgeCount(ctx, t, exec, metaQ, outMeta, 1, "write: out-of-scope USES_METACLASS present")

	// Production retract path: repo-scoped rows under the python-metaclass
	// evidence source fan out to one USES_METACLASS statement per source label
	// (Function/Class/File), run sequentially (#5116).
	retractRows := []reducer.SharedProjectionIntentRow{
		{IntentID: "retract", RepositoryID: metaEdgeInScopeRepoID},
	}
	if err := writer.RetractEdges(ctx, reducer.DomainCodeCalls, retractRows, metaEdgeEvidenceSource); err != nil {
		t.Fatalf("RetractEdges: %v", err)
	}

	assertEdgeCount(ctx, t, exec, metaQ, inMeta, 0, "retract: in-scope USES_METACLASS gone")
	// Scoped retract, not a wipe: the out-of-scope repo's edge survives.
	assertEdgeCount(ctx, t, exec, metaQ, outMeta, 1, "retract: out-of-scope USES_METACLASS survives")
	// Edge retract must never delete endpoint nodes.
	for _, uid := range []string{metaClsUser, metaClsMeta, metaClsUserOut, metaClsMetaOut} {
		assertEdgeCount(ctx, t, exec, nodeQ, map[string]any{"u": uid}, 1, "node survives: "+uid)
	}
}

// seedMetaclassEdgeNodes creates the Class endpoint nodes the write path
// MATCHes by uid. The in-scope pair carries the in-scope repo_id so the
// repo-scoped retract binds it; the out-of-scope pair must survive.
func seedMetaclassEdgeNodes(ctx context.Context, t *testing.T, exec liveExecutor) {
	t.Helper()
	stmt := cypher.Statement{
		Cypher: `CREATE (:Class {uid: $user, repo_id: $in, path: $inPath}),
       (:Class {uid: $meta, repo_id: $in, path: $inPath}),
       (:Class {uid: $userOut, repo_id: $out, path: $outPath}),
       (:Class {uid: $metaOut, repo_id: $out, path: $outPath})`,
		Parameters: map[string]any{
			"user":    metaClsUser,
			"meta":    metaClsMeta,
			"userOut": metaClsUserOut,
			"metaOut": metaClsMetaOut,
			"in":      metaEdgeInScopeRepoID,
			"out":     metaEdgeOutScopeRepoID,
			"inPath":  metaEdgeInScopePath,
			"outPath": metaEdgeOutScopePath,
		},
	}
	if err := exec.Execute(ctx, stmt); err != nil {
		t.Fatalf("seed metaclass-edge nodes: %v", err)
	}
}

// cleanupMetaclassEdgeScope removes every node this test creates, in both repo
// scopes, so a rerun starts clean.
func cleanupMetaclassEdgeScope(ctx context.Context, t *testing.T, exec liveExecutor) {
	t.Helper()
	if err := exec.Execute(ctx, cypher.Statement{
		Cypher:     `MATCH (n) WHERE n.repo_id IN [$in, $out] DETACH DELETE n`,
		Parameters: map[string]any{"in": metaEdgeInScopeRepoID, "out": metaEdgeOutScopeRepoID},
	}); err != nil {
		t.Fatalf("cleanup metaclass-edge scope: %v", err)
	}
}
