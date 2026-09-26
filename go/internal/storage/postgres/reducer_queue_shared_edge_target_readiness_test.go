// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
	"github.com/eshu-hq/eshu/go/internal/storage/cypher/edge/writer"
)

// absentEdgeTargetExecutor reports every target-presence probe as a miss, the
// state a deployment Repository node is in until another scope's
// materialization commits it. Writes are counted so a test can prove the
// guard ran no edge statement.
type absentEdgeTargetExecutor struct {
	writes int
}

func (e *absentEdgeTargetExecutor) Execute(context.Context, sourcecypher.Statement) error {
	e.writes++
	return nil
}

func (e *absentEdgeTargetExecutor) ExecuteProbe(context.Context, sourcecypher.Statement) (bool, error) {
	return false, nil
}

// sharedEdgeTargetMissCause drives the real shared-edge writer over a
// deployable-unit batch whose deployment Repository is absent and wraps the
// error exactly as DeployableUnitCorrelationHandler.materializeDeployableUnitEdges
// does, so the queue sees the production error chain.
func sharedEdgeTargetMissCause(t *testing.T) error {
	t.Helper()

	executor := &absentEdgeTargetExecutor{}
	_, err := writer.NewEdgeWriter(executor, 0).WriteEdges(
		context.Background(),
		reducer.DomainDeployableUnitEdges,
		[]reducer.SharedProjectionIntentRow{{
			IntentID:     "intent-du-edge-1",
			RepositoryID: "repo-app",
			GenerationID: "gen-1",
			Payload: map[string]any{
				"repo_id":             "repo-app",
				"deployment_repo_id":  "repo-deploy",
				"deployable_unit_key": "du-1",
				"correlation_key":     "corr-1",
				"confidence":          0.9,
				"reason":              "test",
			},
		}},
		"reducer/deployable-unit-correlation",
	)
	if err == nil {
		t.Fatal("WriteEdges() with an absent deployment target returned nil, want the guard's deferral error")
	}
	if executor.writes != 0 {
		t.Fatalf("edge writes = %d, want 0 while a target is absent", executor.writes)
	}
	return fmt.Errorf("write deployable unit correlation edges: %w", err)
}

// TestReducerQueueFailDefersSharedEdgeTargetMissPastAttemptBudget is the #7268
// regression. The shared-edge target guard fails a deployable_unit_edges batch
// with a retryable error when a target Repository is not yet committed, and
// its own doc calls the miss "a timing state, not a payload defect". Without a
// non-counting failure class the durable queue counted each retry toward
// MaxAttempts, so a slow sibling-scope write dead-lettered the
// deployable_unit_correlation intent and CORRELATES_DEPLOYABLE_UNIT was lost:
// a dead letter is never reopened.
func TestReducerQueueFailDefersSharedEdgeTargetMissPastAttemptBudget(t *testing.T) {
	t.Parallel()

	cause := sharedEdgeTargetMissCause(t)
	now := time.Date(2026, time.September, 26, 11, 0, 0, 0, time.UTC)
	database := &fakeExecQueryer{}
	queue := ReducerQueue{
		database:      database,
		LeaseOwner:    "reducer-1",
		LeaseDuration: time.Minute,
		RetryDelay:    2 * time.Minute,
		MaxAttempts:   3,
		Now:           func() time.Time { return now },
	}
	intent := reducer.Intent{
		IntentID:     "intent-deployable-unit-correlation-1",
		Domain:       reducer.DomainDeployableUnitCorrelation,
		AttemptCount: 3,
	}

	if err := queue.Fail(context.Background(), intent, cause); err != nil {
		t.Fatalf("Fail() error = %v, want nil", err)
	}
	if got, want := len(database.execs), 1; got != want {
		t.Fatalf("exec count = %d, want %d", got, want)
	}
	query := database.execs[0].query
	for _, want := range []string{
		"UPDATE fact_work_items",
		"status = 'retrying'",
		"next_attempt_at = $5",
		"failure_class = $2",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("deferred retry query missing %q (the intent was dead-lettered at the retry budget):\n%s", want, query)
		}
	}
	if got, want := database.execs[0].args[1], reducer.SharedEdgeTargetNotReadyFailureClass; got != want {
		t.Fatalf("failure class = %v, want %v", got, want)
	}
}

// TestReducerQueueClaimKeepsSharedEdgeTargetDeferralAttemptCount asserts both
// claim paths' attempt-count CASE freeze a retrying row in the shared-edge
// target class, so the reclaim after a miss does not erode the retry budget.
// The #7123 Shape C run shows why it matters beyond dead letters: a counted
// miss pushed the next real failure onto attempt 2 and doubled its backoff.
func TestReducerQueueClaimKeepsSharedEdgeTargetDeferralAttemptCount(t *testing.T) {
	t.Parallel()

	want := "work.failure_class = '" + reducer.SharedEdgeTargetNotReadyFailureClass + "'"
	if got := reducerClaimAttemptCountCaseSQL(); !strings.Contains(got, want) {
		t.Fatalf("claim attempt-count CASE missing %q, so a reclaimed target miss increments attempt_count:\n%s", want, got)
	}
	if !IsNonCountingReducerRetryFailureClass(reducer.SharedEdgeTargetNotReadyFailureClass) {
		t.Fatalf("IsNonCountingReducerRetryFailureClass(%q) = false, want true", reducer.SharedEdgeTargetNotReadyFailureClass)
	}
}
