// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"strings"
	"testing"
	"time"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/securityalert"
)

// securityAlertImpactNotReadyTestError stands in for the handler's
// impactInFlightError: a retryable expected wait self-classified with the
// security_alert_reconciliation impact-not-ready class (#8827).
type securityAlertImpactNotReadyTestError struct{}

func (securityAlertImpactNotReadyTestError) Error() string {
	return "security alert reconciliation waiting for supply chain impact work in flight"
}
func (securityAlertImpactNotReadyTestError) Retryable() bool { return true }
func (securityAlertImpactNotReadyTestError) FailureClass() string {
	return securityalert.ImpactNotReadyFailureClass
}

// TestReducerQueueFailDefersSecurityAlertImpactNotReadyPastAttemptBudget proves
// that even at an attempt count far past MaxAttempts a reconciliation waiting
// on its producer is re-queued as retrying, never dead-lettered: a dead letter
// is not reopened, so the alerts would never reconcile.
func TestReducerQueueFailDefersSecurityAlertImpactNotReadyPastAttemptBudget(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 24, 12, 0, 0, 0, time.UTC)
	db := &fakeExecQueryer{}
	queue := ReducerQueue{
		database: db, LeaseOwner: "reducer-1", LeaseDuration: time.Minute,
		RetryDelay: 2 * time.Minute, MaxAttempts: 3, Now: func() time.Time { return now },
	}
	intent := reducercontract.Intent{IntentID: "intent-alert-impact-wait-1", AttemptCount: 42}

	if err := queue.Fail(context.Background(), intent, securityAlertImpactNotReadyTestError{}); err != nil {
		t.Fatalf("Fail() error = %v, want nil", err)
	}
	if got, want := len(db.execs), 1; got != want {
		t.Fatalf("exec count = %d, want %d (a non-counting defer, not a dead-letter)", got, want)
	}
	query := db.execs[0].query
	for _, want := range []string{"UPDATE fact_work_items", "status = 'retrying'", "next_attempt_at = $5", "failure_class = $2"} {
		if !strings.Contains(query, want) {
			t.Fatalf("deferred retry query missing %q:\n%s", want, query)
		}
	}
	if got, want := db.execs[0].args[1], securityalert.ImpactNotReadyFailureClass; got != want {
		t.Fatalf("failure class = %v, want %v", got, want)
	}
}

// TestReducerQueueClaimsKeepAttemptCountForSecurityAlertImpactNotReady asserts
// both claim paths (single and batch) derive their attempt-count CASE with the
// class, so a retrying row in it keeps attempt_count on every claim.
func TestReducerQueueClaimsKeepAttemptCountForSecurityAlertImpactNotReady(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 24, 12, 0, 0, 0, time.UTC)
	for name, claim := range map[string]func(ReducerQueue) error{
		"Claim": func(q ReducerQueue) error { _, _, err := q.Claim(context.Background()); return err },
		"ClaimBatch": func(q ReducerQueue) error {
			_, err := q.ClaimBatch(context.Background(), 5)
			return err
		},
	} {
		db := &fakeExecQueryer{queryResponses: []queueFakeRows{{rows: nil}}}
		queue := ReducerQueue{
			database: db, LeaseOwner: "test", LeaseDuration: time.Minute, Now: func() time.Time { return now },
		}
		if err := claim(queue); err != nil {
			t.Fatalf("%s() error = %v", name, err)
		}
		query := db.queries[0].query
		for _, want := range []string{
			"attempt_count = CASE",
			"work.status = 'retrying'",
			"work.failure_class = 'security_alert_reconciliation_impact_not_ready'",
			"THEN work.attempt_count",
			"ELSE work.attempt_count + 1",
		} {
			if !strings.Contains(query, want) {
				t.Fatalf("%s query missing %q:\n%s", name, want, query)
			}
		}
	}
}
