// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// Hermetic guard for the #9871 post-admission statistics gate. These tests
// need no database: they script the pg_stat gate read and the ANALYZE exec
// and assert the helper's outcome, its statement order, and its non-fatal
// behavior. The live proof that the gate flips the real plan lives in
// control_claim_stats_live_test.go.

// statsGateRecorder scripts one gate query result plus exec behavior and
// records every statement the helper issues.
type statsGateRecorder struct {
	mu           sync.Mutex
	statements   []string
	gateRow      []any
	gateErr      error
	execErr      error
	execAffected int64
	noGateRows   bool
}

func (r *statsGateRecorder) ExecContext(_ context.Context, query string, _ ...any) (sql.Result, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.statements = append(r.statements, query)
	if r.execErr != nil {
		return nil, r.execErr
	}
	return fakeResultWithRowsAffected{rowsAffected: r.execAffected}, nil
}

func (r *statsGateRecorder) QueryContext(_ context.Context, query string, _ ...any) (db.Rows, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.statements = append(r.statements, query)
	if r.gateErr != nil {
		return nil, r.gateErr
	}
	if r.noGateRows {
		return &queueFakeRows{}, nil
	}
	return &queueFakeRows{rows: [][]any{r.gateRow}}, nil
}

func (r *statsGateRecorder) issued(needle string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, statement := range r.statements {
		if strings.Contains(statement, needle) {
			return true
		}
	}
	return false
}

func (r *statsGateRecorder) statementCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.statements)
}

// TestWorkflowStatsGateAnalyzesNeverAnalyzedTable pins the core contract: a
// table with no manual and no auto ANALYZE gets exactly one ANALYZE
// (SKIP_LOCKED), and the outcome is analyzed.
func TestWorkflowStatsGateAnalyzesNeverAnalyzedTable(t *testing.T) {
	t.Parallel()

	recorder := &statsGateRecorder{gateRow: []any{false, false, int64(0), float64(0)}}
	store := NewWorkflowControlStore(recorder)

	if got := store.analyzeWorkflowWorkItemsOnce(context.Background(), telemetry.WorkflowStatsAnalyzeSiteEnqueue, 100); got != telemetry.WorkflowStatsAnalyzeOutcomeAnalyzed {
		t.Fatalf("analyzeWorkflowWorkItemsOnce() = %q, want %q", got, telemetry.WorkflowStatsAnalyzeOutcomeAnalyzed)
	}
	if !recorder.issued("ANALYZE (SKIP_LOCKED) workflow_work_items") {
		t.Fatal("never-analyzed table ran no ANALYZE; the gate must fire exactly once")
	}
}

// TestWorkflowStatsGateSkipsAnalyzedTable pins the steady state: a table
// that already carries statistics runs no ANALYZE, and the outcome is
// skipped. Every admission after the first takes this path.
func TestWorkflowStatsGateSkipsAnalyzedTable(t *testing.T) {
	t.Parallel()

	for _, row := range [][]any{
		{true, false, int64(0), float64(100000)},
		{false, true, int64(0), float64(100000)},
		{true, true, int64(0), float64(100000)},
	} {
		recorder := &statsGateRecorder{gateRow: row}
		store := NewWorkflowControlStore(recorder)

		if got := store.analyzeWorkflowWorkItemsOnce(context.Background(), telemetry.WorkflowStatsAnalyzeSiteGuarded, 10); got != telemetry.WorkflowStatsAnalyzeOutcomeSkipped {
			t.Fatalf("analyzeWorkflowWorkItemsOnce(%v) = %q, want %q", row, got, telemetry.WorkflowStatsAnalyzeOutcomeSkipped)
		}
		if recorder.issued("ANALYZE") {
			t.Fatalf("analyzed table %v ran ANALYZE; the gate must stay silent on warm statistics", row)
		}
	}
}

// TestWorkflowStatsGateSkipsEmptyAdmission pins the empty-table rule: an
// admission that landed nothing issues no statement at all, because ANALYZE
// on an empty table records nothing.
func TestWorkflowStatsGateSkipsEmptyAdmission(t *testing.T) {
	t.Parallel()

	recorder := &statsGateRecorder{gateRow: []any{false, false, int64(0), float64(0)}}
	store := NewWorkflowControlStore(recorder)

	store.maybeAnalyzeWorkflowWorkItems(context.Background(), telemetry.WorkflowStatsAnalyzeSiteEnqueue, 0)
	if n := recorder.statementCount(); n != 0 {
		t.Fatalf("empty admission issued %d statements, want none: without landed rows there is no statistics debt", n)
	}
}

// TestWorkflowStatsGateFailsClosedOnGateReadError pins the non-fatal rule:
// an unreadable gate is counted as failed and never aborts the admission
// that already committed. maybeAnalyzeWorkflowWorkItems returns nothing,
// so the test asserts the absence of a panic plus the absence of ANALYZE.
func TestWorkflowStatsGateFailsClosedOnGateReadError(t *testing.T) {
	t.Parallel()

	recorder := &statsGateRecorder{gateErr: errors.New("connection reset")}
	store := NewWorkflowControlStore(recorder)

	if got := store.analyzeWorkflowWorkItemsOnce(context.Background(), telemetry.WorkflowStatsAnalyzeSiteScanner, 50); got != telemetry.WorkflowStatsAnalyzeOutcomeFailed {
		t.Fatalf("analyzeWorkflowWorkItemsOnce() = %q, want %q", got, telemetry.WorkflowStatsAnalyzeOutcomeFailed)
	}
	if recorder.issued("ANALYZE") {
		t.Fatal("unreadable gate ran ANALYZE; without statistics state the helper must not act")
	}
	// The production entry point swallows the outcome the same way: no
	// return, no panic, admission already committed.
	store.maybeAnalyzeWorkflowWorkItems(context.Background(), telemetry.WorkflowStatsAnalyzeSiteScanner, 50)
}

// TestWorkflowStatsGateFailsWithoutMaintain pins the permission rule: a role
// without MAINTAIN gets an ANALYZE error, the outcome is failed, and the
// admission stands. The owner measured this shape against a restricted role;
// the hermetic form keeps it in CI.
func TestWorkflowStatsGateFailsWithoutMaintain(t *testing.T) {
	t.Parallel()

	recorder := &statsGateRecorder{
		gateRow: []any{false, false, int64(0), float64(0)},
		execErr: errors.New(`permission denied for table workflow_work_items`),
	}
	store := NewWorkflowControlStore(recorder)

	if got := store.analyzeWorkflowWorkItemsOnce(context.Background(), telemetry.WorkflowStatsAnalyzeSiteEnqueue, 7); got != telemetry.WorkflowStatsAnalyzeOutcomeFailed {
		t.Fatalf("analyzeWorkflowWorkItemsOnce() = %q, want %q", got, telemetry.WorkflowStatsAnalyzeOutcomeFailed)
	}
}

// TestWorkflowStatsGateCountsMissingStatsRow pins the schema-missing rule:
// a database whose catalog has no row for the table counts the outcome as
// failed and runs no ANALYZE, because there are no statistics to refresh.
// The admission stands: the gate never aborts a committed write.
func TestWorkflowStatsGateCountsMissingStatsRow(t *testing.T) {
	t.Parallel()

	recorder := &statsGateRecorder{noGateRows: true}
	store := NewWorkflowControlStore(recorder)

	if got := store.analyzeWorkflowWorkItemsOnce(context.Background(), telemetry.WorkflowStatsAnalyzeSiteEnqueue, 100); got != telemetry.WorkflowStatsAnalyzeOutcomeFailed {
		t.Fatalf("analyzeWorkflowWorkItemsOnce() = %q, want %q", got, telemetry.WorkflowStatsAnalyzeOutcomeFailed)
	}
	if recorder.issued("ANALYZE") {
		t.Fatal("missing statistics row ran ANALYZE; without a catalog row there is nothing to refresh")
	}
}

// TestWorkflowStatsGateAnalyzesStatlessTable pins the #9926 staleness rule:
// a table whose last ANALYZE covered a trivially small base (reltuples under
// the measured flip boundary) while large growth landed since must run
// ANALYZE, even though the never-analyzed gate passes. Measured on PG 18:
// analyzed-empty then +100k plans all five scans on the family index;
// analyzed base 1 degrades to claimable-index sorts; base 10 and up holds
// the fair-claim plan through +185% growth and 60% churn.
func TestWorkflowStatsGateAnalyzesStatlessTable(t *testing.T) {
	t.Parallel()

	recorder := &statsGateRecorder{gateRow: []any{true, false, int64(100000), float64(0)}}
	store := NewWorkflowControlStore(recorder)

	if got := store.analyzeWorkflowWorkItemsOnce(context.Background(), telemetry.WorkflowStatsAnalyzeSiteEnqueue, 100000); got != telemetry.WorkflowStatsAnalyzeOutcomeAnalyzed {
		t.Fatalf("analyzeWorkflowWorkItemsOnce() = %q, want %q", got, telemetry.WorkflowStatsAnalyzeOutcomeAnalyzed)
	}
	if !recorder.issued("ANALYZE (SKIP_LOCKED) workflow_work_items") {
		t.Fatal("statless analyzed table ran no ANALYZE; growth past a trivial base must refresh statistics")
	}
}

// TestWorkflowStatsGateSkipsHealthyGrowth pins the other half of #9926: an
// analyzed table whose base is non-trivial keeps skipping the ANALYZE no
// matter how much growth landed since, because measured plans hold the
// fair-claim index through +185% inserts and 60% update churn.
func TestWorkflowStatsGateSkipsHealthyGrowth(t *testing.T) {
	t.Parallel()

	recorder := &statsGateRecorder{gateRow: []any{true, false, int64(185000), float64(100000)}}
	store := NewWorkflowControlStore(recorder)

	if got := store.analyzeWorkflowWorkItemsOnce(context.Background(), telemetry.WorkflowStatsAnalyzeSiteEnqueue, 85000); got != telemetry.WorkflowStatsAnalyzeOutcomeSkipped {
		t.Fatalf("analyzeWorkflowWorkItemsOnce() = %q, want %q", got, telemetry.WorkflowStatsAnalyzeOutcomeSkipped)
	}
	if recorder.issued("ANALYZE") {
		t.Fatal("healthy-growth analyzed table ran ANALYZE; the gate must stay silent while plans hold")
	}
}

// TestWorkflowStatsGateSkipsDribbleOntoTrivialBase pins the self-limiting
// half of the staleness rule: growth that does not exceed a trivially small
// analyzed base runs no ANALYZE, so dribbles onto a genuinely small table do
// not pay an ANALYZE on every admission. Growth accumulates in
// n_mod_since_analyze until it passes the base, when the stale rule fires.
func TestWorkflowStatsGateSkipsDribbleOntoTrivialBase(t *testing.T) {
	t.Parallel()

	for _, row := range []struct {
		name     string
		gateRow  []any
		admitted int
	}{
		{"dribble under base", []any{true, false, int64(4), float64(5)}, 1},
		{"growth at equality", []any{true, false, int64(4), float64(5)}, 1},
		{"base at ceiling", []any{true, false, int64(100000), float64(10)}, 100000},
	} {
		recorder := &statsGateRecorder{gateRow: row.gateRow}
		store := NewWorkflowControlStore(recorder)

		if got := store.analyzeWorkflowWorkItemsOnce(context.Background(), telemetry.WorkflowStatsAnalyzeSiteEnqueue, row.admitted); got != telemetry.WorkflowStatsAnalyzeOutcomeSkipped {
			t.Fatalf("%s: analyzeWorkflowWorkItemsOnce() = %q, want %q", row.name, got, telemetry.WorkflowStatsAnalyzeOutcomeSkipped)
		}
		if recorder.issued("ANALYZE") {
			t.Fatalf("%s: ran ANALYZE; the gate must stay silent below the stale boundary", row.name)
		}
	}
}

// TestEnqueueWorkItemsRunsStatsGateAfterBatches pins the unguarded wire: a
// bulk admission through EnqueueWorkItems ends with the statistics gate, and
// the gate fires because rows landed on a never-analyzed table.
func TestEnqueueWorkItemsRunsStatsGateAfterBatches(t *testing.T) {
	t.Parallel()

	recorder := &statsGateRecorder{
		gateRow:      []any{false, false, int64(0), float64(0)},
		execAffected: 2,
	}
	store := NewWorkflowControlStore(recorder)
	now := time.Date(2026, time.November, 2, 12, 0, 0, 0, time.UTC)
	if err := store.EnqueueWorkItems(context.Background(), []workflow.WorkItem{
		awsScheduledWorkItem("stats-gate-wire-run", "lambda", now),
		awsScheduledWorkItem("stats-gate-wire-run", "s3", now),
	}); err != nil {
		t.Fatalf("EnqueueWorkItems() error = %v, want nil", err)
	}
	if !recorder.issued("ANALYZE (SKIP_LOCKED) workflow_work_items") {
		t.Fatal("bulk enqueue ran no post-admission ANALYZE; the #9871 wire is missing")
	}
}
