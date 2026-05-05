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
	if got, want := len(db.execs), 4; got != want {
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
				"UPDATE scope_generations",
				"status = 'active'",
				"activated_at = COALESCE(activated_at, $1)",
			},
		},
		{
			query: db.execs[2].query,
			want: []string{
				"UPDATE ingestion_scopes",
				"active_generation_id = $3",
			},
		},
		{
			query: db.execs[3].query,
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

	for _, idx := range []int{2, 3} {
		got, _ := db.execs[0].args[idx].(string)
		if strings.Contains(got, "\x00") {
			t.Fatalf("failure arg[%d] contains NUL: %q", idx, got)
		}
		if !utf8.ValidString(got) {
			t.Fatalf("failure arg[%d] is not valid UTF-8: %q", idx, got)
		}
		if got != "badmessage" {
			t.Fatalf("failure arg[%d] = %q, want %q", idx, got, "badmessage")
		}
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

func TestProjectorQueueHeartbeatRenewsClaim(t *testing.T) {
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

	if err := queue.Heartbeat(context.Background(), work); err != nil {
		t.Fatalf("Heartbeat() error = %v, want nil", err)
	}

	if got, want := len(db.execs), 1; got != want {
		t.Fatalf("exec count = %d, want %d", got, want)
	}
	query := db.execs[0].query
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
	if got, want := db.execs[0].args[0], queue.Now().Add(queue.LeaseDuration); got != want {
		t.Fatalf("claim_until arg = %v, want %v", got, want)
	}
}

func TestProjectorQueueHeartbeatRejectsStaleClaim(t *testing.T) {
	t.Parallel()

	db := &recordingExecQueryer{result: projectorRowsAffectedResult{rowsAffected: 0}}
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
