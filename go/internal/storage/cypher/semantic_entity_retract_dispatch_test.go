// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// semanticRetractRow is a minimal valid Variable row (Variable is the #4367
// retractable-node label whose only creator is this semantic path).
func semanticRetractRow() reducer.SemanticEntityRow {
	return reducer.SemanticEntityRow{
		RepoID: "repo-1", EntityID: "var-1", EntityType: "Variable", EntityName: "SETTING",
		FilePath: "pkg/config.py", RelativePath: "pkg/config.py", Language: "python",
		StartLine: 1, EndLine: 1,
	}
}

func cyphersContainDelete(cyphers []string) bool {
	for _, c := range cyphers {
		if strings.Contains(c, "DELETE") {
			return true
		}
	}
	return false
}

// TestSemanticDeltaRetractRoutesThroughAutocommitExecute proves the delta
// retract path dispatches its DETACH DELETE statements through Execute
// (autocommit), never ExecuteGroup. Grouped DELETEs under-apply on the pinned
// NornicDB v1.1.11 (#4367 semantic Variable delta-retract hole); routing a
// retract back through ExecuteGroup would reproduce that silent data-retention
// bug, so this recorder makes such a regression fail a no-backend CI run.
func TestSemanticDeltaRetractRoutesThroughAutocommitExecute(t *testing.T) {
	t.Parallel()

	rec := &dispatchRouteRecorder{}
	w := NewSemanticEntityWriterWithCanonicalNodeRows(rec, 500).WithLabelScopedRetract().WithSequentialRetract()
	if _, err := w.WriteSemanticEntities(context.Background(), reducer.SemanticEntityWrite{
		RepoIDs: []string{"repo-1"}, DeltaProjection: true, DeltaFilePaths: []string{"pkg/config.py"},
	}); err != nil {
		t.Fatalf("delta retract error = %v, want nil", err)
	}
	if len(rec.executeCyphers) == 0 {
		t.Fatal("delta retract Execute calls = 0, want >=1 (autocommit route)")
	}
	if !cyphersContainDelete(rec.executeCyphers) {
		t.Fatal("delta retract Execute statements carry no DELETE; the Variable DETACH DELETE must route through Execute")
	}
	if cyphersContainDelete(rec.groupCyphers) {
		t.Fatalf("delta retract routed a DELETE through ExecuteGroup (%d grouped stmts); grouped DELETEs under-apply on NornicDB v1.1.11 — see #4367", len(rec.groupCyphers))
	}
}

// TestSemanticFullRetractRoutesThroughAutocommitExecute proves the whole-repo
// (non-delta) retract path likewise routes DELETEs through Execute, not
// ExecuteGroup.
func TestSemanticFullRetractRoutesThroughAutocommitExecute(t *testing.T) {
	t.Parallel()

	rec := &dispatchRouteRecorder{}
	w := NewSemanticEntityWriterWithCanonicalNodeRows(rec, 500).WithLabelScopedRetract().WithSequentialRetract()
	if _, err := w.WriteSemanticEntities(context.Background(), reducer.SemanticEntityWrite{
		RepoIDs: []string{"repo-1"},
	}); err != nil {
		t.Fatalf("full retract error = %v, want nil", err)
	}
	if len(rec.executeCyphers) == 0 {
		t.Fatal("full retract Execute calls = 0, want >=1 (autocommit route)")
	}
	if cyphersContainDelete(rec.groupCyphers) {
		t.Fatalf("full retract routed a DELETE through ExecuteGroup; grouped DELETEs under-apply on NornicDB v1.1.11 — see #4367")
	}
}

// TestSemanticWriteWithRetractSplitsDispatch proves that when a delta write
// carries both a retract and upserts, the retract DELETEs route through Execute
// while the MERGE upserts still batch through ExecuteGroup (the retract fix did
// not downgrade the write path to per-statement autocommit).
func TestSemanticWriteWithRetractSplitsDispatch(t *testing.T) {
	t.Parallel()

	rec := &dispatchRouteRecorder{}
	w := NewSemanticEntityWriterWithCanonicalNodeRows(rec, 500).WithLabelScopedRetract().WithSequentialRetract()
	if _, err := w.WriteSemanticEntities(context.Background(), reducer.SemanticEntityWrite{
		RepoIDs:         []string{"repo-1"},
		Rows:            []reducer.SemanticEntityRow{semanticRetractRow()},
		DeltaProjection: true,
		DeltaFilePaths:  []string{"pkg/config.py"},
	}); err != nil {
		t.Fatalf("write+retract error = %v, want nil", err)
	}
	if !cyphersContainDelete(rec.executeCyphers) {
		t.Fatal("write+retract: no DELETE routed through Execute; the retract must be sequential")
	}
	if cyphersContainDelete(rec.groupCyphers) {
		t.Fatal("write+retract: a DELETE routed through ExecuteGroup; grouped DELETEs under-apply on NornicDB v1.1.11 — see #4367")
	}
	if len(rec.groupCyphers) == 0 {
		t.Fatal("write+retract: MERGE upserts must still batch through ExecuteGroup (0 grouped stmts)")
	}
}

// TestSemanticDefaultRetractGroupsAtomicallyWithUpserts proves the DEFAULT
// (no WithSequentialRetract, i.e. the Neo4j path) keeps retract and upsert in a
// single grouped transaction so they commit or roll back atomically. Splitting
// retracts onto autocommit Execute unconditionally would regress Neo4j, where
// grouped DELETEs apply correctly and the atomic retract+upsert guarantee must
// hold; the sequential split is reserved for NornicDB via WithSequentialRetract.
func TestSemanticDefaultRetractGroupsAtomicallyWithUpserts(t *testing.T) {
	t.Parallel()

	rec := &dispatchRouteRecorder{}
	// No WithSequentialRetract: the default, group-capable (Neo4j) dispatch.
	w := NewSemanticEntityWriterWithCanonicalNodeRows(rec, 500).WithLabelScopedRetract()
	if _, err := w.WriteSemanticEntities(context.Background(), reducer.SemanticEntityWrite{
		RepoIDs:         []string{"repo-1"},
		Rows:            []reducer.SemanticEntityRow{semanticRetractRow()},
		DeltaProjection: true,
		DeltaFilePaths:  []string{"pkg/config.py"},
	}); err != nil {
		t.Fatalf("default write+retract error = %v, want nil", err)
	}
	if len(rec.executeCyphers) != 0 {
		t.Fatalf("default path routed %d statement(s) through autocommit Execute, want 0 — retract and upsert must stay in one atomic grouped transaction on Neo4j", len(rec.executeCyphers))
	}
	if !cyphersContainDelete(rec.groupCyphers) {
		t.Fatal("default path: the retract DELETE must be inside the grouped transaction (atomic with upserts)")
	}
}
