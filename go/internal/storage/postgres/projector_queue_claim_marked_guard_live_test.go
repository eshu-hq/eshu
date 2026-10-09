// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/projector/runtime"
)

// markGuardSeedScope seeds the #7469 shape: gen-mg1 is the older generation with a
// retrying projector row, gen-mg2 the newer pending generation. marked sets
// gen-mg1's projection_write_started_at; olderVisibleAt/olderUpdatedAt control
// its visibility and oldest-ready order against gen-mg2.
func markGuardSeedScope(
	t *testing.T,
	database *sql.DB,
	at time.Time,
	marked bool,
	olderWorkStatus string,
	olderVisibleAt, olderUpdatedAt time.Time,
) {
	t.Helper()
	seedClaimMaintenanceScopes(t, database, "scope-mg")
	seedFenceProofWork(t, database, fenceProofWork{
		"scope-mg", "gen-mg1", "pending", olderWorkStatus, 2,
		at.Add(-2 * time.Hour), olderVisibleAt, olderUpdatedAt,
	})
	seedFenceProofWork(t, database, fenceProofWork{
		"scope-mg", "gen-mg2", "pending", "pending", 0,
		at.Add(-time.Hour), at.Add(-time.Hour), at.Add(-time.Hour),
	})
	if marked {
		if _, err := database.Exec(
			`UPDATE scope_generations SET projection_write_started_at = $1 WHERE generation_id = 'gen-mg1'`,
			at.Add(-90*time.Minute),
		); err != nil {
			t.Fatalf("mark gen-mg1: %v", err)
		}
	}
}

// TestProjectorClaimHoldsNewerBehindInvisibleMarkedRetry is the #7469
// admission guard: gen-mg1 set its write marker, wrote part of the graph, and
// is backing off a retryable failure, while the newer gen-mg2 is ready. The
// claim must neither supersede gen-mg1 nor hand out gen-mg2 first. Once the
// backoff elapses, the retry itself must be claimed.
func TestProjectorClaimHoldsNewerBehindInvisibleMarkedRetry(t *testing.T) {
	dsn := claimMaintenanceProofDSN(t)
	database := openClaimDeadlockProofDB(t, dsn, 2)
	at := time.Now().UTC().Truncate(time.Second)
	markGuardSeedScope(t, database, at, true, "retrying", at.Add(time.Minute), at.Add(-2*time.Hour))
	ctx := context.Background()

	queue := NewProjectorQueue(SQLDB{DB: database}, "claimer", time.Minute)
	queue.Now = func() time.Time { return at }
	if work, ok, err := queue.Claim(ctx); err != nil || ok {
		t.Fatalf("Claim() during backoff = (%q, %v, %v), want no claim while the marked retry waits",
			work.Generation.GenerationID, ok, err)
	}
	if status, class, _ := workState(t, database, "scope-mg", "gen-mg1"); status != "retrying" || class != "" {
		t.Fatalf("gen-mg1 work = (%s, %q), want retrying untouched, never superseded", status, class)
	}
	if got := generationState(t, database, "gen-mg1"); got != "pending" {
		t.Fatalf("gen-mg1 generation = %q, want pending", got)
	}
	if status, _, _ := workState(t, database, "scope-mg", "gen-mg2"); status != "pending" {
		t.Fatalf("gen-mg2 work = %s, want pending behind the marked retry", status)
	}

	queue.Now = func() time.Time { return at.Add(61 * time.Second) }
	work, ok, err := queue.Claim(ctx)
	if err != nil || !ok || work.Generation.GenerationID != "gen-mg1" {
		t.Fatalf("Claim() after backoff = (%q, %v, %v), want the gen-mg1 retry",
			work.Generation.GenerationID, ok, err)
	}
	if status, _, _ := workState(t, database, "scope-mg", "gen-mg2"); status != "pending" {
		t.Fatalf("gen-mg2 work = %s after the retry claimed, want still pending", status)
	}
}

// TestProjectorClaimRunsVisibleMarkedRetryBeforeNewer covers the #7469 holder
// that the oldest-ready order would skip: gen-mg1's retry failed recently, so
// its updated_at is newer than gen-mg2's, yet the claim must run the marked
// retry first. A replayed marked row (pending with a fresh updated_at) sorts
// the same way and must win too.
func TestProjectorClaimRunsVisibleMarkedRetryBeforeNewer(t *testing.T) {
	dsn := claimMaintenanceProofDSN(t)
	ctx := context.Background()
	for _, tc := range []struct {
		name        string
		workStatus  string
		workUpdated time.Duration // relative to at; newer than gen-mg2's at-1h
	}{
		{"retrying", "retrying", 0},
		{"replayed_pending", "pending", -time.Minute},
	} {
		t.Run(tc.name, func(t *testing.T) {
			database := openClaimDeadlockProofDB(t, dsn, 2)
			at := time.Now().UTC().Truncate(time.Second)
			markGuardSeedScope(t, database, at, true, tc.workStatus, at.Add(-time.Minute), at.Add(tc.workUpdated))
			queue := NewProjectorQueue(SQLDB{DB: database}, "claimer", time.Minute)
			queue.Now = func() time.Time { return at }

			work, ok, err := queue.Claim(ctx)
			if err != nil || !ok || work.Generation.GenerationID != "gen-mg1" {
				t.Fatalf("Claim() = (%q, %v, %v), want the marked gen-mg1 %s first",
					work.Generation.GenerationID, ok, err, tc.workStatus)
			}
			if got := generationState(t, database, "gen-mg1"); got != "pending" {
				t.Fatalf("gen-mg1 generation = %q, want pending", got)
			}
			if status, _, _ := workState(t, database, "scope-mg", "gen-mg2"); status != "pending" {
				t.Fatalf("gen-mg2 work = %s, want pending behind the marked retry", status)
			}
		})
	}
}

// TestProjectorClaimStillSupersedesUnmarkedStaleGeneration is the #7469
// control: without a write marker the stale retry is genuinely superseded with
// the same failure class, so the graph_dirty residual watch keeps its meaning.
func TestProjectorClaimStillSupersedesUnmarkedStaleGeneration(t *testing.T) {
	dsn := claimMaintenanceProofDSN(t)
	database := openClaimDeadlockProofDB(t, dsn, 2)
	at := time.Now().UTC().Truncate(time.Second)
	markGuardSeedScope(t, database, at, false, "retrying", at.Add(-time.Minute), at.Add(-2*time.Hour))
	queue := NewProjectorQueue(SQLDB{DB: database}, "claimer", time.Minute)
	queue.Now = func() time.Time { return at }

	work, ok, err := queue.Claim(context.Background())
	if err != nil || !ok || work.Generation.GenerationID != "gen-mg2" {
		t.Fatalf("Claim() = (%q, %v, %v), want gen-mg2 over the unmarked stale retry",
			work.Generation.GenerationID, ok, err)
	}
	if status, class, _ := workState(t, database, "scope-mg", "gen-mg1"); status != "superseded" ||
		class != "projector_superseded_by_newer_generation" {
		t.Fatalf("gen-mg1 work = (%s, %q), want superseded with the supersede marker class", status, class)
	}
	if got := generationState(t, database, "gen-mg1"); got != "superseded" {
		t.Fatalf("gen-mg1 generation = %q, want superseded", got)
	}
}

// TestProjectorClaimClaimsNewerBehindTerminalMarkedGeneration pins the #7469
// boundary: a dead-lettered marked generation never becomes claimable again
// on its own, so it must not hold its scope's newer generation forever. The
// newer generation is claimed; the terminal row and its failed generation are
// left for replay and the graph_dirty full snapshot, not superseded away.
func TestProjectorClaimClaimsNewerBehindTerminalMarkedGeneration(t *testing.T) {
	dsn := claimMaintenanceProofDSN(t)
	database := openClaimDeadlockProofDB(t, dsn, 2)
	at := time.Now().UTC().Truncate(time.Second)
	markGuardSeedDeadMarkedScope(t, database, at, true)
	queue := NewProjectorQueue(SQLDB{DB: database}, "claimer", time.Minute)
	queue.Now = func() time.Time { return at }

	work, ok, err := queue.Claim(context.Background())
	if err != nil || !ok || work.Generation.GenerationID != "gen-mg2" {
		t.Fatalf("Claim() = (%q, %v, %v), want gen-mg2 past the terminal marked row",
			work.Generation.GenerationID, ok, err)
	}
	if status, _, _ := workState(t, database, "scope-mg", "gen-mg1"); status != "dead_letter" {
		t.Fatalf("gen-mg1 work = %s, want dead_letter left for replay", status)
	}
	if got := generationState(t, database, "gen-mg1"); got != "failed" {
		t.Fatalf("gen-mg1 generation = %q, want failed", got)
	}
}

// markGuardSeedDeadMarkedScope seeds gen-mg1 as a dead-lettered failed
// generation (marked when asked) beside newer pending gen-mg2.
func markGuardSeedDeadMarkedScope(t *testing.T, database *sql.DB, at time.Time, marked bool) {
	t.Helper()
	seedClaimMaintenanceScopes(t, database, "scope-mg")
	seedFenceProofWork(t, database, fenceProofWork{
		"scope-mg", "gen-mg1", "failed", "dead_letter", 3,
		at.Add(-2 * time.Hour), at.Add(-2 * time.Hour), at.Add(-2 * time.Hour),
	})
	seedFenceProofWork(t, database, fenceProofWork{
		"scope-mg", "gen-mg2", "pending", "pending", 0,
		at.Add(-time.Hour), at.Add(-time.Hour), at.Add(-time.Hour),
	})
	if marked {
		if _, err := database.Exec(
			`UPDATE scope_generations SET projection_write_started_at = $1 WHERE generation_id = 'gen-mg1'`,
			at.Add(-90*time.Minute),
		); err != nil {
			t.Fatalf("mark gen-mg1: %v", err)
		}
	}
}

// markGuardReplayOldest replays gen-mg1's dead row the way the dead-letter
// replay does: back to pending with its failure cleared.
func markGuardReplayOldest(t *testing.T, database *sql.DB, at time.Time) {
	t.Helper()
	if _, err := database.Exec(
		`UPDATE fact_work_items SET status = 'pending', attempt_count = GREATEST(attempt_count, 1),
		    lease_owner = NULL, claim_until = NULL, visible_at = $1, next_attempt_at = NULL,
		    failure_class = NULL, failure_message = NULL, failure_details = NULL, updated_at = $1
		 WHERE work_item_id = $2`,
		at, projectorWorkItemID("scope-mg", "gen-mg1"),
	); err != nil {
		t.Fatalf("replay gen-mg1: %v", err)
	}
}

// TestProjectorAckKeepsMarkedObsoleteGeneration gates Ack's obsolete-generation
// supersede (#7469): gen-mg1 was dead-lettered marked and gen-mg2 was claimed
// past it. Whether gen-mg1 stays terminal or the operator replays it before
// gen-mg2 Acks, the Ack must publish gen-mg2 without retiring the marked
// generation. On the ungated tree the claim sweep retires gen-mg1 first, so
// RED there also runs through the claim path; after the claim gate lands this
// test isolates the Ack query.
func TestProjectorAckKeepsMarkedObsoleteGeneration(t *testing.T) {
	dsn := claimMaintenanceProofDSN(t)
	ctx := context.Background()
	for _, tc := range []struct {
		name           string
		replay         bool
		wantWorkStatus string
	}{
		{"dead_letter", false, "dead_letter"},
		{"replayed_pending", true, "pending"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			database := openClaimDeadlockProofDB(t, dsn, 2)
			at := time.Now().UTC().Truncate(time.Second)
			markGuardSeedDeadMarkedScope(t, database, at, true)
			queue := NewProjectorQueue(SQLDB{DB: database}, "claimer", time.Minute)
			queue.Now = func() time.Time { return at }
			work, ok, err := queue.Claim(ctx)
			if err != nil || !ok || work.Generation.GenerationID != "gen-mg2" {
				t.Fatalf("Claim() = (%q, %v, %v), want gen-mg2", work.Generation.GenerationID, ok, err)
			}
			if tc.replay {
				markGuardReplayOldest(t, database, at)
			}
			if err := queue.Ack(ctx, work, runtime.Result{}); err != nil {
				t.Fatalf("Ack(gen-mg2) error = %v, want success", err)
			}
			if got := generationState(t, database, "gen-mg2"); got != "active" {
				t.Fatalf("gen-mg2 generation = %q, want active", got)
			}
			if status, class, _ := workState(t, database, "scope-mg", "gen-mg1"); status != tc.wantWorkStatus || class != "" {
				t.Fatalf("gen-mg1 work = (%s, %q), want %s with no supersede class",
					status, class, tc.wantWorkStatus)
			}
			if got := generationState(t, database, "gen-mg1"); got != "failed" {
				t.Fatalf("gen-mg1 generation = %q, want failed", got)
			}
		})
	}
}

// TestProjectorAckStillSupersedesUnmarkedObsoleteGeneration is the #7469 Ack
// control: an unmarked older generation that arrives while the newer one is in
// flight is genuinely obsolete, so the Ack retires it with the supersede
// marker class. Late arrival is the only realistic way to reach Ack's obsolete
// supersede: anything present at claim time is swept there instead.
func TestProjectorAckStillSupersedesUnmarkedObsoleteGeneration(t *testing.T) {
	dsn := claimMaintenanceProofDSN(t)
	database := openClaimDeadlockProofDB(t, dsn, 2)
	at := time.Now().UTC().Truncate(time.Second)
	seedClaimMaintenanceScopes(t, database, "scope-mg")
	seedFenceProofWork(t, database, fenceProofWork{
		"scope-mg", "gen-mg2", "pending", "pending", 0,
		at.Add(-time.Hour), at.Add(-time.Hour), at.Add(-time.Hour),
	})
	ctx := context.Background()
	queue := NewProjectorQueue(SQLDB{DB: database}, "claimer", time.Minute)
	queue.Now = func() time.Time { return at }
	work, ok, err := queue.Claim(ctx)
	if err != nil || !ok || work.Generation.GenerationID != "gen-mg2" {
		t.Fatalf("Claim() = (%q, %v, %v), want gen-mg2", work.Generation.GenerationID, ok, err)
	}
	// An older generation committed while gen-mg2 was being projected.
	seedFenceProofWork(t, database, fenceProofWork{
		"scope-mg", "gen-mg1", "pending", "pending", 0,
		at.Add(-2 * time.Hour), at.Add(-time.Hour), at.Add(-time.Hour),
	})
	if err := queue.Ack(ctx, work, runtime.Result{}); err != nil {
		t.Fatalf("Ack(gen-mg2) error = %v, want success", err)
	}
	if got := generationState(t, database, "gen-mg2"); got != "active" {
		t.Fatalf("gen-mg2 generation = %q, want active", got)
	}
	if status, class, _ := workState(t, database, "scope-mg", "gen-mg1"); status != "superseded" ||
		class != "projector_superseded_by_newer_generation" {
		t.Fatalf("gen-mg1 work = (%s, %q), want superseded with the supersede marker class", status, class)
	}
	if got := generationState(t, database, "gen-mg1"); got != "superseded" {
		t.Fatalf("gen-mg1 generation = %q, want superseded", got)
	}
}
