// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// Hermetic companion to workflow_control_open_targets_lock_live_test.go.
//
// The live proof needs ESHU_POSTGRES_DSN and skips without one, so CI would not
// notice the collector-instance planning lock disappearing. These tests need no
// database: they record the statement order the guard issues and assert the
// lock is taken before planned targets are read. Reversing the two, which the
// live race catches as duplicate rows, is invisible to every other hermetic
// test in this package.

// orderedStatementRecorder records exec and query statements in one sequence,
// which the shared fakeExecQueryer does not: it keeps execs and queries in
// separate slices, so ordering between an exec and a query cannot be asserted
// against it.
type orderedStatementRecorder struct {
	mu         sync.Mutex
	statements []recordedStatement
	rows       []queueFakeRows
	execRows   []int64
	committed  bool
	rolledBack bool
}

type recordedStatement struct {
	query string
	args  []any
}

func (r *orderedStatementRecorder) Begin(context.Context) (Transaction, error) {
	return r, nil
}

func (r *orderedStatementRecorder) ExecContext(_ context.Context, query string, args ...any) (sql.Result, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.statements = append(r.statements, recordedStatement{query: query, args: args})
	rowsAffected := int64(1)
	if len(r.execRows) > 0 {
		rowsAffected = r.execRows[0]
		r.execRows = r.execRows[1:]
	}
	return fakeResultWithRowsAffected{rowsAffected: rowsAffected}, nil
}

func (r *orderedStatementRecorder) QueryContext(_ context.Context, query string, args ...any) (Rows, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.statements = append(r.statements, recordedStatement{query: query, args: args})
	if len(r.rows) == 0 {
		return &queueFakeRows{}, nil
	}
	next := r.rows[0]
	r.rows = r.rows[1:]
	return &next, nil
}

// Commit, Rollback, and their readers take the same mutex the statement
// recording does. Nothing in this file drives the recorder from two goroutines
// today, but a recorder whose fields are half-guarded is a race waiting for the
// first concurrent reuse, and the race detector would report it there rather
// than here.
func (r *orderedStatementRecorder) Commit() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.committed = true
	return nil
}

func (r *orderedStatementRecorder) Rollback() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.rolledBack = true
	return nil
}

// didCommit reports whether the recorded transaction committed.
func (r *orderedStatementRecorder) didCommit() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.committed
}

// didRollBack reports whether the recorded transaction rolled back.
func (r *orderedStatementRecorder) didRollBack() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.rolledBack
}

// indexOfStatement returns the position of the first recorded statement
// containing needle, or -1.
func (r *orderedStatementRecorder) indexOfStatement(needle string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i, statement := range r.statements {
		if strings.Contains(statement.query, needle) {
			return i
		}
	}
	return -1
}

// argsOfStatements returns the arguments of every recorded statement containing
// needle, in the order the statements were issued.
func (r *orderedStatementRecorder) argsOfStatements(needle string) []any {
	r.mu.Lock()
	defer r.mu.Unlock()
	var args []any
	for _, statement := range r.statements {
		if strings.Contains(statement.query, needle) {
			args = append(args, statement.args...)
		}
	}
	return args
}

func workflowPlanningLockFixture(t *testing.T) (workflow.Run, []workflow.WorkItem) {
	t.Helper()
	now := time.Date(2026, time.May, 21, 12, 0, 0, 0, time.UTC)
	run := workflow.Run{
		RunID:              "aws:collector-aws:schedule:continuous-20260521T120000Z",
		TriggerKind:        workflow.TriggerKindSchedule,
		Status:             workflow.RunStatusCollectionPending,
		RequestedScopeSet:  "[]",
		RequestedCollector: string(scope.CollectorAWS),
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	items := []workflow.WorkItem{
		awsScheduledWorkItem(run.RunID, "lambda", now),
		awsScheduledWorkItem(run.RunID, "s3", now),
	}
	return run, items
}

// TestWorkflowControlStoreGuardedRunTakesPlanningLockBeforeReadingTargets is the
// in-CI guard for #4586. It fails if the advisory lock is removed, and also if
// it is merely moved after the planned-target read -- the reordering that
// reopens the read-then-write race while every other hermetic test in this
// package stays green.
func TestWorkflowControlStoreGuardedRunTakesPlanningLockBeforeReadingTargets(t *testing.T) {
	t.Parallel()

	recorder := &orderedStatementRecorder{
		rows: []queueFakeRows{
			{rows: [][]any{{false}}},  // terminal-run check
			{rows: [][]any{{0}, {1}}}, // both targets eligible
		},
	}
	store := NewWorkflowControlStore(recorder)
	run, items := workflowPlanningLockFixture(t)

	if _, err := store.CreateRunWithWorkItemsIfNoOpenTargets(context.Background(), run, items); err != nil {
		t.Fatalf("CreateRunWithWorkItemsIfNoOpenTargets() error = %v, want nil", err)
	}

	lockAt := recorder.indexOfStatement("pg_advisory_xact_lock")
	if lockAt < 0 {
		t.Fatal("the guard never took the collector-instance planning lock; without it two coordinators both read \"nothing planned\" and both insert the same target (#4586)")
	}
	readAt := recorder.indexOfStatement("planned_targets")
	if readAt < 0 {
		t.Fatal("the guard never read planned targets")
	}
	if lockAt > readAt {
		t.Fatalf("planning lock taken at statement %d, planned-target read at %d: the lock must come first, or both coordinators read before either writes (#4586)", lockAt, readAt)
	}
	insertAt := recorder.indexOfStatement("INSERT INTO workflow_work_items")
	if insertAt < 0 {
		t.Fatal("the guard never inserted the eligible work items")
	}
	if lockAt > insertAt {
		t.Fatalf("planning lock taken at statement %d, work-item insert at %d: the lock must cover the insert too", lockAt, insertAt)
	}
	if !recorder.didCommit() {
		t.Fatal("the guard did not commit; the advisory lock is transaction-scoped, so it only covers work that commits")
	}
	if recorder.didRollBack() {
		t.Fatal("the guard rolled back an admission it reported as enqueued")
	}
}

// TestWorkflowControlStoreGuardedRunLocksTheKeyDerivedFromCollectorInstance
// pins the lock key to the production derivation. Both planned targets belong
// to one collector instance, so competing coordinators contend on the same key;
// a key that included the run, generation, or scope would let them pass each
// other.
func TestWorkflowControlStoreGuardedRunLocksTheKeyDerivedFromCollectorInstance(t *testing.T) {
	t.Parallel()

	recorder := &orderedStatementRecorder{
		rows: []queueFakeRows{
			{rows: [][]any{{false}}},
			{rows: [][]any{{0}, {1}}},
		},
	}
	store := NewWorkflowControlStore(recorder)
	run, items := workflowPlanningLockFixture(t)

	if _, err := store.CreateRunWithWorkItemsIfNoOpenTargets(context.Background(), run, items); err != nil {
		t.Fatalf("CreateRunWithWorkItemsIfNoOpenTargets() error = %v, want nil", err)
	}

	want := workflowPlanningLockKey(items[0])
	if other := workflowPlanningLockKey(items[1]); other != want {
		t.Fatalf("two targets of one collector instance derived different lock keys (%d and %d); competing coordinators would not contend", want, other)
	}

	lockArgs := recorder.argsOfStatements("pg_advisory_xact_lock")
	if len(lockArgs) != 1 {
		t.Fatalf("advisory lock argument count = %d, want one key for one collector instance", len(lockArgs))
	}
	if got, ok := lockArgs[0].(int64); !ok || got != want {
		t.Fatalf("advisory lock key = %v, want %d from workflowPlanningLockKey", lockArgs[0], want)
	}
}

// TestWorkflowControlStoreGuardedRunReportsRowsActuallyInserted covers the
// #4586 accounting gap. Two targets pass the eligibility read, but the database
// accepts one row -- the shape a partial unique index produces. The guard used
// to return the planned count, so the coordinator logged two work items
// enqueued when one existed.
//
// Both halves are asserted: the admitted count still has to say two, or the
// coordinator blames the open-target guard for a row the database refused and
// logs lost work as a harmless duplicate skip.
func TestWorkflowControlStoreGuardedRunReportsRowsActuallyInserted(t *testing.T) {
	t.Parallel()

	recorder := &orderedStatementRecorder{
		rows: []queueFakeRows{
			{rows: [][]any{{false}}},  // terminal-run check
			{rows: [][]any{{0}, {1}}}, // both targets eligible
		},
		// Exec order: planning lock, run insert, work-item batch insert. The
		// batch reports one accepted row for two planned targets.
		execRows: []int64{1, 1, 1},
	}
	store := NewWorkflowControlStore(recorder)
	run, items := workflowPlanningLockFixture(t)

	admission, err := store.CreateRunWithWorkItemsIfNoOpenTargets(context.Background(), run, items)
	if err != nil {
		t.Fatalf("CreateRunWithWorkItemsIfNoOpenTargets() error = %v, want nil", err)
	}
	if admission.InsertedWorkItems != 1 {
		t.Fatalf("InsertedWorkItems = %d, want 1: the count must be rows the database accepted, not the %d targets planned (#4586)",
			admission.InsertedWorkItems, len(items))
	}
	if admission.EligibleTargets != len(items) {
		t.Fatalf("EligibleTargets = %d, want %d: the open-target guard admitted both targets, and reporting fewer blames it for a row the database dropped (#4586)",
			admission.EligibleTargets, len(items))
	}
}

// TestWorkflowControlStoreGuardedRunRejectsImpossibleRowsAffected pins the
// bound the insert count is narrowed under. One INSERT ... ON CONFLICT DO
// NOTHING cannot write more rows than it was handed, so a larger count is a
// broken number rather than one to hand to an operator -- and keeping it inside
// 0..len(batch) is what makes the int64-to-int narrowing safe on any platform.
func TestWorkflowControlStoreGuardedRunRejectsImpossibleRowsAffected(t *testing.T) {
	t.Parallel()

	recorder := &orderedStatementRecorder{
		rows: []queueFakeRows{
			{rows: [][]any{{false}}},
			{rows: [][]any{{0}, {1}}},
		},
		// Planning lock, run insert, then a work-item batch claiming it wrote
		// three rows for two supplied items.
		execRows: []int64{1, 1, 3},
	}
	store := NewWorkflowControlStore(recorder)
	run, items := workflowPlanningLockFixture(t)

	admission, err := store.CreateRunWithWorkItemsIfNoOpenTargets(context.Background(), run, items)
	if err == nil {
		t.Fatalf("CreateRunWithWorkItemsIfNoOpenTargets() error = nil (admission = %+v), want a rejected rows-affected count", admission)
	}
	if !strings.Contains(err.Error(), "rows affected 3 is outside 0..2") {
		t.Fatalf("error = %v, want the out-of-range rows-affected count named", err)
	}
	if admission.InsertedWorkItems != 0 || admission.EligibleTargets != 0 {
		t.Fatalf("admission = %+v, want a zero admission alongside the error", admission)
	}
	if recorder.didCommit() {
		t.Fatal("the guard committed a transaction whose insert count it could not trust")
	}
}
