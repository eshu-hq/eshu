// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"context"
	"database/sql"
	"reflect"
	"slices"
	"testing"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
)

// The #10972 late-claim proofs. The fan-out reads the prefilter once, then
// walks the kept holders; a holder can be claimed by a real worker between the
// two. Every state change here goes through the shipped reopen and a real
// ReducerQueue.Claim, never a direct status UPDATE: a succeeded row cannot be
// claimed directly (the claim selects pending, retrying, claimed, and running
// only), so the late claim of a succeeded holder takes a second fan-out's
// reopen first.

// widenCVEC is a third change, from a second origin.
const widenCVEC = "CVE-2026-7003"

var widenOriginC = supplychaincore.FanOutOrigin{
	SourceScopeID: "scope:source-c", SourceGenerationID: "gen-c", SourceIntentID: "intent-c",
	CVEIDs: []string{widenCVEC},
}

// lateClaim claims the only claimable item and fails unless it is id.
func lateClaim(t *testing.T, ctx context.Context, db *sql.DB, id string) reducercontract.Intent {
	t.Helper()
	intent, ok, err := fanOutLiveQueue(db).Claim(ctx)
	if err != nil || !ok || intent.IntentID != id {
		t.Fatalf("claim = %q ok=%v err=%v, want %s", intent.IntentID, ok, err, id)
	}
	return intent
}

// TestFindQueuedHolderScopesKeepsASucceededHolderAtTheReadAndTheReopenFlagsItAfterARealClaimLive
// is the F2 scenario built through the real path: a holder succeeded at the
// prefilter read is kept; before the loop reaches it, a second fan-out reopens
// it and a worker claims it. The loop's reopen then flags the running pass and
// merges its change into the stamp the second fan-out wrote, so the one rerun
// derives both.
func TestFindQueuedHolderScopesKeepsASucceededHolderAtTheReadAndTheReopenFlagsItAfterARealClaimLive(t *testing.T) {
	ctx, db := fanOutLiveDB(t)
	store := fanOutLiveStore(db)
	fanOutSeedScope(t, ctx, db, "h-late-succeeded", "g1")
	id := fanOutSeedItem(t, ctx, db, "h-late-succeeded", "g1")
	if _, err := db.ExecContext(ctx, `UPDATE fact_work_items SET status = 'succeeded' WHERE work_item_id = $1`, id); err != nil {
		t.Fatal(err)
	}

	dropped, err := store.FindQueuedHolderScopes(ctx, []string{"h-late-succeeded"}, widenOriginB.CVEIDs)
	if err != nil || len(dropped) != 0 {
		t.Fatalf("prefilter = %v err=%v, want the succeeded holder kept at the read", dropped, err)
	}
	// Between the read and the loop: another fan-out reopens the holder, and a
	// worker claims it.
	if reopen, err := store.ReopenHolderDetailed(ctx, "h-late-succeeded", widenOriginC); err != nil || reopen.Outcome != supplychaincore.FanOutReopened {
		t.Fatalf("second origin reopen = %+v err=%v, want reopened", reopen, err)
	}
	claimed := lateClaim(t, ctx, db, id)
	if got := supplychaincore.ScopedPassCVEIDs(claimed); !reflect.DeepEqual(got, []string{widenCVEC}) {
		t.Fatalf("claimed pass scope = %v, want [%s]", got, widenCVEC)
	}

	reopen, err := store.ReopenHolderDetailed(ctx, "h-late-succeeded", widenOriginB)
	if err != nil || reopen.Outcome != supplychaincore.FanOutReplayFlagged {
		t.Fatalf("loop reopen = %+v err=%v, want replay_flagged", reopen, err)
	}
	row := widenRead(t, ctx, db, id)
	if want := `["` + widenCVEB + `", "` + widenCVEC + `"]`; row.cves != want || row.status != "claimed" || !row.flag {
		t.Fatalf("row = %+v, want claimed, flagged, stamp %s", row, want)
	}
}

// TestFindQueuedHolderScopesDropsACoveredHolderThatIsClaimedAfterTheReadLive:
// a pending holder whose stamp already names the change is dropped at the read,
// then claimed before the loop. Nothing in the loop touches it, and that is
// safe: the read ran after the source's writer commit, so the claim loads the
// committed source facts, and the stamp only grows, so the pass it runs is
// scoped on a superset of the change (or full).
func TestFindQueuedHolderScopesDropsACoveredHolderThatIsClaimedAfterTheReadLive(t *testing.T) {
	ctx, db := fanOutLiveDB(t)
	store := fanOutLiveStore(db)
	fanOutSeedScope(t, ctx, db, "h-late-covered", "g1")
	id := fanOutSeedItem(t, ctx, db, "h-late-covered", "g1")
	both := widenOriginA
	both.CVEIDs = []string{widenCVEA, widenCVEB}
	widenSetStamp(t, ctx, db, id, widenStamp(t, both))
	// A neighbour stamped for another change is kept, so the loop runs a
	// statement; the covered holder is older, so the worker claims it first.
	neighbour := widenSeedPending(t, ctx, db, "h-late-neighbour")
	if _, err := db.ExecContext(ctx, `UPDATE fact_work_items SET updated_at = updated_at - interval '1 minute' WHERE work_item_id = $1`, id); err != nil {
		t.Fatal(err)
	}

	holders := []string{"h-late-covered", "h-late-neighbour"}
	dropped, err := store.FindQueuedHolderScopes(ctx, holders, widenOriginB.CVEIDs)
	if err != nil || !reflect.DeepEqual(dropped, []string{"h-late-covered"}) {
		t.Fatalf("prefilter = %v err=%v, want only the covered holder dropped", dropped, err)
	}
	claimed := lateClaim(t, ctx, db, id)
	before := widenRead(t, ctx, db, id)
	// The loop walks the kept holders only.
	for _, holder := range holders {
		if slices.Contains(dropped, holder) {
			continue
		}
		if reopen, err := store.ReopenHolderDetailed(ctx, holder, widenOriginB); err != nil || string(reopen.Outcome) != "stamp_widened" {
			t.Fatalf("loop reopen %s = %+v err=%v, want stamp_widened", holder, reopen, err)
		}
	}
	if after := widenRead(t, ctx, db, id); after.version != before.version {
		t.Fatalf("dropped holder changed version %s -> %s", before.version, after.version)
	}
	if got := widenRead(t, ctx, db, neighbour); got.cves != `["`+widenCVEA+`", "`+widenCVEB+`"]` {
		t.Fatalf("kept neighbour stamp = %s, want it widened", got.cves)
	}
	scope := supplychaincore.ScopedPassCVEIDs(claimed)
	if scope != nil && !slices.Contains(scope, widenCVEB) {
		t.Fatalf("claimed pass scope = %v, want full or a superset of [%s]", scope, widenCVEB)
	}
}

// TestFindQueuedHolderScopesKeepsAnUncoveredHolderThatIsClaimedAfterTheReadLive:
// a pending holder stamped for another change is kept at the read; claimed
// before the loop, it is flagged by the loop's reopen, which merges the change
// into its stamp.
func TestFindQueuedHolderScopesKeepsAnUncoveredHolderThatIsClaimedAfterTheReadLive(t *testing.T) {
	ctx, db := fanOutLiveDB(t)
	store := fanOutLiveStore(db)
	id := widenSeedPending(t, ctx, db, "h-late-uncovered")

	dropped, err := store.FindQueuedHolderScopes(ctx, []string{"h-late-uncovered"}, widenOriginB.CVEIDs)
	if err != nil || len(dropped) != 0 {
		t.Fatalf("prefilter = %v err=%v, want the uncovered holder kept", dropped, err)
	}
	lateClaim(t, ctx, db, id)
	reopen, err := store.ReopenHolderDetailed(ctx, "h-late-uncovered", widenOriginB)
	if err != nil || reopen.Outcome != supplychaincore.FanOutReplayFlagged {
		t.Fatalf("loop reopen = %+v err=%v, want replay_flagged", reopen, err)
	}
	row := widenRead(t, ctx, db, id)
	if want := `["` + widenCVEA + `", "` + widenCVEB + `"]`; row.cves != want || row.status != "claimed" || !row.flag {
		t.Fatalf("row = %+v, want claimed, flagged, stamp %s", row, want)
	}
}
