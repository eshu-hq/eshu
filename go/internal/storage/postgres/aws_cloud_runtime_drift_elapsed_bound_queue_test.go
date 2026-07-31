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

	"github.com/eshu-hq/eshu/go/internal/correlation/drift/cloudruntime"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// awsCloudRuntimeDriftElapsedBoundQueueDB fakes just enough of the reducer
// queue's Postgres surface to drive the REAL reducer.ReducerQueue.Claim/Fail
// through repeated cycles against ONE aws_cloud_runtime_drift work item,
// tracking status/attempt_count exactly as the real claim and retry SQL
// would: attempt_count increments on claim only when the row is NOT already
// 'retrying' under a non-counting failure class
// (reducerClaimAttemptCountCaseSQL's CASE), and the retry statement
// (retryReducerWorkQuery) never touches attempt_count at all -- this is the
// P0 mechanism itself, faked faithfully rather than assumed away.
type awsCloudRuntimeDriftElapsedBoundQueueDB struct {
	status       string // "pending" -> "claimed" -> "retrying" -> ...
	attemptCount int
	failureClass string
	enqueuedAt   time.Time
	claims       int
}

func (db *awsCloudRuntimeDriftElapsedBoundQueueDB) QueryContext(
	_ context.Context, query string, _ ...any,
) (Rows, error) {
	if !strings.Contains(query, "FROM fact_work_items") || !strings.Contains(query, "FROM claimed") {
		return nil, fmt.Errorf("unexpected query: %s", query)
	}
	if db.status != "pending" && db.status != "retrying" {
		return &queueFakeRows{}, nil
	}

	// reducerClaimAttemptCountCaseSQL: only skip the increment when the row
	// is ALREADY 'retrying' under a class this package's shared registry
	// treats as non-counting -- the exact production predicate, not a stand-in.
	nonCounting := db.status == "retrying" && isNonCountingReducerRetryFailureClass(db.failureClass)
	if !nonCounting {
		db.attemptCount++
	}
	db.status = "claimed"
	db.claims++

	return &queueFakeRows{rows: [][]any{{
		"work-aws-drift-elapsed-bound",
		"aws:123456789012:us-east-1",
		"gen-elapsed-bound",
		string(reducer.DomainAWSCloudRuntimeDrift),
		db.attemptCount,
		// container_image_identity_claim_epoch: this test never exercises the
		// container_image_identity domain, so the epoch stays at its zero
		// opt-out value.
		int64(0),
		db.enqueuedAt,
		db.enqueuedAt,
		// cycle_started_at: this test proves the attempt_count freeze, not the
		// reopen anchor (that is
		// TestAWSCloudRuntimeDriftReopenGetsFreshElapsedBoundWhileStatePendingLive),
		// so it always equals enqueuedAt -- COALESCE(reopened_at, created_at)
		// with reopened_at NULL, matching a row that has never been reopened.
		db.enqueuedAt,
		[]byte(`{"reason":"aws runtime resource facts observed","source_system":"aws"}`),
	}}}, nil
}

func (db *awsCloudRuntimeDriftElapsedBoundQueueDB) ExecContext(
	_ context.Context, query string, args ...any,
) (sql.Result, error) {
	if strings.Contains(query, "SET status = 'retrying'") {
		db.status = "retrying"
		if len(args) > 1 {
			db.failureClass, _ = args[1].(string)
		}
	}
	return fakeResult{}, nil
}

// alwaysOrphanedAWSCloudRuntimeDriftEvidenceLoader always returns one AWS
// resource with no matching Terraform state, so Classify admits exactly one
// orphaned_cloud_resource candidate on every Handle call -- the only finding
// kind a pending state scope can still improve, and therefore the only shape
// that ever reaches the readiness defer this test exercises.
type alwaysOrphanedAWSCloudRuntimeDriftEvidenceLoader struct{}

func (alwaysOrphanedAWSCloudRuntimeDriftEvidenceLoader) LoadAWSCloudRuntimeDriftEvidence(
	_ context.Context, scopeID, _ string,
) ([]cloudruntime.AddressedRow, error) {
	arn := "arn:aws:lambda:us-east-1:123456789012:function:elapsed-bound"
	return []cloudruntime.AddressedRow{{
		ARN:          arn,
		ResourceType: "aws_lambda_function",
		Cloud: &cloudruntime.ResourceRow{
			ARN:          arn,
			ResourceType: "aws_lambda_function",
			ScopeID:      scopeID,
		},
	}}, nil
}

// countingAWSCloudRuntimeDriftFindingWriter counts WriteAWSCloudRuntimeDriftFindings
// calls without touching Postgres -- this test's terminal-commit assertion is
// "the writer was called", not the writer's own transaction, which is proven
// live elsewhere (aws_cloud_runtime_drift_admission_live_test.go).
type countingAWSCloudRuntimeDriftFindingWriter struct{ calls int }

func (w *countingAWSCloudRuntimeDriftFindingWriter) WriteAWSCloudRuntimeDriftFindings(
	_ context.Context, write reducer.AWSCloudRuntimeDriftWrite,
) (reducer.AWSCloudRuntimeDriftWriteResult, error) {
	w.calls++
	return reducer.AWSCloudRuntimeDriftWriteResult{CanonicalWrites: len(write.Candidates)}, nil
}

// sequentialAWSCloudRuntimeDriftFencingTokenIssuer fakes
// reducer.AWSCloudRuntimeDriftFencingTokenIssuer with a plain in-memory
// counter -- this test drives a fake queue backend specifically so a
// 30-minute bound can be crossed in test time, so it has no real Postgres
// sequence to issue from; the fencing-token mechanism itself (#5875 P1) is
// proven live by TestAWSCloudRuntimeDriftFencingTokenIssuerIssuesStrictlyIncreasingValuesLive.
type sequentialAWSCloudRuntimeDriftFencingTokenIssuer struct{ next int64 }

func (s *sequentialAWSCloudRuntimeDriftFencingTokenIssuer) NextAWSCloudRuntimeDriftFencingToken(
	context.Context,
) (int64, error) {
	s.next++
	return s.next, nil
}

// alwaysPendingAWSCloudRuntimeDriftReadinessChecker always reports a pending
// state_snapshot scope -- the durably-stuck-ingestion shape the P0 fix must
// still converge against.
type alwaysPendingAWSCloudRuntimeDriftReadinessChecker struct{ calls int }

func (c *alwaysPendingAWSCloudRuntimeDriftReadinessChecker) HasPendingStateSnapshotEvidence(
	context.Context,
) (bool, error) {
	c.calls++
	return true, nil
}

// TestAWSCloudRuntimeDriftHandlerConvergesAfterElapsedBoundOverRealQueueLive
// is the P0 regression the hostile review required: a hand-set
// Intent.AttemptCount cannot prove the domain ever reaches its terminal
// commit against the REAL queue, because reducerClaimAttemptCountCaseSQL
// freezes attempt_count for this domain's own non-counting failure class the
// very first time the row is claimed while 'retrying' under it. This test
// drives the real reducer.ReducerQueue.Claim/Fail through repeated cycles
// against a state_snapshot scope that NEVER activates (an ordinary
// operational fault, not a rare race), advancing a fake wall clock between
// cycles, and asserts: (1) attempt_count freezes at 1 exactly as the P0
// analysis predicted -- proving the OLD AttemptCount-based bound could never
// have fired -- and (2) the domain still reaches a terminal write once
// elapsed time since Intent.EnqueuedAt crosses
// awsCloudRuntimeDriftStatePendingMaxWait, proving the elapsed-time bound
// converges where the old one would have starved forever.
//
// Named "Live" for consistency with this package's other real-Postgres
// aws_cloud_runtime_drift proofs, though it needs no database: the queue
// backend here is a fake, deliberately, so a 30-minute bound can be crossed
// in test time rather than real time. The registration and SQL-shape halves
// of the P0 fix are proven against real Postgres by
// TestReducerQueueFailDefersAWSCloudRuntimeDriftStatePendingPastAttemptBudget
// and its claim-query siblings; this test proves the SEQUENCE those pieces
// compose into, end to end through the real Handler and the real queue's
// Claim/Fail methods.
func TestAWSCloudRuntimeDriftHandlerConvergesAfterElapsedBoundOverRealQueueLive(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, time.July, 29, 15, 0, 0, 0, time.UTC)
	now := start
	clock := func() time.Time { return now }

	db := &awsCloudRuntimeDriftElapsedBoundQueueDB{status: "pending", enqueuedAt: start}
	queue := ReducerQueue{
		db:            db,
		LeaseOwner:    "reducer-1",
		LeaseDuration: time.Minute,
		RetryDelay:    2 * time.Minute,
		MaxAttempts:   3,
		Now:           clock,
	}

	writer := &countingAWSCloudRuntimeDriftFindingWriter{}
	readiness := &alwaysPendingAWSCloudRuntimeDriftReadinessChecker{}
	handler := reducer.AWSCloudRuntimeDriftHandler{
		EvidenceLoader:     alwaysOrphanedAWSCloudRuntimeDriftEvidenceLoader{},
		Writer:             writer,
		ReadinessChecker:   readiness,
		FencingTokenIssuer: &sequentialAWSCloudRuntimeDriftFencingTokenIssuer{},
		Now:                clock,
	}

	// awsCloudRuntimeDriftStatePendingMaxWait is unexported in package
	// reducer; 30 minutes is asserted against directly here (matching the
	// constant's documented value) rather than referenced, so a change to
	// that constant that is not ALSO reflected in this test's own timeline
	// fails loudly instead of silently retuning around it.
	const maxWait = 30 * time.Minute
	const perCycleAdvance = 12 * time.Minute
	const maxCycles = 10

	attemptCountsSeen := make([]int, 0, maxCycles)
	converged := false
	for cycle := 0; cycle < maxCycles; cycle++ {
		intent, claimed, err := queue.Claim(context.Background())
		if err != nil {
			t.Fatalf("cycle %d: Claim() error = %v", cycle, err)
		}
		if !claimed {
			t.Fatalf("cycle %d: Claim() claimed = false, want true (row must stay pending/retrying)", cycle)
		}
		attemptCountsSeen = append(attemptCountsSeen, intent.AttemptCount)

		_, handleErr := handler.Handle(context.Background(), intent)
		if handleErr == nil {
			converged = true
			break
		}

		if err := queue.Fail(context.Background(), intent, handleErr); err != nil {
			t.Fatalf("cycle %d: Fail() error = %v, want nil (a non-counting defer must never error)", cycle, err)
		}
		now = now.Add(perCycleAdvance)
	}

	if !converged {
		t.Fatalf(
			"domain never reached a terminal commit within %d cycles (%v of simulated elapsed time): "+
				"attempt_count sequence seen = %v -- the elapsed-time bound did not fire",
			maxCycles, time.Duration(maxCycles)*perCycleAdvance, attemptCountsSeen,
		)
	}

	if writer.calls != 1 {
		t.Fatalf("writer.calls = %d, want exactly 1 (the terminal commit)", writer.calls)
	}

	// THE P0 PROOF: attempt_count froze at 1 for every cycle after the first
	// claim, exactly as reducerClaimAttemptCountCaseSQL's non-counting CASE
	// predicts. A bound compared against this sequence could never have
	// fired past its first value -- this is what made the original
	// AttemptCount-based bound unreachable against the real queue.
	if len(attemptCountsSeen) < 2 {
		t.Fatalf("attempt count sequence = %v, want at least 2 samples to prove the freeze", attemptCountsSeen)
	}
	for i, got := range attemptCountsSeen {
		if got != 1 {
			t.Fatalf("attempt_count sequence = %v: cycle %d = %d, want 1 (frozen after the first claim)", attemptCountsSeen, i, got)
		}
	}

	if got, want := now.Sub(start), maxWait; got < want {
		t.Fatalf(
			"converged after %v of simulated elapsed time, want at least %v: "+
				"convergence must not fire BEFORE the documented bound",
			got, want,
		)
	}

	t.Logf(
		"converged after %d claim/fail cycles, %v elapsed, attempt_count sequence = %v (frozen), writer.calls = %d",
		len(attemptCountsSeen), now.Sub(start), attemptCountsSeen, writer.calls,
	)
}
