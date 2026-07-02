// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// This file holds the ProjectorQueue Heartbeat lifecycle unit tests plus the
// recordingExecQueryer test fixtures shared with projector_queue_lifecycle_test.go's
// Ack/Fail tests. Split out (#4450) to keep both files under the repo's
// 500-line cap after the exponential-backoff-with-jitter change added
// assertions and comments to several retry tests.

func TestProjectorQueueHeartbeatRenewsClaim(t *testing.T) {
	t.Parallel()

	db := &recordingExecQueryer{
		results: []sql.Result{
			projectorRowsAffectedResult{rowsAffected: 0},
			projectorRowsAffectedResult{rowsAffected: 1},
		},
	}
	queue := NewProjectorQueue(db, "projector-1", 30*time.Second)
	queue.Now = func() time.Time {
		return time.Date(2026, time.April, 12, 14, 30, 0, 0, time.UTC)
	}

	work := projector.ScopeGenerationWork{
		Scope: scope.IngestionScope{ScopeID: "scope-123"},
		Generation: scope.ScopeGeneration{
			GenerationID: "generation-456",
		},
	}

	if err := queue.Heartbeat(context.Background(), work); err != nil {
		t.Fatalf("Heartbeat() error = %v, want nil", err)
	}

	if got, want := len(db.execs), 2; got != want {
		t.Fatalf("exec count = %d, want %d", got, want)
	}
	query := db.execs[1].query
	for _, want := range []string{
		"UPDATE fact_work_items",
		"status = 'running'",
		"claim_until = $1",
		"lease_owner = $5",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("Heartbeat() query missing %q:\n%s", want, query)
		}
	}
	if got, want := db.execs[1].args[0], queue.Now().Add(queue.LeaseDuration); got != want {
		t.Fatalf("claim_until arg = %v, want %v", got, want)
	}
}

func TestProjectorQueueHeartbeatRejectsStaleClaim(t *testing.T) {
	t.Parallel()

	db := &recordingExecQueryer{
		results: []sql.Result{
			projectorRowsAffectedResult{rowsAffected: 0},
			projectorRowsAffectedResult{rowsAffected: 0},
		},
	}
	queue := NewProjectorQueue(db, "projector-1", 30*time.Second)
	work := projector.ScopeGenerationWork{
		Scope: scope.IngestionScope{ScopeID: "scope-123"},
		Generation: scope.ScopeGeneration{
			GenerationID: "generation-456",
		},
	}

	err := queue.Heartbeat(context.Background(), work)
	if err == nil {
		t.Fatal("Heartbeat() error = nil, want non-nil")
	}
	if !errors.Is(err, ErrProjectorClaimRejected) {
		t.Fatalf("Heartbeat() error = %v, want %v", err, ErrProjectorClaimRejected)
	}
}

func TestProjectorQueueHeartbeatSupersedesOlderRunningGeneration(t *testing.T) {
	t.Parallel()

	db := &recordingExecQueryer{
		results: []sql.Result{
			projectorRowsAffectedResult{rowsAffected: 1},
		},
	}
	queue := NewProjectorQueue(db, "projector-1", 30*time.Second)
	queue.Now = func() time.Time {
		return time.Date(2026, time.April, 12, 14, 30, 0, 0, time.UTC)
	}
	work := projector.ScopeGenerationWork{
		Scope: scope.IngestionScope{ScopeID: "scope-123"},
		Generation: scope.ScopeGeneration{
			GenerationID: "generation-old",
		},
	}

	err := queue.Heartbeat(context.Background(), work)
	if !errors.Is(err, projector.ErrWorkSuperseded) {
		t.Fatalf("Heartbeat() error = %v, want %v", err, projector.ErrWorkSuperseded)
	}
	if got, want := len(db.execs), 1; got != want {
		t.Fatalf("exec count = %d, want %d", got, want)
	}

	supersedeWorkQuery := db.execs[0].query
	for _, want := range []string{
		"UPDATE fact_work_items AS work",
		"status = 'superseded'",
		"projector_superseded_by_newer_generation",
		"newer.scope_id = current_generation.scope_id",
		"newer.ingested_at > current_generation.ingested_at",
		"newer.generation_id > current_generation.generation_id",
		"work.lease_owner = $4",
		"RETURNING work.generation_id",
		"UPDATE scope_generations AS generation",
		"status = 'superseded'",
		"superseded_at = $1",
		"FROM superseded_work",
		"generation.generation_id = superseded_work.generation_id",
	} {
		if !strings.Contains(supersedeWorkQuery, want) {
			t.Fatalf("supersede query missing %q:\n%s", want, supersedeWorkQuery)
		}
	}
}

var errProjectionFailed = &testError{message: "projection failed"}

type testError struct {
	message string
}

func (e *testError) Error() string {
	return e.message
}

type retryableTestError struct {
	message string
}

func (e *retryableTestError) Error() string {
	return e.message
}

func (e *retryableTestError) Retryable() bool {
	return true
}

type recordingExecQueryer struct {
	beginCalls int
	execs      []recordedExecCall
	result     sql.Result
	results    []sql.Result
}

type recordedExecCall struct {
	query string
	args  []any
}

func (r *recordingExecQueryer) ExecContext(_ context.Context, query string, args ...any) (sql.Result, error) {
	r.execs = append(r.execs, recordedExecCall{
		query: query,
		args:  append([]any(nil), args...),
	})
	if r.result != nil {
		return r.result, nil
	}
	if len(r.results) > 0 {
		result := r.results[0]
		r.results = r.results[1:]
		return result, nil
	}
	return proofResult{}, nil
}

func (r *recordingExecQueryer) QueryContext(context.Context, string, ...any) (Rows, error) {
	return nil, nil
}

func (r *recordingExecQueryer) Begin(context.Context) (Transaction, error) {
	r.beginCalls++
	return recordingTransaction{parent: r}, nil
}

type projectorRowsAffectedResult struct {
	rowsAffected int64
}

func (r projectorRowsAffectedResult) LastInsertId() (int64, error) { return 0, nil }
func (r projectorRowsAffectedResult) RowsAffected() (int64, error) { return r.rowsAffected, nil }

type recordingTransaction struct {
	parent *recordingExecQueryer
}

func (tx recordingTransaction) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return tx.parent.ExecContext(ctx, query, args...)
}

func (recordingTransaction) QueryContext(context.Context, string, ...any) (Rows, error) {
	return nil, nil
}

func (recordingTransaction) Commit() error { return nil }

func (recordingTransaction) Rollback() error { return nil }
