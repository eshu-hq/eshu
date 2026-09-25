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
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
)

// TestReducerQueueFailDefersGenerationActivationPastAttemptBudget pins the
// #6686 not-yet-active generation deferral as a non-counting readiness miss:
// the exact error Runtime.execute returns (the freshness check's
// GenerationNotYetActiveError wrapped with %w) drives Fail far past
// MaxAttempts, and the row stays 'retrying' in the
// generation_activation_not_ready class instead of dead-lettering.
func TestReducerQueueFailDefersGenerationActivationPastAttemptBudget(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.September, 25, 5, 0, 0, 0, time.UTC)
	database := &fakeExecQueryer{}
	queue := ReducerQueue{
		database:      database,
		LeaseOwner:    "reducer-1",
		LeaseDuration: time.Minute,
		RetryDelay:    2 * time.Minute,
		MaxAttempts:   3,
		Now:           func() time.Time { return now },
	}
	intent := reducer.Intent{IntentID: "intent-6686", ScopeID: "scope-1", GenerationID: "gen-2", AttemptCount: 42}
	cause := fmt.Errorf("generation freshness check: %w", reducercontract.GenerationNotYetActiveError{
		ScopeID: "scope-1", GenerationID: "gen-2", ActiveGenerationID: "gen-1",
	})

	if err := queue.Fail(context.Background(), intent, cause); err != nil {
		t.Fatalf("Fail() error = %v, want nil", err)
	}
	if got, want := len(database.execs), 1; got != want {
		t.Fatalf("exec count = %d, want %d", got, want)
	}
	query := database.execs[0].query
	for _, want := range []string{"UPDATE fact_work_items", "status = 'retrying'", "failure_class = $2"} {
		if !strings.Contains(query, want) {
			t.Fatalf("deferred retry query missing %q:\n%s", want, query)
		}
	}
	if strings.Contains(query, "dead_letter") {
		t.Fatalf("not-yet-active deferral past the budget must not dead-letter:\n%s", query)
	}
	if got, want := database.execs[0].args[1], reducercontract.GenerationActivationNotReadyFailureClass; got != want {
		t.Fatalf("failure class = %v, want %v", got, want)
	}
}

// TestReducerClaimAttemptCountFreezesGenerationActivationClass pins the SQL
// half of the non-counting contract: both claim paths render the
// attempt-count CASE from nonCountingReducerRetryFailureClasses, so a
// retrying generation_activation_not_ready row keeps its attempt_count.
func TestReducerClaimAttemptCountFreezesGenerationActivationClass(t *testing.T) {
	t.Parallel()

	want := "work.failure_class = '" + reducercontract.GenerationActivationNotReadyFailureClass + "'"
	if !strings.Contains(reducerClaimAttemptCountCaseSQL(), want) {
		t.Fatalf("claim attempt-count CASE missing %q:\n%s", want, reducerClaimAttemptCountCaseSQL())
	}
	if !IsNonCountingReducerRetryFailureClass(reducercontract.GenerationActivationNotReadyFailureClass) {
		t.Fatal("generation_activation_not_ready is not enrolled as non-counting")
	}
}
