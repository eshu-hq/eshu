// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Reducer-materialized content/runtime edge retract coverage (C-14 #4367
// retract axis): EXECUTES_SHELL, DOCUMENTS, CORRELATES_DEPLOYABLE_UNIT, and
// TAINT_FLOWS_TO.
//
// EXECUTES_SHELL and the documentation delta retract dispatched multiple
// statements through ExecuteGroup — one managed Bolt transaction — which
// under-applies on the pinned NornicDB v1.1.11 (measured on the SQL retract in
// #5128 and on the repo-dependency retract in #5146: the first grouped
// statement never applies). The shell-exec whole-repo retract groups its edge
// DELETE with the orphan ShellCommand cleanup, and the documentation delta
// retract groups its section-uid and document-id statements. Both now run
// sequentially. CORRELATES_DEPLOYABLE_UNIT (single-statement Repository-anchored
// retract) and TAINT_FLOWS_TO (Function-anchored, rel-property scoped,
// dedicated CodeInterprocEvidenceWriter) were already safe shapes and are
// live-claimed here.
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
	ctInRepo  = "replay-content-edge:in"
	ctOutRepo = "replay-content-edge:out"
	ctMarker  = "replay-content-edge"

	ctFnShell     = "content-edge:fn:shell"
	ctFnShellOut  = "content-edge:fn:shell-out"
	ctCmd         = "content-edge:cmd:in"
	ctCmdOut      = "content-edge:cmd:out"
	ctShellPath   = "content-edge/in/tool.py"
	ctShellPathB  = "content-edge/out/tool.py"
	ctFnDoc       = "content-edge:fn:doc"
	ctFnDocOut    = "content-edge:fn:doc-out"
	ctSection     = "content-edge:section:in"
	ctSectionOut  = "content-edge:section:out"
	ctScopeIn     = "content-edge:scope:in"
	ctScopeOut    = "content-edge:scope:out"
	ctDocumentIn  = "content-edge:doc:in"
	ctDocumentOut = "content-edge:doc:out"
	ctDepTarget   = "content-edge:repo:deploy"
	ctDepOutSrc   = "content-edge:repo:dep-out-src"
	ctDepOutTgt   = "content-edge:repo:dep-out-tgt"
	ctFnTaintSrc  = "content-edge:fn:taint-src"
	ctFnTaintDst  = "content-edge:fn:taint-dst"
	ctFnTaintOutS = "content-edge:fn:taint-out-src"
	ctFnTaintOutD = "content-edge:fn:taint-out-dst"

	ctShellSource = "reducer/shell-exec"
	ctDocSource   = "reducer/documentation"
	ctDepSource   = "reducer/deployable-unit-correlation"
	ctTaintSource = "reducer/code-interproc"
)

// TestReducerContentEdgeRetractGraphTruth proves the EXECUTES_SHELL,
// DOCUMENTS (delta scope), and CORRELATES_DEPLOYABLE_UNIT retract paths delete
// only the in-scope edges on a real NornicDB. It is the failing-then-green
// regression for the grouped-transaction defect in the shell-exec and
// documentation-delta retract executors.
func TestReducerContentEdgeRetractGraphTruth(t *testing.T) {
	if !liveTierEnabled() {
		t.Skipf("set %s=1 (and ESHU_GRAPH_BACKEND/NEO4J_URI/ESHU_NEO4J_DATABASE) to run the content-edge retract tier against a real NornicDB", liveTierEnv)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	exec, _ := openDeltaLiveBackend(ctx, t)
	cleanupContentEdgeScope(ctx, t, exec)
	t.Cleanup(func() {
		cleanCtx, cleanCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanCancel()
		cleanupContentEdgeScope(cleanCtx, t, exec)
	})

	seedContentEdgeNodes(ctx, t, exec)

	writer := cypher.NewEdgeWriter(exec, 0)
	// The shell-exec orphan ShellCommand cleanup now runs a Go-side anti-join
	// (S1 candidate keys, S2 connected keys) instead of a relationship-
	// existence predicate (#5310); it needs a Reader for those reads.
	writer.Reader = exec
	write := func(domain, source string, rows []reducer.SharedProjectionIntentRow) {
		t.Helper()
		if err := writer.WriteEdges(ctx, domain, rows, source); err != nil {
			t.Fatalf("WriteEdges(%s): %v", domain, err)
		}
	}

	write(reducer.DomainShellExec, ctShellSource, []reducer.SharedProjectionIntentRow{
		{IntentID: "shell-in", RepositoryID: ctInRepo, Payload: map[string]any{
			"source_entity_id": ctFnShell, "target_entity_id": ctCmd,
			"repo_id": ctInRepo, "source_path": ctShellPath,
		}},
		{IntentID: "shell-out", RepositoryID: ctOutRepo, Payload: map[string]any{
			"source_entity_id": ctFnShellOut, "target_entity_id": ctCmdOut,
			"repo_id": ctOutRepo, "source_path": ctShellPathB,
		}},
	})
	write(reducer.DomainDocumentationEdges, ctDocSource, []reducer.SharedProjectionIntentRow{
		{IntentID: "doc-in", RepositoryID: ctInRepo, ScopeID: ctScopeIn, Payload: map[string]any{
			"section_uid": ctSection, "target_entity_id": ctFnDoc,
			"scope_id": ctScopeIn, "document_id": ctDocumentIn, "mention_kind": "exact",
		}},
		{IntentID: "doc-out", RepositoryID: ctOutRepo, ScopeID: ctScopeOut, Payload: map[string]any{
			"section_uid": ctSectionOut, "target_entity_id": ctFnDocOut,
			"scope_id": ctScopeOut, "document_id": ctDocumentOut, "mention_kind": "exact",
		}},
	})
	write(reducer.DomainDeployableUnitEdges, ctDepSource, []reducer.SharedProjectionIntentRow{
		{IntentID: "dep-in", RepositoryID: ctInRepo, Payload: map[string]any{
			"repo_id": ctInRepo, "deployment_repo_id": ctDepTarget,
			"deployable_unit_key": "unit-a", "correlation_key": "corr-a",
		}},
		{IntentID: "dep-out", RepositoryID: ctOutRepo, Payload: map[string]any{
			"repo_id": ctDepOutSrc, "deployment_repo_id": ctDepOutTgt,
			"deployable_unit_key": "unit-b", "correlation_key": "corr-b",
		}},
	})

	shellQ := "MATCH (:Function {uid: $f})-[r:EXECUTES_SHELL]->(:ShellCommand {uid: $c}) RETURN count(r)"
	docQ := "MATCH (:DocumentationSection {uid: $s})-[r:DOCUMENTS]->(:Function {uid: $f}) RETURN count(r)"
	depQ := "MATCH (:Repository {id: $s})-[r:CORRELATES_DEPLOYABLE_UNIT]->(:Repository {id: $t}) RETURN count(r)"
	cmdNodeQ := "MATCH (n:ShellCommand {uid: $u}) RETURN count(n)"

	inShell := map[string]any{"f": ctFnShell, "c": ctCmd}
	outShell := map[string]any{"f": ctFnShellOut, "c": ctCmdOut}
	inDoc := map[string]any{"s": ctSection, "f": ctFnDoc}
	outDoc := map[string]any{"s": ctSectionOut, "f": ctFnDocOut}
	inDep := map[string]any{"s": ctInRepo, "t": ctDepTarget}
	outDep := map[string]any{"s": ctDepOutSrc, "t": ctDepOutTgt}

	assertEdgeCount(ctx, t, exec, shellQ, inShell, 1, "write: in-scope EXECUTES_SHELL present")
	assertEdgeCount(ctx, t, exec, shellQ, outShell, 1, "write: out-of-scope EXECUTES_SHELL present")
	assertEdgeCount(ctx, t, exec, docQ, inDoc, 1, "write: in-scope DOCUMENTS present")
	assertEdgeCount(ctx, t, exec, docQ, outDoc, 1, "write: out-of-scope DOCUMENTS present")
	assertEdgeCount(ctx, t, exec, depQ, inDep, 1, "write: in-scope CORRELATES_DEPLOYABLE_UNIT present")
	assertEdgeCount(ctx, t, exec, depQ, outDep, 1, "write: out-of-scope CORRELATES_DEPLOYABLE_UNIT present")

	// Shell-exec whole-repo retract: edge DELETE + orphan ShellCommand cleanup,
	// run sequentially (the grouped path under-applies on v1.1.11).
	if err := writer.RetractEdges(ctx, reducer.DomainShellExec, []reducer.SharedProjectionIntentRow{
		{IntentID: "retract-shell", RepositoryID: ctInRepo, Payload: map[string]any{"repo_id": ctInRepo}},
	}, ctShellSource); err != nil {
		t.Fatalf("RetractEdges(shell): %v", err)
	}
	// Documentation delta retract: section-uid and document-id statements, run
	// sequentially for the same reason.
	if err := writer.RetractEdges(ctx, reducer.DomainDocumentationEdges, []reducer.SharedProjectionIntentRow{
		{IntentID: "retract-doc", RepositoryID: ctInRepo, ScopeID: ctScopeIn, Payload: map[string]any{
			"scope_id": ctScopeIn, "delta_projection": true,
			"document_ids": []string{ctDocumentIn}, "section_uids": []string{ctSection},
		}},
	}, ctDocSource); err != nil {
		t.Fatalf("RetractEdges(doc): %v", err)
	}
	if err := writer.RetractEdges(ctx, reducer.DomainDeployableUnitEdges, []reducer.SharedProjectionIntentRow{
		{IntentID: "retract-dep", RepositoryID: ctInRepo, Payload: map[string]any{"repo_id": ctInRepo}},
	}, ctDepSource); err != nil {
		t.Fatalf("RetractEdges(dep): %v", err)
	}

	assertEdgeCount(ctx, t, exec, shellQ, inShell, 0, "retract: in-scope EXECUTES_SHELL gone")
	assertEdgeCount(ctx, t, exec, cmdNodeQ, map[string]any{"u": ctCmd}, 0, "retract: orphan ShellCommand cleaned")
	assertEdgeCount(ctx, t, exec, docQ, inDoc, 0, "retract: in-scope DOCUMENTS gone")
	assertEdgeCount(ctx, t, exec, depQ, inDep, 0, "retract: in-scope CORRELATES_DEPLOYABLE_UNIT gone")
	// Scoped retracts, not wipes.
	assertEdgeCount(ctx, t, exec, shellQ, outShell, 1, "retract: out-of-scope EXECUTES_SHELL survives")
	assertEdgeCount(ctx, t, exec, docQ, outDoc, 1, "retract: out-of-scope DOCUMENTS survives")
	assertEdgeCount(ctx, t, exec, depQ, outDep, 1, "retract: out-of-scope CORRELATES_DEPLOYABLE_UNIT survives")
	// Non-orphan endpoints survive.
	for _, q := range []struct {
		cypherText string
		key        string
	}{
		{"MATCH (n:Function {uid: $u}) RETURN count(n)", ctFnShell},
		{"MATCH (n:Function {uid: $u}) RETURN count(n)", ctFnDoc},
		{"MATCH (n:DocumentationSection {uid: $u}) RETURN count(n)", ctSection},
		{"MATCH (n:Repository {id: $u}) RETURN count(n)", ctInRepo},
		{"MATCH (n:Repository {id: $u}) RETURN count(n)", ctDepTarget},
		{"MATCH (n:ShellCommand {uid: $u}) RETURN count(n)", ctCmdOut},
	} {
		assertEdgeCount(ctx, t, exec, q.cypherText, map[string]any{"u": q.key}, 1, "node survives: "+q.key)
	}
}

// TestCodeInterprocTaintEdgeRetractGraphTruth proves the TAINT_FLOWS_TO
// retract path (scope-id anchored on the relationship, exact Function labels)
// deletes only the in-scope edges on a real NornicDB, through the dedicated
// CodeInterprocEvidenceWriter production paths.
func TestCodeInterprocTaintEdgeRetractGraphTruth(t *testing.T) {
	if !liveTierEnabled() {
		t.Skipf("set %s=1 (and ESHU_GRAPH_BACKEND/NEO4J_URI/ESHU_NEO4J_DATABASE) to run the taint-edge retract tier against a real NornicDB", liveTierEnv)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	exec, _ := openDeltaLiveBackend(ctx, t)
	cleanupContentEdgeScope(ctx, t, exec)
	t.Cleanup(func() {
		cleanCtx, cleanCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanCancel()
		cleanupContentEdgeScope(cleanCtx, t, exec)
	})

	seedContentEdgeNodes(ctx, t, exec)

	writer := cypher.NewCodeInterprocEvidenceWriter(exec, 0)
	// The writer stamps scope, generation, and evidence source per call, so the
	// in-scope and out-of-scope edges are written in separate calls, exactly as
	// production writes one scope at a time.
	if err := writer.WriteCodeInterprocEvidence(ctx, []map[string]any{{
		"uid": "taint-in", "source_function_uid": ctFnTaintSrc, "sink_function_uid": ctFnTaintDst,
		"sink_kind": "exec", "source_kind": "http", "confidence": 0.8,
	}}, ctScopeIn, "gen-1", ctTaintSource); err != nil {
		t.Fatalf("WriteCodeInterprocEvidence(in): %v", err)
	}
	if err := writer.WriteCodeInterprocEvidence(ctx, []map[string]any{{
		"uid": "taint-out", "source_function_uid": ctFnTaintOutS, "sink_function_uid": ctFnTaintOutD,
		"sink_kind": "exec", "source_kind": "http", "confidence": 0.8,
	}}, ctScopeOut, "gen-1", ctTaintSource); err != nil {
		t.Fatalf("WriteCodeInterprocEvidence(out): %v", err)
	}

	taintQ := "MATCH (:Function {uid: $s})-[r:TAINT_FLOWS_TO]->(:Function {uid: $t}) RETURN count(r)"
	inTaint := map[string]any{"s": ctFnTaintSrc, "t": ctFnTaintDst}
	outTaint := map[string]any{"s": ctFnTaintOutS, "t": ctFnTaintOutD}

	assertEdgeCount(ctx, t, exec, taintQ, inTaint, 1, "write: in-scope TAINT_FLOWS_TO present")
	assertEdgeCount(ctx, t, exec, taintQ, outTaint, 1, "write: out-of-scope TAINT_FLOWS_TO present")

	if err := writer.RetractCodeInterprocEvidence(ctx, []string{ctScopeIn}, "gen-1", ctTaintSource); err != nil {
		t.Fatalf("RetractCodeInterprocEvidence: %v", err)
	}

	assertEdgeCount(ctx, t, exec, taintQ, inTaint, 0, "retract: in-scope TAINT_FLOWS_TO gone")
	assertEdgeCount(ctx, t, exec, taintQ, outTaint, 1, "retract: out-of-scope TAINT_FLOWS_TO survives")
	for _, uid := range []string{ctFnTaintSrc, ctFnTaintDst, ctFnTaintOutS, ctFnTaintOutD} {
		assertEdgeCount(ctx, t, exec, "MATCH (n:Function {uid: $u}) RETURN count(n)", map[string]any{"u": uid}, 1, "node survives: "+uid)
	}
}

// seedContentEdgeNodes creates the Function, Repository, and documentation
// target nodes the write templates MATCH. ShellCommand and
// DocumentationSection nodes are MERGEd by the write templates themselves.
func seedContentEdgeNodes(ctx context.Context, t *testing.T, exec liveExecutor) {
	t.Helper()
	stmt := cypher.Statement{
		Cypher: `CREATE (:Function {uid: $fnShell, repo_id: $in, marker: $marker}),
       (:Function {uid: $fnShellOut, repo_id: $out, marker: $marker}),
       (:Function {uid: $fnDoc, repo_id: $in, marker: $marker}),
       (:Function {uid: $fnDocOut, repo_id: $out, marker: $marker}),
       (:Function {uid: $taintSrc, repo_id: $in, marker: $marker}),
       (:Function {uid: $taintDst, repo_id: $in, marker: $marker}),
       (:Function {uid: $taintOutS, repo_id: $out, marker: $marker}),
       (:Function {uid: $taintOutD, repo_id: $out, marker: $marker}),
       (:Repository {id: $in, marker: $marker}),
       (:Repository {id: $depTarget, marker: $marker}),
       (:Repository {id: $depOutSrc, marker: $marker}),
       (:Repository {id: $depOutTgt, marker: $marker})`,
		Parameters: map[string]any{
			"fnShell": ctFnShell, "fnShellOut": ctFnShellOut,
			"fnDoc": ctFnDoc, "fnDocOut": ctFnDocOut,
			"taintSrc": ctFnTaintSrc, "taintDst": ctFnTaintDst,
			"taintOutS": ctFnTaintOutS, "taintOutD": ctFnTaintOutD,
			"in": ctInRepo, "depTarget": ctDepTarget,
			"depOutSrc": ctDepOutSrc, "depOutTgt": ctDepOutTgt, "marker": ctMarker,
		},
	}
	if err := exec.Execute(ctx, stmt); err != nil {
		t.Fatalf("seed content-edge nodes: %v", err)
	}
}

// cleanupContentEdgeScope removes every node these tests create, including the
// write-MERGEd ShellCommand and DocumentationSection nodes.
func cleanupContentEdgeScope(ctx context.Context, t *testing.T, exec liveExecutor) {
	t.Helper()
	for _, stmt := range []cypher.Statement{
		{
			Cypher:     `MATCH (n {marker: $marker}) DETACH DELETE n`,
			Parameters: map[string]any{"marker": ctMarker},
		},
		{
			Cypher:     `MATCH (c:ShellCommand) WHERE c.uid IN [$cmdIn, $cmdOut] DETACH DELETE c`,
			Parameters: map[string]any{"cmdIn": ctCmd, "cmdOut": ctCmdOut},
		},
		{
			Cypher:     `MATCH (s:DocumentationSection) WHERE s.uid IN [$secIn, $secOut] DETACH DELETE s`,
			Parameters: map[string]any{"secIn": ctSection, "secOut": ctSectionOut},
		},
	} {
		if err := exec.Execute(ctx, stmt); err != nil {
			t.Fatalf("cleanup content-edge scope: %v", err)
		}
	}
}
