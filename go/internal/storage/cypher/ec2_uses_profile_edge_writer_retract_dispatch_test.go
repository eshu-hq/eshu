// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"testing"
)

// TestEC2UsesProfileEdgeWriterRetractRoutesThroughAutocommitExecute proves
// RetractEC2UsesProfileEdges dispatches its DELETE through Execute
// (autocommit), never through ExecuteGroup, on a GroupExecutor-capable
// executor. Issue #5152: dispatch() (used by both the write and retract
// paths) groups whenever the executor supports it, so the single USES_PROFILE
// retract statement was being sent through a managed ExecuteWrite transaction
// — the same shape measured to under-apply on NornicDB v1.1.11 for
// TAINT_FLOWS_TO, the SQL-relationship retract, and the repo-dependency
// retract (#4367/#5128/#5146). dispatchRetract fixes this the same way
// CodeInterprocEvidenceWriter does: sequential Execute, never ExecuteGroup.
func TestEC2UsesProfileEdgeWriterRetractRoutesThroughAutocommitExecute(t *testing.T) {
	t.Parallel()

	rec := &dispatchRouteRecorder{}
	w := NewEC2UsesProfileEdgeWriter(rec, 0)

	if err := w.RetractEC2UsesProfileEdges(context.Background(), []string{"scope-1"}, "gen-1", "reducer/ec2-uses-profile"); err != nil {
		t.Fatalf("RetractEC2UsesProfileEdges() error = %v, want nil", err)
	}
	if len(rec.executeCyphers) != 1 {
		t.Fatalf("Execute calls = %d, want 1 (autocommit route)", len(rec.executeCyphers))
	}
	if len(rec.groupCyphers) != 0 {
		t.Fatalf("ExecuteGroup calls = %d, want 0; USES_PROFILE DELETE must not use ExecuteGroup (NornicDB v1.1.11 under-applies) — see #5152", len(rec.groupCyphers))
	}
}

// TestEC2UsesProfileEdgeWriterWriteRoutesThroughExecuteGroup proves the write
// path still uses ExecuteGroup (the batched MERGE route) so the retract fix
// did not accidentally downgrade writes to per-statement autocommit.
func TestEC2UsesProfileEdgeWriterWriteRoutesThroughExecuteGroup(t *testing.T) {
	t.Parallel()

	rec := &dispatchRouteRecorder{}
	w := NewEC2UsesProfileEdgeWriter(rec, 0)

	if err := w.WriteEC2UsesProfileEdges(context.Background(), ec2UsesProfileEdgeRows(1), "scope-1", "gen-1", "reducer/ec2-uses-profile"); err != nil {
		t.Fatalf("WriteEC2UsesProfileEdges() error = %v, want nil", err)
	}
	if len(rec.groupCyphers) == 0 {
		t.Fatal("write ExecuteGroup calls = 0, want >=1; the MERGE write path must use ExecuteGroup")
	}
	if len(rec.executeCyphers) != 0 {
		t.Fatalf("write Execute calls = %d, want 0; the write path must batch through ExecuteGroup", len(rec.executeCyphers))
	}
}
