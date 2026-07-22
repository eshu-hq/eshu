// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package nornicdb

import (
	"context"
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
// DELETE as auto-commit-only" rule. Production is safe ONLY because
// PhaseGroupExecutor.executeSequentialRetractPhase unconditionally routes
// every OperationCanonicalRetract statement through Execute, never
// ExecuteGroup. This test locks that routing in place: a future change that
// starts grouping retract-only phases through ExecuteGroup (e.g. an
// unreviewed "batch retracts for fewer round trips" optimization) would
// silently reintroduce the write-loss class this guards.
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
