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

// fullGuardSeedScope seeds the #7473 shape: gen-fg1 is the older generation
// with a projector row in olderWorkStatus, gen-fg2 the newer pending
// generation. olderDelta/newerDelta set each generation's is_delta; a full is
// is_delta = false. olderVisibleAt/olderUpdatedAt control gen-fg1's
// visibility and oldest-ready order against gen-fg2.
func fullGuardSeedScope(
	t *testing.T,
	database *sql.DB,
	at time.Time,
	olderWorkStatus string,
	olderVisibleAt, olderUpdatedAt time.Time,
	olderDelta, newerDelta bool,
) {
	t.Helper()
	seedClaimMaintenanceScopes(t, database, "scope-fg")
	seedFenceProofWork(t, database, fenceProofWork{
		"scope-fg", "gen-fg1", "pending", olderWorkStatus, 2,
		at.Add(-2 * time.Hour), olderVisibleAt, olderUpdatedAt,
	})
	seedFenceProofWork(t, database, fenceProofWork{
		"scope-fg", "gen-fg2", "pending", "pending", 0,
		at.Add(-time.Hour), at.Add(-time.Hour), at.Add(-time.Hour),
	})
	for _, isDelta := range []struct {
		generation string
		delta      bool
	}{
		{"gen-fg1", olderDelta},
		{"gen-fg2", newerDelta},
	} {
		if _, err := database.Exec(
			`UPDATE scope_generations SET is_delta = $1 WHERE generation_id = $2`,
			isDelta.delta, isDelta.generation,
		); err != nil {
			t.Fatalf("set is_delta on %s: %v", isDelta.generation, err)
		}
	}
}

// TestProjectorClaimFullSurvivesNewerDelta is the #7473 regression: gen-fg1
// is a pending reconcile full and gen-fg2 a newer pending delta. The claim
// sweep must not retire the full for a delta, and the oldest-ready order must
// hand out the full first so the heal projects before the delta.
func TestProjectorClaimFullSurvivesNewerDelta(t *testing.T) {
	dsn := claimMaintenanceProofDSN(t)
	database := openClaimDeadlockProofDB(t, dsn, 2)
	at := time.Now().UTC().Truncate(time.Second)
	fullGuardSeedScope(t, database, at, "pending", at.Add(-2*time.Hour), at.Add(-2*time.Hour), false, true)
	queue := NewProjectorQueue(SQLDB{DB: database}, "claimer", time.Minute)
	queue.Now = func() time.Time { return at }

	work, ok, err := queue.Claim(context.Background())
	if err != nil || !ok || work.Generation.GenerationID != "gen-fg1" {
		t.Fatalf("Claim() = (%q, %v, %v), want the gen-fg1 full first",
			work.Generation.GenerationID, ok, err)
	}
	if got := generationState(t, database, "gen-fg1"); got != "pending" {
		t.Fatalf("gen-fg1 generation = %q, want pending", got)
	}
	if status, _, _ := workState(t, database, "scope-fg", "gen-fg2"); status != "pending" {
		t.Fatalf("gen-fg2 work = %s, want pending behind the full", status)
	}
}

// TestProjectorClaimFailedFullStillHoldsDelta pins the #7473 fail-safe
// direction for a failed holder: gen-fg1 is a full whose generation failed
// but whose retry is still waiting, with a newer pending delta behind it.
// The shape is drifted — gen-fg1 failed after gen-fg2 was enqueued, so its
// updated_at sorts after the delta and oldest-ready order alone would hand
// out gen-fg2: only the hold keeps the delta back. The hold admits
// generation status pending and failed, so the failed full must still be
// handed out first; only dead-lettered work stops holding.
func TestProjectorClaimFailedFullStillHoldsDelta(t *testing.T) {
	dsn := claimMaintenanceProofDSN(t)
	database := openClaimDeadlockProofDB(t, dsn, 2)
	at := time.Now().UTC().Truncate(time.Second)
	fullGuardSeedScope(t, database, at, "retrying", at.Add(-time.Minute), at, false, true)
	if _, err := database.Exec(
		`UPDATE scope_generations SET status = 'failed' WHERE generation_id = 'gen-fg1'`); err != nil {
		t.Fatalf("mark gen-fg1 failed: %v", err)
	}
	queue := NewProjectorQueue(SQLDB{DB: database}, "claimer", time.Minute)
	queue.Now = func() time.Time { return at }

	work, ok, err := queue.Claim(context.Background())
	if err != nil || !ok || work.Generation.GenerationID != "gen-fg1" {
		t.Fatalf("Claim() = (%q, %v, %v), want the failed gen-fg1 full first",
			work.Generation.GenerationID, ok, err)
	}
	if got := generationState(t, database, "gen-fg1"); got != "failed" {
		t.Fatalf("gen-fg1 generation = %q, want failed", got)
	}
	if status, _, _ := workState(t, database, "scope-fg", "gen-fg2"); status != "pending" {
		t.Fatalf("gen-fg2 work = %s, want pending behind the failed full", status)
	}
}

// TestProjectorClaimHoldsDeltaBehindBackoffFull is the #7473 admission guard:
// gen-fg1 is a full backing off a retryable failure while the newer delta
// gen-fg2 is ready. The claim must neither supersede the full nor hand out
// the delta first: projecting the delta first would strand the heal, and
// projecting the stale full after the delta would regress the graph. Once the
// backoff elapses, the full itself must be claimed.
func TestProjectorClaimHoldsDeltaBehindBackoffFull(t *testing.T) {
	dsn := claimMaintenanceProofDSN(t)
	database := openClaimDeadlockProofDB(t, dsn, 2)
	at := time.Now().UTC().Truncate(time.Second)
	fullGuardSeedScope(t, database, at, "retrying", at.Add(time.Minute), at.Add(-2*time.Hour), false, true)
	ctx := context.Background()

	queue := NewProjectorQueue(SQLDB{DB: database}, "claimer", time.Minute)
	queue.Now = func() time.Time { return at }
	if work, ok, err := queue.Claim(ctx); err != nil || ok {
		t.Fatalf("Claim() during backoff = (%q, %v, %v), want no claim while the full retry waits",
			work.Generation.GenerationID, ok, err)
	}
	if status, class, _ := workState(t, database, "scope-fg", "gen-fg1"); status != "retrying" || class != "" {
		t.Fatalf("gen-fg1 work = (%s, %q), want retrying untouched, never superseded", status, class)
	}
	if got := generationState(t, database, "gen-fg1"); got != "pending" {
		t.Fatalf("gen-fg1 generation = %q, want pending", got)
	}
	if status, _, _ := workState(t, database, "scope-fg", "gen-fg2"); status != "pending" {
		t.Fatalf("gen-fg2 work = %s, want pending behind the full retry", status)
	}

	queue.Now = func() time.Time { return at.Add(61 * time.Second) }
	work, ok, err := queue.Claim(ctx)
	if err != nil || !ok || work.Generation.GenerationID != "gen-fg1" {
		t.Fatalf("Claim() after backoff = (%q, %v, %v), want the gen-fg1 full",
			work.Generation.GenerationID, ok, err)
	}
	if status, _, _ := workState(t, database, "scope-fg", "gen-fg2"); status != "pending" {
		t.Fatalf("gen-fg2 work = %s after the full claimed, want still pending", status)
	}
}

// TestProjectorClaimRunsDriftedFullBeforeNewerDelta covers the #7473 holder
// that the oldest-ready order would skip: gen-fg1's retry failed recently, so
// its updated_at is newer than gen-fg2's, yet the claim must run the full
// first. The hold compares generation order, not row recency.
func TestProjectorClaimRunsDriftedFullBeforeNewerDelta(t *testing.T) {
	dsn := claimMaintenanceProofDSN(t)
	database := openClaimDeadlockProofDB(t, dsn, 2)
	at := time.Now().UTC().Truncate(time.Second)
	fullGuardSeedScope(t, database, at, "retrying", at.Add(-time.Minute), at, false, true)
	queue := NewProjectorQueue(SQLDB{DB: database}, "claimer", time.Minute)
	queue.Now = func() time.Time { return at }

	work, ok, err := queue.Claim(context.Background())
	if err != nil || !ok || work.Generation.GenerationID != "gen-fg1" {
		t.Fatalf("Claim() = (%q, %v, %v), want the drifted gen-fg1 full first",
			work.Generation.GenerationID, ok, err)
	}
	if got := generationState(t, database, "gen-fg1"); got != "pending" {
		t.Fatalf("gen-fg1 generation = %q, want pending", got)
	}
	if status, _, _ := workState(t, database, "scope-fg", "gen-fg2"); status != "pending" {
		t.Fatalf("gen-fg2 work = %s, want pending behind the full", status)
	}
}

// TestProjectorClaimStillSupersedesFullBehindNewerFull is the #7473 control:
// a newer full covers the older full's heal, so the stale full is genuinely
// superseded with the same failure class.
func TestProjectorClaimStillSupersedesFullBehindNewerFull(t *testing.T) {
	dsn := claimMaintenanceProofDSN(t)
	database := openClaimDeadlockProofDB(t, dsn, 2)
	at := time.Now().UTC().Truncate(time.Second)
	fullGuardSeedScope(t, database, at, "pending", at.Add(-2*time.Hour), at.Add(-2*time.Hour), false, false)
	queue := NewProjectorQueue(SQLDB{DB: database}, "claimer", time.Minute)
	queue.Now = func() time.Time { return at }

	work, ok, err := queue.Claim(context.Background())
	if err != nil || !ok || work.Generation.GenerationID != "gen-fg2" {
		t.Fatalf("Claim() = (%q, %v, %v), want gen-fg2 over the stale full",
			work.Generation.GenerationID, ok, err)
	}
	if status, class, _ := workState(t, database, "scope-fg", "gen-fg1"); status != "superseded" ||
		class != "projector_superseded_by_newer_generation" {
		t.Fatalf("gen-fg1 work = (%s, %q), want superseded with the supersede marker class", status, class)
	}
	if got := generationState(t, database, "gen-fg1"); got != "superseded" {
		t.Fatalf("gen-fg1 generation = %q, want superseded", got)
	}
}

// TestProjectorClaimStillSupersedesDeltaBehindNewerDelta is the #7473 delta
// control: a stale delta beside a newer delta keeps today's behavior.
func TestProjectorClaimStillSupersedesDeltaBehindNewerDelta(t *testing.T) {
	dsn := claimMaintenanceProofDSN(t)
	database := openClaimDeadlockProofDB(t, dsn, 2)
	at := time.Now().UTC().Truncate(time.Second)
	fullGuardSeedScope(t, database, at, "pending", at.Add(-2*time.Hour), at.Add(-2*time.Hour), true, true)
	queue := NewProjectorQueue(SQLDB{DB: database}, "claimer", time.Minute)
	queue.Now = func() time.Time { return at }

	work, ok, err := queue.Claim(context.Background())
	if err != nil || !ok || work.Generation.GenerationID != "gen-fg2" {
		t.Fatalf("Claim() = (%q, %v, %v), want gen-fg2 over the stale delta",
			work.Generation.GenerationID, ok, err)
	}
	if status, class, _ := workState(t, database, "scope-fg", "gen-fg1"); status != "superseded" ||
		class != "projector_superseded_by_newer_generation" {
		t.Fatalf("gen-fg1 work = (%s, %q), want superseded with the supersede marker class", status, class)
	}
	if got := generationState(t, database, "gen-fg1"); got != "superseded" {
		t.Fatalf("gen-fg1 generation = %q, want superseded", got)
	}
}

// TestProjectorClaimClaimsDeltaBehindTerminalFull pins the #7473 boundary: a
// dead-lettered full never becomes claimable again on its own, so it must not
// hold its scope's newer delta forever. The delta is claimed; the terminal
// full and its pending generation are left for replay and the collector's
// next reconcile decision, not superseded away by the claim sweep.
func TestProjectorClaimClaimsDeltaBehindTerminalFull(t *testing.T) {
	dsn := claimMaintenanceProofDSN(t)
	database := openClaimDeadlockProofDB(t, dsn, 2)
	at := time.Now().UTC().Truncate(time.Second)
	seedClaimMaintenanceScopes(t, database, "scope-fg")
	seedFenceProofWork(t, database, fenceProofWork{
		"scope-fg", "gen-fg1", "failed", "dead_letter", 3,
		at.Add(-2 * time.Hour), at.Add(-2 * time.Hour), at.Add(-2 * time.Hour),
	})
	seedFenceProofWork(t, database, fenceProofWork{
		"scope-fg", "gen-fg2", "pending", "pending", 0,
		at.Add(-time.Hour), at.Add(-time.Hour), at.Add(-time.Hour),
	})
	if _, err := database.Exec(
		`UPDATE scope_generations SET is_delta = true WHERE generation_id = 'gen-fg2'`); err != nil {
		t.Fatalf("mark gen-fg2 delta: %v", err)
	}
	queue := NewProjectorQueue(SQLDB{DB: database}, "claimer", time.Minute)
	queue.Now = func() time.Time { return at }

	work, ok, err := queue.Claim(context.Background())
	if err != nil || !ok || work.Generation.GenerationID != "gen-fg2" {
		t.Fatalf("Claim() = (%q, %v, %v), want gen-fg2 past the terminal full",
			work.Generation.GenerationID, ok, err)
	}
	if status, _, _ := workState(t, database, "scope-fg", "gen-fg1"); status != "dead_letter" {
		t.Fatalf("gen-fg1 work = %s, want dead_letter left for replay", status)
	}
	if got := generationState(t, database, "gen-fg1"); got != "failed" {
		t.Fatalf("gen-fg1 generation = %q, want failed", got)
	}
}

// TestProjectorAckStillSupersedesFullBehindAckedDelta pins the #7473 backstop:
// Ack's obsolete-generation sweep keeps today's full-blind predicate, so a
// full that loses the race corner (a delta claimed while the full's retry
// committed after the claim snapshot) is still retired when the delta Acks
// instead of projecting stale content over the newer delta. Late arrival is
// the deterministic way to reach that state: the full commits while gen-fg2
// is being projected.
func TestProjectorAckStillSupersedesFullBehindAckedDelta(t *testing.T) {
	dsn := claimMaintenanceProofDSN(t)
	database := openClaimDeadlockProofDB(t, dsn, 2)
	at := time.Now().UTC().Truncate(time.Second)
	seedClaimMaintenanceScopes(t, database, "scope-fg")
	seedFenceProofWork(t, database, fenceProofWork{
		"scope-fg", "gen-fg2", "pending", "pending", 0,
		at.Add(-time.Hour), at.Add(-time.Hour), at.Add(-time.Hour),
	})
	if _, err := database.Exec(
		`UPDATE scope_generations SET is_delta = true WHERE generation_id = 'gen-fg2'`); err != nil {
		t.Fatalf("mark gen-fg2 delta: %v", err)
	}
	ctx := context.Background()
	queue := NewProjectorQueue(SQLDB{DB: database}, "claimer", time.Minute)
	queue.Now = func() time.Time { return at }
	work, ok, err := queue.Claim(ctx)
	if err != nil || !ok || work.Generation.GenerationID != "gen-fg2" {
		t.Fatalf("Claim() = (%q, %v, %v), want gen-fg2", work.Generation.GenerationID, ok, err)
	}
	// An older full committed while gen-fg2 was being projected.
	seedFenceProofWork(t, database, fenceProofWork{
		"scope-fg", "gen-fg1", "pending", "pending", 0,
		at.Add(-2 * time.Hour), at.Add(-time.Hour), at.Add(-time.Hour),
	})
	if err := queue.Ack(ctx, work, runtime.Result{}); err != nil {
		t.Fatalf("Ack(gen-fg2) error = %v, want success", err)
	}
	if got := generationState(t, database, "gen-fg2"); got != "active" {
		t.Fatalf("gen-fg2 generation = %q, want active", got)
	}
	if status, class, _ := workState(t, database, "scope-fg", "gen-fg1"); status != "superseded" ||
		class != "projector_superseded_by_newer_generation" {
		t.Fatalf("gen-fg1 work = (%s, %q), want superseded with the supersede marker class", status, class)
	}
	if got := generationState(t, database, "gen-fg1"); got != "superseded" {
		t.Fatalf("gen-fg1 generation = %q, want superseded", got)
	}
}
