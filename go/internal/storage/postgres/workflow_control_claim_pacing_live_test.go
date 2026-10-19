// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #8470 depth-pacing proofs on real PostgreSQL: pacing engages on per-domain/global depth with hysteresis behind a shared latch, releases below the low watermark, fails open when the depth query fails, and emits a stuck signal instead of releasing blindly; pacing-configured drains hold the anti-serialization floor
//eshu:readiness:env ESHU_POSTGRES_DSN

import (
	"fmt"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// testClaimPacingConfig returns tight thresholds the pacing proofs drive
// small seeded backlogs across: domain engages above 50 pending, releases at
// or below 20; global engages above 500, releases at or below 200.
func testClaimPacingConfig() workflow.ClaimPacingConfig {
	return workflow.ClaimPacingConfig{
		DomainHigh:    50,
		DomainLow:     20,
		GlobalHigh:    500,
		GlobalLow:     200,
		GrantDelay:    50 * time.Millisecond,
		MaxEngagement: 100 * time.Millisecond,
	}
}

// TestWorkflowClaimPacingEngagesOnDomainDepth proves pacing engages on
// per-domain depth alone: 60 pending in the claiming family with a quiet
// system paces the domain signal with the configured grant delay, and the
// engagement survives a fresh store handle (the latch is shared storage,
// not process memory).
func TestWorkflowClaimPacingEngagesOnDomainDepth(t *testing.T) {
	database, store, ctx, _ := openClaimDepthHarness(t, 1)
	base := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	seedClaimDepthBacklog(t, store, ctx, "pacing-engage", base,
		[]claimDepthKeyBacklog{{FairnessKey: "pacing:hot", Count: 60}})
	cfg := testClaimPacingConfig()
	selector := workflow.ClaimSelector{
		CollectorKind:       scope.CollectorGit,
		CollectorInstanceID: "claim-depth-instance",
		OwnerID:             "pacing-owner",
		ClaimID:             "pacing-claim-1",
	}

	decision, err := store.CheckClaimPacing(ctx, selector, cfg, time.Now().UTC())
	if err != nil {
		t.Fatalf("CheckClaimPacing() error = %v, want nil", err)
	}
	if !decision.Paced {
		t.Fatal("CheckClaimPacing() Paced = false, want true with 60 pending over domain high 50")
	}
	if decision.Signal != workflow.ClaimPacingSignalDomain {
		t.Fatalf("Signal = %q, want domain", decision.Signal)
	}
	if decision.RetryAfter != cfg.GrantDelay {
		t.Fatalf("RetryAfter = %s, want %s", decision.RetryAfter, cfg.GrantDelay)
	}
	if decision.Stuck {
		t.Fatal("fresh engagement Stuck = true, want false")
	}

	fresh := NewWorkflowControlStore(SQLDB{DB: database})
	if err := fresh.EnsureSchema(ctx); err != nil {
		t.Fatalf("EnsureSchema() error = %v, want nil", err)
	}
	again, err := fresh.CheckClaimPacing(ctx, selector, cfg, time.Now().UTC())
	if err != nil {
		t.Fatalf("CheckClaimPacing() on a fresh handle error = %v, want nil", err)
	}
	if !again.Paced || again.Signal != workflow.ClaimPacingSignalDomain {
		t.Fatalf("fresh-handle verdict = %+v, want still paced on domain (shared latch)", again)
	}
}

// TestWorkflowClaimPacingHysteresisRelease proves the engage/release cycle on
// a live latch: engagement holds through the mid band, and draining below the
// low watermark releases so claims flow again.
func TestWorkflowClaimPacingHysteresisRelease(t *testing.T) {
	_, store, ctx, _ := openClaimDepthHarness(t, 1)
	base := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	seedClaimDepthBacklog(t, store, ctx, "pacing-hysteresis", base,
		[]claimDepthKeyBacklog{{FairnessKey: "pacing:hot", Count: 60}})
	cfg := testClaimPacingConfig()
	selector := workflow.ClaimSelector{
		CollectorKind:       scope.CollectorGit,
		CollectorInstanceID: "claim-depth-instance",
		OwnerID:             "pacing-owner",
		ClaimID:             "pacing-claim-1",
	}
	now := time.Now().UTC()

	first, err := store.CheckClaimPacing(ctx, selector, cfg, now)
	if err != nil || !first.Paced {
		t.Fatalf("CheckClaimPacing() = %+v, %v; want paced, nil", first, err)
	}

	// Complete 30 of 60: 30 pending sits between low 20 and high 50, so the
	// engaged latch must hold.
	completeClaimDepthItems(t, store, ctx, "pacing-owner", 30)
	mid, err := store.CheckClaimPacing(ctx, selector, cfg, now)
	if err != nil {
		t.Fatalf("mid-band CheckClaimPacing() error = %v, want nil", err)
	}
	if !mid.Paced {
		t.Fatal("mid-band CheckClaimPacing() Paced = false, want true (latch holds)")
	}

	// Complete 25 more: 5 pending is below low 20, so pacing releases.
	completeClaimDepthItems(t, store, ctx, "pacing-owner", 25)
	released, err := store.CheckClaimPacing(ctx, selector, cfg, now)
	if err != nil {
		t.Fatalf("release CheckClaimPacing() error = %v, want nil", err)
	}
	if released.Paced {
		t.Fatalf("release CheckClaimPacing() = %+v, want unpaced below low watermark", released)
	}
}

// TestWorkflowClaimPacingFailsOpenOnDepthQueryFailure proves a dead depth
// signal never paces: against a closed pool the check reports the error and
// an unpaced verdict, so the caller claims normally.
func TestWorkflowClaimPacingFailsOpenOnDepthQueryFailure(t *testing.T) {
	database, store, ctx, _ := openClaimDepthHarness(t, 1)
	if err := database.Close(); err != nil {
		t.Fatalf("close pool: %v", err)
	}
	cfg := testClaimPacingConfig()
	selector := workflow.ClaimSelector{
		CollectorKind:       scope.CollectorGit,
		CollectorInstanceID: "claim-depth-instance",
		OwnerID:             "pacing-owner",
		ClaimID:             "pacing-claim-1",
	}

	decision, err := store.CheckClaimPacing(ctx, selector, cfg, time.Now().UTC())
	if err == nil {
		t.Fatal("CheckClaimPacing() on a closed pool error = nil, want the depth failure surfaced")
	}
	if decision.Paced {
		t.Fatalf("CheckClaimPacing() = %+v, want unpaced (fail open)", decision)
	}
}

// TestWorkflowClaimPacingStuckSignal proves a wedged engagement stays loud:
// depth still above the release watermark past MaxEngagement stays paced and
// raises Stuck instead of releasing blindly, and draining clears both.
func TestWorkflowClaimPacingStuckSignal(t *testing.T) {
	_, store, ctx, _ := openClaimDepthHarness(t, 1)
	base := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	seeded := seedClaimDepthBacklog(t, store, ctx, "pacing-stuck", base,
		[]claimDepthKeyBacklog{{FairnessKey: "pacing:hot", Count: 60}})
	cfg := testClaimPacingConfig()
	selector := workflow.ClaimSelector{
		CollectorKind:       scope.CollectorGit,
		CollectorInstanceID: "claim-depth-instance",
		OwnerID:             "pacing-owner",
		ClaimID:             "pacing-claim-1",
	}
	engagedAt := time.Now().UTC()

	first, err := store.CheckClaimPacing(ctx, selector, cfg, engagedAt)
	if err != nil || !first.Paced || first.Stuck {
		t.Fatalf("engage = %+v, %v; want paced without stuck", first, err)
	}
	overdue, err := store.CheckClaimPacing(ctx, selector, cfg, engagedAt.Add(2*cfg.MaxEngagement))
	if err != nil {
		t.Fatalf("overdue CheckClaimPacing() error = %v, want nil", err)
	}
	if !overdue.Paced || !overdue.Stuck {
		t.Fatalf("overdue engagement = %+v, want paced with stuck", overdue)
	}

	completeClaimDepthItems(t, store, ctx, "pacing-owner", len(seeded))
	drained, err := store.CheckClaimPacing(ctx, selector, cfg, engagedAt.Add(3*cfg.MaxEngagement))
	if err != nil {
		t.Fatalf("drained CheckClaimPacing() error = %v, want nil", err)
	}
	if drained.Paced || drained.Stuck {
		t.Fatalf("drained overdue engagement = %+v, want released without stuck", drained)
	}
}

// TestWorkflowClaimPacingGlobalCeiling proves the global signal: every family
// quiet on its own but 600 pending system-wide paces on global, throttling
// claims that no single domain tripped.
func TestWorkflowClaimPacingGlobalCeiling(t *testing.T) {
	_, store, ctx, _ := openClaimDepthHarness(t, 1)
	base := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	keys := make([]claimDepthKeyBacklog, 0, 20)
	for i := 0; i < 20; i++ {
		keys = append(keys, claimDepthKeyBacklog{
			FairnessKey: fmt.Sprintf("pacing:global-%d", i),
			Count:       30,
		})
	}
	seedClaimDepthBacklog(t, store, ctx, "pacing-global", base, keys)
	cfg := testClaimPacingConfig()
	selector := workflow.ClaimSelector{
		CollectorKind:       scope.CollectorGit,
		CollectorInstanceID: "claim-depth-instance",
		OwnerID:             "pacing-owner",
		ClaimID:             "pacing-claim-1",
	}
	// One family holds all 600: split across two runs is unnecessary; instead
	// assert the global ceiling with a config whose domain band cannot trip.
	// (The shared instance carries 600 pending, so domain would trip first —
	// raise the domain band to isolate the global signal.)
	cfg.DomainHigh = 10000
	cfg.DomainLow = 9000

	decision, err := store.CheckClaimPacing(ctx, selector, cfg, time.Now().UTC())
	if err != nil {
		t.Fatalf("CheckClaimPacing() error = %v, want nil", err)
	}
	if !decision.Paced || decision.Signal != workflow.ClaimPacingSignalGlobal {
		t.Fatalf("600 pending over global high = %+v, want paced on global", decision)
	}
}

// TestWorkflowClaimPacingIdleDrainHoldsFloor is the #8470 anti-serialization
// gate: with pacing configured but the backlog below every threshold, the
// full eight-claimer concurrent drain holds the shipped throughput floor
// with every claimer sharing work. A pacing mechanism that fixes the tail by
// throttling the burst — fewer concurrent grants, one claimer at a time —
// fails here.
func TestWorkflowClaimPacingIdleDrainHoldsFloor(t *testing.T) {
	database, store, ctx, poolLimit := openClaimDepthHarness(t, claimDepthClaimers)
	base := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	seeded := seedClaimDepthBacklog(t, store, ctx, "pacing-idle", base, []claimDepthKeyBacklog{
		{FairnessKey: "pacing:idle-large", Count: 500},
		{FairnessKey: "pacing:idle-small-1", Count: 20, CreatedOffset: time.Second},
		{FairnessKey: "pacing:idle-small-2", Count: 20, CreatedOffset: time.Second},
		{FairnessKey: "pacing:idle-small-3", Count: 20, CreatedOffset: time.Second},
		{FairnessKey: "pacing:idle-small-4", Count: 20, CreatedOffset: time.Second},
	})
	cfg := testClaimPacingConfig()
	// Idle band: the 580-item backlog sits far below every threshold, so no
	// check may ever pace; the hook fails the test if one does.
	cfg.DomainHigh = 100000
	cfg.GlobalHigh = 1000000
	priorHook := claimDepthPacingHook
	idle := claimDepthPacingConsult(t, store, cfg)
	claimDepthPacingHook = idle.hook
	defer func() { claimDepthPacingHook = priorHook }()

	result := drainClaimDepthBacklog(t, database, ctx, claimDepthClaimers, poolLimit)

	assertClaimDepthConcurrency(t, result, claimDepthClaimers)
	assertClaimDepthExactlyOnce(t, seeded, result)
	assertClaimDepthMinGrantsPerOwner(t, result, claimDepthClaimers, 10)
	if got := result.ClaimsPerSecond(); got < 119 {
		t.Fatalf("pacing-idle drain throughput = %.1f claims/s, want >= 119 (design floor)", got)
	}
	if got := idle.count(); got != 0 {
		t.Fatalf("pacing-idle paced waits = %d, want 0 (backlog below every threshold)", got)
	}
	t.Logf("pacing-idle drain items=%d claimers=%d duration=%s claims_per_second=%.1f empty_attempts=%d backends=%v",
		len(seeded), claimDepthClaimers, result.Duration, result.ClaimsPerSecond(), result.EmptyAttempts, result.BackendPIDs)
}

// TestWorkflowClaimPacingEngagedDrainCompletesExactlyOnce proves pacing
// delays grants without serializing or stranding them: thresholds trip
// immediately, every claim waits out the grant delay, and the backlog still
// drains exactly once with every claimer sharing work.
func TestWorkflowClaimPacingEngagedDrainCompletesExactlyOnce(t *testing.T) {
	database, store, ctx, poolLimit := openClaimDepthHarness(t, claimDepthClaimers)
	base := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	seeded := seedClaimDepthBacklog(t, store, ctx, "pacing-engaged", base, []claimDepthKeyBacklog{
		{FairnessKey: "pacing:hot-1", Count: 20},
		{FairnessKey: "pacing:hot-2", Count: 20},
	})
	cfg := testClaimPacingConfig()
	// Trip on any backlog: the 40 seeded items exceed domain high 1, so every
	// check paces until the drain crosses below low 0... which never happens
	// mid-drain — instead release comes from the drain itself: use low 0 so
	// only a fully drained family releases. Each grant waits 20ms.
	cfg.DomainHigh = 1
	cfg.DomainLow = 0
	cfg.GlobalHigh = 1000000
	cfg.GrantDelay = 20 * time.Millisecond
	cfg.MaxEngagement = time.Hour
	priorHook := claimDepthPacingHook
	pacedWaits := claimDepthPacingConsult(t, store, cfg)
	claimDepthPacingHook = pacedWaits.hook
	defer func() { claimDepthPacingHook = priorHook }()

	result := drainClaimDepthBacklog(t, database, ctx, claimDepthClaimers, poolLimit)

	assertClaimDepthConcurrency(t, result, claimDepthClaimers)
	assertClaimDepthExactlyOnce(t, seeded, result)
	assertClaimDepthMinGrantsPerOwner(t, result, claimDepthClaimers, 1)
	if got := pacedWaits.count(); got == 0 {
		t.Fatal("paced waits = 0, want the engaged drain to actually pace grants")
	}
	t.Logf("pacing-engaged drain items=%d claimers=%d duration=%s claims_per_second=%.1f paced_waits=%d backends=%v",
		len(seeded), claimDepthClaimers, result.Duration, result.ClaimsPerSecond(), pacedWaits.count(), result.BackendPIDs)
}
