// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// kubernetesCorrelationReadinessQueueDB is a fake reducer-queue backend that
// returns one pending kubernetes_correlation_materialization intent only when the
// KubernetesWorkload canonical-nodes readiness gate is satisfied. It proves the
// durable SQL gate (not just the in-handler ReadinessLookup) holds RUNS_IMAGE edge
// work until the #388 node slice's KubernetesWorkload nodes commit on the
// kubernetes_workload_uid keyspace — a different keyspace than the AWS/COVERS
// cloud_resource_uid gate, so it requires its own clause.
type kubernetesCorrelationReadinessQueueDB struct {
	now          time.Time
	phaseReady   bool
	status       string
	attemptCount int
	claimQueries int
}

func (db *kubernetesCorrelationReadinessQueueDB) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	return fakeResult{}, nil
}

func (db *kubernetesCorrelationReadinessQueueDB) QueryContext(_ context.Context, query string, _ ...any) (Rows, error) {
	if !strings.Contains(query, "FROM fact_work_items") || !strings.Contains(query, "FROM claimed") {
		return nil, fmt.Errorf("unexpected query: %s", query)
	}
	db.claimQueries++

	// The claim gate must fence the kubernetes edge domain on the workload-uid
	// keyspace, not the cloud_resource_uid keyspace the AWS/COVERS edges use.
	if !strings.Contains(query, "kubernetes_correlation_materialization") {
		return nil, fmt.Errorf("claim query missing kubernetes correlation readiness gate:\n%s", query)
	}
	if !strings.Contains(query, "kubernetes_workload_uid") {
		return nil, fmt.Errorf("claim query missing kubernetes_workload_uid keyspace gate:\n%s", query)
	}

	hasReadinessGate := queryHasBoundedReadinessRequirement(
		query,
		string(reducer.DomainKubernetesCorrelationMaterialization),
		"kubernetes_workload_uid",
		"canonical_nodes_committed",
	) && queryHasPayloadReadinessLookup(query, "fact_work_items", "readiness_req", "readiness_phase")
	if hasReadinessGate && !db.phaseReady {
		return &queueFakeRows{}, nil
	}

	status := strings.TrimSpace(db.status)
	if status == "" {
		status = "pending"
	}
	if status != "pending" && status != "retrying" {
		return &queueFakeRows{}, nil
	}

	return &queueFakeRows{rows: [][]any{{
		"reducer-k8s-edge-1",
		"k8s:prod-us-east-1",
		"gen-k8s-1",
		string(reducer.DomainKubernetesCorrelationMaterialization),
		db.attemptCount + 1,
		db.now.Add(-time.Minute),
		db.now.Add(-time.Minute),
		[]byte(`{"entity_key":"kubernetes_workload_materialization:k8s:prod-us-east-1","reason":"kubernetes live workload facts observed","fact_id":"fact-pod-1","source_system":"kubernetes"}`),
	}}}, nil
}

func kubernetesCorrelationReadinessQueue(db *kubernetesCorrelationReadinessQueueDB, now time.Time) ReducerQueue {
	return ReducerQueue{
		db:            db,
		LeaseOwner:    "test-owner",
		LeaseDuration: time.Minute,
		Now:           func() time.Time { return now },
	}
}

func TestReducerQueueClaimWaitsForKubernetesWorkloadReadinessBehavior(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 1, 11, 10, 0, 0, time.UTC)
	db := &kubernetesCorrelationReadinessQueueDB{
		now:        now,
		phaseReady: false,
		status:     "pending",
	}
	queue := kubernetesCorrelationReadinessQueue(db, now)

	intent, claimed, err := queue.Claim(context.Background())
	if err != nil {
		t.Fatalf("Claim() error = %v", err)
	}
	if claimed {
		t.Fatalf("Claim() claimed %q before workload-node readiness, want unclaimed waiting work", intent.IntentID)
	}

	db.phaseReady = true
	intent, claimed, err = queue.Claim(context.Background())
	if err != nil {
		t.Fatalf("Claim() after readiness error = %v", err)
	}
	if !claimed {
		t.Fatal("Claim() after readiness claimed = false, want true")
	}
	if got, want := intent.Domain, reducer.DomainKubernetesCorrelationMaterialization; got != want {
		t.Fatalf("claimed domain = %q, want %q", got, want)
	}
	if got, want := intent.EntityKeys, []string{"kubernetes_workload_materialization:k8s:prod-us-east-1"}; fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("claimed entity keys = %v, want %v", got, want)
	}
}

// kubernetesCorrelationNodesNotReadyTestError stands in for the handler's
// kubernetesCorrelationNodesNotReadyError: a retryable readiness-gate miss that
// self-classifies with the kubernetes-correlation not-ready failure class. The
// queue must defer it without consuming the retry budget (issue #4142 item 3), so
// an in-handler readiness miss can never dead-letter a still-pending edge intent
// that the succeeded-only reopen path would not reopen.
type kubernetesCorrelationNodesNotReadyTestError struct{}

func (kubernetesCorrelationNodesNotReadyTestError) Error() string {
	return "canonical kubernetes workload nodes not committed"
}

func (kubernetesCorrelationNodesNotReadyTestError) Retryable() bool { return true }

func (kubernetesCorrelationNodesNotReadyTestError) FailureClass() string {
	return reducer.KubernetesCorrelationNodesNotReadyFailureClass
}

// TestReducerQueueFailDefersKubernetesCorrelationReadinessPastAttemptBudget proves
// the Go retry classifier treats a kubernetes-correlation readiness miss as
// non-counting: even at an attempt count far past MaxAttempts, Fail re-queues the
// row as retrying rather than dead-lettering it. Mirrors the secrets/IAM endpoint
// readiness contract.
func TestReducerQueueFailDefersKubernetesCorrelationReadinessPastAttemptBudget(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 2, 11, 0, 0, 0, time.UTC)
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
		IntentID:     "intent-k8s-edge-1",
		AttemptCount: 42,
	}

	if err := queue.Fail(context.Background(), intent, kubernetesCorrelationNodesNotReadyTestError{}); err != nil {
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
			t.Fatalf("deferred retry query missing %q:\n%s", want, query)
		}
	}
	if got, want := db.execs[0].args[1], reducer.KubernetesCorrelationNodesNotReadyFailureClass; got != want {
		t.Fatalf("failure class = %v, want %v", got, want)
	}
	if got, want := db.execs[0].args[4], now.Add(2*time.Minute); got != want {
		t.Fatalf("next attempt = %v, want %v", got, want)
	}
}

// TestReducerQueueClaimDoesNotCountKubernetesCorrelationReadinessDefers asserts the
// single-claim attempt-count CASE leaves a kubernetes-correlation readiness defer's
// attempt count unchanged, so a row that briefly re-enters the in-handler readiness
// gate does not erode its retry budget on claim.
func TestReducerQueueClaimDoesNotCountKubernetesCorrelationReadinessDefers(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 2, 12, 0, 0, 0, time.UTC)
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: nil},
		},
	}
	queue := ReducerQueue{
		db:            db,
		LeaseOwner:    "test-owner",
		LeaseDuration: 30 * time.Second,
		Now:           func() time.Time { return now },
	}

	if _, claimed, err := queue.Claim(context.Background()); err != nil {
		t.Fatalf("Claim() error = %v", err)
	} else if claimed {
		t.Fatal("Claim() claimed = true, want false from empty rows")
	}

	assertKubernetesCorrelationReadinessClaimDoesNotCountAttempt(t, db.queries[0].query)
}

// TestClaimBatchDoesNotCountKubernetesCorrelationReadinessDefers asserts the batch
// claim query carries the same non-counting attempt-count CASE, since both claim
// paths must agree on which readiness classes are exempt from the retry budget.
func TestClaimBatchDoesNotCountKubernetesCorrelationReadinessDefers(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 2, 12, 0, 0, 0, time.UTC)
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: nil},
		},
	}
	queue := ReducerQueue{
		db:            db,
		LeaseOwner:    "test",
		LeaseDuration: time.Minute,
		Now:           func() time.Time { return now },
	}

	if _, err := queue.ClaimBatch(context.Background(), 5); err != nil {
		t.Fatalf("ClaimBatch() error = %v", err)
	}

	assertKubernetesCorrelationReadinessClaimDoesNotCountAttempt(t, db.queries[0].query)
}

func assertKubernetesCorrelationReadinessClaimDoesNotCountAttempt(t *testing.T, query string) {
	t.Helper()

	for _, want := range []string{
		"attempt_count = CASE",
		"work.status = 'retrying'",
		"work.failure_class = 'kubernetes_correlation_nodes_not_ready'",
		"THEN work.attempt_count",
		"ELSE work.attempt_count + 1",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("claim query missing non-counting defer attempt predicate %q:\n%s", want, query)
		}
	}
}
