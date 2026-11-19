// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package writer

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/sharedintent"
	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
	"github.com/eshu-hq/eshu/go/internal/storage/cypher/canonical/node"
)

// TestCodeCallTemplatesReportMatchedCount is the #10938 RED regression: every
// code-call edge template must report how many rows actually matched, atomically
// with the write, so a MATCH-only row that binds nothing is visible instead of
// completing silently. Each template is one UNWIND statement; the RETURN is part
// of that same statement, so the statement count per WriteEdges call is unchanged.
func TestCodeCallTemplatesReportMatchedCount(t *testing.T) {
	t.Parallel()

	templates := map[string]string{
		"call":               node.BatchCanonicalCodeCallUpsertCypher,
		"reference":          node.BatchCanonicalCodeReferenceUpsertCypher,
		"metaclass":          node.BatchCanonicalMetaclassUpsertCypher,
		"instantiates":       node.BatchCanonicalInstantiatesUpsertCypher,
		"label-call":         buildLabelScopedCodeCallCypher("Function", "Function"),
		"label-reference":    buildLabelScopedCodeReferenceCypher("Function", "Class"),
		"label-instantiates": buildLabelScopedInstantiatesCypher("Function", "Class"),
	}
	for name, cypher := range templates {
		if !strings.Contains(cypher, "RETURN count(rel) AS matched") {
			t.Errorf("%s template does not report matched rows, want `RETURN count(rel) AS matched` atomic with the write:\n%s", name, cypher)
		}
		if got := strings.Count(cypher, "UNWIND $rows AS row"); got != 1 {
			t.Errorf("%s template has %d UNWIND clauses, want exactly 1 (no added statements)", name, got)
		}
	}
}

// countedWriteFakeExecutor is a fake Executor with a scripted matched count
// per executed statement. groupCounts serves ExecuteCountedWriteGroup in
// order; singleCounts serves sequential ExecuteCountedWrite in order. When the
// corresponding queue is empty the fake reports a full match derived from the
// statement's own row count, so tests only script the interesting statements.
type countedWriteFakeExecutor struct {
	singleCounts []int64
	groupCounts  [][]int64
	groupCalls   int
	singleCalls  int
	stmts        []sourcecypher.Statement
}

func (e *countedWriteFakeExecutor) Execute(context.Context, sourcecypher.Statement) error {
	return nil
}

func (e *countedWriteFakeExecutor) ExecuteCountedWrite(_ context.Context, stmt sourcecypher.Statement) (int64, error) {
	e.singleCalls++
	e.stmts = append(e.stmts, stmt)
	if len(e.singleCounts) > 0 {
		count := e.singleCounts[0]
		e.singleCounts = e.singleCounts[1:]
		return count, nil
	}
	rows, _ := sourcecypher.StatementRowsCount(stmt)
	return int64(rows), nil
}

func (e *countedWriteFakeExecutor) ExecuteCountedWriteGroup(_ context.Context, stmts []sourcecypher.Statement) ([]int64, error) {
	e.groupCalls++
	e.stmts = append(e.stmts, stmts...)
	if len(e.groupCounts) > 0 {
		counts := e.groupCounts[0]
		e.groupCounts = e.groupCounts[1:]
		return counts, nil
	}
	counts := make([]int64, 0, len(stmts))
	for _, stmt := range stmts {
		rows, _ := sourcecypher.StatementRowsCount(stmt)
		counts = append(counts, int64(rows))
	}
	return counts, nil
}

// sequentialCountedFakeExecutor exposes only the single-statement counted
// seam, forcing the writer down its sequential path.
type sequentialCountedFakeExecutor struct {
	countedWriteFakeExecutor
}

func validCodeCallRow(intentID string) sharedintent.Row {
	return sharedintent.Row{
		IntentID:     intentID,
		RepositoryID: "repo-app",
		PartitionKey: "p0",
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		Payload: map[string]any{
			"caller_entity_id": "uid:caller",
			"callee_entity_id": "uid:callee",
		},
	}
}

func validCodeReferenceRow(intentID string) sharedintent.Row {
	row := validCodeCallRow(intentID)
	row.Payload["relationship_type"] = "REFERENCES"
	return row
}

// TestEdgeWriterWriteEdgesCodeCallShortfallParksBatch is the #10938 GREEN
// regression: a CALLS batch whose endpoints are absent (first-generation
// absence, or a non-atomic re-projection clear) matches fewer rows than
// submitted. The write must fail retryably with the non-counting readiness
// class so the intent stays open — never complete with zero edges.
func TestEdgeWriterWriteEdgesCodeCallShortfallParksBatch(t *testing.T) {
	t.Parallel()

	executor := &countedWriteFakeExecutor{groupCounts: [][]int64{{1}}}
	report, err := NewEdgeWriter(executor, 0).WriteEdges(
		context.Background(), reducercontract.DomainCodeCalls,
		[]sharedintent.Row{validCodeCallRow("i-1"), validCodeCallRow("i-2")},
		"parser/code-calls",
	)
	if err == nil {
		t.Fatal("WriteEdges() with 1 matched of 2 submitted code-call rows succeeded silently, want the retryable park error")
	}
	if !reducer.IsRetryable(err) {
		t.Fatalf("WriteEdges() error = %v, want retryable so the rows stay queued", err)
	}
	class, ok := failureClassOf(err)
	if !ok || class != reducer.SharedEdgeTargetNotReadyFailureClass {
		t.Fatalf("failure class = %q (found=%v), want %q so the queue defers without spending the retry budget",
			class, ok, reducer.SharedEdgeTargetNotReadyFailureClass)
	}
	if report.SubmittedRows != 2 || report.MatchedRows != 1 {
		t.Fatalf("report = %+v, want SubmittedRows=2 MatchedRows=1", report)
	}
}

// TestEdgeWriterWriteEdgesCodeCallFullMatchCompletes is the steady-state
// half of #10938: when every row matches, the batch completes and the report
// carries the verified counts.
func TestEdgeWriterWriteEdgesCodeCallFullMatchCompletes(t *testing.T) {
	t.Parallel()

	executor := &countedWriteFakeExecutor{}
	report, err := NewEdgeWriter(executor, 0).WriteEdges(
		context.Background(), reducercontract.DomainCodeCalls,
		[]sharedintent.Row{validCodeCallRow("i-1"), validCodeCallRow("i-2")},
		"parser/code-calls",
	)
	if err != nil {
		t.Fatalf("WriteEdges() with a full match = %v, want nil", err)
	}
	if report.SubmittedRows != 2 || report.MatchedRows != 2 {
		t.Fatalf("report = %+v, want SubmittedRows=2 MatchedRows=2", report)
	}
	// Statement-count pin: the same batching as before #10938 — one UNWIND
	// statement for the batch, no added probe statements.
	if len(executor.stmts) != 1 {
		t.Fatalf("dispatched %d statements, want exactly 1 (statement count unchanged)", len(executor.stmts))
	}
	for _, stmt := range executor.stmts {
		if !strings.Contains(stmt.Cypher, "UNWIND $rows AS row") {
			t.Fatalf("dispatched a non-batch statement, want only the code-call UNWIND write:\n%s", stmt.Cypher)
		}
		if !strings.Contains(stmt.Cypher, "RETURN count(rel) AS matched") {
			t.Fatalf("dispatched statement does not report matched rows:\n%s", stmt.Cypher)
		}
	}
}

// TestEdgeWriterWriteEdgesCodeCallCountOnlyShortfallCompletes pins the D6
// census gate: a REFERENCES shortfall (no steady-state census yet, #5116)
// ships count+telemetry and completes — it must not park the batch.
func TestEdgeWriterWriteEdgesCodeCallCountOnlyShortfallCompletes(t *testing.T) {
	t.Parallel()

	executor := &countedWriteFakeExecutor{groupCounts: [][]int64{{0}}}
	report, err := NewEdgeWriter(executor, 0).WriteEdges(
		context.Background(), reducercontract.DomainCodeCalls,
		[]sharedintent.Row{validCodeReferenceRow("i-1")},
		"parser/code-calls",
	)
	if err != nil {
		t.Fatalf("WriteEdges() with a count-only REFERENCES shortfall = %v, want nil (completes with telemetry)", err)
	}
	if report.SubmittedRows != 1 || report.MatchedRows != 0 {
		t.Fatalf("report = %+v, want SubmittedRows=1 MatchedRows=0", report)
	}
}

// TestEdgeWriterWriteEdgesCodeCallSurplusCompletes pins the duplicate-uid
// anomaly direction: more matches than submissions still completes, because
// the edges did commit.
func TestEdgeWriterWriteEdgesCodeCallSurplusCompletes(t *testing.T) {
	t.Parallel()

	executor := &countedWriteFakeExecutor{groupCounts: [][]int64{{3}}}
	report, err := NewEdgeWriter(executor, 0).WriteEdges(
		context.Background(), reducercontract.DomainCodeCalls,
		[]sharedintent.Row{validCodeCallRow("i-1"), validCodeCallRow("i-2")},
		"parser/code-calls",
	)
	if err != nil {
		t.Fatalf("WriteEdges() with a matched surplus = %v, want nil (edges committed)", err)
	}
	if report.SubmittedRows != 2 || report.MatchedRows != 3 {
		t.Fatalf("report = %+v, want SubmittedRows=2 MatchedRows=3", report)
	}
}

// TestEdgeWriterWriteEdgesCodeCallWithoutSeamFailsClosed pins the fail-closed
// half of #10938: an executor without the counted-write seam must fail the
// code-call batch retryably, never complete it silently.
func TestEdgeWriterWriteEdgesCodeCallWithoutSeamFailsClosed(t *testing.T) {
	t.Parallel()

	executor := &targetMissProbeExecutor{probeAllPresent: true}
	_, err := NewEdgeWriter(executor, 0).WriteEdges(
		context.Background(), reducercontract.DomainCodeCalls,
		[]sharedintent.Row{validCodeCallRow("i-1")},
		"parser/code-calls",
	)
	if err == nil {
		t.Fatal("WriteEdges() on an executor without the counted-write seam succeeded, want the fail-closed counting error")
	}
	if !reducer.IsRetryable(err) {
		t.Fatalf("WriteEdges() error = %v, want retryable so the unverified batch stays queued", err)
	}
	var counting *codeCallCountingError
	if !errors.As(err, &counting) {
		t.Fatalf("WriteEdges() error = %#v, want a *codeCallCountingError", err)
	}
}

// TestEdgeWriterWriteEdgesCodeCallSequentialShortfallParksBatch covers the
// non-grouped executor path: one counted write per statement, same park
// contract.
func TestEdgeWriterWriteEdgesCodeCallSequentialShortfallParksBatch(t *testing.T) {
	t.Parallel()

	inner := &sequentialCountedFakeExecutor{}
	inner.singleCounts = []int64{0}
	_, err := NewEdgeWriter(executeOnlyCountedAdapter{inner: inner}, 0).WriteEdges(
		context.Background(), reducercontract.DomainCodeCalls,
		[]sharedintent.Row{validCodeCallRow("i-1")},
		"parser/code-calls",
	)
	if err == nil {
		t.Fatal("WriteEdges() sequential with 0 matched of 1 submitted row succeeded silently, want the park error")
	}
	if !reducer.IsRetryable(err) {
		t.Fatalf("WriteEdges() error = %v, want retryable", err)
	}
}

// executeOnlyCountedAdapter strips the grouped seam from a counted executor,
// forcing the writer down its sequential counted path.
type executeOnlyCountedAdapter struct {
	inner *sequentialCountedFakeExecutor
}

func (e executeOnlyCountedAdapter) Execute(ctx context.Context, stmt sourcecypher.Statement) error {
	return e.inner.Execute(ctx, stmt)
}

func (e executeOnlyCountedAdapter) ExecuteCountedWrite(ctx context.Context, stmt sourcecypher.Statement) (int64, error) {
	return e.inner.ExecuteCountedWrite(ctx, stmt)
}
