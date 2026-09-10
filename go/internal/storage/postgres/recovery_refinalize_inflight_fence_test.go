// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/recovery"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/rebuildreset"
)

// TestRefinalizeAbortsWhileReducersHoldLiveLeases is the Codex #6184 P1
// failing-first proof: a refinalize that runs while a reducer work item holds
// a live lease must abort instead of retiring the generation under the
// running resolver. Without the fence the refinalize succeeds, the generation
// goes superseded, and the in-flight resolver then re-activates it with stale
// rows while its success ack dedupes the re-emitted intent. The abort must
// roll the whole transaction back: the generation stays active and the
// succeeded row the reset would have deleted survives.
func TestRefinalizeAbortsWhileReducersHoldLiveLeases(t *testing.T) {
	db, ctx := refinalizeRebuildResetLiveDB(t)
	suffix := testSuffix(t)
	scopeID, activeGeneration, _ := refinalizeResetScope(t, ctx, db, suffix)

	claimed := seedRefinalizeResetReducerWork(t, ctx, db, scopeID, activeGeneration, "fence-claimed", "claimed")
	armLiveLease(t, ctx, db, claimed, 5*time.Minute)
	succeeded := seedRefinalizeResetReducerWork(t, ctx, db, scopeID, activeGeneration, "fence-succeeded", "succeeded")
	seedActiveRelationshipGeneration(t, ctx, db, activeGeneration, scopeID)

	store := NewRecoveryStore(SQLDB{DB: db},
		WithRefinalizeDrainTimeout(2*time.Second),
		WithRefinalizeDrainPollInterval(25*time.Millisecond),
	)
	_, err := store.RefinalizeScopeProjections(ctx, recovery.RefinalizeFilter{
		ScopeIDs: []string{scopeID},
	}, time.Now().UTC())
	var fenceErr *rebuildreset.InflightReducersError
	if !errors.As(err, &fenceErr) {
		t.Fatalf("RefinalizeScopeProjections() error = %v, want *rebuildreset.InflightReducersError: "+
			"retiring under a live reducer lease strands the in-flight resolution", err)
	}
	if fenceErr.Inflight < 1 {
		t.Fatalf("fence error reports %d in-flight rows, want >= 1", fenceErr.Inflight)
	}
	if got := relationshipGenerationStatus(t, ctx, db, activeGeneration); got != "active" {
		t.Fatalf("relationship generation status = %q, want %q: the aborted refinalize "+
			"must not retire anything", got, "active")
	}
	if got := refinalizeResetWorkItemStatus(t, ctx, db, succeeded); got != "succeeded" {
		t.Fatalf("succeeded work item status = %q, want %q: the abort must roll back "+
			"the whole refinalize, not leave a half-applied reset", got, "succeeded")
	}
	if got := refinalizeResetWorkItemStatus(t, ctx, db, claimed); got != "claimed" {
		t.Fatalf("claimed work item status = %q, want %q: the fence must not disturb "+
			"the live lease it waited on", got, "claimed")
	}
}

// TestRefinalizeProceedsOnceReducersDrain proves the fence waits rather than
// only aborts: a claimed row that completes mid-refinalize lets the refinalize
// through, and the generation is retired.
func TestRefinalizeProceedsOnceReducersDrain(t *testing.T) {
	db, ctx := refinalizeRebuildResetLiveDB(t)
	suffix := testSuffix(t)
	scopeID, activeGeneration, _ := refinalizeResetScope(t, ctx, db, suffix)

	claimed := seedRefinalizeResetReducerWork(t, ctx, db, scopeID, activeGeneration, "fence-draining", "claimed")
	armLiveLease(t, ctx, db, claimed, 5*time.Minute)
	seedActiveRelationshipGeneration(t, ctx, db, activeGeneration, scopeID)

	// The worker finishes shortly after the refinalize starts waiting: the
	// row leaves the live-lease set (claimed with an expired lease is
	// reclaimable, not in-flight) and the drain observes it on a later poll.
	// The margins are wide on purpose: poll every 25ms, completion at ~200ms,
	// bound at 30s.
	time.AfterFunc(200*time.Millisecond, func() {
		if _, err := db.Exec(
			`UPDATE fact_work_items SET status = 'succeeded', lease_owner = NULL, claim_until = NULL WHERE work_item_id = $1`,
			claimed); err != nil {
			t.Errorf("complete draining work item: %v", err)
		}
	})

	store := NewRecoveryStore(SQLDB{DB: db},
		WithRefinalizeDrainTimeout(30*time.Second),
		WithRefinalizeDrainPollInterval(25*time.Millisecond),
	)
	result, err := store.RefinalizeScopeProjections(ctx, recovery.RefinalizeFilter{
		ScopeIDs: []string{scopeID},
	}, time.Now().UTC())
	if err != nil {
		t.Fatalf("RefinalizeScopeProjections() error = %v, want nil: the fence must let "+
			"a drained refinalize through", err)
	}
	if got := relationshipGenerationStatus(t, ctx, db, activeGeneration); got != "superseded" {
		t.Fatalf("relationship generation status = %q, want %q", got, "superseded")
	}
	if result.GenerationsRetired != 1 {
		t.Fatalf("result.GenerationsRetired = %d, want 1", result.GenerationsRetired)
	}
}

// TestRefinalizeIgnoresExpiredReducerLeases proves the fence keys on live
// leases, not on claimed status alone: a crashed worker's expired lease is
// reclaimable, and whoever reclaims it resolves post-retirement, so holding
// recovery behind it would wedge every refinalize on dead rows.
func TestRefinalizeIgnoresExpiredReducerLeases(t *testing.T) {
	db, ctx := refinalizeRebuildResetLiveDB(t)
	suffix := testSuffix(t)
	scopeID, activeGeneration, _ := refinalizeResetScope(t, ctx, db, suffix)

	stuck := seedRefinalizeResetReducerWork(t, ctx, db, scopeID, activeGeneration, "fence-stuck", "claimed")
	armLiveLease(t, ctx, db, stuck, -5*time.Minute)
	seedActiveRelationshipGeneration(t, ctx, db, activeGeneration, scopeID)

	store := NewRecoveryStore(SQLDB{DB: db},
		WithRefinalizeDrainTimeout(2*time.Second),
		WithRefinalizeDrainPollInterval(25*time.Millisecond),
	)
	result, err := store.RefinalizeScopeProjections(ctx, recovery.RefinalizeFilter{
		ScopeIDs: []string{scopeID},
	}, time.Now().UTC())
	if err != nil {
		t.Fatalf("RefinalizeScopeProjections() error = %v, want nil: an expired lease "+
			"is not in-flight work", err)
	}
	if got := relationshipGenerationStatus(t, ctx, db, activeGeneration); got != "superseded" {
		t.Fatalf("relationship generation status = %q, want %q", got, "superseded")
	}
	if result.GenerationsRetired != 1 {
		t.Fatalf("result.GenerationsRetired = %d, want 1", result.GenerationsRetired)
	}
}

// TestAssertRetirementFencedDistinguishesGuardTripFromNoOp covers the
// post-Apply distinguish deterministically, without timing: a zero retired
// count with a live lease outstanding is the tripped atomic guard and must
// abort, while a zero count with no live lease is the convergent re-run and
// must commit, as must any positive count.
func TestAssertRetirementFencedDistinguishesGuardTripFromNoOp(t *testing.T) {
	db, ctx := refinalizeRebuildResetLiveDB(t)
	suffix := testSuffix(t)
	scopeID, activeGeneration, _ := refinalizeResetScope(t, ctx, db, suffix)

	claimed := seedRefinalizeResetReducerWork(t, ctx, db, scopeID, activeGeneration, "fence-guard-trip", "claimed")
	armLiveLease(t, ctx, db, claimed, 5*time.Minute)

	tx, err := SQLDB{DB: db}.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	t.Cleanup(func() { _ = tx.Rollback() })

	var fenced rebuildreset.Generations
	fenced.Append(scopeID, activeGeneration)
	var idle rebuildreset.Generations
	idle.Append(scopeID, "fence-nonexistent-generation-"+suffix)

	var fenceErr *rebuildreset.InflightReducersError
	if err := rebuildreset.AssertRetirementFenced(ctx, rebuildresetQueryer{Transaction: tx}, fenced, 0); !errors.As(err, &fenceErr) {
		t.Fatalf("assertRetirementFenced(retired=0, live lease) = %v, want *rebuildreset.InflightReducersError", err)
	}
	if err := rebuildreset.AssertRetirementFenced(ctx, rebuildresetQueryer{Transaction: tx}, idle, 0); err != nil {
		t.Fatalf("assertRetirementFenced(retired=0, no lease) = %v, want nil: a convergent "+
			"re-run must commit", err)
	}
	if err := rebuildreset.AssertRetirementFenced(ctx, rebuildresetQueryer{Transaction: tx}, fenced, 1); err != nil {
		t.Fatalf("assertRetirementFenced(retired=1) = %v, want nil", err)
	}
}

// armLiveLease gives a seeded work item a claim lease expiring ttl from now,
// so the fence tests control live versus expired without a real worker. A
// negative ttl arms an already-expired lease, the crashed-worker shape.
func armLiveLease(t *testing.T, ctx context.Context, db *sql.DB, workItemID string, ttl time.Duration) {
	t.Helper()
	if _, err := db.ExecContext(ctx,
		`UPDATE fact_work_items SET lease_owner = 'inflight-fence-proof', claim_until = $2 WHERE work_item_id = $1`,
		workItemID, time.Now().UTC().Add(ttl)); err != nil {
		t.Fatalf("arm lease on %s: %v", workItemID, err)
	}
}

// seedActiveRelationshipGeneration publishes one active relationship
// generation for the refinalized pair, the row the fence protects.
func seedActiveRelationshipGeneration(t *testing.T, ctx context.Context, db *sql.DB, generationID, scopeID string) {
	t.Helper()
	if _, err := db.ExecContext(ctx,
		`INSERT INTO relationship_generations (generation_id, scope, status, created_at) VALUES ($1, $2, 'active', now())`,
		generationID, scopeID); err != nil {
		t.Fatalf("seed active relationship generation %s: %v", generationID, err)
	}
}

// relationshipGenerationStatus reads one generation's status for the
// retired-versus-held assertions.
func relationshipGenerationStatus(t *testing.T, ctx context.Context, db *sql.DB, generationID string) string {
	t.Helper()
	var status string
	if err := db.QueryRowContext(ctx,
		`SELECT status FROM relationship_generations WHERE generation_id = $1`,
		generationID).Scan(&status); err != nil {
		t.Fatalf("read relationship generation %s status: %v", generationID, err)
	}
	return status
}
