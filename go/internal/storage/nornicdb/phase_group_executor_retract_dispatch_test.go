// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package nornicdb

import (
	"context"
	"sync"
	"testing"

	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

// dispatchRecordingExecutor records which method (Execute vs ExecuteGroup)
// PhaseGroupExecutor called for each statement, so a test can assert dispatch
// mode without a real Bolt backend.
type dispatchRecordingExecutor struct {
	executeCalls      int
	executeGroupCalls int
}

func (e *dispatchRecordingExecutor) Execute(context.Context, sourcecypher.Statement) error {
	e.executeCalls++
	return nil
}

func (e *dispatchRecordingExecutor) ExecuteGroup(_ context.Context, _ []sourcecypher.Statement) error {
	e.executeGroupCalls++
	return nil
}

// TestPhaseGroupExecutorRetractPhaseNeverUsesExecuteGroup is the regression
// guard for the P0 #5652 follow-up (canonicalNodeRefreshCurrent*Cypher
// DELETE write-loss investigation, docs/internal/evidence/
// 5652-followup-file-directory-edge-writeloss-investigation.md). Live-proven
// against the pinned NornicDB v1.1.11 image: the shipped UNWIND-batched
// DELETE statements
// (canonicalNodeRefreshCurrentFileImportEdgesCypher/DirectoryFileEdgesCypher/
// DirectoryParentEdgesCypher) silently drop their DELETE when dispatched
// through a managed Bolt transaction (ExecuteGroup), but delete correctly
// when dispatched auto-commit (Execute) -- matching
// docs/public/reference/nornicdb-query-pitfalls.md's "treat every retract
// DELETE as auto-commit-only" rule. These three statements are safe today
// because they always ship together in the homogeneous "retract" phase
// (buildRetractStatements), where PhaseGroupExecutor.ExecutePhaseGroup's
// allStatementsUseOperation gate routes the whole phase through
// executeSequentialRetractPhase, which calls Execute for every statement and
// never ExecuteGroup. This test locks THAT routing in place for an all-retract
// phase: a future change that starts grouping retract-only phases through
// ExecuteGroup (e.g. an unreviewed "batch retracts for fewer round trips"
// optimization) would silently reintroduce the write-loss class this guards.
//
// The MIXED-phase case (retract statements alongside an upsert in the same
// phase) is covered separately below and by #5680's order-preserving dispatch
// fix.
func TestPhaseGroupExecutorRetractPhaseNeverUsesExecuteGroup(t *testing.T) {
	t.Parallel()

	inner := &dispatchRecordingExecutor{}
	executor := PhaseGroupExecutor{Inner: inner}

	retractStatements := []sourcecypher.Statement{
		{
			Operation:  sourcecypher.OperationCanonicalRetract,
			Cypher:     "UNWIND $file_paths AS file_path MATCH (f:File {path: file_path}) MATCH (f)-[r:IMPORTS]->(:Module) DELETE r",
			Parameters: map[string]any{"file_paths": []string{"a.go"}},
		},
		{
			Operation:  sourcecypher.OperationCanonicalRetract,
			Cypher:     "UNWIND $file_paths AS file_path MATCH (f:File {path: file_path}) MATCH (:Directory)-[r:CONTAINS]->(f) DELETE r",
			Parameters: map[string]any{"file_paths": []string{"a.go"}},
		},
		{
			Operation: sourcecypher.OperationCanonicalRetract,
			Cypher:    "UNWIND $rows AS row MATCH (p:Directory)-[r:CONTAINS]->(d:Directory {path: row.path}) WHERE p.path <> row.parent_path DELETE r",
			Parameters: map[string]any{
				"repo_id": "repo-1",
				"rows":    []map[string]any{{"path": "d1", "parent_path": "d0"}},
			},
		},
	}

	if err := executor.ExecutePhaseGroup(context.Background(), retractStatements); err != nil {
		t.Fatalf("ExecutePhaseGroup() error = %v, want nil", err)
	}

	if inner.executeGroupCalls != 0 {
		t.Fatalf("ExecuteGroup called %d times for an all-retract phase, want 0 -- retract DELETE statements must dispatch auto-commit only (see docs/public/reference/nornicdb-query-pitfalls.md)", inner.executeGroupCalls)
	}
	if inner.executeCalls != len(retractStatements) {
		t.Fatalf("Execute called %d times, want %d (one per retract statement)", inner.executeCalls, len(retractStatements))
	}
}

// TestExecuteGroupedChunksWithDrainNeverDispatchesRetractViaExecuteGroup is
// the no-backend invariant guard for #5680: on the pinned NornicDB v1.1.11 the
// established rule is that a retract DELETE must run through Execute, never a
// managed ExecuteGroup transaction (docs/public/reference/nornicdb-query-pitfalls.md).
// Before the #5680 fix, a non-Drain OperationCanonicalRetract statement (e.g.
// the terraform_state DETACH DELETE sweeps) was bundled into
// executeGroupedChunksWithDrain's single ge.ExecuteGroup call for "remaining"
// statements -- this test proves that can never happen again, independent of
// any real backend.
func TestExecuteGroupedChunksWithDrainNeverDispatchesRetractViaExecuteGroup(t *testing.T) {
	t.Parallel()

	recorder := newOrderRecordingExecutor()
	executor := PhaseGroupExecutor{Inner: recorder}
	stmts := mixedTerraformStatePhaseStatements()

	if err := executor.ExecutePhaseGroup(context.Background(), stmts); err != nil {
		t.Fatalf("ExecutePhaseGroup() error = %v, want nil", err)
	}

	for callIdx, call := range recorder.calls {
		if call.kind != "execute_group" {
			continue
		}
		for _, stmt := range call.stmts {
			if stmt.Operation == sourcecypher.OperationCanonicalRetract {
				t.Fatalf(
					"ExecuteGroup call %d dispatched a retract statement (seq=%v): every retract DELETE is ExecuteGroup-unsafe on NornicDB v1.1.11",
					callIdx, stmt.Parameters["seq"],
				)
			}
		}
	}
}

// TestExecuteGroupedChunksWithDrainPreservesEmittedOrder is the no-backend
// invariant guard for #5680 defect 2: before the fix, every Drain-marked
// statement was hoisted to run before ANY upsert in the phase, regardless of
// its emitted position, breaking any retract whose predicate depends on a
// property an earlier upsert in the SAME phase refreshes (the tfstate
// MATCHES_STATE edge retract's `s.generation_id = $generation_id` anchor).
// This test proves dispatch order -- flattening every recorded Execute and
// ExecuteGroup call, in call order and in per-call statement order -- exactly
// reconstructs the statements' original emitted order.
func TestExecuteGroupedChunksWithDrainPreservesEmittedOrder(t *testing.T) {
	t.Parallel()

	recorder := newOrderRecordingExecutor()
	executor := PhaseGroupExecutor{Inner: recorder}
	stmts := mixedTerraformStatePhaseStatements()

	if err := executor.ExecutePhaseGroup(context.Background(), stmts); err != nil {
		t.Fatalf("ExecutePhaseGroup() error = %v, want nil", err)
	}

	var dispatchedSeq []int
	for _, call := range recorder.calls {
		for _, stmt := range call.stmts {
			seq, ok := stmt.Parameters["seq"].(int)
			if !ok {
				t.Fatalf("dispatched statement missing int seq marker: %#v", stmt.Parameters)
			}
			dispatchedSeq = append(dispatchedSeq, seq)
		}
	}

	if len(dispatchedSeq) != len(stmts) {
		t.Fatalf("dispatched %d statements, want %d (some statement was dropped or duplicated)", len(dispatchedSeq), len(stmts))
	}
	for i, seq := range dispatchedSeq {
		if seq != i {
			t.Fatalf(
				"dispatch order mismatch at position %d: got seq=%d, want seq=%d (dispatch reordered the emitted statement sequence: %v)",
				i, seq, i, dispatchedSeq,
			)
		}
	}
}

// mixedTerraformStatePhaseStatements builds a statement sequence shaped like
// buildTerraformStateStatements' real output: an upsert, then a non-Drain
// retract (the resource-sweep DETACH DELETE shape), more upserts, then a
// Drain-marked retract with an empty DrainVar (the MATCHES_STATE edge-retract
// shape), then a trailing upsert. Every statement is tagged
// StatementMetadataPhaseKey="terraform_state" so ExecutePhaseGroup routes the
// whole group into executeGroupedChunksWithDrain (statementPhaseUsesEntityLabelStats
// is false for this phase name, and not every statement is a retract, so
// neither executeSequentialRetractPhase nor executeEntityPhaseGroup applies).
func mixedTerraformStatePhaseStatements() []sourcecypher.Statement {
	return []sourcecypher.Statement{
		dispatchTestStatement(0, sourcecypher.OperationCanonicalUpsert, false, ""),
		dispatchTestStatement(1, sourcecypher.OperationCanonicalRetract, false, ""),
		dispatchTestStatement(2, sourcecypher.OperationCanonicalRetract, false, ""),
		dispatchTestStatement(3, sourcecypher.OperationCanonicalUpsert, false, ""),
		dispatchTestStatement(4, sourcecypher.OperationCanonicalUpsert, false, ""),
		dispatchTestStatement(5, sourcecypher.OperationCanonicalRetract, true, ""),
		dispatchTestStatement(6, sourcecypher.OperationCanonicalUpsert, false, ""),
	}
}

func dispatchTestStatement(seq int, operation sourcecypher.Operation, drain bool, drainVar string) sourcecypher.Statement {
	return sourcecypher.Statement{
		Operation: operation,
		Cypher:    "MATCH (n) RETURN n",
		Parameters: map[string]any{
			sourcecypher.StatementMetadataPhaseKey: "terraform_state",
			"seq":                                  seq,
		},
		Drain:    drain,
		DrainVar: drainVar,
	}
}

// orderRecordingExecutor implements sourcecypher.Executor and
// sourcecypher.GroupExecutor, recording every dispatched call (kind plus the
// statements it carried) in the exact order PhaseGroupExecutor issues them.
// It never actually talks to a backend -- the #5680 dispatch tests only assert
// on dispatch shape and order, never on graph state, so the recorder never
// needs real Cypher execution.
type orderRecordingExecutor struct {
	mu    sync.Mutex
	calls []dispatchRecordedCall
}

type dispatchRecordedCall struct {
	kind  string // "execute" or "execute_group"
	stmts []sourcecypher.Statement
}

func newOrderRecordingExecutor() *orderRecordingExecutor {
	return &orderRecordingExecutor{}
}

func (e *orderRecordingExecutor) Execute(_ context.Context, stmt sourcecypher.Statement) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.calls = append(e.calls, dispatchRecordedCall{kind: "execute", stmts: []sourcecypher.Statement{stmt}})
	return nil
}

func (e *orderRecordingExecutor) ExecuteGroup(_ context.Context, stmts []sourcecypher.Statement) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	captured := append([]sourcecypher.Statement(nil), stmts...)
	e.calls = append(e.calls, dispatchRecordedCall{kind: "execute_group", stmts: captured})
	return nil
}
