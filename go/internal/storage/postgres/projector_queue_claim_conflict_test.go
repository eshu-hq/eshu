// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/projector/failure"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// claimConflictQueryer answers each claim query with the next scripted
// outcome: a query error, a rows error, or one claimable row.
type claimConflictQueryer struct {
	mu       sync.Mutex
	outcomes []claimConflictOutcome
	calls    int
}

type claimConflictOutcome struct {
	queryErr error
	rowsErr  error
}

func (f *claimConflictQueryer) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	return nil, errors.New("unexpected exec")
}

func (f *claimConflictQueryer) QueryContext(context.Context, string, ...any) (db.Rows, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if len(f.outcomes) == 0 {
		return &queueFakeRows{rows: [][]any{claimConflictRow()}}, nil
	}
	outcome := f.outcomes[0]
	f.outcomes = f.outcomes[1:]
	if outcome.queryErr != nil {
		return nil, outcome.queryErr
	}
	return &erroringRows{err: outcome.rowsErr}, nil
}

// erroringRows yields no row and reports err from Err, the way pgx surfaces
// an error raised while the statement executes.
type erroringRows struct {
	queueFakeRows
	err error
}

func (r *erroringRows) Err() error { return r.err }

func claimConflictRow() []any {
	observed := time.Date(2026, time.September, 25, 3, 0, 0, 0, time.UTC)
	return []any{
		"scope-123", "git", "repository", "", "", false, "git", "repo-123",
		"generation-456", 1, observed, observed.Add(time.Minute), "pending",
		"snapshot", "", []byte(`{}`),
	}
}

func newClaimConflictQueue(database db.ExecQueryer) ProjectorQueue {
	queue := NewProjectorQueue(database, "projector-1", time.Minute)
	queue.ClaimConflictBackoff = time.Microsecond
	return queue
}

func TestProjectorQueueClaimRetriesDeadlockAndSerializationFailures(t *testing.T) {
	t.Parallel()

	database := &claimConflictQueryer{outcomes: []claimConflictOutcome{
		{queryErr: &pgconn.PgError{Code: "40P01", Message: "deadlock detected"}},
		{rowsErr: &pgconn.PgError{Code: "40001", Message: "could not serialize access"}},
	}}
	queue := newClaimConflictQueue(database)
	instruments, reader := newEnqueueInstruments(t)
	queue.Instruments = instruments

	work, ok, err := queue.Claim(context.Background())
	if err != nil {
		t.Fatalf("Claim() error = %v, want nil after conflict retries", err)
	}
	if !ok || work.Scope.ScopeID != "scope-123" {
		t.Fatalf("Claim() = (%+v, %v), want claimed scope-123", work, ok)
	}
	if got, want := database.calls, 3; got != want {
		t.Fatalf("claim attempts = %d, want %d", got, want)
	}
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("collect metrics: %v", err)
	}
	assertCounterPresentWithLabels(t, rm, "eshu_dp_queue_claim_conflict_retries_total",
		map[string]string{"queue": "projector", "failure_class": "deadlock"})
	assertCounterPresentWithLabels(t, rm, "eshu_dp_queue_claim_conflict_retries_total",
		map[string]string{"queue": "projector", "failure_class": "serialization_failure"})
	if got, want := counterTotal(rm, "eshu_dp_queue_claim_conflict_retries_total"), int64(2); got != want {
		t.Fatalf("claim conflict retries = %d, want %d", got, want)
	}
}

func TestProjectorQueueClaimReportsConflictAfterBoundedRetries(t *testing.T) {
	t.Parallel()

	deadlock := &pgconn.PgError{Code: "40P01", Message: "deadlock detected"}
	outcomes := make([]claimConflictOutcome, 0, projectorClaimConflictAttempts+1)
	for i := 0; i <= projectorClaimConflictAttempts; i++ {
		outcomes = append(outcomes, claimConflictOutcome{queryErr: deadlock})
	}
	database := &claimConflictQueryer{outcomes: outcomes}
	queue := newClaimConflictQueue(database)

	_, ok, err := queue.Claim(context.Background())
	if ok {
		t.Fatal("Claim() ok = true, want false when every attempt conflicts")
	}
	if !errors.Is(err, failure.ErrWorkClaimConflict) {
		t.Fatalf("Claim() error = %v, want ErrWorkClaimConflict", err)
	}
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "40P01" {
		t.Fatalf("Claim() error = %v, want wrapped SQLSTATE 40P01", err)
	}
	if got, want := database.calls, projectorClaimConflictAttempts; got != want {
		t.Fatalf("claim attempts = %d, want bounded %d", got, want)
	}
}

func TestProjectorQueueClaimDoesNotRetryOtherErrors(t *testing.T) {
	t.Parallel()

	database := &claimConflictQueryer{outcomes: []claimConflictOutcome{
		{queryErr: &pgconn.PgError{Code: "42P01", Message: "undefined table"}},
	}}
	queue := newClaimConflictQueue(database)

	_, _, err := queue.Claim(context.Background())
	if err == nil {
		t.Fatal("Claim() error = nil, want undefined-table error")
	}
	if errors.Is(err, failure.ErrWorkClaimConflict) {
		t.Fatalf("Claim() error = %v, must not be classified as a claim conflict", err)
	}
	if got, want := database.calls, 1; got != want {
		t.Fatalf("claim attempts = %d, want %d", got, want)
	}
}

func TestProjectorQueueClaimStopsConflictBackoffOnCancel(t *testing.T) {
	t.Parallel()

	database := &claimConflictQueryer{outcomes: []claimConflictOutcome{
		{queryErr: &pgconn.PgError{Code: "40P01", Message: "deadlock detected"}},
	}}
	queue := NewProjectorQueue(database, "projector-1", time.Minute)
	queue.ClaimConflictBackoff = time.Hour
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()

	_, _, err := queue.Claim(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Claim() error = %v, want context.Canceled during backoff", err)
	}
	if got, want := database.calls, 1; got != want {
		t.Fatalf("claim attempts = %d, want %d", got, want)
	}
}
