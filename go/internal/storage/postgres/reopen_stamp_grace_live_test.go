// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #9320 PostgreSQL 18.6 readiness-grace proof: a dead-letter replay of an aws_cloud_runtime_drift reducer item anchors CycleStartedAt on the replay so the handler defers while the state scope is pending, and the deferral retry keeps that anchor so the handler commits the degraded verdict once the 30-minute bound passes; real ReducerQueue, RecoveryStore, and handler; about 1-2 s locally in an isolated schema
//eshu:readiness:env ESHU_POSTGRES_DSN

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/projector/runtime"
	"github.com/eshu-hq/eshu/go/internal/recovery"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/reducer/awscloud"
)

// TestDeadLetterReplayGetsBoundedFreshReadinessGraceLive is the #9320
// readiness-grace proof for the reducer claim path. The reducer's elapsed-time
// readiness bounds (awsCloudRuntimeDriftStatePendingMaxWait here, and
// crossscope.ReadinessCycleAnchor for the cross-scope gates) anchor on
// Intent.CycleStartedAt, which the claim computes as
// COALESCE(reopened_at, created_at). A dead-letter replay now stamps
// reopened_at, so:
//
//  1. Fresh window: the first claim after the replay anchors on the replay,
//     not on the original enqueue, and the handler defers while its sibling
//     state_snapshot scope is still pending instead of committing a degraded
//     verdict at once. Before #9320 the replay kept created_at, the bound read
//     as long expired, and Handle committed on the first claim.
//  2. Not starved: the deferral's retry does not touch reopened_at, so the
//     window runs out on schedule. Once the bound passes since the replay,
//     the next claim keeps the replay anchor and Handle commits the degraded
//     verdict rather than waiting forever.
//
// It drives the real ReducerQueue, RecoveryStore replay, and
// AWSCloudRuntimeDriftHandler against real Postgres, the shape of the
// ReopenSucceeded proof
// (TestAWSCloudRuntimeDriftReopenGetsFreshElapsedBoundWhileStatePendingLive).
func TestDeadLetterReplayGetsBoundedFreshReadinessGraceLive(t *testing.T) {
	sqlDB, ctx := awsCloudRuntimeDriftIsolatedLiveDB(t)
	suffix := fmt.Sprintf("9320-replay-grace-%d", time.Now().UnixNano())
	start := time.Now().UTC().Truncate(time.Second)
	clock := start

	stateScopeID := "state_snapshot:s3:" + suffix
	seedAWSCloudRuntimeDriftScope(t, ctx, sqlDB, stateScopeID, "terraform_state", "", clock)
	seedAWSCloudRuntimeDriftGeneration(t, ctx, sqlDB, "gen-state-pending-"+suffix, stateScopeID, "pending", clock)
	awsScopeID := "aws:" + suffix
	awsGenerationID := "gen-aws-" + suffix
	arn := "arn:aws:lambda:us-east-1:123456789012:function:replay-grace-" + suffix
	seedAWSCloudRuntimeDriftScope(t, ctx, sqlDB, awsScopeID, "aws", awsGenerationID, clock)
	seedAWSCloudRuntimeDriftGeneration(t, ctx, sqlDB, awsGenerationID, awsScopeID, "active", clock)
	seedAWSResourceFactMinimal(t, ctx, sqlDB, awsScopeID, awsGenerationID, arn, "aws_lambda_function", clock)

	queue := ReducerQueue{
		database:      SQLDB{DB: sqlDB},
		LeaseOwner:    "reducer-replay-grace",
		LeaseDuration: time.Minute,
		RetryDelay:    time.Minute,
		MaxRetryDelay: time.Minute,
		MaxAttempts:   5,
		Now:           func() time.Time { return clock },
	}
	intent := runtime.ReducerIntent{
		ScopeID:      awsScopeID,
		GenerationID: awsGenerationID,
		Domain:       reducer.DomainAWSCloudRuntimeDrift,
		EntityKey:    "aws_resource_materialization:" + awsScopeID,
		Reason:       "aws runtime resource facts observed",
		SourceSystem: "aws",
	}
	if _, err := queue.Enqueue(ctx, []runtime.ReducerIntent{intent}); err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}

	// Dead-letter the row: a non-retryable failure is terminal at once.
	first, claimed, err := queue.Claim(ctx)
	if err != nil || !claimed {
		t.Fatalf("first Claim() = (%v, %v), want a claim", claimed, err)
	}
	if err := queue.Fail(ctx, first, errors.New("permanent handler failure")); err != nil {
		t.Fatalf("Fail() error = %v", err)
	}
	var status string
	if err := sqlDB.QueryRowContext(ctx, `SELECT status FROM fact_work_items WHERE work_item_id = $1`,
		reducerWorkItemID(intent)).Scan(&status); err != nil || status != "dead_letter" {
		t.Fatalf("status after Fail = %q (%v), want dead_letter", status, err)
	}

	// Replay it two hours later, long past the 30-minute bound since enqueue.
	clock = start.Add(2 * time.Hour)
	replayedAt := clock
	replay, err := NewRecoveryStore(SQLDB{DB: sqlDB}).ReplayFailedWorkItems(ctx, recovery.ReplayFilter{
		Stage: recovery.StageReducer, ScopeIDs: []string{awsScopeID},
	}, clock)
	if err != nil || replay.Replayed != 1 {
		t.Fatalf("ReplayFailedWorkItems() = (%d replayed, %v), want 1", replay.Replayed, err)
	}

	// 1. Fresh window: the replayed claim anchors on the replay and defers.
	replayed, claimed, err := queue.Claim(ctx)
	if err != nil || !claimed {
		t.Fatalf("replayed Claim() = (%v, %v), want a claim", claimed, err)
	}
	if elapsed := clock.Sub(replayed.EnqueuedAt); elapsed < awsCloudRuntimeDriftReopenAnchorTestMaxWait {
		t.Fatalf("test setup invalid: EnqueuedAt only %v old at replay, want more than %v", elapsed, awsCloudRuntimeDriftReopenAnchorTestMaxWait)
	}
	if !replayed.CycleStartedAt.Equal(replayedAt) {
		t.Fatalf("CycleStartedAt = %v, want the replay time %v: a dead-letter replay must start a fresh readiness cycle "+
			"(it kept the original enqueue %v)", replayed.CycleStartedAt, replayedAt, replayed.EnqueuedAt)
	}
	handler := awscloud.AWSCloudRuntimeDriftHandler{
		EvidenceLoader:     PostgresAWSCloudRuntimeDriftEvidenceLoader{DB: SQLDB{DB: sqlDB}},
		Writer:             awscloud.PostgresAWSCloudRuntimeDriftWriter{DB: AWSCloudRuntimeDriftAdmissionBeginner{Beginner: SQLDB{DB: sqlDB}}},
		ReadinessChecker:   PostgresAWSCloudRuntimeDriftReadinessChecker{DB: SQLDB{DB: sqlDB}},
		FencingTokenIssuer: PostgresAWSCloudRuntimeDriftFencingTokenIssuer{DB: SQLDB{DB: sqlDB}},
		Now:                func() time.Time { return clock },
	}
	_, deferErr := handler.Handle(ctx, replayed)
	if deferErr == nil {
		t.Fatal("Handle() after the replay = nil, want a readiness deferral: the replay must get a fresh grace window")
	}
	if kinds := countAWSCloudRuntimeDriftFindingRows(t, ctx, sqlDB, awsScopeID, awsGenerationID); len(kinds) != 0 {
		t.Fatalf("finding rows after the deferral = %v, want none", kinds)
	}
	if err := queue.Fail(ctx, replayed, deferErr); err != nil {
		t.Fatalf("Fail(deferral) error = %v", err)
	}

	// 2. Not starved: once the bound passes since the replay, the retry keeps
	// the replay anchor and the handler commits the degraded verdict.
	clock = replayedAt.Add(awsCloudRuntimeDriftReopenAnchorTestMaxWait + 5*time.Minute)
	retried, claimed, err := queue.Claim(ctx)
	if err != nil || !claimed {
		t.Fatalf("retry Claim() = (%v, %v), want a claim", claimed, err)
	}
	if !retried.CycleStartedAt.Equal(replayedAt) {
		t.Fatalf("retry CycleStartedAt = %v, want the replay time %v: a retry must not restart the grace window", retried.CycleStartedAt, replayedAt)
	}
	if _, err := handler.Handle(ctx, retried); err != nil {
		t.Fatalf("Handle() past the bound = %v, want the degraded verdict committed: the replay window must expire", err)
	}
	if kinds := countAWSCloudRuntimeDriftFindingRows(t, ctx, sqlDB, awsScopeID, awsGenerationID); len(kinds) == 0 {
		t.Fatal("finding rows past the bound = none, want the degraded verdict written")
	}
}
