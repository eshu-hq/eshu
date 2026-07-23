// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"testing"
	"time"
)

// TestCrossplaneRedriveSkipsAlreadySatisfiedTargetOnRepeatXRDSyncLive is the
// issue #5476 P1-b regression: the already-satisfied fence must be stable
// across repeated resyncs of the XRD platform repo that change nothing about
// the XRD's own (group, claim_kind) identity. A first review round shipped a
// fence anchored on the XRD fact's own observed_at
// (fact_work_items.updated_at > xrd_fact.observed_at), which looked correct
// but was silently ineffective: observed_at is an ingestion-time value that
// strictly advances on EVERY resync (git_content_fact_envelopes.go /
// git_source_processing.go thread it uniformly per generation), so the fence
// was false for every previously-satisfied target on every subsequent sync,
// and the sweep re-enqueued every matching Claim scope again and again
// forever. This proves the ledger-based replacement actually skips the
// second sweep.
func TestCrossplaneRedriveSkipsAlreadySatisfiedTargetOnRepeatXRDSyncLive(t *testing.T) {
	dsn, schema := crossplaneRedriveProofSchema(t)
	db := crossplaneRedriveProofConn(t, dsn, schema)
	ctx := context.Background()
	now := time.Now().UTC()

	const (
		xrdScopeID    = "scope-xrd-repeat-sync"
		group         = "example.org"
		claimKind     = "XExampleClaim"
		targetScopeID = "scope-claim-repeat-sync"
		targetGenID   = "gen-claim-repeat-sync-001"
	)

	seedCrossplaneRedriveClaimScope(ctx, t, db, targetScopeID, targetGenID, group, claimKind, 1, now)

	reducerQueue := NewReducerQueue(SQLDB{DB: db}, "test-owner", time.Minute)
	sweeper := CrossplaneSatisfiedByRedriveSweeper{
		DB:           SQLQueryer{DB: db},
		State:        NewCrossplaneRedriveStateStore(SQLDB{DB: db}),
		TargetLedger: NewCrossplaneRedriveTargetLedgerStore(SQLDB{DB: db}),
		Replayer:     reducerQueue,
		Owner:        "projector",
	}

	// (1) First XRD generation activates with (group, claimKind). Sweep
	// enqueues the target.
	const xrdGen1 = "gen-xrd-repeat-sync-001"
	seedCrossplaneRedriveXRD(ctx, t, db, xrdScopeID, xrdGen1, group, claimKind, now)

	first, err := sweeper.Sweep(ctx, xrdScopeID, xrdGen1)
	if err != nil {
		t.Fatalf("first sweep: %v", err)
	}
	if first.TargetsEnqueued != 1 {
		t.Fatalf("expected the first sweep to enqueue 1 target, got %d", first.TargetsEnqueued)
	}

	// (2) The XRD platform repo resyncs -- e.g. an unrelated file edit --
	// producing a NEW generation whose observed_at is strictly later, but
	// carrying the EXACT SAME XRD (group, claim_kind) identity. This is
	// exactly the "trivial platform-repo edit" scenario the already-satisfied
	// fence must not re-enqueue on.
	const xrdGen2 = "gen-xrd-repeat-sync-002"
	seedCrossplaneRedriveXRD(ctx, t, db, xrdScopeID, xrdGen2, group, claimKind, now.Add(time.Hour))

	second, err := sweeper.Sweep(ctx, xrdScopeID, xrdGen2)
	if err != nil {
		t.Fatalf("second sweep: %v", err)
	}
	if second.TargetsEnqueued != 0 {
		t.Fatalf("expected the second sweep (same identity, later XRD generation) to enqueue 0 targets (already-satisfied fence), got %d", second.TargetsEnqueued)
	}
	if second.Outcome != crossplaneRedriveOutcomeCompleted {
		t.Fatalf("expected the second sweep to still complete (a no-op fan-out is not an error), got outcome %q", second.Outcome)
	}

	// The second XRD generation's OWN state row completed independently of
	// the first's -- the ledger fence, not the per-generation state row,
	// caused the zero re-enqueue.
	assertCrossplaneRedriveStateStatus(ctx, t, db, xrdScopeID, xrdGen1, "completed")
	assertCrossplaneRedriveStateStatus(ctx, t, db, xrdScopeID, xrdGen2, "completed")
}
