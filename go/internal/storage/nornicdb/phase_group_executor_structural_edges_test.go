// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package nornicdb

import (
	"context"
	"testing"

	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

// Issue #6070. PR #5911 wired the File-[:IMPORTS]->Module producer, which turned
// structural_edges from a near-empty phase into the largest canonical write:
// phase-group chunks went from 2 to 601 across an 896-repository corpus. Four
// repositories then dead-lettered after three attempts, two on the 120s
// canonical write budget and two on TransactionCommitFailed at 64.6s and 86.1s
// (the transaction is too large, not merely too slow).
//
// The chunker could not see it. structural_edges had no narrow phase budget, so
// it fell through to DefaultPhaseGroupStatements (500) while its statements each
// carry up to writer batchSize (500) rows. The worst scope was 147 statements —
// far under the statement cap, and roughly 73,500 rows in one transaction.
//
// This is the shape of that scope. It fails before the phase budget exists.

// recordingGroupExecutor captures the statement count and row count handed to
// each ExecuteGroup call, which is one NornicDB transaction.
type recordingGroupExecutor struct {
	statementsPerCall []int
	rowsPerCall       []int
	singleStatements  int
	// events records grouped and singleton dispatches in the order they
	// happened, so a test can assert ordering and not merely counts.
	events []string
}

// Execute satisfies cypher.Executor. The structural-edge phase runs through the
// grouped path, so a call here would mean the phase stopped being grouped.
func (r *recordingGroupExecutor) Execute(_ context.Context, stmt sourcecypher.Statement) error {
	r.singleStatements++
	if stmt.Operation == sourcecypher.OperationCanonicalRetract {
		r.events = append(r.events, "retract")
	} else {
		r.events = append(r.events, "single")
	}
	return nil
}

func (r *recordingGroupExecutor) ExecuteGroup(_ context.Context, stmts []sourcecypher.Statement) error {
	rows := 0
	for _, stmt := range stmts {
		if raw, ok := stmt.Parameters["rows"]; ok {
			if list, ok := raw.([]map[string]any); ok {
				rows += len(list)
			}
		}
	}
	r.statementsPerCall = append(r.statementsPerCall, len(stmts))
	r.rowsPerCall = append(r.rowsPerCall, rows)
	r.events = append(r.events, "group")
	return nil
}

// structuralEdgeStatements builds `count` statements each carrying `rowsEach`
// rows, tagged as the structural_edges phase the way the canonical writer tags
// them.
func structuralEdgeStatements(count, rowsEach int) []sourcecypher.Statement {
	stmts := make([]sourcecypher.Statement, 0, count)
	for i := 0; i < count; i++ {
		rows := make([]map[string]any, 0, rowsEach)
		for j := 0; j < rowsEach; j++ {
			rows = append(rows, map[string]any{
				"file_path":   "pkg/service/handler.go",
				"module_name": "github.com/acme/lib-common",
			})
		}
		stmts = append(stmts, sourcecypher.Statement{
			Cypher: "UNWIND $rows AS row\nMATCH (f:File {path: row.file_path})\n" +
				"MATCH (m:Module {name: row.module_name})\nMERGE (f)-[r:IMPORTS]->(m)",
			Parameters: map[string]any{
				"rows":                                 rows,
				sourcecypher.StatementMetadataPhaseKey: sourcecypher.CanonicalPhaseStructuralEdges,
			},
		})
	}
	return stmts
}

// The worst observed failing scope: r_962c9686 at 147 statements. Before the
// phase budget it lands in a single transaction; the defect is that no chunk
// boundary falls anywhere inside it.
func TestStructuralEdgePhaseBoundsTransactionSize(t *testing.T) {
	const (
		statements  = 147
		rowsEach    = 500
		wantMaxRows = DefaultStructuralEdgePhaseStatements * rowsEach
	)

	recorder := &recordingGroupExecutor{}
	executor := PhaseGroupExecutor{Inner: recorder}

	if err := executor.ExecutePhaseGroup(context.Background(), structuralEdgeStatements(statements, rowsEach)); err != nil {
		t.Fatalf("ExecutePhaseGroup() error = %v", err)
	}

	if len(recorder.statementsPerCall) == 0 {
		t.Fatal("no ExecuteGroup calls recorded")
	}

	// The budget must subdivide the grouped path, not push statements onto the
	// singleton Execute path. Without this assertion a regression that ran every
	// statement as its own Execute call would still satisfy the bounds below,
	// because one ExecuteGroup call is enough to populate the recorder.
	if recorder.singleStatements != 0 {
		t.Errorf("%d statements ran through Execute, want all through ExecuteGroup",
			recorder.singleStatements)
	}
	for i, got := range recorder.statementsPerCall {
		if got > DefaultStructuralEdgePhaseStatements {
			t.Errorf("transaction %d carried %d statements, want <= %d",
				i, got, DefaultStructuralEdgePhaseStatements)
		}
	}
	for i, got := range recorder.rowsPerCall {
		if got > wantMaxRows {
			t.Errorf("transaction %d carried %d rows, want <= %d (%d statements x %d rows)",
				i, got, wantMaxRows, DefaultStructuralEdgePhaseStatements, rowsEach)
		}
	}

	// Every statement must still be executed exactly once: bounding the
	// transaction must not drop work.
	total := 0
	for _, got := range recorder.statementsPerCall {
		total += got
	}
	if total != statements {
		t.Fatalf("executed %d statements in total, want %d", total, statements)
	}
}

// The phase budget must be reachable through the executor field so an operator
// can tune it, and must fall back to the default when unset.
func TestStructuralEdgePhaseStatementLimit(t *testing.T) {
	stmts := structuralEdgeStatements(1, 1)

	if got := (PhaseGroupExecutor{}).PhaseGroupStatementLimit(stmts); got != DefaultStructuralEdgePhaseStatements {
		t.Fatalf("default limit = %d, want %d", got, DefaultStructuralEdgePhaseStatements)
	}

	tuned := PhaseGroupExecutor{StructuralEdgeMaxStatements: 3}
	if got := tuned.PhaseGroupStatementLimit(stmts); got != 3 {
		t.Fatalf("tuned limit = %d, want 3", got)
	}

	// A non-positive override must not disable the budget and fall back to the
	// broad 500-statement default, which is the defect this issue fixes.
	zeroed := PhaseGroupExecutor{StructuralEdgeMaxStatements: 0}
	if got := zeroed.PhaseGroupStatementLimit(stmts); got != DefaultStructuralEdgePhaseStatements {
		t.Fatalf("zero override limit = %d, want %d", got, DefaultStructuralEdgePhaseStatements)
	}
}

// The structural-edge phase is not always pure MERGE. The Atlantis, Flux,
// GitLab, Helm, and Kustomize family-edge builders emit Drain-marked retracts
// interleaved with their upserts, and the retract for a family MUST commit
// before that family's own upserts. The PR for issue #6070 argued that
// narrowing the budget from 500 to 5 cannot reorder those, because
// executeGroupedChunksWithDrain walks in emitted order and flushes pending
// merges before each retract. Argument is not proof: this pins the invariant so
// a later refactor of flushPending fails here rather than in a corpus run.
func TestStructuralEdgeBudgetPreservesRetractOrdering(t *testing.T) {
	const (
		leading  = 7
		trailing = 7
		rowsEach = 10
	)

	stmts := structuralEdgeStatements(leading, rowsEach)
	retract := sourcecypher.Statement{
		Cypher:    "MATCH (a:AtlantisProject)-[r:MANAGES]->() DELETE r",
		Operation: sourcecypher.OperationCanonicalRetract,
		Drain:     true,
		Parameters: map[string]any{
			sourcecypher.StatementMetadataPhaseKey: sourcecypher.CanonicalPhaseStructuralEdges,
		},
	}
	stmts = append(stmts, retract)
	stmts = append(stmts, structuralEdgeStatements(trailing, rowsEach)...)

	recorder := &recordingGroupExecutor{}
	executor := PhaseGroupExecutor{Inner: recorder}

	if err := executor.ExecutePhaseGroup(context.Background(), stmts); err != nil {
		t.Fatalf("ExecutePhaseGroup() error = %v", err)
	}

	retractIndex := -1
	for i, event := range recorder.events {
		if event == "retract" {
			if retractIndex != -1 {
				t.Fatalf("recorded %d retracts, want exactly 1: %v", i, recorder.events)
			}
			retractIndex = i
		}
	}
	if retractIndex == -1 {
		t.Fatalf("no retract dispatched, events = %v", recorder.events)
	}

	// Every leading MERGE must have committed before the retract, and every
	// trailing MERGE after it. With a 5-statement budget the 7 leading
	// statements cannot fit in one transaction, so this also proves the flush
	// respects the narrowed budget instead of deferring work past the retract.
	groupsBefore, groupsAfter := 0, 0
	for i, event := range recorder.events {
		if event != "group" {
			continue
		}
		if i < retractIndex {
			groupsBefore++
		} else {
			groupsAfter++
		}
	}
	if groupsBefore < 2 {
		t.Errorf("%d grouped chunks before the retract, want >= 2 (7 statements at a 5 budget): %v",
			groupsBefore, recorder.events)
	}
	if groupsAfter < 2 {
		t.Errorf("%d grouped chunks after the retract, want >= 2: %v", groupsAfter, recorder.events)
	}

	total := 0
	for _, got := range recorder.statementsPerCall {
		if got > DefaultStructuralEdgePhaseStatements {
			t.Errorf("transaction carried %d statements, want <= %d",
				got, DefaultStructuralEdgePhaseStatements)
		}
		total += got
	}
	if total != leading+trailing {
		t.Fatalf("executed %d merge statements, want %d", total, leading+trailing)
	}
}

// Unrelated phases must keep the broad default: this budget is scoped to
// structural_edges, not a global narrowing.
func TestStructuralEdgeBudgetDoesNotNarrowOtherPhases(t *testing.T) {
	stmts := []sourcecypher.Statement{{
		Cypher: "UNWIND $rows AS row MERGE (n:TerraformStateResource {uid: row.uid})",
		Parameters: map[string]any{
			"rows":                                 []map[string]any{{"uid": "tf-1"}},
			sourcecypher.StatementMetadataPhaseKey: "terraform_state",
		},
	}}

	if got := (PhaseGroupExecutor{}).PhaseGroupStatementLimit(stmts); got != DefaultPhaseGroupStatements {
		t.Fatalf("terraform_state limit = %d, want the broad default %d", got, DefaultPhaseGroupStatements)
	}
}
