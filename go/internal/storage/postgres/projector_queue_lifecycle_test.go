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
	"unicode/utf8"

	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/scope"
	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

func TestProjectorQueueAckPromotesGenerationAndSupersedesPriorActive(t *testing.T) {
	t.Parallel()

	db := &recordingExecQueryer{}
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

	if err := queue.Ack(context.Background(), work, projector.Result{}); err != nil {
		t.Fatalf("Ack() error = %v, want nil", err)
	}

	if got, want := db.beginCalls, 1; got != want {
		t.Fatalf("begin count = %d, want %d", got, want)
	}
	if got, want := len(db.execs), 5; got != want {
		t.Fatalf("exec count = %d, want %d", got, want)
	}

	checks := []struct {
		query string
		want  []string
	}{
		{
			query: db.execs[0].query,
			want: []string{
				"UPDATE scope_generations",
				"status = 'superseded'",
				"generation_id <> $3",
				"status = 'active'",
			},
		},
		{
			query: db.execs[1].query,
			want: []string{
				"UPDATE fact_work_items AS stale",
				"status = 'superseded'",
				"stale.status IN ('pending', 'retrying', 'failed', 'dead_letter')",
			},
		},
		{
			query: db.execs[2].query,
			want: []string{
				"UPDATE scope_generations",
				"status = 'active'",
				"activated_at = COALESCE(activated_at, $1)",
			},
		},
		{
			query: db.execs[3].query,
			want: []string{
				"UPDATE ingestion_scopes",
				"active_generation_id = $3",
			},
		},
		{
			query: db.execs[4].query,
			want: []string{
				"UPDATE fact_work_items",
				"status = 'succeeded'",
			},
		},
	}
	for _, check := range checks {
		for _, want := range check.want {
			if !strings.Contains(check.query, want) {
				t.Fatalf("Ack() query missing %q:\n%s", want, check.query)
			}
		}
	}
}

func TestProjectorQueueFailMarksGenerationFailedWithoutClearingOtherActiveGeneration(t *testing.T) {
	t.Parallel()

	db := &recordingExecQueryer{}
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

	if err := queue.Fail(context.Background(), work, errProjectionFailed); err != nil {
		t.Fatalf("Fail() error = %v, want nil", err)
	}

	if got, want := len(db.execs), 1; got != want {
		t.Fatalf("exec count = %d, want %d", got, want)
	}

	query := db.execs[0].query
	for _, want := range []string{
		"UPDATE scope_generations",
		"status = 'failed'",
		"UPDATE ingestion_scopes",
		"active_generation_id = CASE",
		"UPDATE fact_work_items",
		"status = 'dead_letter'",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("Fail() query missing %q:\n%s", want, query)
		}
	}
}

func TestProjectorQueueFailRetriesRetryableErrorWithinAttemptBudget(t *testing.T) {
	t.Parallel()

	db := &recordingExecQueryer{}
	queue := NewProjectorQueue(db, "projector-1", 30*time.Second)
	queue.RetryDelay = 2 * time.Minute
	queue.MaxAttempts = 3
	queue.Now = func() time.Time {
		return time.Date(2026, time.April, 12, 14, 30, 0, 0, time.UTC)
	}

	work := projector.ScopeGenerationWork{
		Scope: scope.IngestionScope{ScopeID: "scope-123"},
		Generation: scope.ScopeGeneration{
			GenerationID: "generation-456",
		},
		AttemptCount: 1,
	}

	if err := queue.Fail(context.Background(), work, &retryableTestError{message: "transient projector failure"}); err != nil {
		t.Fatalf("Fail() error = %v, want nil", err)
	}

	if got, want := len(db.execs), 1; got != want {
		t.Fatalf("exec count = %d, want %d", got, want)
	}

	query := db.execs[0].query
	for _, want := range []string{
		"UPDATE fact_work_items",
		"status = 'retrying'",
		"next_attempt_at = $5",
		"visible_at = $5",
		"failure_class = $2",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("Fail() retry query missing %q:\n%s", want, query)
		}
	}

	if got, want := db.execs[0].args[1], "projection_retryable"; got != want {
		t.Fatalf("failure class = %v, want %v", got, want)
	}
	if got, want := db.execs[0].args[4], queue.Now().Add(queue.RetryDelay); got != want {
		t.Fatalf("next attempt = %v, want %v", got, want)
	}
}

func TestProjectorQueueFailSanitizesFailureTextForPostgres(t *testing.T) {
	t.Parallel()

	db := &recordingExecQueryer{}
	queue := NewProjectorQueue(db, "projector-1", 30*time.Second)
	work := projector.ScopeGenerationWork{
		Scope: scope.IngestionScope{ScopeID: "scope-123"},
		Generation: scope.ScopeGeneration{
			GenerationID: "generation-456",
		},
	}

	raw := "bad\x00" + string([]byte{0xff}) + "message"
	if err := queue.Fail(context.Background(), work, errors.New(raw)); err != nil {
		t.Fatalf("Fail() error = %v, want nil", err)
	}
	if got, want := len(db.execs), 1; got != want {
		t.Fatalf("exec count = %d, want %d", got, want)
	}

	// arg[2] is the failure message; arg[3] is the structured triage details.
	// Both must be NUL-free and valid UTF-8 so Postgres accepts them.
	for _, idx := range []int{2, 3} {
		got, _ := db.execs[0].args[idx].(string)
		if strings.Contains(got, "\x00") {
			t.Fatalf("failure arg[%d] contains NUL: %q", idx, got)
		}
		if !utf8.ValidString(got) {
			t.Fatalf("failure arg[%d] is not valid UTF-8: %q", idx, got)
		}
	}

	// The message field is exactly the sanitized cause text.
	if got, _ := db.execs[0].args[2].(string); got != "badmessage" {
		t.Fatalf("failure message arg = %q, want %q", got, "badmessage")
	}
	// The triage details field carries the structured triage classification.
	if got, _ := db.execs[0].args[3].(string); !strings.Contains(got, "triage=") {
		t.Fatalf("failure details arg = %q, want it to contain triage classification", got)
	}
}

func TestProjectorQueueFailLifecycleRetriesGraphWriteTimeoutWithinAttemptBudget(t *testing.T) {
	t.Parallel()

	db := &recordingExecQueryer{}
	queue := NewProjectorQueue(db, "projector-1", 30*time.Second)
	queue.Now = func() time.Time {
		return time.Date(2026, time.April, 12, 14, 30, 0, 0, time.UTC)
	}
	work := projector.ScopeGenerationWork{
		Scope: scope.IngestionScope{ScopeID: "scope-123"},
		Generation: scope.ScopeGeneration{
			GenerationID: "generation-456",
		},
		AttemptCount: 1,
	}
	cause := sourcecypher.GraphWriteTimeoutError{
		Operation:   "neo4j execute group timed out",
		Timeout:     30 * time.Second,
		TimeoutHint: "ESHU_CANONICAL_WRITE_TIMEOUT",
		Summary:     "phase=files rows=100 chunk=21/24",
		Cause:       context.DeadlineExceeded,
	}

	if err := queue.Fail(context.Background(), work, cause); err != nil {
		t.Fatalf("Fail() error = %v, want nil", err)
	}

	if !strings.Contains(db.execs[0].query, "status = 'retrying'") {
		t.Fatalf("timeout should retry within attempt budget, query:\n%s", db.execs[0].query)
	}
	if got, want := db.execs[0].args[1], "projection_retryable"; got != want {
		t.Fatalf("failure class = %v, want %v", got, want)
	}
	if got, want := db.execs[0].args[3], "phase=files rows=100 chunk=21/24"; got != want {
		t.Fatalf("failure details = %v, want %v", got, want)
	}
	if got, want := db.execs[0].args[4], queue.Now().Add(30*time.Second); got != want {
		t.Fatalf("next attempt = %v, want %v", got, want)
	}
}

func TestProjectorQueueFailMarksRetryableErrorTerminalWhenAttemptBudgetExhausted(t *testing.T) {
	t.Parallel()

	db := &recordingExecQueryer{}
	queue := NewProjectorQueue(db, "projector-1", 30*time.Second)
	queue.MaxAttempts = 2
	queue.Now = func() time.Time {
		return time.Date(2026, time.April, 12, 14, 30, 0, 0, time.UTC)
	}

	work := projector.ScopeGenerationWork{
		Scope: scope.IngestionScope{ScopeID: "scope-123"},
		Generation: scope.ScopeGeneration{
			GenerationID: "generation-456",
		},
		AttemptCount: 2,
	}

	if err := queue.Fail(context.Background(), work, &retryableTestError{message: "still broken"}); err != nil {
		t.Fatalf("Fail() error = %v, want nil", err)
	}

	if got, want := len(db.execs), 1; got != want {
		t.Fatalf("exec count = %d, want %d", got, want)
	}

	query := db.execs[0].query
	for _, want := range []string{
		"UPDATE scope_generations",
		"status = 'failed'",
		"UPDATE fact_work_items",
		"status = 'dead_letter'",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("Fail() terminal query missing %q:\n%s", want, query)
		}
	}
}

// TestProjectorQueueFailDeadLettersWithTriageClass proves that when a
// non-retryable failure is dead-lettered, the durable failure_class carries an
// operator-facing triage class (input_invalid / projection_bug / …) instead of
// the coarse "projection_failed" fallback. This is the dead-letter-triage
// surface for issue #3502 and the live wiring of projector.ClassifyFailure that
// issue #3514 flagged as dead.
func TestProjectorQueueFailDeadLettersWithTriageClass(t *testing.T) {
	t.Parallel()

	db := &recordingExecQueryer{}
	queue := NewProjectorQueue(db, "projector-1", 30*time.Second)
	queue.Now = func() time.Time {
		return time.Date(2026, time.April, 12, 14, 30, 0, 0, time.UTC)
	}

	work := projector.ScopeGenerationWork{
		Scope:      scope.IngestionScope{ScopeID: "scope-123"},
		Generation: scope.ScopeGeneration{GenerationID: "generation-456"},
	}

	// A non-retryable input-validation failure must be triaged as input_invalid.
	cause := projector.NewInputValidationError("bad scope_id in fact payload")
	if err := queue.Fail(context.Background(), work, cause); err != nil {
		t.Fatalf("Fail() error = %v, want nil", err)
	}

	if got, want := len(db.execs), 1; got != want {
		t.Fatalf("exec count = %d, want %d", got, want)
	}
	if !strings.Contains(db.execs[0].query, "status = 'dead_letter'") {
		t.Fatalf("Fail() should dead-letter, query:\n%s", db.execs[0].query)
	}
	gotClass, _ := db.execs[0].args[1].(string)
	if gotClass != string(projector.TriageClassInputInvalid) {
		t.Fatalf("dead-letter failure_class = %q, want %q", gotClass, projector.TriageClassInputInvalid)
	}
	gotDetails, _ := db.execs[0].args[3].(string)
	if !strings.Contains(gotDetails, "disposition=non_retryable") {
		t.Fatalf("dead-letter details = %q, want disposition=non_retryable", gotDetails)
	}
}

// TestProjectorQueueFailDeadLettersRetryExhaustedWithTriageClass proves a
// retryable cause that exhausted its attempt budget is dead-lettered as
// retry_exhausted (the transient bucket), so an operator can distinguish a
// transient pileup from a terminal defect.
func TestProjectorQueueFailDeadLettersRetryExhaustedWithTriageClass(t *testing.T) {
	t.Parallel()

	db := &recordingExecQueryer{}
	queue := NewProjectorQueue(db, "projector-1", 30*time.Second)
	queue.MaxAttempts = 2
	queue.Now = func() time.Time {
		return time.Date(2026, time.April, 12, 14, 30, 0, 0, time.UTC)
	}

	work := projector.ScopeGenerationWork{
		Scope:        scope.IngestionScope{ScopeID: "scope-123"},
		Generation:   scope.ScopeGeneration{GenerationID: "generation-456"},
		AttemptCount: 2,
	}

	if err := queue.Fail(context.Background(), work, &retryableTestError{message: "still broken"}); err != nil {
		t.Fatalf("Fail() error = %v, want nil", err)
	}

	if !strings.Contains(db.execs[0].query, "status = 'dead_letter'") {
		t.Fatalf("exhausted retryable should dead-letter, query:\n%s", db.execs[0].query)
	}
	gotClass, _ := db.execs[0].args[1].(string)
	if gotClass != string(projector.TriageClassRetryExhausted) {
		t.Fatalf("dead-letter failure_class = %q, want %q", gotClass, projector.TriageClassRetryExhausted)
	}
}

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
