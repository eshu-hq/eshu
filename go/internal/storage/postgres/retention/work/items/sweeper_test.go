// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package items

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// batchReply is one scripted answer to the batch statement.
type batchReply struct {
	cursor          sql.NullString
	scanned         int64
	deletedComplete int64
	deletedFailed   int64
	err             error
}

// fakeDatabase records transactions and statements and answers the batch
// statement from a script, one reply per call.
type fakeDatabase struct {
	replies   []batchReply
	calls     int
	begins    int
	commits   int
	rollbacks int
	execs     []recordedStatement
	queries   []recordedStatement
	sizeRow   []any
}

type recordedStatement struct {
	query string
	args  []any
}

func (f *fakeDatabase) Begin(context.Context) (db.Transaction, error) {
	f.begins++
	return &fakeTransaction{parent: f}, nil
}

func (f *fakeDatabase) QueryContext(_ context.Context, query string, args ...any) (db.Rows, error) {
	f.queries = append(f.queries, recordedStatement{query: query, args: args})
	if query == tableSizeQuery {
		return &fakeRows{rows: [][]any{f.sizeRow}}, nil
	}
	return nil, errors.New("unexpected non-transactional query")
}

type fakeTransaction struct {
	parent *fakeDatabase
}

func (t *fakeTransaction) ExecContext(_ context.Context, query string, args ...any) (sql.Result, error) {
	t.parent.execs = append(t.parent.execs, recordedStatement{query: query, args: args})
	return nil, nil
}

func (t *fakeTransaction) QueryContext(_ context.Context, query string, args ...any) (db.Rows, error) {
	f := t.parent
	f.queries = append(f.queries, recordedStatement{query: query, args: args})
	if query != pruneBatchQuery {
		return nil, errors.New("unexpected transactional query")
	}
	if f.calls >= len(f.replies) {
		return nil, errors.New("batch statement called more times than scripted")
	}
	reply := f.replies[f.calls]
	f.calls++
	if reply.err != nil {
		return nil, reply.err
	}
	return &fakeRows{rows: [][]any{{reply.cursor, reply.scanned, reply.deletedComplete, reply.deletedFailed}}}, nil
}

func (t *fakeTransaction) Commit() error   { t.parent.commits++; return nil }
func (t *fakeTransaction) Rollback() error { t.parent.rollbacks++; return nil }

type fakeRows struct {
	rows  [][]any
	index int
}

func (r *fakeRows) Next() bool {
	if r.index >= len(r.rows) {
		return false
	}
	r.index++
	return true
}

func (r *fakeRows) Scan(dest ...any) error {
	row := r.rows[r.index-1]
	for i := range dest {
		switch target := dest[i].(type) {
		case *sql.NullString:
			*target = row[i].(sql.NullString)
		case *int64:
			*target = row[i].(int64)
		default:
			return errors.New("unsupported scan target")
		}
	}
	return nil
}

func (r *fakeRows) Err() error   { return nil }
func (r *fakeRows) Close() error { return nil }

func cursor(value string) sql.NullString { return sql.NullString{String: value, Valid: true} }

func testPolicy(batchSize, maxBatches int) workflow.WorkItemRetentionPolicy {
	policy := workflow.DefaultWorkItemRetentionPolicy()
	policy.BatchSize = batchSize
	policy.MaxBatches = maxBatches
	return policy
}

func TestPruneWorkItemsStopsAtTableEndAndWrapsCursor(t *testing.T) {
	t.Parallel()

	database := &fakeDatabase{replies: []batchReply{
		{cursor: cursor("b"), scanned: 2, deletedComplete: 1, deletedFailed: 1},
		{cursor: cursor("c"), scanned: 1, deletedComplete: 1},
		{scanned: 0},
	}}
	sweeper := NewSweeper(database)
	asOf := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)

	result, err := sweeper.PruneWorkItems(context.Background(), asOf, testPolicy(2, 10))
	if err != nil {
		t.Fatalf("PruneWorkItems() error = %v, want nil", err)
	}
	if result.Batches != 2 || result.ScannedRows != 3 || result.DeletedCompleted != 2 || result.DeletedFailedTerminal != 1 {
		t.Fatalf("PruneWorkItems() = %+v, want 2 batches, 3 scanned, 2 completed, 1 failed_terminal", result)
	}
	if !result.Wrapped {
		t.Fatal("Wrapped = false, want true after a short window reached the table end")
	}
	if len(result.BatchDurations) != 2 {
		t.Fatalf("BatchDurations = %d entries, want 2", len(result.BatchDurations))
	}
	if got := database.queries[0].args[0]; got != "" {
		t.Fatalf("first batch cursor = %v, want empty start", got)
	}
	if got := database.queries[1].args[0]; got != "b" {
		t.Fatalf("second batch cursor = %v, want b", got)
	}
	if got := database.queries[0].args[1]; got != 2 {
		t.Fatalf("batch size arg = %v, want 2", got)
	}
	if got := database.queries[0].args[2]; got != asOf.Add(-30*24*time.Hour) {
		t.Fatalf("cutoff arg = %v, want asOf minus 30 days", got)
	}

	// The next pass starts again from the table head because the walk wrapped.
	if _, err := sweeper.PruneWorkItems(context.Background(), asOf, testPolicy(2, 10)); err != nil {
		t.Fatalf("second PruneWorkItems() error = %v, want nil", err)
	}
	if got := database.queries[2].args[0]; got != "" {
		t.Fatalf("pass after wrap cursor = %v, want empty start", got)
	}
}

func TestPruneWorkItemsHonorsMaxBatchesAndResumesFromCursor(t *testing.T) {
	t.Parallel()

	database := &fakeDatabase{replies: []batchReply{
		{cursor: cursor("b"), scanned: 2, deletedComplete: 2},
		{cursor: cursor("d"), scanned: 2},
		{cursor: cursor("f"), scanned: 2, deletedFailed: 1},
	}}
	sweeper := NewSweeper(database)
	asOf := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)

	result, err := sweeper.PruneWorkItems(context.Background(), asOf, testPolicy(2, 2))
	if err != nil {
		t.Fatalf("PruneWorkItems() error = %v, want nil", err)
	}
	if result.Batches != 2 || result.Wrapped {
		t.Fatalf("PruneWorkItems() = %+v, want exactly 2 batches and no wrap", result)
	}
	if result.ScannedRows > 2*2 {
		t.Fatalf("ScannedRows = %d, want at most BatchSize*MaxBatches = 4", result.ScannedRows)
	}
	if _, err := sweeper.PruneWorkItems(context.Background(), asOf, testPolicy(2, 1)); err != nil {
		t.Fatalf("second PruneWorkItems() error = %v, want nil", err)
	}
	if got := database.queries[2].args[0]; got != "d" {
		t.Fatalf("resumed cursor = %v, want d", got)
	}
}

func TestPruneWorkItemsRunsEachBatchInItsOwnBoundedTransaction(t *testing.T) {
	t.Parallel()

	database := &fakeDatabase{replies: []batchReply{
		{cursor: cursor("b"), scanned: 2},
		{cursor: cursor("c"), scanned: 1},
	}}
	sweeper := NewSweeper(database)
	policy := testPolicy(2, 10)
	policy.LockTimeout = 1500 * time.Millisecond

	if _, err := sweeper.PruneWorkItems(context.Background(), time.Now(), policy); err != nil {
		t.Fatalf("PruneWorkItems() error = %v, want nil", err)
	}
	if database.begins != 2 || database.commits != 2 || database.rollbacks != 0 {
		t.Fatalf("begins/commits/rollbacks = %d/%d/%d, want 2/2/0", database.begins, database.commits, database.rollbacks)
	}
	if len(database.execs) != 2 {
		t.Fatalf("timeout statements = %d, want one per batch transaction", len(database.execs))
	}
	for _, exec := range database.execs {
		if exec.query != setBatchTimeoutsQuery {
			t.Fatalf("batch transaction ran %q before the delete, want the local timeout statement", exec.query)
		}
		if exec.args[0] != "1500ms" {
			t.Fatalf("lock_timeout arg = %v, want 1500ms", exec.args[0])
		}
	}
}

func TestPruneWorkItemsRollsBackAndKeepsCursorOnBatchError(t *testing.T) {
	t.Parallel()

	database := &fakeDatabase{replies: []batchReply{
		{cursor: cursor("b"), scanned: 2, deletedComplete: 2},
		{err: errors.New("canceling statement due to lock timeout")},
		{cursor: cursor("d"), scanned: 2},
	}}
	sweeper := NewSweeper(database)

	result, err := sweeper.PruneWorkItems(context.Background(), time.Now(), testPolicy(2, 10))
	if err == nil {
		t.Fatal("PruneWorkItems() error = nil, want the batch failure")
	}
	if result.Batches != 1 || result.DeletedCompleted != 2 {
		t.Fatalf("partial result = %+v, want the committed first batch reported", result)
	}
	if database.rollbacks != 1 {
		t.Fatalf("rollbacks = %d, want 1", database.rollbacks)
	}
	if _, err := sweeper.PruneWorkItems(context.Background(), time.Now(), testPolicy(2, 1)); err != nil {
		t.Fatalf("retry PruneWorkItems() error = %v, want nil", err)
	}
	if got := database.queries[2].args[0]; got != "b" {
		t.Fatalf("retry cursor = %v, want b (the failed window is retried)", got)
	}
}

func TestPruneWorkItemsRejectsInvalidPolicyWithoutTouchingTheDatabase(t *testing.T) {
	t.Parallel()

	database := &fakeDatabase{}
	policy := testPolicy(0, 1)
	if _, err := NewSweeper(database).PruneWorkItems(context.Background(), time.Now(), policy); err == nil {
		t.Fatal("PruneWorkItems() error = nil, want policy validation error")
	}
	if database.begins != 0 {
		t.Fatalf("begins = %d, want 0", database.begins)
	}
}

func TestWorkItemTableSizeReadsCatalogEstimate(t *testing.T) {
	t.Parallel()

	database := &fakeDatabase{sizeRow: []any{int64(4096), int64(12)}}
	size, err := NewSweeper(database).WorkItemTableSize(context.Background())
	if err != nil {
		t.Fatalf("WorkItemTableSize() error = %v, want nil", err)
	}
	if size.Bytes != 4096 || size.EstimatedRows != 12 {
		t.Fatalf("WorkItemTableSize() = %+v, want 4096 bytes and 12 rows", size)
	}
}

// TestPruneBatchQueryKeepsItsSafetyClauses pins the clauses the live proofs
// and the EXPLAIN evidence depend on, so an edit that drops one fails here
// even when no DSN is configured.
func TestPruneBatchQueryKeepsItsSafetyClauses(t *testing.T) {
	t.Parallel()

	for _, clause := range []string{
		"ORDER BY work_item_id",
		"LIMIT $2",
		"item.status IN ('completed', 'failed_terminal')",
		"item.updated_at < $3",
		"run.status IN ('complete', 'failed')",
		"scope.active_generation_id = item.generation_id",
		"newer.status = item.status",
		"FOR UPDATE OF item SKIP LOCKED",
	} {
		if !strings.Contains(pruneBatchQuery, clause) {
			t.Fatalf("pruneBatchQuery lost clause %q", clause)
		}
	}
	if got := strings.Count(pruneBatchQuery, "OFFSET 0"); got != 1 {
		t.Fatalf("pruneBatchQuery has %d OFFSET 0 fences, want 1 (it keeps the newer-row probe per row)", got)
	}
}
