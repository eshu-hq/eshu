package postgres

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/scope"
	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

func TestProjectorQueueClaimReturnsScopeGenerationWork(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 12, 11, 0, 0, 0, time.UTC)
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{
				rows: [][]any{{
					"scope-123",
					"git",
					"repository",
					"",
					"generation-active",
					true,
					"git",
					"repo-123",
					"generation-456",
					1,
					time.Date(2026, time.April, 12, 10, 0, 0, 0, time.UTC),
					time.Date(2026, time.April, 12, 10, 5, 0, 0, time.UTC),
					"pending",
					"snapshot",
					"",
					[]byte(`{"repo_id":"repository:r_test"}`),
				}},
			},
		},
	}

	queue := ProjectorQueue{
		db:            db,
		LeaseOwner:    "projector-1",
		LeaseDuration: 30 * time.Second,
		Now:           func() time.Time { return now },
	}

	work, ok, err := queue.Claim(context.Background())
	if err != nil {
		t.Fatalf("Claim() error = %v, want nil", err)
	}
	if !ok {
		t.Fatal("Claim() ok = false, want true")
	}
	if got, want := work.Scope.ScopeID, "scope-123"; got != want {
		t.Fatalf("Claim().Scope.ScopeID = %q, want %q", got, want)
	}
	if got, want := work.Generation.GenerationID, "generation-456"; got != want {
		t.Fatalf("Claim().Generation.GenerationID = %q, want %q", got, want)
	}
	if got, want := work.AttemptCount, 1; got != want {
		t.Fatalf("Claim().AttemptCount = %d, want %d", got, want)
	}
	if got, want := work.Scope.ActiveGenerationID, "generation-active"; got != want {
		t.Fatalf("Claim().Scope.ActiveGenerationID = %q, want %q", got, want)
	}
	if !work.Scope.PreviousGenerationExists {
		t.Fatal("Claim().Scope.PreviousGenerationExists = false, want true")
	}
	if !strings.Contains(db.queries[0].query, "stage = 'projector'") {
		t.Fatalf("claim query = %q, want projector stage filter", db.queries[0].query)
	}
}

func TestProjectorQueueClaimPopulatesScopeMetadataFromPayload(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 12, 11, 0, 0, 0, time.UTC)
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{
				rows: [][]any{{
					"scope-123",
					"git",
					"repository",
					"",
					"",
					false,
					"git",
					"repo-123",
					"generation-456",
					2,
					time.Date(2026, time.April, 12, 10, 0, 0, 0, time.UTC),
					time.Date(2026, time.April, 12, 10, 5, 0, 0, time.UTC),
					"pending",
					"snapshot",
					"",
					[]byte(`{"repo_id":"repository:r_test","source_key":"repo-123"}`),
				}},
			},
		},
	}

	queue := ProjectorQueue{
		db:            db,
		LeaseOwner:    "projector-1",
		LeaseDuration: 30 * time.Second,
		Now:           func() time.Time { return now },
	}

	work, ok, err := queue.Claim(context.Background())
	if err != nil {
		t.Fatalf("Claim() error = %v, want nil", err)
	}
	if !ok {
		t.Fatal("Claim() ok = false, want true")
	}
	if got, want := work.Scope.Metadata["repo_id"], "repository:r_test"; got != want {
		t.Fatalf("Claim().Scope.Metadata[repo_id] = %q, want %q", got, want)
	}
}

func TestProjectorQueueEnqueueInsertsProjectorStageWork(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 12, 11, 0, 0, 0, time.UTC)
	db := &fakeExecQueryer{}
	queue := ProjectorQueue{
		db:  db,
		Now: func() time.Time { return now },
	}

	err := queue.Enqueue(
		context.Background(),
		"scope-123",
		"generation-456",
	)
	if err != nil {
		t.Fatalf("Enqueue() error = %v, want nil", err)
	}
	if got, want := len(db.execs), 1; got != want {
		t.Fatalf("exec count = %d, want %d", got, want)
	}
	if !strings.Contains(db.execs[0].query, "INSERT INTO fact_work_items") {
		t.Fatalf("enqueue query = %q, want fact_work_items insert", db.execs[0].query)
	}
	if got, want := db.execs[0].args[3], "source_local"; got != want {
		t.Fatalf("domain arg = %v, want %v", got, want)
	}
}

func TestProjectorQueueFailRetriesGraphWriteTimeoutWithinAttemptBudget(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 12, 11, 0, 0, 0, time.UTC)
	db := &fakeExecQueryer{}
	queue := ProjectorQueue{
		db:            db,
		LeaseOwner:    "projector-1",
		LeaseDuration: time.Minute,
		RetryDelay:    2 * time.Minute,
		MaxAttempts:   3,
		Now:           func() time.Time { return now },
	}
	work := projector.ScopeGenerationWork{
		Scope: scope.IngestionScope{
			ScopeID: "scope-123",
		},
		Generation: scope.ScopeGeneration{
			GenerationID: "generation-456",
		},
		AttemptCount: 1,
	}
	cause := fmt.Errorf("canonical projection: %w", sourcecypher.GraphWriteTimeoutError{
		Operation:   "neo4j execute timed out",
		Timeout:     30 * time.Second,
		TimeoutHint: "ESHU_CANONICAL_WRITE_TIMEOUT",
		Summary:     "phase=entities label=Function rows=15",
		Cause:       context.DeadlineExceeded,
	})

	if err := queue.Fail(context.Background(), work, cause); err != nil {
		t.Fatalf("Fail() error = %v, want nil", err)
	}

	if got, want := len(db.execs), 1; got != want {
		t.Fatalf("exec count = %d, want %d", got, want)
	}
	if !strings.Contains(db.execs[0].query, "status = 'retrying'") {
		t.Fatalf("timeout should retry within attempt budget, query:\n%s", db.execs[0].query)
	}
	if got, want := db.execs[0].args[1], "projection_retryable"; got != want {
		t.Fatalf("failure class = %v, want %v", got, want)
	}
	if got, want := db.execs[0].args[3], "phase=entities label=Function rows=15"; got != want {
		t.Fatalf("failure details = %v, want %v", got, want)
	}
	if got, want := db.execs[0].args[4], now.Add(2*time.Minute); got != want {
		t.Fatalf("next attempt = %v, want %v", got, want)
	}
}

func TestReducerQueueEnqueueAndClaimRoundTrip(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 12, 11, 0, 0, 0, time.UTC)
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{
				rows: [][]any{{
					"reducer_scope-123_generation-456_workload_identity_repo-123_fact-1_20260412110000.000000000_0",
					"scope-123",
					"generation-456",
					"workload_identity",
					1,
					now,
					now,
					[]byte(`{"entity_key":"repo-123","reason":"shared follow-up","fact_id":"fact-1","source_system":"git"}`),
				}},
			},
		},
	}

	queue := ReducerQueue{
		db:            db,
		LeaseOwner:    "reducer-1",
		LeaseDuration: time.Minute,
		Now:           func() time.Time { return now },
	}

	result, err := queue.Enqueue(context.Background(), []projector.ReducerIntent{{
		ScopeID:      "scope-123",
		GenerationID: "generation-456",
		Domain:       "workload_identity",
		EntityKey:    "repo-123",
		Reason:       "shared follow-up",
		FactID:       "fact-1",
		SourceSystem: "git",
	}})
	if err != nil {
		t.Fatalf("Enqueue() error = %v, want nil", err)
	}
	if got, want := result.Count, 1; got != want {
		t.Fatalf("Enqueue().Count = %d, want %d", got, want)
	}

	intent, ok, err := queue.Claim(context.Background())
	if err != nil {
		t.Fatalf("Claim() error = %v, want nil", err)
	}
	if !ok {
		t.Fatal("Claim() ok = false, want true")
	}
	if got, want := intent.Domain, reducer.DomainWorkloadIdentity; got != want {
		t.Fatalf("Claim().Domain = %q, want %q", got, want)
	}
	if got, want := intent.SourceSystem, "git"; got != want {
		t.Fatalf("Claim().SourceSystem = %q, want %q", got, want)
	}
	if got, want := intent.EntityKeys[0], "repo-123"; got != want {
		t.Fatalf("Claim().EntityKeys[0] = %q, want %q", got, want)
	}
	if got, want := intent.AttemptCount, 1; got != want {
		t.Fatalf("Claim().AttemptCount = %d, want %d", got, want)
	}
	if got, want := len(db.execs), 1; got != want {
		t.Fatalf("exec count = %d, want %d", got, want)
	}
	if !strings.Contains(db.execs[0].query, "INSERT INTO fact_work_items") {
		t.Fatalf("enqueue query = %q, want fact_work_items insert", db.execs[0].query)
	}
}

func TestReducerQueueEnqueueRejectsUnknownDomain(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 12, 11, 0, 0, 0, time.UTC)
	queue := ReducerQueue{
		db:            &fakeExecQueryer{},
		LeaseOwner:    "reducer-1",
		LeaseDuration: time.Minute,
		Now:           func() time.Time { return now },
	}

	_, err := queue.Enqueue(context.Background(), []projector.ReducerIntent{{
		ScopeID:      "scope-123",
		GenerationID: "generation-456",
		Domain:       "not_a_real_domain",
		EntityKey:    "repo-123",
		Reason:       "shared follow-up",
		FactID:       "fact-1",
		SourceSystem: "git",
	}})
	if err == nil {
		t.Fatal("Enqueue() error = nil, want non-nil")
	}
}

func TestScanReducerIntentRejectsUnknownDomain(t *testing.T) {
	t.Parallel()

	rows := &queueFakeRows{
		rows: [][]any{{
			"intent-1",
			"scope-123",
			"generation-456",
			"not_a_real_domain",
			1,
			time.Date(2026, time.April, 12, 11, 0, 0, 0, time.UTC),
			time.Date(2026, time.April, 12, 11, 0, 0, 0, time.UTC),
			[]byte(`{"entity_key":"repo-123","reason":"shared follow-up","fact_id":"fact-1","source_system":"git"}`),
		}},
	}

	if _, err := scanReducerIntent(rows); err == nil {
		t.Fatal("scanReducerIntent() error = nil, want non-nil")
	}
}

func TestReducerQueueFailRetriesRetryableErrorWithinAttemptBudget(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 12, 11, 0, 0, 0, time.UTC)
	db := &fakeExecQueryer{}
	queue := ReducerQueue{
		db:            db,
		LeaseOwner:    "reducer-1",
		LeaseDuration: time.Minute,
		RetryDelay:    2 * time.Minute,
		MaxAttempts:   3,
		Now:           func() time.Time { return now },
	}

	intent := reducer.Intent{
		IntentID:     "intent-1",
		AttemptCount: 1,
	}

	if err := queue.Fail(context.Background(), intent, retryableReducerTestError{message: "transient failure"}); err != nil {
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
			t.Fatalf("retry query missing %q:\n%s", want, query)
		}
	}
	if got, want := db.execs[0].args[1], "reducer_retryable"; got != want {
		t.Fatalf("failure class = %v, want %v", got, want)
	}
	if got, want := db.execs[0].args[4], now.Add(2*time.Minute); got != want {
		t.Fatalf("next attempt = %v, want %v", got, want)
	}
}

func TestReducerQueueFailMarksRetryableErrorTerminalWhenAttemptBudgetExhausted(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 12, 11, 0, 0, 0, time.UTC)
	db := &fakeExecQueryer{}
	queue := ReducerQueue{
		db:            db,
		LeaseOwner:    "reducer-1",
		LeaseDuration: time.Minute,
		RetryDelay:    2 * time.Minute,
		MaxAttempts:   2,
		Now:           func() time.Time { return now },
	}

	intent := reducer.Intent{
		IntentID:     "intent-1",
		AttemptCount: 2,
	}

	if err := queue.Fail(context.Background(), intent, retryableReducerTestError{message: "still broken"}); err != nil {
		t.Fatalf("Fail() error = %v, want nil", err)
	}

	if got, want := len(db.execs), 1; got != want {
		t.Fatalf("exec count = %d, want %d", got, want)
	}

	query := db.execs[0].query
	for _, want := range []string{
		"UPDATE fact_work_items",
		"status = 'dead_letter'",
		"failure_class = $2",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("terminal query missing %q:\n%s", want, query)
		}
	}
	if got, want := db.execs[0].args[1], "reducer_failed"; got != want {
		t.Fatalf("failure class = %v, want %v", got, want)
	}
}

func TestReducerQueueFailRetriesGraphWriteTimeoutWithinAttemptBudget(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 12, 11, 0, 0, 0, time.UTC)
	db := &fakeExecQueryer{}
	queue := ReducerQueue{
		db:            db,
		LeaseOwner:    "reducer-1",
		LeaseDuration: time.Minute,
		RetryDelay:    2 * time.Minute,
		MaxAttempts:   3,
		Now:           func() time.Time { return now },
	}
	intent := reducer.Intent{
		IntentID:     "intent-1",
		AttemptCount: 1,
	}
	cause := fmt.Errorf("semantic materialization: %w", sourcecypher.GraphWriteTimeoutError{
		Operation:   "neo4j execute timed out",
		Timeout:     30 * time.Second,
		TimeoutHint: "ESHU_CANONICAL_WRITE_TIMEOUT",
		Summary:     "semantic label=Annotation rows=10",
		Cause:       context.DeadlineExceeded,
	})

	if err := queue.Fail(context.Background(), intent, cause); err != nil {
		t.Fatalf("Fail() error = %v, want nil", err)
	}

	if got, want := len(db.execs), 1; got != want {
		t.Fatalf("exec count = %d, want %d", got, want)
	}
	if !strings.Contains(db.execs[0].query, "status = 'retrying'") {
		t.Fatalf("timeout should retry within attempt budget, query:\n%s", db.execs[0].query)
	}
	if got, want := db.execs[0].args[1], "reducer_retryable"; got != want {
		t.Fatalf("failure class = %v, want %v", got, want)
	}
	if got, want := db.execs[0].args[3], "semantic label=Annotation rows=10"; got != want {
		t.Fatalf("failure details = %v, want %v", got, want)
	}
	if got, want := db.execs[0].args[4], now.Add(2*time.Minute); got != want {
		t.Fatalf("next attempt = %v, want %v", got, want)
	}
}
