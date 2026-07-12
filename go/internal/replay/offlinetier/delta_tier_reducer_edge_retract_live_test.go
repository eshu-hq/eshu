// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Reducer-materialized code-call edge retract coverage (C-14 #4367 retract axis).
//
// This is the regression test for #5116: the code-call edge retract Cyphers in
// storage/cypher matched their source node by a node-label DISJUNCTION
// (MATCH (source:Function|Class|Struct|Interface|TypeAlias|File)-[rel:CALLS|...]).
// On the pinned NornicDB a node-label disjunction in a MATCH returns zero rows
// even when nodes of those labels exist (proven: MATCH (s:Function) matches,
// MATCH (s:Function|Class) does not), so the retract deleted NOTHING and stale
// CALLS/REFERENCES/INSTANTIATES edges survived every reprojection. The fix drops
// the node-label disjunction; the rel-type disjunction plus the WHERE scope
// (source.repo_id / source.path IN ... AND rel.evidence_source = ...) already
// bound the retract.
//
// The test drives the REAL production write and retract paths
// (cypher.EdgeWriter.WriteEdges / RetractEdges for reducer.DomainCodeCalls). It
// writes CALLS (Function->Function), REFERENCES (File->TypeAlias), and
// INSTANTIATES (Function->Class) edges in one repo scope plus an out-of-scope
// CALLS edge, retracts the in-scope repo, and asserts the in-scope edges are
// gone (0), the out-of-scope edge survives (1, proving a scoped retract not a
// wipe), and every endpoint node survives (an edge retract must not delete
// nodes). Before the #5116 fix the in-scope assertions fail because the broken
// disjunction retracts nothing.
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
	reducerEdgeInScopeRepoID  = "replay-reducer-edge:inscope"
	reducerEdgeOutScopeRepoID = "replay-reducer-edge:outscope"
	reducerEdgeInScopePath    = "reducer-edge/in/mod.py"
	reducerEdgeOutScopePath   = "reducer-edge/out/mod.py"
	reducerEdgeEvidenceSource = "parser/code-calls"

	reFnCaller  = "reducer-edge:fn:caller"
	reFnCallee  = "reducer-edge:fn:callee"
	reFnInst    = "reducer-edge:fn:instantiator"
	reClsTarget = "reducer-edge:cls:target"
	reFileRef   = "reducer-edge:file:ref"
	reTypeAlias = "reducer-edge:ta:target"
	reFnOutA    = "reducer-edge:fn:out-a"
	reFnOutB    = "reducer-edge:fn:out-b"
)

// TestReducerCodeCallEdgeRetractGraphTruth proves the code-call edge retract
// path (CALLS/REFERENCES/INSTANTIATES) deletes only the in-scope edges on a real
// NornicDB. It is the failing-then-green regression for #5116.
func TestReducerCodeCallEdgeRetractGraphTruth(t *testing.T) {
	if !liveTierEnabled() {
		t.Skipf("set %s=1 (and ESHU_GRAPH_BACKEND/NEO4J_URI/ESHU_NEO4J_DATABASE) to run the reducer-edge retract tier against a real NornicDB", liveTierEnv)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	exec, _ := openDeltaLiveBackend(ctx, t)
	cleanupReducerEdgeScope(ctx, t, exec)
	t.Cleanup(func() {
		cleanCtx, cleanCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanCancel()
		cleanupReducerEdgeScope(cleanCtx, t, exec)
	})

	seedReducerEdgeNodes(ctx, t, exec)

	writer := cypher.NewEdgeWriter(exec, 0)

	// Production write path: typed endpoints route to exact-label MATCH Cyphers
	// (NornicDB matches single labels), so these edges are actually created.
	writeRows := []reducer.SharedProjectionIntentRow{
		{IntentID: "calls-in", RepositoryID: reducerEdgeInScopeRepoID, Payload: map[string]any{
			"caller_entity_id": reFnCaller, "caller_entity_type": "Function",
			"callee_entity_id": reFnCallee, "callee_entity_type": "Function",
		}},
		{IntentID: "ref-in", RepositoryID: reducerEdgeInScopeRepoID, Payload: map[string]any{
			"relationship_type": "REFERENCES",
			"caller_entity_id":  reFileRef, "caller_entity_type": "File",
			"callee_entity_id": reTypeAlias, "callee_entity_type": "TypeAlias",
		}},
		{IntentID: "inst-in", RepositoryID: reducerEdgeInScopeRepoID, Payload: map[string]any{
			"relationship_type": "INSTANTIATES",
			"caller_entity_id":  reFnInst, "caller_entity_type": "Function",
			"callee_entity_id": reClsTarget, "callee_entity_type": "Class",
		}},
		{IntentID: "calls-out", RepositoryID: reducerEdgeOutScopeRepoID, Payload: map[string]any{
			"caller_entity_id": reFnOutA, "caller_entity_type": "Function",
			"callee_entity_id": reFnOutB, "callee_entity_type": "Function",
		}},
	}
	if err := writer.WriteEdges(ctx, reducer.DomainCodeCalls, writeRows, reducerEdgeEvidenceSource); err != nil {
		t.Fatalf("WriteEdges: %v", err)
	}

	callsQ := "MATCH (:Function {uid: $s})-[r:CALLS]->(:Function {uid: $t}) RETURN count(r)"
	refQ := "MATCH (:File {uid: $s})-[r:REFERENCES]->(:TypeAlias {uid: $t}) RETURN count(r)"
	instQ := "MATCH (:Function {uid: $s})-[r:INSTANTIATES]->(:Class {uid: $t}) RETURN count(r)"
	nodeQ := "MATCH (n {uid: $u}) RETURN count(n)"
	inCalls := map[string]any{"s": reFnCaller, "t": reFnCallee}
	inRef := map[string]any{"s": reFileRef, "t": reTypeAlias}
	inInst := map[string]any{"s": reFnInst, "t": reClsTarget}
	outCalls := map[string]any{"s": reFnOutA, "t": reFnOutB}

	assertEdgeCount(ctx, t, exec, callsQ, inCalls, 1, "write: in-scope CALLS present")
	assertEdgeCount(ctx, t, exec, refQ, inRef, 1, "write: in-scope REFERENCES present")
	assertEdgeCount(ctx, t, exec, instQ, inInst, 1, "write: in-scope INSTANTIATES present")
	assertEdgeCount(ctx, t, exec, callsQ, outCalls, 1, "write: out-of-scope CALLS present")

	// Production retract path: repo-scoped rows route to
	// BuildRetractCodeCallEdgeStatements -> one per-source-label DELETE, run
	// sequentially (the #5116 fix).
	retractRows := []reducer.SharedProjectionIntentRow{
		{IntentID: "retract", RepositoryID: reducerEdgeInScopeRepoID},
	}
	if err := writer.RetractEdges(ctx, reducer.DomainCodeCalls, retractRows, reducerEdgeEvidenceSource); err != nil {
		t.Fatalf("RetractEdges: %v", err)
	}

	// #5116 fix: in-scope edges of every code-call rel-type must be gone.
	assertEdgeCount(ctx, t, exec, callsQ, inCalls, 0, "retract: in-scope CALLS gone")
	assertEdgeCount(ctx, t, exec, refQ, inRef, 0, "retract: in-scope REFERENCES gone")
	assertEdgeCount(ctx, t, exec, instQ, inInst, 0, "retract: in-scope INSTANTIATES gone")
	// Scoped retract, not a wipe: the out-of-scope repo's edge survives.
	assertEdgeCount(ctx, t, exec, callsQ, outCalls, 1, "retract: out-of-scope CALLS survives")
	// Edge retract must never delete endpoint nodes.
	for _, uid := range []string{
		reFnCaller, reFnCallee, reFnInst, reClsTarget,
		reFileRef, reTypeAlias, reFnOutA, reFnOutB,
	} {
		assertEdgeCount(ctx, t, exec, nodeQ, map[string]any{"u": uid}, 1, "node survives: "+uid)
	}
}

// seedReducerEdgeNodes creates the code-entity endpoint nodes the write path
// MATCHes by exact label + uid. Every in-scope node carries the in-scope
// repo_id so the repo-scoped retract binds it; the out-of-scope pair carries a
// different repo_id so it must survive the retract.
func seedReducerEdgeNodes(ctx context.Context, t *testing.T, exec liveExecutor) {
	t.Helper()
	stmt := cypher.Statement{
		Cypher: `CREATE (:Function {uid: $fnCaller, repo_id: $in, path: $inPath}),
       (:Function {uid: $fnCallee, repo_id: $in, path: $inPath}),
       (:Function {uid: $fnInst, repo_id: $in, path: $inPath}),
       (:Class {uid: $clsTarget, repo_id: $in, path: $inPath}),
       (:File {uid: $fileRef, repo_id: $in, path: $inPath}),
       (:TypeAlias {uid: $taTarget, repo_id: $in, path: $inPath}),
       (:Function {uid: $fnOutA, repo_id: $out, path: $outPath}),
       (:Function {uid: $fnOutB, repo_id: $out, path: $outPath})`,
		Parameters: map[string]any{
			"fnCaller":  reFnCaller,
			"fnCallee":  reFnCallee,
			"fnInst":    reFnInst,
			"clsTarget": reClsTarget,
			"fileRef":   reFileRef,
			"taTarget":  reTypeAlias,
			"fnOutA":    reFnOutA,
			"fnOutB":    reFnOutB,
			"in":        reducerEdgeInScopeRepoID,
			"out":       reducerEdgeOutScopeRepoID,
			"inPath":    reducerEdgeInScopePath,
			"outPath":   reducerEdgeOutScopePath,
		},
	}
	if err := exec.Execute(ctx, stmt); err != nil {
		t.Fatalf("seed reducer-edge nodes: %v", err)
	}
}

// cleanupReducerEdgeScope removes every node this test creates, in both repo
// scopes, so a rerun starts clean and leaves no residue for sibling tests.
func cleanupReducerEdgeScope(ctx context.Context, t *testing.T, exec liveExecutor) {
	t.Helper()
	if err := exec.Execute(ctx, cypher.Statement{
		Cypher:     `MATCH (n) WHERE n.repo_id IN [$in, $out] DETACH DELETE n`,
		Parameters: map[string]any{"in": reducerEdgeInScopeRepoID, "out": reducerEdgeOutScopeRepoID},
	}); err != nil {
		t.Fatalf("cleanup reducer-edge scope: %v", err)
	}
}
