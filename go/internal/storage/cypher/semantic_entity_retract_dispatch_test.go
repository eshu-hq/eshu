// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer/semanticentity"
)

// semanticRetractRow is a minimal valid Variable row (Variable is the #4367
// retractable-node label whose only creator is this semantic path).
func semanticRetractRow() semanticentity.SemanticEntityRow {
	return semanticentity.SemanticEntityRow{
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

// executeOnlyRouteRecorder records statements for an executor that exposes only
// Execute — the shape of NornicDB's default ExecuteOnlyExecutor, which hides
// GroupExecutor. It deliberately does NOT implement ExecuteGroup.
type executeOnlyRouteRecorder struct {
	executeCyphers []string
}

func (r *executeOnlyRouteRecorder) Execute(_ context.Context, statement Statement) error {
	r.executeCyphers = append(r.executeCyphers, statement.Cypher)
	return nil
}

// TestSemanticDeltaRetractGroupsWithUpserts proves the delta retract path keeps
// its DETACH DELETE statements inside the grouped (atomic) transaction on a
// group-capable executor, with nothing split onto autocommit Execute.
//
// This assertion is the deliberate inversion of the #4367 one it replaces.
// That test required the opposite — every retract DELETE on autocommit Execute
// — because grouped DETACH DELETEs under-applied on NornicDB v1.1.11 and
// silently left semantic nodes behind. #5323 measured the grouped retract
// correct on 1.2.1 and 1.2.2, #6176 re-measured it on the deployed 1.2.2 build,
// and Eshu's supported floor now sits above v1.1.11, so the atomic
// retract+upsert transaction is the contract again (#6176).
func TestSemanticDeltaRetractGroupsWithUpserts(t *testing.T) {
	t.Parallel()

	rec := &dispatchRouteRecorder{}
	w := NewSemanticEntityWriterWithCanonicalNodeRows(rec, 500).WithLabelScopedRetract()
	if _, err := w.WriteSemanticEntities(context.Background(), semanticentity.SemanticEntityWrite{
		RepoIDs: []string{"repo-1"}, DeltaProjection: true, DeltaFilePaths: []string{"pkg/config.py"},
	}); err != nil {
		t.Fatalf("delta retract error = %v, want nil", err)
	}
	if len(rec.executeCyphers) != 0 {
		t.Fatalf("delta retract routed %d statement(s) through autocommit Execute, want 0 — retract must stay in the atomic grouped transaction", len(rec.executeCyphers))
	}
	if !cyphersContainDelete(rec.groupCyphers) {
		t.Fatal("delta retract grouped statements carry no DELETE; the Variable DETACH DELETE must route through ExecuteGroup")
	}
}

// TestSemanticFullRetractGroupsWithUpserts proves the whole-repo (non-delta)
// retract path likewise keeps its DELETEs inside ExecuteGroup.
func TestSemanticFullRetractGroupsWithUpserts(t *testing.T) {
	t.Parallel()

	rec := &dispatchRouteRecorder{}
	w := NewSemanticEntityWriterWithCanonicalNodeRows(rec, 500).WithLabelScopedRetract()
	if _, err := w.WriteSemanticEntities(context.Background(), semanticentity.SemanticEntityWrite{
		RepoIDs: []string{"repo-1"},
	}); err != nil {
		t.Fatalf("full retract error = %v, want nil", err)
	}
	if len(rec.executeCyphers) != 0 {
		t.Fatalf("full retract routed %d statement(s) through autocommit Execute, want 0", len(rec.executeCyphers))
	}
	if !cyphersContainDelete(rec.groupCyphers) {
		t.Fatal("full retract grouped statements carry no DELETE")
	}
}

// TestSemanticWriteWithRetractGroupsRetractBeforeUpserts proves that when a
// delta write carries both a retract and upserts, both ride the same
// ExecuteGroup call AND the retract is ordered first. Order is load-bearing:
// an upsert that landed before its own retract would be deleted by it, so the
// generation would project nothing.
func TestSemanticWriteWithRetractGroupsRetractBeforeUpserts(t *testing.T) {
	t.Parallel()

	rec := &dispatchRouteRecorder{}
	w := NewSemanticEntityWriterWithCanonicalNodeRows(rec, 500).WithLabelScopedRetract()
	if _, err := w.WriteSemanticEntities(context.Background(), semanticentity.SemanticEntityWrite{
		RepoIDs:         []string{"repo-1"},
		Rows:            []semanticentity.SemanticEntityRow{semanticRetractRow()},
		DeltaProjection: true,
		DeltaFilePaths:  []string{"pkg/config.py"},
	}); err != nil {
		t.Fatalf("write+retract error = %v, want nil", err)
	}
	if len(rec.executeCyphers) != 0 {
		t.Fatalf("write+retract routed %d statement(s) through autocommit Execute, want 0 — retract and upsert must commit or roll back together", len(rec.executeCyphers))
	}

	lastDelete, firstMerge := -1, -1
	for i, c := range rec.groupCyphers {
		if strings.Contains(c, "DELETE") {
			lastDelete = i
		}
		if firstMerge < 0 && strings.Contains(c, "MERGE") {
			firstMerge = i
		}
	}
	if lastDelete < 0 {
		t.Fatal("write+retract: no DELETE in the grouped statements; the retract must be inside the transaction")
	}
	if firstMerge < 0 {
		t.Fatal("write+retract: no MERGE upsert in the grouped statements")
	}
	if lastDelete > firstMerge {
		t.Fatalf("write+retract: a retract DELETE (index %d) is ordered after the first upsert MERGE (index %d); the retract must lead the group or it deletes the rows just written", lastDelete, firstMerge)
	}
}

// TestSemanticRetractFallsBackToExecuteWithoutGroupExecutor proves the
// per-statement fallback still covers the retract on an executor that exposes
// only Execute. That is NornicDB's default wiring (ExecuteOnlyExecutor hides
// GroupExecutor), so this is the path most deployments actually take, and it
// must not silently skip the DELETEs.
func TestSemanticRetractFallsBackToExecuteWithoutGroupExecutor(t *testing.T) {
	t.Parallel()

	rec := &executeOnlyRouteRecorder{}
	if _, ok := any(rec).(GroupExecutor); ok {
		t.Fatal("executeOnlyRouteRecorder implements GroupExecutor; it must not, or this test proves nothing about the fallback")
	}
	w := NewSemanticEntityWriterWithCanonicalNodeRows(rec, 500).WithLabelScopedRetract()
	if _, err := w.WriteSemanticEntities(context.Background(), semanticentity.SemanticEntityWrite{
		RepoIDs:         []string{"repo-1"},
		Rows:            []semanticentity.SemanticEntityRow{semanticRetractRow()},
		DeltaProjection: true,
		DeltaFilePaths:  []string{"pkg/config.py"},
	}); err != nil {
		t.Fatalf("execute-only write+retract error = %v, want nil", err)
	}
	if !cyphersContainDelete(rec.executeCyphers) {
		t.Fatal("execute-only fallback: no DELETE dispatched; the retract must still run when the executor hides GroupExecutor")
	}
}
