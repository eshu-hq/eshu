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

// Expired-holder reclaim proofs for the #4137 follow-up. The live-lease unique
// index (migration 005) admits at most one claimed/running row per conflict key;
// because a partial index cannot use now() to mean "live", an EXPIRED holder
// still occupies the key. The claim path must therefore reclaim that holder
// before an older pending sibling — otherwise the sibling claim hits the unique
// index (23505), returns no work, and repeats forever, wedging the key. These
// proofs are skipped unless a DSN is provided, like the other contention gates.

// seedReducerExpiredHolderWithOlderPendingSibling claims a holder on a conflict
// key (so it occupies the live-lease unique index) and then enqueues an older
// pending sibling on the same key. It returns the schema-bound db. The holder's
// lease is expired relative to reducerExpiredHolderReclaimAt, so a correct claim
// must reclaim the holder rather than start the older pending sibling.
func seedReducerExpiredHolderWithOlderPendingSibling(t *testing.T, ctx context.Context, dsn string) *sql.DB {
	t.Helper()
	db, _ := openReducerFairnessDBWithSchema(t, ctx, dsn)
	base := time.Date(2026, time.June, 28, 6, 0, 0, 0, time.UTC)
	seedReducerFairnessScope(t, ctx, db, "scope-holder", base)
	insertReducerFairnessWorkItem(t, ctx, db, reducerFairnessWorkItem{
		workItemID:     "holder",
		scopeID:        "scope-holder",
		conflictDomain: reducerConflictDomainCodeGraph,
		conflictKey:    "holder-key",
		generationID:   "gen-fair",
		domain:         string(contentionGateClaimDomain),
		updatedAt:      base,
	})
	holderQ := ReducerQueue{
		db:            SQLDB{DB: db},
		LeaseOwner:    "holder-worker",
		LeaseDuration: time.Minute,
		Now:           func() time.Time { return base.Add(time.Hour) },
		ClaimDomains:  []reducer.Domain{contentionGateClaimDomain},
	}
	if intent, ok, err := holderQ.Claim(ctx); err != nil || !ok || intent.IntentID != "holder" {
		t.Fatalf("seed holder claim: intent=%q ok=%v err=%v", intent.IntentID, ok, err)
	}
	// Older pending sibling (updated_at before the holder's claim time).
	insertReducerFairnessWorkItem(t, ctx, db, reducerFairnessWorkItem{
		workItemID:     "sibling",
		scopeID:        "scope-holder",
		conflictDomain: reducerConflictDomainCodeGraph,
		conflictKey:    "holder-key",
		generationID:   "gen-fair",
		domain:         string(contentionGateClaimDomain),
		updatedAt:      base.Add(30 * time.Minute),
	})
	return db
}

// reducerExpiredHolderReclaimAt is the wall time after the seeded holder's lease
// has expired, used to drive the reclaiming claim deterministically.
var reducerExpiredHolderReclaimAt = time.Date(2026, time.June, 28, 8, 0, 0, 0, time.UTC)

// TestReducerClaimReclaimsExpiredHolderBeforeOlderPendingSibling proves the
// single claim reclaims an expired holder rather than starting an older pending
// sibling on the same conflict key (#4137 follow-up). Picking the sibling would
// hit the unique index (23505) and wedge the key forever.
func TestReducerClaimReclaimsExpiredHolderBeforeOlderPendingSibling(t *testing.T) {
	dsn := reducerDomainFairnessDSN()
	if dsn == "" {
		t.Skip("set ESHU_REDUCER_FAIRNESS_PROOF_DSN or ESHU_POSTGRES_DSN to run the contention gate")
	}
	ctx := context.Background()
	db := seedReducerExpiredHolderWithOlderPendingSibling(t, ctx, dsn)

	reclaimQ := ReducerQueue{
		db:            SQLDB{DB: db},
		LeaseOwner:    "reclaim-worker",
		LeaseDuration: time.Minute,
		Now:           func() time.Time { return reducerExpiredHolderReclaimAt },
		ClaimDomains:  []reducer.Domain{contentionGateClaimDomain},
	}
	intent, ok, err := reclaimQ.Claim(ctx)
	if err != nil {
		t.Fatalf("reclaim Claim() error = %v", err)
	}
	if !ok {
		t.Fatal("reclaim Claim() ok = false; the expired holder was not reclaimed (key wedged behind the older pending sibling)")
	}
	if intent.IntentID != "holder" {
		t.Fatalf("reclaim claimed %q, want the expired holder %q", intent.IntentID, "holder")
	}
}

// TestReducerClaimBatchReclaimsExpiredHolderBeforeOlderPendingSibling proves the
// same for the batch claim path: the representative prefers the live/expired
// holder over an older pending sibling so the batch reclaims it rather than
// wedging on a 23505.
func TestReducerClaimBatchReclaimsExpiredHolderBeforeOlderPendingSibling(t *testing.T) {
	dsn := reducerDomainFairnessDSN()
	if dsn == "" {
		t.Skip("set ESHU_REDUCER_FAIRNESS_PROOF_DSN or ESHU_POSTGRES_DSN to run the contention gate")
	}
	ctx := context.Background()
	db := seedReducerExpiredHolderWithOlderPendingSibling(t, ctx, dsn)

	reclaimQ := ReducerQueue{
		db:            SQLDB{DB: db},
		LeaseOwner:    "reclaim-batch-worker",
		LeaseDuration: time.Minute,
		Now:           func() time.Time { return reducerExpiredHolderReclaimAt },
		ClaimDomains:  []reducer.Domain{contentionGateClaimDomain},
	}
	intents, err := reclaimQ.ClaimBatch(ctx, 8)
	if err != nil {
		t.Fatalf("reclaim ClaimBatch() error = %v", err)
	}
	if len(intents) != 1 || intents[0].IntentID != "holder" {
		got := make([]string, 0, len(intents))
		for _, in := range intents {
			got = append(got, in.IntentID)
		}
		t.Fatalf("reclaim ClaimBatch() = %v, want exactly [holder] (reclaim the expired holder, not the older pending sibling)", got)
	}
}
