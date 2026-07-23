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

// ec2InstanceIdentityReadinessQueueDB proves the durable reducer queue gate
// keeps EC2 instance identity (#5448 ami_id) node-property updates waiting
// until the EC2 instance CloudResource nodes for the same scope generation
// have committed. Before this enrollment,
// ec2_instance_identity_materialization was absent from
// reducerClaimReadinessRequirementsSQL's VALUES list, so the claim gate's
// NOT EXISTS predicate was vacuously satisfied for this domain — it could
// claim work immediately, before the EC2 instance node phase committed, with
// only the in-handler ReadinessLookup (a defense-in-depth check, not the
// load-bearing fence) standing between a claim and a premature write attempt.
type ec2InstanceIdentityReadinessQueueDB struct {
	now          time.Time
	phaseReady   bool
	status       string
	attemptCount int
}

func (db *ec2InstanceIdentityReadinessQueueDB) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	return fakeResult{}, nil
}

func (db *ec2InstanceIdentityReadinessQueueDB) QueryContext(_ context.Context, query string, _ ...any) (Rows, error) {
	if !strings.Contains(query, "FROM fact_work_items") || !strings.Contains(query, "FROM claimed") {
		return nil, fmt.Errorf("unexpected query: %s", query)
	}
	if !strings.Contains(query, "ec2_instance_identity_materialization") {
		return nil, fmt.Errorf("claim query missing ec2 instance identity readiness gate:\n%s", query)
	}
	hasReadinessGate := queryHasBoundedReadinessRequirement(
		query,
		string(reducer.DomainEC2InstanceIdentityMaterialization),
		"cloud_resource_uid",
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
		"reducer-ec2-instance-identity-1",
		"aws:123456789012:us-east-1:ec2",
		"gen-aws-1",
		string(reducer.DomainEC2InstanceIdentityMaterialization),
		db.attemptCount + 1,
		db.now.Add(-time.Minute),
		db.now.Add(-time.Minute),
		[]byte(`{"entity_key":"ec2_instance_node_materialization:aws:123456789012:us-east-1:ec2","reason":"aws resource facts observed for ec2 instance identity projection","fact_id":"fact-aws-resource-1","source_system":"aws"}`),
	}}}, nil
}

func TestReducerQueueClaimQueryGatesEC2InstanceIdentityOnInstanceNodeReadiness(t *testing.T) {
	t.Parallel()

	if !queryHasBoundedReadinessRequirement(
		claimReducerWorkQuery,
		string(reducer.DomainEC2InstanceIdentityMaterialization),
		"cloud_resource_uid",
		"canonical_nodes_committed",
	) {
		t.Fatalf("claim query missing EC2 instance identity readiness requirement:\n%s", claimReducerWorkQuery)
	}
	if !queryHasPayloadReadinessLookup(claimReducerWorkQuery, "fact_work_items", "readiness_req", "readiness_phase") {
		t.Fatalf("claim query missing EC2 instance identity payload readiness lookup:\n%s", claimReducerWorkQuery)
	}
}

func TestReducerQueueBatchClaimQueryGatesEC2InstanceIdentityOnInstanceNodeReadiness(t *testing.T) {
	t.Parallel()

	if !queryHasBoundedReadinessRequirement(
		claimReducerWorkBatchQuery,
		string(reducer.DomainEC2InstanceIdentityMaterialization),
		"cloud_resource_uid",
		"canonical_nodes_committed",
	) {
		t.Fatalf("batch claim query missing EC2 instance identity readiness requirement:\n%s", claimReducerWorkBatchQuery)
	}
	if !queryHasRankOnceRepresentativeReadinessGate(claimReducerWorkBatchQuery, "fact_work_items", "readiness_req", "readiness_phase") {
		t.Fatalf("batch claim query missing EC2 instance identity representative readiness lookup:\n%s", claimReducerWorkBatchQuery)
	}
}

// TestReducerQueueClaimWaitsForEC2InstanceIdentityReadinessBehavior is the
// fail-before/pass-after proof: with phaseReady=false the claim query's
// bounded readiness gate (exercised via the real ReducerQueue.Claim path, not
// a hand-rolled predicate) returns no claimable row, and only after the EC2
// instance node phase becomes ready does the claim succeed. RED before the
// requirements-SQL enrollment (the domain claimed on the very first attempt
// regardless of db.phaseReady, because the NOT EXISTS predicate had no
// requirement row to check against), GREEN after.
func TestReducerQueueClaimWaitsForEC2InstanceIdentityReadinessBehavior(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 24, 14, 0, 0, 0, time.UTC)
	db := &ec2InstanceIdentityReadinessQueueDB{
		now:        now,
		phaseReady: false,
		status:     "pending",
	}
	queue := ReducerQueue{
		db:            db,
		LeaseOwner:    "test-owner",
		LeaseDuration: time.Minute,
		Now:           func() time.Time { return now },
	}

	intent, claimed, err := queue.Claim(context.Background())
	if err != nil {
		t.Fatalf("Claim() error = %v", err)
	}
	if claimed {
		t.Fatalf("Claim() claimed %q before canonical readiness, want unclaimed waiting work", intent.IntentID)
	}

	db.phaseReady = true
	intent, claimed, err = queue.Claim(context.Background())
	if err != nil {
		t.Fatalf("Claim() after readiness error = %v", err)
	}
	if !claimed {
		t.Fatal("Claim() after readiness claimed = false, want true")
	}
	if got, want := intent.Domain, reducer.DomainEC2InstanceIdentityMaterialization; got != want {
		t.Fatalf("claimed domain = %q, want %q", got, want)
	}
	if got, want := intent.EntityKeys, []string{"ec2_instance_node_materialization:aws:123456789012:us-east-1:ec2"}; fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("claimed entity keys = %v, want %v", got, want)
	}
}

func TestReducerConflictBlockageReportsEC2InstanceIdentityReadiness(t *testing.T) {
	t.Parallel()

	if !strings.Contains(reducerConflictBlockageQuery, "ec2_instance_identity_materialization") {
		t.Fatalf("blockage query missing ec2 instance identity readiness domain:\n%s", reducerConflictBlockageQuery)
	}
}

// ec2InstanceIdentityNodesNotReadyTestError stands in for the handler's
// ec2InstanceIdentityNodesNotReadyError: a retryable readiness-gate miss that
// self-classifies with reducer.EC2InstanceIdentityNodesNotReadyFailureClass.
// The queue must defer it without consuming the retry budget, mirroring the
// GCP-relationship / kubernetes-correlation / secrets-IAM non-counting
// contracts, so an in-handler readiness miss can never dead-letter a
// still-pending ami_id intent the succeeded-only reopen path would not
// reopen.
type ec2InstanceIdentityNodesNotReadyTestError struct{}

func (ec2InstanceIdentityNodesNotReadyTestError) Error() string {
	return "canonical ec2 instance nodes not committed"
}

func (ec2InstanceIdentityNodesNotReadyTestError) Retryable() bool { return true }

func (ec2InstanceIdentityNodesNotReadyTestError) FailureClass() string {
	return reducer.EC2InstanceIdentityNodesNotReadyFailureClass
}

// TestReducerQueueFailDefersEC2InstanceIdentityReadinessPastAttemptBudget
// proves the Go retry classifier treats an EC2 instance identity readiness
// miss as non-counting: even at an attempt count far past MaxAttempts, Fail
// re-queues the row as retrying rather than dead-lettering it. This fails red
// without reducer.EC2InstanceIdentityNodesNotReadyFailureClass registered in
// nonCountingReducerRetryFailureClasses, in which case Fail would dead-letter
// instead of deferring.
func TestReducerQueueFailDefersEC2InstanceIdentityReadinessPastAttemptBudget(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 24, 11, 0, 0, 0, time.UTC)
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
		IntentID:     "intent-ec2-instance-identity-1",
		AttemptCount: 42,
	}

	if err := queue.Fail(context.Background(), intent, ec2InstanceIdentityNodesNotReadyTestError{}); err != nil {
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
	if got, want := db.execs[0].args[1], reducer.EC2InstanceIdentityNodesNotReadyFailureClass; got != want {
		t.Fatalf("failure class = %v, want %v", got, want)
	}
	// Exponential backoff (#4450): AttemptCount=42 (a non-counting readiness
	// class keeps retrying indefinitely) drives the exponential term far past
	// MaxRetryDelay's default 1-hour fallback (unset here), so the delay
	// clamps to defaultRetryMaxDelayFallback rather than doubling forever.
	if got, want := db.execs[0].args[4], now.Add(defaultRetryMaxDelayFallback); got != want {
		t.Fatalf("next attempt = %v, want %v", got, want)
	}
}

// TestReducerQueueClaimDoesNotCountEC2InstanceIdentityReadinessDefers asserts
// the single-claim attempt-count CASE leaves an EC2 instance identity
// readiness defer's attempt count unchanged, so a row that briefly re-enters
// the in-handler readiness gate does not erode its retry budget on claim.
func TestReducerQueueClaimDoesNotCountEC2InstanceIdentityReadinessDefers(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 24, 12, 0, 0, 0, time.UTC)
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

	assertEC2InstanceIdentityReadinessClaimDoesNotCountAttempt(t, db.queries[0].query)
}

// TestClaimBatchDoesNotCountEC2InstanceIdentityReadinessDefers asserts the
// batch claim query carries the same non-counting attempt-count CASE, since
// both claim paths must agree on which readiness classes are exempt from the
// retry budget.
func TestClaimBatchDoesNotCountEC2InstanceIdentityReadinessDefers(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 24, 12, 0, 0, 0, time.UTC)
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

	assertEC2InstanceIdentityReadinessClaimDoesNotCountAttempt(t, db.queries[0].query)
}

func assertEC2InstanceIdentityReadinessClaimDoesNotCountAttempt(t *testing.T, query string) {
	t.Helper()

	for _, want := range []string{
		"attempt_count = CASE",
		"work.status = 'retrying'",
		"work.failure_class = 'ec2_instance_identity_nodes_not_ready'",
		"THEN work.attempt_count",
		"ELSE work.attempt_count + 1",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("claim query missing non-counting defer attempt predicate %q:\n%s", want, query)
		}
	}
}
