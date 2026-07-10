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

// gcpRelationshipReadinessQueueDB is a fake reducer-queue backend that returns
// one pending gcp_relationship_materialization intent only when the GCP
// resource canonical-nodes readiness gate is satisfied. It proves the durable
// SQL claim-time gate — not just the in-handler ReadinessLookup — holds GCP
// relationship edge work until DomainGCPResourceMaterialization publishes its
// cloud_resource_uid/canonical_nodes_committed phase for the same acceptance
// unit, mirroring the AWS/Azure/workload_cloud claim-gate enrollment
// (reducerClaimReadinessRequirementsSQL in reducer_queue_readiness_sql.go).
// Before this enrollment, gcp_relationship_materialization was absent from
// that CTE and could claim before GCP nodes committed.
type gcpRelationshipReadinessQueueDB struct {
	now          time.Time
	phaseReady   bool
	status       string
	attemptCount int
	claimQueries int
}

func (db *gcpRelationshipReadinessQueueDB) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	return fakeResult{}, nil
}

func (db *gcpRelationshipReadinessQueueDB) QueryContext(_ context.Context, query string, _ ...any) (Rows, error) {
	if !strings.Contains(query, "FROM fact_work_items") || !strings.Contains(query, "FROM claimed") {
		return nil, fmt.Errorf("unexpected query: %s", query)
	}
	db.claimQueries++

	if !strings.Contains(query, "gcp_relationship_materialization") {
		return nil, fmt.Errorf("claim query missing gcp relationship readiness gate:\n%s", query)
	}
	if !strings.Contains(query, "cloud_resource_uid") {
		return nil, fmt.Errorf("claim query missing cloud_resource_uid keyspace gate:\n%s", query)
	}

	hasReadinessGate := queryHasBoundedReadinessRequirement(
		query,
		string(reducer.DomainGCPRelationshipMaterialization),
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
		"reducer-gcp-rel-1",
		"gcp:my-project:us-central1:compute",
		"gen-gcp-1",
		string(reducer.DomainGCPRelationshipMaterialization),
		db.attemptCount + 1,
		db.now.Add(-time.Minute),
		db.now.Add(-time.Minute),
		[]byte(`{"entity_key":"gcp_resource_materialization:gcp:my-project:us-central1:compute","reason":"gcp runtime relationship facts observed","fact_id":"fact-gcp-rel-1","source_system":"gcp"}`),
	}}}, nil
}

func gcpRelationshipReadinessQueue(db *gcpRelationshipReadinessQueueDB, now time.Time) ReducerQueue {
	return ReducerQueue{
		db:            db,
		LeaseOwner:    "test-owner",
		LeaseDuration: time.Minute,
		Now:           func() time.Time { return now },
	}
}

// TestReducerQueueClaimWaitsForGCPRelationshipReadinessBehavior is the
// claim-gate readiness regression: a gcp_relationship_materialization intent
// must not be claimable until its upstream cloud_resource_uid/
// canonical_nodes_committed phase row exists for the same acceptance unit.
// This fails red on main because gcp_relationship_materialization is absent
// from reducerClaimReadinessRequirementsSQL, so the fake claim query never
// finds the domain/keyspace gate clause and the first Claim() call succeeds
// immediately instead of waiting.
func TestReducerQueueClaimWaitsForGCPRelationshipReadinessBehavior(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 1, 11, 10, 0, 0, time.UTC)
	db := &gcpRelationshipReadinessQueueDB{
		now:        now,
		phaseReady: false,
		status:     "pending",
	}
	queue := gcpRelationshipReadinessQueue(db, now)

	intent, claimed, err := queue.Claim(context.Background())
	if err != nil {
		t.Fatalf("Claim() error = %v", err)
	}
	if claimed {
		t.Fatalf("Claim() claimed %q before GCP resource-node readiness, want unclaimed waiting work", intent.IntentID)
	}

	db.phaseReady = true
	intent, claimed, err = queue.Claim(context.Background())
	if err != nil {
		t.Fatalf("Claim() after readiness error = %v", err)
	}
	if !claimed {
		t.Fatal("Claim() after readiness claimed = false, want true")
	}
	if got, want := intent.Domain, reducer.DomainGCPRelationshipMaterialization; got != want {
		t.Fatalf("claimed domain = %q, want %q", got, want)
	}
	if got, want := intent.EntityKeys, []string{"gcp_resource_materialization:gcp:my-project:us-central1:compute"}; fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("claimed entity keys = %v, want %v", got, want)
	}
}

// gcpRelationshipNodesNotReadyTestError stands in for the handler's
// gcpRelationshipNodesNotReadyError: a retryable readiness-gate miss that
// self-classifies with the GCP-relationship not-ready failure class. The
// queue must defer it without consuming the retry budget, mirroring the
// secrets/IAM endpoint and kubernetes-correlation non-counting contracts, so
// an in-handler readiness miss can never dead-letter a still-pending GCP edge
// intent that the succeeded-only reopen path would not reopen.
type gcpRelationshipNodesNotReadyTestError struct{}

func (gcpRelationshipNodesNotReadyTestError) Error() string {
	return "canonical gcp cloud resource nodes not committed"
}

func (gcpRelationshipNodesNotReadyTestError) Retryable() bool { return true }

func (gcpRelationshipNodesNotReadyTestError) FailureClass() string {
	return reducer.GCPRelationshipNodesNotReadyFailureClass
}

// TestReducerQueueFailDefersGCPRelationshipReadinessPastAttemptBudget proves
// the Go retry classifier treats a GCP-relationship readiness miss as
// non-counting: even at an attempt count far past MaxAttempts, Fail re-queues
// the row as retrying rather than dead-lettering it. This fails red on main
// because reducer.GCPRelationshipNodesNotReadyFailureClass does not exist and
// "gcp_relationship_nodes_not_ready" is absent from
// nonCountingReducerRetryFailureClasses, so Fail would dead-letter instead of
// deferring.
func TestReducerQueueFailDefersGCPRelationshipReadinessPastAttemptBudget(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 2, 11, 0, 0, 0, time.UTC)
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
		IntentID:     "intent-gcp-edge-1",
		AttemptCount: 42,
	}

	if err := queue.Fail(context.Background(), intent, gcpRelationshipNodesNotReadyTestError{}); err != nil {
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
	if got, want := db.execs[0].args[1], reducer.GCPRelationshipNodesNotReadyFailureClass; got != want {
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

// TestReducerQueueClaimDoesNotCountGCPRelationshipReadinessDefers asserts the
// single-claim attempt-count CASE leaves a GCP-relationship readiness defer's
// attempt count unchanged, so a row that briefly re-enters the in-handler
// readiness gate does not erode its retry budget on claim.
func TestReducerQueueClaimDoesNotCountGCPRelationshipReadinessDefers(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 2, 12, 0, 0, 0, time.UTC)
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

	assertGCPRelationshipReadinessClaimDoesNotCountAttempt(t, db.queries[0].query)
}

// TestClaimBatchDoesNotCountGCPRelationshipReadinessDefers asserts the batch
// claim query carries the same non-counting attempt-count CASE, since both
// claim paths must agree on which readiness classes are exempt from the retry
// budget.
func TestClaimBatchDoesNotCountGCPRelationshipReadinessDefers(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 2, 12, 0, 0, 0, time.UTC)
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

	assertGCPRelationshipReadinessClaimDoesNotCountAttempt(t, db.queries[0].query)
}

func assertGCPRelationshipReadinessClaimDoesNotCountAttempt(t *testing.T, query string) {
	t.Helper()

	for _, want := range []string{
		"attempt_count = CASE",
		"work.status = 'retrying'",
		"work.failure_class = 'gcp_relationship_nodes_not_ready'",
		"THEN work.attempt_count",
		"ELSE work.attempt_count + 1",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("claim query missing non-counting defer attempt predicate %q:\n%s", want, query)
		}
	}
}
