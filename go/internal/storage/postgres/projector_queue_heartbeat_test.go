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

	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/projector/failure"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// This file holds the ProjectorQueue Heartbeat lifecycle unit tests plus the
// recordingExecQueryer test fixtures shared with projector_queue_lifecycle_test.go's
// Ack/Fail tests. Split out (#4450) to keep both files under the repo's
// 500-line cap after the exponential-backoff-with-jitter change added
// assertions and comments to several retry tests.

func TestProjectorQueueHeartbeatRenewsClaim(t *testing.T) {
	t.Parallel()

	database := &recordingExecQueryer{
		results: []sql.Result{
			projectorRowsAffectedResult{rowsAffected: 1},
		},
	}
	queue := NewProjectorQueue(database, "projector-1", 30*time.Second)
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

	// The supersede check returned no row, so Heartbeat renewed the lease.
	if got, want := len(database.queries), 1; got != want {
		t.Fatalf("query count = %d, want %d supersede check", got, want)
	}
	if got, want := len(database.execs), 1; got != want {
		t.Fatalf("exec count = %d, want %d", got, want)
	}
	query := database.execs[0].query
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
	if got, want := database.execs[0].args[0], queue.Now().Add(queue.LeaseDuration); got != want {
		t.Fatalf("claim_until arg = %v, want %v", got, want)
	}
}

func TestProjectorQueueHeartbeatRejectsStaleClaim(t *testing.T) {
	t.Parallel()

	database := &recordingExecQueryer{
		results: []sql.Result{
			projectorRowsAffectedResult{rowsAffected: 0},
			projectorRowsAffectedResult{rowsAffected: 0},
		},
	}
	queue := NewProjectorQueue(database, "projector-1", 30*time.Second)
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
	// The projector service drops a lost claim only when it can see this.
	if !errors.Is(err, failure.ErrWorkClaimLost) {
		t.Fatalf("Heartbeat() error = %v, want failure.ErrWorkClaimLost", err)
	}
}

func TestProjectorQueueHeartbeatSupersedesOlderRunningGeneration(t *testing.T) {
	t.Parallel()

	database := &recordingExecQueryer{
		queryRows: []db.Rows{&singleStringRows{value: "pending"}},
	}
	queue := NewProjectorQueue(database, "projector-1", 30*time.Second)
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
	if !errors.Is(err, failure.ErrWorkSuperseded) {
		t.Fatalf("Heartbeat() error = %v, want %v", err, failure.ErrWorkSuperseded)
	}
	if got := supersededFailureClass(err); got != "projector_superseded_by_newer_generation" {
		t.Fatalf("superseded failure class = %q, want projector_superseded_by_newer_generation", got)
	}
	if got, want := len(database.execs), 0; got != want {
		t.Fatalf("exec count = %d, want %d: a superseded heartbeat must not renew", got, want)
	}
	if got, want := len(database.queries), 1; got != want {
		t.Fatalf("query count = %d, want %d", got, want)
	}

	supersedeWorkQuery := database.queries[0].query
	for _, want := range []string{
		// #7130: the work's own superseded generation is a trigger by itself,
		// and the outer statement must match it so the verdict row returns.
		"current_generation.status = 'superseded'",
		"projector_heartbeat_generation_superseded",
		"'generation_status', current_generation.status",
		"generation.status IN ('pending', 'active', 'superseded')",
		"RETURNING superseded_work.generation_status",
		"UPDATE fact_work_items AS work",
		"status = 'superseded'",
		"projector_superseded_by_newer_generation",
		"newer.scope_id = current_generation.scope_id",
		"newer.ingested_at > current_generation.ingested_at",
		"newer.generation_id > current_generation.generation_id",
		"work.lease_owner = $4",
		"RETURNING work.generation_id",
		"UPDATE scope_generations AS generation",
		"status = CASE WHEN generation.status = 'pending' THEN 'superseded' ELSE generation.status END",
		"superseded_at = CASE WHEN generation.status = 'pending' THEN $1 ELSE generation.superseded_at END",
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
	queries    []recordedExecCall
	result     sql.Result
	results    []sql.Result
	// queryRows are returned by QueryContext in order; empty rows once used up.
	queryRows []db.Rows
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

func (r *recordingExecQueryer) QueryContext(_ context.Context, query string, args ...any) (db.Rows, error) {
	r.queries = append(r.queries, recordedExecCall{
		query: query,
		args:  append([]any(nil), args...),
	})
	if len(r.queryRows) > 0 {
		rows := r.queryRows[0]
		r.queryRows = r.queryRows[1:]
		return rows, nil
	}
	return &recordingRows{}, nil
}

// singleStringRows yields one row with one text column.
type singleStringRows struct {
	value string
	done  bool
}

func (r *singleStringRows) Next() bool {
	if r.done {
		return false
	}
	r.done = true
	return true
}

func (r *singleStringRows) Scan(dest ...any) error {
	*dest[0].(*string) = r.value
	return nil
}
func (r *singleStringRows) Err() error   { return nil }
func (r *singleStringRows) Close() error { return nil }

// supersededFailureClass returns the failure class a superseded error carries.
func supersededFailureClass(err error) string {
	var classed interface{ FailureClass() string }
	if errors.As(err, &classed) {
		return classed.FailureClass()
	}
	return ""
}

type recordingRows struct{}

func (r *recordingRows) Next() bool        { return false }
func (r *recordingRows) Scan(...any) error { return nil }
func (r *recordingRows) Err() error        { return nil }
func (r *recordingRows) Close() error      { return nil }

func (r *recordingExecQueryer) Begin(context.Context) (db.Transaction, error) {
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

func (tx recordingTransaction) QueryContext(ctx context.Context, query string, args ...any) (db.Rows, error) {
	return tx.parent.QueryContext(ctx, query, args...)
}

func (recordingTransaction) Commit() error { return nil }

func (recordingTransaction) Rollback() error { return nil }

// TestProjectorQueueHeartbeatRefusesSupersededGenerationCountsFence covers the
// #7130 trigger without a database: a verdict row carrying the superseded
// generation status stops the work, carries the heartbeat failure class, and
// counts once on eshu_dp_superseded_generation_fence_total.
func TestProjectorQueueHeartbeatRefusesSupersededGenerationCountsFence(t *testing.T) {
	t.Parallel()

	database := &recordingExecQueryer{
		queryRows: []db.Rows{&singleStringRows{value: "superseded"}},
	}
	queue := NewProjectorQueue(database, "projector-1", 30*time.Second)
	instruments, reader := newEnqueueInstruments(t)
	queue.Instruments = instruments
	work := projector.ScopeGenerationWork{
		Scope:      scope.IngestionScope{ScopeID: "scope-123"},
		Generation: scope.ScopeGeneration{GenerationID: "generation-retired"},
	}

	err := queue.Heartbeat(context.Background(), work)
	if !errors.Is(err, failure.ErrWorkSuperseded) {
		t.Fatalf("Heartbeat() error = %v, want %v", err, failure.ErrWorkSuperseded)
	}
	if got := supersededFailureClass(err); got != projectorHeartbeatGenerationSupersededClass {
		t.Fatalf("superseded failure class = %q, want %q", got, projectorHeartbeatGenerationSupersededClass)
	}
	if len(database.execs) != 0 {
		t.Fatalf("exec count = %d, want 0: a superseded heartbeat must not renew", len(database.execs))
	}
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("collect metrics: %v", err)
	}
	assertCounterPresentWithLabels(t, rm, "eshu_dp_superseded_generation_fence_total",
		map[string]string{"failure_class": projectorHeartbeatGenerationSupersededClass})
	if got := counterTotal(rm, "eshu_dp_superseded_generation_fence_total"); got != 1 {
		t.Fatalf("superseded generation fence count = %d, want 1", got)
	}
}
