// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// TestCrossplaneRedriveDeadLetteredTargetIsNotPermanentlySuppressedLive is the
// hostile-read regression for the ledger-write-timing bug: the cross-scope
// redrive sweep enqueues a target's SATISFIED_BY intent via
// ReplayCrossplaneSatisfiedByMaterialization, which is ENQUEUE-ONLY -- it
// returns as soon as the work item is durably queued, strictly BEFORE the
// reducer handler runs. The original (rejected) design had the SWEEP write
// the ledger immediately after that enqueue call. If the intent later
// dead-lettered (a handler bug, or an infra outage exceeding the retry
// budget -- auto-retry-on-dead-letter is disabled by default,
// cmd/reducer/poison_liveness_wiring.go), that design would PERMANENTLY and
// SILENTLY mark the (target scope, XRD identity) pair satisfied, since the
// ledger's primary key is not generation-scoped and never expires --
// reopening the exact false-negative window #5476 exists to close, worse
// than the original bug because it is now silent and permanent. This proves
// the fix: because the sweep no longer writes the ledger (only the reducer
// handler does, and only after it actually commits an edge), a
// dead-lettered target is re-enqueued by every subsequent sweep for the same
// identity, never permanently suppressed.
func TestCrossplaneRedriveDeadLetteredTargetIsNotPermanentlySuppressedLive(t *testing.T) {
	dsn, schema := crossplaneRedriveProofSchema(t)
	db := crossplaneRedriveProofConn(t, dsn, schema)
	ctx := context.Background()
	now := time.Now().UTC()

	const (
		xrdScopeID    = "scope-xrd-deadletter"
		group         = "example.org"
		claimKind     = "XExampleClaim"
		targetScopeID = "scope-claim-deadletter"
		targetGenID   = "gen-claim-deadletter-001"
	)

	seedCrossplaneRedriveClaimScope(ctx, t, db, targetScopeID, targetGenID, group, claimKind, 1, now)

	reducerQueue := NewReducerQueue(SQLDB{DB: db}, "test-owner", time.Minute)
	sweeper := CrossplaneSatisfiedByRedriveSweeper{
		DB:       SQLQueryer{DB: db},
		State:    NewCrossplaneRedriveStateStore(SQLDB{DB: db}),
		Replayer: reducerQueue,
		Owner:    "projector",
	}

	// (1) First XRD generation activates. The sweep enqueues the target's
	// intent but does NOT write the ledger (enqueue-only, per the fix).
	const xrdGen1 = "gen-xrd-deadletter-001"
	seedCrossplaneRedriveXRD(ctx, t, db, xrdScopeID, xrdGen1, group, claimKind, now)
	first, err := sweeper.Sweep(ctx, xrdScopeID, xrdGen1)
	if err != nil {
		t.Fatalf("first sweep: %v", err)
	}
	if first.TargetsEnqueued != 1 {
		t.Fatalf("expected the first sweep to enqueue 1 target, got %d", first.TargetsEnqueued)
	}
	assertCrossplaneRedriveLedgerEntry(ctx, t, db, targetScopeID, group, claimKind, false)

	// (2) The intent's underlying work item dead-letters WITHOUT the handler
	// ever successfully committing an edge -- simulating a handler bug or an
	// infra outage that exhausts the retry budget (auto-retry-on-dead-letter
	// is disabled by default in production).
	forceCrossplaneRedriveWorkItemDeadLetter(ctx, t, db, targetScopeID, targetGenID)

	// (3) The XRD platform repo produces a SECOND generation carrying the
	// SAME (group, claim_kind) identity (e.g. an unrelated file edit). If the
	// ledger had been written at enqueue time, this second sweep would
	// wrongly skip the target forever. It must NOT: the target was never
	// actually satisfied.
	const xrdGen2 = "gen-xrd-deadletter-002"
	seedCrossplaneRedriveXRD(ctx, t, db, xrdScopeID, xrdGen2, group, claimKind, now.Add(time.Hour))
	second, err := sweeper.Sweep(ctx, xrdScopeID, xrdGen2)
	if err != nil {
		t.Fatalf("second sweep: %v", err)
	}
	if second.TargetsEnqueued != 1 {
		t.Fatalf("expected the dead-lettered target to be RE-enqueued (not permanently suppressed), got TargetsEnqueued=%d", second.TargetsEnqueued)
	}
	assertCrossplaneRedriveLedgerEntry(ctx, t, db, targetScopeID, group, claimKind, false)
}

// TestCrossplaneRedriveSuccessfulMaterializationWritesLedgerThenSkipsLive
// proves the correct half of the fix: when the reducer handler actually
// commits a SATISFIED_BY edge, IT (not the sweep) records the ledger entry,
// and a later sweep for the SAME (group, claim_kind) identity then correctly
// skips the already-satisfied target -- closing the original #5476 P1-b
// regression (a repeat XRD-repo sync must not re-enqueue an already-resolved
// target forever) the correct way.
func TestCrossplaneRedriveSuccessfulMaterializationWritesLedgerThenSkipsLive(t *testing.T) {
	dsn, schema := crossplaneRedriveProofSchema(t)
	db := crossplaneRedriveProofConn(t, dsn, schema)
	ctx := context.Background()
	now := time.Now().UTC()

	const (
		xrdScopeID    = "scope-xrd-satisfy-skip"
		group         = "example.org"
		claimKind     = "XExampleClaim"
		targetScopeID = "scope-claim-satisfy-skip"
		targetGenID   = "gen-claim-satisfy-skip-001"
	)

	seedCrossplaneRedriveClaimScope(ctx, t, db, targetScopeID, targetGenID, group, claimKind, 1, now)

	const xrdGen1 = "gen-xrd-satisfy-skip-001"
	seedCrossplaneRedriveXRD(ctx, t, db, xrdScopeID, xrdGen1, group, claimKind, now)

	// The target's own SATISFIED_BY materialization runs directly (as the
	// reducer would after the intent is claimed) and successfully commits an
	// edge -- the handler itself records the ledger entry after the edge
	// write succeeds.
	factStore := NewFactStore(SQLDB{DB: db})
	ledger := NewCrossplaneRedriveTargetLedgerStore(SQLDB{DB: db})
	handler := reducer.CrossplaneSatisfiedByMaterializationHandler{
		FactLoader:          factStore,
		EdgeWriter:          &crossplaneRedriveSpyEdgeWriter{},
		RedriveTargetLedger: ledger,
	}
	intent := reducer.Intent{
		IntentID:     "intent-satisfy-skip-1",
		ScopeID:      targetScopeID,
		GenerationID: targetGenID,
		Domain:       reducer.DomainCrossplaneSatisfiedByMaterialization,
		AttemptCount: 1,
	}
	if _, err := handler.Handle(ctx, intent); err != nil {
		t.Fatalf("handle intent: %v", err)
	}
	spy := handler.EdgeWriter.(*crossplaneRedriveSpyEdgeWriter)
	if len(spy.written) != 1 {
		t.Fatalf("expected the handler to resolve exactly 1 edge, got %d: %v", len(spy.written), spy.written)
	}
	assertCrossplaneRedriveLedgerEntry(ctx, t, db, targetScopeID, group, claimKind, true)

	// A later XRD generation with the SAME identity must now skip the
	// already-satisfied target.
	reducerQueue := NewReducerQueue(SQLDB{DB: db}, "test-owner", time.Minute)
	sweeper := CrossplaneSatisfiedByRedriveSweeper{
		DB:       SQLQueryer{DB: db},
		State:    NewCrossplaneRedriveStateStore(SQLDB{DB: db}),
		Replayer: reducerQueue,
		Owner:    "projector",
	}
	const xrdGen2 = "gen-xrd-satisfy-skip-002"
	seedCrossplaneRedriveXRD(ctx, t, db, xrdScopeID, xrdGen2, group, claimKind, now.Add(time.Hour))
	result, err := sweeper.Sweep(ctx, xrdScopeID, xrdGen2)
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if result.TargetsEnqueued != 0 {
		t.Fatalf("expected the already-satisfied target to be skipped, got TargetsEnqueued=%d", result.TargetsEnqueued)
	}
}

// forceCrossplaneRedriveWorkItemDeadLetter directly transitions the target
// scope's SATISFIED_BY reducer work item to 'dead_letter', simulating an
// exhausted retry budget without needing to drive the real claim/fail loop
// through every retry.
func forceCrossplaneRedriveWorkItemDeadLetter(ctx context.Context, t *testing.T, db *sql.DB, scopeID, generationID string) {
	t.Helper()
	result, err := db.ExecContext(ctx, `
		UPDATE fact_work_items
		SET status = 'dead_letter',
		    lease_owner = NULL,
		    claim_until = NULL,
		    visible_at = NULL,
		    failure_class = 'test_injected_dead_letter'
		WHERE scope_id = $1 AND generation_id = $2
		  AND stage = 'reducer' AND domain = 'crossplane_satisfied_by_materialization'
	`, scopeID, generationID)
	if err != nil {
		t.Fatalf("force dead_letter: %v", err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		t.Fatalf("force dead_letter rows affected: %v", err)
	}
	if rowsAffected == 0 {
		t.Fatalf("expected an existing work item for scope %q generation %q to force into dead_letter", scopeID, generationID)
	}
}
