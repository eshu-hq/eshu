// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"testing"
)

// dispatchRouteRecorder is a GroupExecutor-capable fake that records which
// dispatch route each statement took: Execute (single, autocommit) versus
// ExecuteGroup (batched, session.ExecuteWrite). It exists to guard the #4893
// fix in CI: value-flow retract DELETEs MUST route through Execute (autocommit)
// because NornicDB v1.1.9's bolt session.ExecuteWrite/tx.Run silently deletes
// zero rows for an UNWIND + MATCH-on-relationship + DELETE inside an explicit
// transaction. A future refactor that routes a retract back through
// ExecuteGroup would reproduce that data-loss bug; this recorder makes such a
// regression fail a normal (no-backend) CI run instead of only the DSN-gated
// bolt integration test.
type dispatchRouteRecorder struct {
	executeCyphers []string
	groupCyphers   []string
}

func (r *dispatchRouteRecorder) Execute(_ context.Context, statement Statement) error {
	r.executeCyphers = append(r.executeCyphers, statement.Cypher)
	return nil
}

func (r *dispatchRouteRecorder) ExecuteGroup(_ context.Context, statements []Statement) error {
	for _, statement := range statements {
		r.groupCyphers = append(r.groupCyphers, statement.Cypher)
	}
	return nil
}

// TestCodeInterprocEvidenceRetractByUIDsRoutesThroughAutocommitExecute proves
// the three interproc by-uid retract methods dispatch their DELETE statements
// through Execute (autocommit), never through ExecuteGroup, on a
// GroupExecutor-capable executor.
func TestCodeInterprocEvidenceRetractByUIDsRoutesThroughAutocommitExecute(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		call func(w *CodeInterprocEvidenceWriter) error
	}{
		{
			name: "scoped",
			call: func(w *CodeInterprocEvidenceWriter) error {
				return w.RetractCodeInterprocEvidenceByUIDs(context.Background(), []string{"fn-1"}, []string{"scope-1"}, "reducer/code-interproc")
			},
		},
		{
			name: "source",
			call: func(w *CodeInterprocEvidenceWriter) error {
				return w.RetractCodeInterprocEvidenceSourceByUIDs(context.Background(), []string{"fn-1"}, "reducer/code-interproc-fixpoint")
			},
		},
		{
			name: "stale",
			call: func(w *CodeInterprocEvidenceWriter) error {
				return w.RetractStaleCodeInterprocEvidenceByUIDs(context.Background(), []string{"fn-1"}, "scope-1", "gen-1", "reducer/code-interproc")
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			rec := &dispatchRouteRecorder{}
			w := NewCodeInterprocEvidenceWriter(rec, 0)
			if err := tc.call(w); err != nil {
				t.Fatalf("retract %s error = %v, want nil", tc.name, err)
			}
			if len(rec.executeCyphers) != 1 {
				t.Fatalf("retract %s Execute calls = %d, want 1 (autocommit route)", tc.name, len(rec.executeCyphers))
			}
			if len(rec.groupCyphers) != 0 {
				t.Fatalf("retract %s ExecuteGroup calls = %d, want 0; DELETE must not use ExecuteGroup (NornicDB v1.1.9 tx.Run deletes 0 rows) — see #4893", tc.name, len(rec.groupCyphers))
			}
		})
	}
}

// TestCodeTaintEvidenceRetractByUIDsRoutesThroughAutocommitExecute proves the
// two taint by-uid retract methods dispatch through Execute, never ExecuteGroup.
func TestCodeTaintEvidenceRetractByUIDsRoutesThroughAutocommitExecute(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		call func(w *CodeTaintEvidenceWriter) error
	}{
		{
			name: "scoped",
			call: func(w *CodeTaintEvidenceWriter) error {
				return w.RetractCodeTaintEvidenceByUIDs(context.Background(), []string{"node-1"}, []string{"scope-1"}, "reducer/code-taint")
			},
		},
		{
			name: "stale",
			call: func(w *CodeTaintEvidenceWriter) error {
				return w.RetractStaleCodeTaintEvidenceByUIDs(context.Background(), []string{"node-1"}, "scope-1", "gen-1", "reducer/code-taint")
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			rec := &dispatchRouteRecorder{}
			w := NewCodeTaintEvidenceWriter(rec, 0)
			if err := tc.call(w); err != nil {
				t.Fatalf("retract %s error = %v, want nil", tc.name, err)
			}
			if len(rec.executeCyphers) != 1 {
				t.Fatalf("retract %s Execute calls = %d, want 1 (autocommit route)", tc.name, len(rec.executeCyphers))
			}
			if len(rec.groupCyphers) != 0 {
				t.Fatalf("retract %s ExecuteGroup calls = %d, want 0; DELETE must not use ExecuteGroup (NornicDB v1.1.9 tx.Run deletes 0 rows) — see #4893", tc.name, len(rec.groupCyphers))
			}
		})
	}
}

// TestCodeInterprocEvidenceWriteRoutesThroughExecuteGroup proves the write path
// still uses ExecuteGroup (the batched MERGE route) so the retract fix did not
// accidentally downgrade writes to per-statement autocommit.
func TestCodeInterprocEvidenceWriteRoutesThroughExecuteGroup(t *testing.T) {
	t.Parallel()

	rec := &dispatchRouteRecorder{}
	w := NewCodeInterprocEvidenceWriter(rec, 0)
	rows := []map[string]any{{
		"uid":                 "ev-1",
		"source_function_uid": "fn-1",
		"sink_function_uid":   "fn-2",
	}}
	if err := w.WriteCodeInterprocEvidence(context.Background(), rows, "scope-1", "gen-1", "reducer/code-interproc"); err != nil {
		t.Fatalf("write error = %v, want nil", err)
	}
	if len(rec.groupCyphers) == 0 {
		t.Fatal("write ExecuteGroup calls = 0, want >=1; the MERGE write path must use ExecuteGroup")
	}
	if len(rec.executeCyphers) != 0 {
		t.Fatalf("write Execute calls = %d, want 0; the write path must batch through ExecuteGroup", len(rec.executeCyphers))
	}
}
