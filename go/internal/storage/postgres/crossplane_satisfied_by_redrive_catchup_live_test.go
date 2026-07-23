// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"
)

// crossplaneRedriveFailingReplayer wraps a real CrossplaneRedriveIntentReplayer
// and injects one failure at call number failAfter (1-indexed), simulating a
// transient DB blip partway through a page's fan-out (issue #5476 P1-a).
// Every other call passes through to the real replayer so the targets before
// and after the injected failure are genuinely enqueued/reopened.
type crossplaneRedriveFailingReplayer struct {
	real      CrossplaneRedriveIntentReplayer
	failAfter int
	calls     int
}

func (f *crossplaneRedriveFailingReplayer) ReplayCrossplaneSatisfiedByMaterialization(
	ctx context.Context, targetScopeID, targetGenerationID string,
) (bool, error) {
	f.calls++
	if f.calls == f.failAfter {
		return false, errors.New("injected transient failure")
	}
	return f.real.ReplayCrossplaneSatisfiedByMaterialization(ctx, targetScopeID, targetGenerationID)
}

// TestCrossplaneRedriveSweepMidFanOutFailureRecoveredByCatchUpLive is the
// issue #5476 P1-a regression: a Sweep call that fails partway through the
// paged fan-out (a transient DB blip, not even a crash) leaves the
// crossplane_satisfied_by_redrive_state row 'claimed' with an expiring
// lease. Nothing else revisits that row except a later Sweep/SweepBatch
// attempt for the SAME generation. This proves SweepBatch -- the
// startup/periodic catch-up path cmd/projector's runCrossplaneRedriveCatchUpLoop
// calls -- reclaims the row once its lease expires and completes the
// remaining targets the failed attempt never reached.
func TestCrossplaneRedriveSweepMidFanOutFailureRecoveredByCatchUpLive(t *testing.T) {
	dsn, schema := crossplaneRedriveProofSchema(t)
	db := crossplaneRedriveProofConn(t, dsn, schema)
	ctx := context.Background()
	now := time.Now().UTC()

	const (
		xrdScopeID      = "scope-xrd-catchup"
		xrdGenerationID = "gen-xrd-catchup-001"
		group           = "example.org"
		claimKind       = "XExampleClaim"
	)
	// Three target scopes, alphabetically ordered so the deterministic
	// scope_id ASC page ordering is predictable: target-a is replayed
	// successfully, target-b's replay call is the injected failure, and
	// target-c is never reached by the failed attempt.
	targets := []string{"scope-claim-catchup-a", "scope-claim-catchup-b", "scope-claim-catchup-c"}

	seedCrossplaneRedriveXRD(ctx, t, db, xrdScopeID, xrdGenerationID, group, claimKind, now)
	for i, targetScopeID := range targets {
		generationID := targetScopeID + "-gen-001"
		seedCrossplaneRedriveClaimScope(ctx, t, db, targetScopeID, generationID, group, claimKind, 1, now.Add(time.Duration(i)*time.Second))
	}

	realReducerQueue := NewReducerQueue(SQLDB{DB: db}, "test-owner", time.Minute)
	failingReplayer := &crossplaneRedriveFailingReplayer{real: realReducerQueue, failAfter: 2}

	shortLease := 300 * time.Millisecond
	failingSweeper := CrossplaneSatisfiedByRedriveSweeper{
		DB:            SQLQueryer{DB: db},
		State:         NewCrossplaneRedriveStateStore(SQLDB{DB: db}),
		Replayer:      failingReplayer,
		Owner:         "projector",
		LeaseDuration: shortLease,
	}

	// The live-trigger Sweep call fails partway through the fan-out.
	result, err := failingSweeper.Sweep(ctx, xrdScopeID, xrdGenerationID)
	if err == nil {
		t.Fatalf("expected the injected failure to surface as a Sweep error, got result %+v", result)
	}

	// The row must be left 'claimed', not 'completed' and not rolled back to
	// 'queued' -- a crash/error must not silently discard the in-progress
	// claim.
	assertCrossplaneRedriveStateStatus(ctx, t, db, xrdScopeID, xrdGenerationID, "claimed")

	// Only target-a succeeded before the injected failure on call 2
	// (target-b); target-c was never attempted. None of the three carry a
	// ledger entry: enqueuing an intent (this test never runs the actual
	// reducer handler) must never write the ledger -- only a handler that
	// actually commits an edge does (see crossplane_satisfied_by_redrive_ledger_live_test.go).
	assertCrossplaneRedriveTargetPending(ctx, t, db, targets[0], targets[0]+"-gen-001", true)
	assertCrossplaneRedriveLedgerEntry(ctx, t, db, targets[0], group, claimKind, false)
	assertCrossplaneRedriveLedgerEntry(ctx, t, db, targets[1], group, claimKind, false)
	assertCrossplaneRedriveLedgerEntry(ctx, t, db, targets[2], group, claimKind, false)

	// Wait past the short lease, then run the catch-up path with a sweeper
	// wired to the REAL (non-failing) replayer -- exactly what
	// runCrossplaneRedriveCatchUpLoop does periodically in production.
	time.Sleep(shortLease + 200*time.Millisecond)

	catchUpSweeper := CrossplaneSatisfiedByRedriveSweeper{
		DB:            SQLQueryer{DB: db},
		State:         NewCrossplaneRedriveStateStore(SQLDB{DB: db}),
		Replayer:      realReducerQueue,
		Owner:         "projector",
		LeaseDuration: time.Minute,
	}
	results, err := catchUpSweeper.SweepBatch(ctx, 10)
	if err != nil {
		t.Fatalf("SweepBatch: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected SweepBatch to reclaim exactly 1 generation, got %d: %+v", len(results), results)
	}
	if results[0].Outcome != crossplaneRedriveOutcomeCompleted {
		t.Fatalf("expected the reclaimed sweep to complete, got outcome %q", results[0].Outcome)
	}

	// All three targets are now enqueued/reopened and the state row is
	// completed. None carry a ledger entry -- this test never runs the real
	// reducer handler, so the ledger (correctly) stays empty throughout; see
	// crossplane_satisfied_by_redrive_ledger_live_test.go for the ledger's
	// own write-timing proof.
	assertCrossplaneRedriveStateStatus(ctx, t, db, xrdScopeID, xrdGenerationID, "completed")
	for _, targetScopeID := range targets {
		assertCrossplaneRedriveLedgerEntry(ctx, t, db, targetScopeID, group, claimKind, false)
		assertCrossplaneRedriveTargetPending(ctx, t, db, targetScopeID, targetScopeID+"-gen-001", true)
	}
}

func assertCrossplaneRedriveStateStatus(ctx context.Context, t *testing.T, db *sql.DB, xrdScopeID, xrdGenerationID, expectedStatus string) {
	t.Helper()
	rows, err := db.QueryContext(ctx, `
		SELECT status FROM crossplane_satisfied_by_redrive_state
		WHERE xrd_scope_id = $1 AND xrd_generation_id = $2
	`, xrdScopeID, xrdGenerationID)
	if err != nil {
		t.Fatalf("query crossplane_satisfied_by_redrive_state: %v", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		t.Fatalf("expected a crossplane_satisfied_by_redrive_state row for %s/%s", xrdScopeID, xrdGenerationID)
	}
	var status string
	if err := rows.Scan(&status); err != nil {
		t.Fatalf("scan status: %v", err)
	}
	if status != expectedStatus {
		t.Fatalf("expected status %q, got %q", expectedStatus, status)
	}
}

func assertCrossplaneRedriveLedgerEntry(ctx context.Context, t *testing.T, db *sql.DB, targetScopeID, group, claimKind string, expectExists bool) {
	t.Helper()
	rows, err := db.QueryContext(ctx, `
		SELECT 1 FROM crossplane_satisfied_by_redrive_target_ledger
		WHERE target_scope_id = $1 AND xrd_group = $2 AND xrd_claim_kind = $3
	`, targetScopeID, group, claimKind)
	if err != nil {
		t.Fatalf("query crossplane_satisfied_by_redrive_target_ledger: %v", err)
	}
	defer func() { _ = rows.Close() }()
	exists := rows.Next()
	if exists != expectExists {
		t.Fatalf("ledger entry for %s (%s/%s): expected exists=%v, got %v", targetScopeID, group, claimKind, expectExists, exists)
	}
}

func assertCrossplaneRedriveTargetPending(ctx context.Context, t *testing.T, db *sql.DB, scopeID, generationID string, expectExists bool) {
	t.Helper()
	rows, err := db.QueryContext(ctx, `
		SELECT 1 FROM fact_work_items
		WHERE scope_id = $1 AND generation_id = $2
		  AND stage = 'reducer' AND domain = 'crossplane_satisfied_by_materialization'
	`, scopeID, generationID)
	if err != nil {
		t.Fatalf("query fact_work_items: %v", err)
	}
	defer func() { _ = rows.Close() }()
	exists := rows.Next()
	if exists != expectExists {
		t.Fatalf("work item for %s/%s: expected exists=%v, got %v", scopeID, generationID, expectExists, exists)
	}
}
