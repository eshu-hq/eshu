// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"testing"
	"time"
)

// fullGuardRaceScope seeds the #7473 cross-snapshot race: scope-s holds the
// full gen-s1 retry (visible at visibleAt) with newer pending delta gen-s2,
// and scope-z holds the supersedable gen-z1 beside pending gen-z2, which
// fires the pause trigger and gives the paused claimer its other-scope row.
// All generations are fulls except gen-s2.
func fullGuardRaceScope(t *testing.T, database *sql.DB, at, visibleAt time.Time) {
	t.Helper()
	seedClaimMaintenanceScopes(t, database, "scope-s", "scope-z")
	for _, work := range []fenceProofWork{
		{"scope-s", "gen-s1", "pending", "retrying", 2, at.Add(-2 * time.Hour), visibleAt, at.Add(-2 * time.Hour)},
		{"scope-s", "gen-s2", "pending", "pending", 0, at.Add(-time.Hour), at.Add(-time.Hour), at.Add(-time.Hour)},
		{"scope-z", "gen-z1", "pending", "pending", 0, at.Add(-3 * time.Hour), at.Add(-3 * time.Hour), at.Add(-3 * time.Hour)},
		{"scope-z", "gen-z2", "pending", "pending", 0, at.Add(-2 * time.Hour), at.Add(-2 * time.Hour), at.Add(-30 * time.Minute)},
	} {
		seedFenceProofWork(t, database, work)
	}
	if _, err := database.Exec(
		`UPDATE scope_generations SET is_delta = true WHERE generation_id = 'gen-s2'`); err != nil {
		t.Fatalf("mark gen-s2 delta: %v", err)
	}
}

// TestProjectorClaimDropsFullHolderClaimedAfterSnapshot is the #7473
// EvalPlanQual proof for the delta hold. The paused claimer B snapshots the
// gen-s1 full as the oldest row of scope-s and offers it to its lock step.
// Racer A then claims gen-s1 and commits, bumping the scope fence. B must
// drop gen-s1 at lock time through the work-row and fence rechecks, must not
// fall through to the held gen-s2 delta, and must land on gen-z2 instead,
// leaving scope-s with exactly the one lease A holds.
func TestProjectorClaimDropsFullHolderClaimedAfterSnapshot(t *testing.T) {
	dsn := claimMaintenanceProofDSN(t)
	database := openClaimDeadlockProofDB(t, dsn, 4)
	at := time.Now().UTC().Truncate(time.Second)
	fullGuardRaceScope(t, database, at, at.Add(-time.Hour))
	paused := openPausedClaimPool(t, database, dsn)

	type claimResult struct {
		generation string
		ok         bool
		err        error
	}
	done := make(chan claimResult, 1)
	go func() {
		queue := NewProjectorQueue(SQLDB{DB: paused}, "worker-b", time.Minute)
		work, ok, err := queue.Claim(context.Background())
		done <- claimResult{work.Generation.GenerationID, ok, err}
	}()
	waitForPausedClaimer(t, dsn)

	racer := NewProjectorQueue(SQLDB{DB: database}, "worker-a", time.Minute)
	work, ok, err := racer.Claim(context.Background())
	if err != nil || !ok || work.Generation.GenerationID != "gen-s1" {
		t.Fatalf("racing Claim() = (%q, %v, %v), want gen-s1", work.Generation.GenerationID, ok, err)
	}

	result := <-done
	if result.err != nil {
		t.Fatalf("paused Claim() error = %v", result.err)
	}
	if got := claimedLeaseCount(t, database, "scope-s"); got != 1 {
		t.Fatalf("scope-s holds %d leases after both claims, want 1 (paused claim took %q)", got, result.generation)
	}
	if !result.ok || result.generation != "gen-z2" {
		t.Fatalf("paused Claim() = (%q, %v), want gen-z2 in the other scope", result.generation, result.ok)
	}
	if status, _, owner := workState(t, database, "scope-s", "gen-s1"); status != "claimed" || owner != "worker-a" {
		t.Fatalf("gen-s1 = (%s, %q), want claimed by worker-a", status, owner)
	}
	if status, _, _ := workState(t, database, "scope-s", "gen-s2"); status != "pending" {
		t.Fatalf("gen-s2 = %s, want pending: the paused claimer must not fall through to the held delta", status)
	}
	observer := NewQueueObserverStore(SQLQueryer{DB: database})
	if got, err := observer.ProjectorScopesWithMultipleLiveLeases(context.Background()); err != nil || got != 0 {
		t.Fatalf("ProjectorScopesWithMultipleLiveLeases() = (%d, %v), want 0", got, err)
	}
}

// TestProjectorClaimFullGuardAgreesWithFenceAcrossClocks replays the #7115
// race with a full holder: claimer B snapshots at T, when gen-s1's full retry
// is still invisible, and pauses inside its supersede UPDATE. Racer A, with
// clock T+10s, claims the now-visible gen-s1 and commits. B's snapshot still
// sees gen-s1 waiting, so the hold keeps gen-s2 back there, and the fence A's
// claim bumped drops scope-s at lock time. Both defenders must agree: B lands
// on gen-z2 and scope-s keeps exactly A's lease.
func TestProjectorClaimFullGuardAgreesWithFenceAcrossClocks(t *testing.T) {
	dsn := claimMaintenanceProofDSN(t)
	database := openClaimDeadlockProofDB(t, dsn, 4)
	at := time.Now().UTC().Truncate(time.Second)
	fullGuardRaceScope(t, database, at, at.Add(5*time.Second))
	paused := openPausedClaimPool(t, database, dsn)

	type claimResult struct {
		generation string
		ok         bool
		err        error
	}
	done := make(chan claimResult, 1)
	go func() {
		queue := NewProjectorQueue(SQLDB{DB: paused}, "worker-b", time.Minute)
		queue.Now = func() time.Time { return at }
		work, ok, err := queue.Claim(context.Background())
		done <- claimResult{work.Generation.GenerationID, ok, err}
	}()
	waitForPausedClaimer(t, dsn)

	racer := NewProjectorQueue(SQLDB{DB: database}, "worker-a", time.Minute)
	racer.Now = func() time.Time { return at.Add(10 * time.Second) }
	work, ok, err := racer.Claim(context.Background())
	if err != nil || !ok || work.Generation.GenerationID != "gen-s1" {
		t.Fatalf("racing Claim() = (%q, %v, %v), want gen-s1", work.Generation.GenerationID, ok, err)
	}

	result := <-done
	if result.err != nil {
		t.Fatalf("paused Claim() error = %v", result.err)
	}
	if got := claimedLeaseCount(t, database, "scope-s"); got != 1 {
		t.Fatalf("scope-s holds %d leases after both claims, want 1 (paused claim took %q)", got, result.generation)
	}
	if !result.ok || result.generation != "gen-z2" {
		t.Fatalf("paused Claim() = (%q, %v), want gen-z2 in the other scope", result.generation, result.ok)
	}
	observer := NewQueueObserverStore(SQLQueryer{DB: database})
	if got, err := observer.ProjectorScopesWithMultipleLiveLeases(context.Background()); err != nil || got != 0 {
		t.Fatalf("ProjectorScopesWithMultipleLiveLeases() = (%d, %v), want 0", got, err)
	}
}
