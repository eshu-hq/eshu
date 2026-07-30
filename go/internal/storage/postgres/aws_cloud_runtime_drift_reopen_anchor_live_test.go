// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// TestAWSCloudRuntimeDriftReopenGetsFreshElapsedBoundWhileStatePendingLive is
// the round-4 review's required live proof of the reopen-anchor fix
// (#5848/#5837): a round-3 review found that anchoring
// awsCloudRuntimeDriftStatePendingMaxWait on Intent.EnqueuedAt alone gave a
// maintenance-triggered reopen NO grace window, because
// fact_work_items.created_at never changes across a reopen and
// aws_cloud_runtime_drift is unconditionally reopened on every ingester shard
// drain and bootstrap maintenance pass. The fix anchors the bound on
// Intent.CycleStartedAt (COALESCE(reopened_at, created_at)) instead, and
// ReopenSucceeded resets reopened_at (migration 088) in the same statement
// that resets attempt_count.
//
// This drives the REAL ReducerQueue.Enqueue/Claim/Ack/ReopenSucceeded/Claim
// sequence against real Postgres, then the REAL AWSCloudRuntimeDriftHandler,
// so the proof covers the actual SQL round-trip and the actual Handle
// decision, not a re-implementation of either:
//
//  1. Enqueue a work item at time `past`, so created_at = past.
//  2. Claim and Ack it (status -> 'succeeded'), simulating a prior pass that
//     already completed a long time ago.
//  3. Advance the clock more than awsCloudRuntimeDriftStatePendingMaxWait past
//     `past`, then ReopenSucceeded it (status -> 'pending',
//     reopened_at -> the reopen time).
//  4. Claim it again and confirm the returned Intent.CycleStartedAt reflects
//     the REOPEN time, not the original `past` enqueue -- proving the SQL
//     round-trip independent of the Handler.
//  5. Feed that Intent through the real Handler with a state_snapshot scope
//     seeded permanently 'pending'. Assert Handle DEFERS (a non-nil,
//     retryable error) and writes NO finding row -- the reopened claim must
//     get a fresh window, not commit a degraded verdict immediately just
//     because its ORIGINAL enqueue is long past the bound.
//
// Failing-then-green: temporarily reverting awsCloudRuntimeDriftCycleAnchor
// to `return intent.EnqueuedAt` (bypassing CycleStartedAt) reproduces the
// round-3 regression this test guards -- Handle commits immediately and
// writes orphaned_cloud_resource despite the state scope still being
// pending, because EnqueuedAt alone is already past the bound at reopen time.
func TestAWSCloudRuntimeDriftReopenGetsFreshElapsedBoundWhileStatePendingLive(t *testing.T) {
	sqlDB, ctx := awsCloudRuntimeDriftAdmissionLiveDB(t)
	suffix := fmt.Sprintf("5848-reopen-anchor-%d", time.Now().UnixNano())

	// clock starts well in the past. The state_snapshot scope is seeded
	// PENDING and never activated, so the readiness checker reports "pending"
	// for the whole test -- the durably-stuck shape this bound must still
	// converge against, on its OWN cycle's schedule.
	past := time.Date(2026, time.June, 1, 9, 0, 0, 0, time.UTC)
	clock := past

	stateScopeID := "state_snapshot:s3:" + suffix
	statePendingGenerationID := "gen-state-pending-" + suffix
	seedAWSCloudRuntimeDriftScope(t, ctx, sqlDB, stateScopeID, "terraform_state", "", clock)
	seedAWSCloudRuntimeDriftGeneration(t, ctx, sqlDB, statePendingGenerationID, stateScopeID, "pending", clock)
	// HasPendingStateSnapshotEvidence is intentionally coarse (see
	// AWSCloudRuntimeDriftReadinessChecker's doc comment): it reports "some
	// state_snapshot scope, anywhere, is pending", not this test's specific
	// scope. This test's whole point requires the seeded scope to stay
	// pending through the assertions -- but leaving it pending after the
	// test ends would permanently pollute this shared, long-lived container
	// (eshu-pg-5848) for every OTHER test that reads the same coarse signal,
	// e.g. TestPostgresAWSCloudRuntimeDriftReadinessCheckerLive's "no longer
	// pending after activation" assertion. Activate it once this test's own
	// assertions are done, mirroring the deterministic repro test's own
	// Phase 3 cleanup.
	t.Cleanup(func() {
		if _, err := sqlDB.ExecContext(
			context.Background(),
			`UPDATE scope_generations SET status = 'active', activated_at = now() WHERE generation_id = $1`,
			statePendingGenerationID,
		); err != nil {
			t.Logf("cleanup: activate state_snapshot generation %s: %v", statePendingGenerationID, err)
		}
	})

	awsScopeID := "aws:" + suffix
	awsGenerationID := "gen-aws-" + suffix
	arn := "arn:aws:lambda:us-east-1:123456789012:function:reopen-anchor-" + suffix
	seedAWSCloudRuntimeDriftScope(t, ctx, sqlDB, awsScopeID, "aws", awsGenerationID, clock)
	seedAWSCloudRuntimeDriftGeneration(t, ctx, sqlDB, awsGenerationID, awsScopeID, "active", clock)
	seedAWSResourceFactMinimal(t, ctx, sqlDB, awsScopeID, awsGenerationID, arn, "aws_lambda_function", clock)

	queue := ReducerQueue{
		db:            SQLDB{DB: sqlDB},
		LeaseOwner:    "reducer-reopen-anchor",
		LeaseDuration: time.Minute,
		RetryDelay:    time.Minute,
		MaxAttempts:   3,
		Now:           func() time.Time { return clock },
	}

	intent := projector.ReducerIntent{
		ScopeID:      awsScopeID,
		GenerationID: awsGenerationID,
		Domain:       reducer.DomainAWSCloudRuntimeDrift,
		EntityKey:    "aws_resource_materialization:" + awsScopeID,
		Reason:       "aws runtime resource facts observed",
		SourceSystem: "aws",
	}
	if _, err := queue.Enqueue(ctx, []projector.ReducerIntent{intent}); err != nil {
		t.Fatalf("Enqueue() error = %v, want nil", err)
	}

	// --- Step 1: claim and ack once, so the row reaches 'succeeded' -- the
	// terminal state ReopenSucceeded requires. ---
	firstClaim, claimed, err := queue.Claim(ctx)
	if err != nil {
		t.Fatalf("first Claim() error = %v, want nil", err)
	}
	if !claimed {
		t.Fatal("first Claim() claimed = false, want true")
	}
	if err := queue.Ack(ctx, firstClaim, reducer.Result{}); err != nil {
		t.Fatalf("Ack() error = %v, want nil", err)
	}

	// --- Step 2: advance well past the bound relative to `past`, then reopen. ---
	clock = past.Add(2 * time.Hour)
	workItemID := reducerWorkItemID(intent)
	reopened, err := queue.ReopenSucceeded(ctx, workItemID)
	if err != nil {
		t.Fatalf("ReopenSucceeded() error = %v, want nil", err)
	}
	if !reopened {
		t.Fatal("ReopenSucceeded() reopened = false, want true")
	}

	// --- Step 3: claim the reopened row and confirm the anchor round-trip. ---
	reopenedClaim, claimed, err := queue.Claim(ctx)
	if err != nil {
		t.Fatalf("reopened Claim() error = %v, want nil", err)
	}
	if !claimed {
		t.Fatal("reopened Claim() claimed = false, want true")
	}

	if enqueuedElapsed := clock.Sub(reopenedClaim.EnqueuedAt); enqueuedElapsed < awsCloudRuntimeDriftReopenAnchorTestMaxWait {
		t.Fatalf(
			"test setup invalid: EnqueuedAt only %v old at reopen time, want more than %v "+
				"(otherwise this test cannot distinguish the anchor fix from the regression it guards)",
			enqueuedElapsed, awsCloudRuntimeDriftReopenAnchorTestMaxWait,
		)
	}
	if cycleElapsed := clock.Sub(reopenedClaim.CycleStartedAt); cycleElapsed > time.Minute {
		t.Fatalf(
			"CycleStartedAt = %v (%v old at reopen time), want within a minute of the reopen clock %v: "+
				"the reopen must reset the anchor, not inherit the original EnqueuedAt",
			reopenedClaim.CycleStartedAt, cycleElapsed, clock,
		)
	}

	// --- Step 4: the real Handler, real Postgres, state still pending. ---
	loader := PostgresAWSCloudRuntimeDriftEvidenceLoader{DB: SQLDB{DB: sqlDB}}
	writer := reducer.PostgresAWSCloudRuntimeDriftWriter{
		DB: AWSCloudRuntimeDriftAdmissionBeginner{Beginner: SQLDB{DB: sqlDB}},
	}
	readinessChecker := PostgresAWSCloudRuntimeDriftReadinessChecker{DB: SQLDB{DB: sqlDB}}
	handler := reducer.AWSCloudRuntimeDriftHandler{
		EvidenceLoader:   loader,
		Writer:           writer,
		ReadinessChecker: readinessChecker,
		Now:              func() time.Time { return clock },
	}

	_, handleErr := handler.Handle(ctx, reopenedClaim)
	if handleErr == nil {
		t.Fatal(
			"Handle() error = nil, want a deferred (retryable) error: a reopened claim whose ORIGINAL " +
				"enqueue is long past the bound must still get a fresh grace window for its own repair " +
				"cycle, not commit a degraded verdict on the very first reopened claim",
		)
	}
	kinds := countAWSCloudRuntimeDriftFindingRows(t, ctx, sqlDB, awsScopeID, awsGenerationID)
	if len(kinds) != 0 {
		t.Fatalf(
			"finding rows = %v, want none: the reopened claim deferred, so it must not have durably "+
				"written a degraded verdict",
			kinds,
		)
	}

	t.Logf(
		"reopen anchor held: EnqueuedAt %v old, CycleStartedAt %v old at reopen -- Handle deferred, zero rows written",
		clock.Sub(reopenedClaim.EnqueuedAt), clock.Sub(reopenedClaim.CycleStartedAt),
	)
}

// awsCloudRuntimeDriftReopenAnchorTestMaxWait mirrors
// reducer.awsCloudRuntimeDriftStatePendingMaxWait (unexported in package
// reducer, so restated here) purely as this test's own setup-validity check;
// it is not read by production code.
const awsCloudRuntimeDriftReopenAnchorTestMaxWait = 30 * time.Minute
