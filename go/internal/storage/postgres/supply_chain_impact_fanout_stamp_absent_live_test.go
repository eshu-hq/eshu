// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"context"
	"database/sql"
	"reflect"
	"slices"
	"testing"
	"time"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// The arbiter's Amendment 1 for #10972: an absent stored stamp (key missing,
// JSON null, or not an object) on an in-flight row is not an owed set. An
// unflagged one takes the incoming stamp as is; a flagged one is never
// written, because its rerun is a genuine full pass that a stamp would narrow.
// The rule lives in fanOutStampAbsentSQL.

// absentClaimed seeds one pending item, sets its stored stamp (raw jsonb, or
// "" to leave the key missing), claims it for real, and optionally turns the
// replay flag on, the way a non-fan-out flagger does.
func absentClaimed(t *testing.T, ctx context.Context, db *sql.DB, scope, stamp string, flag bool) (string, reducercontract.Intent) {
	t.Helper()
	fanOutSeedScope(t, ctx, db, scope, "g1")
	id := fanOutSeedItem(t, ctx, db, scope, "g1")
	if stamp != "" {
		if _, err := db.ExecContext(ctx, `UPDATE fact_work_items SET payload = jsonb_set(payload, '{fanout_origin}', $2::jsonb, TRUE) WHERE work_item_id = $1`, id, stamp); err != nil {
			t.Fatal(err)
		}
	}
	intent := lateClaim(t, ctx, db, id)
	if flag {
		if _, err := db.ExecContext(ctx, `UPDATE fact_work_items SET cross_scope_replay_required = TRUE WHERE work_item_id = $1`, id); err != nil {
			t.Fatal(err)
		}
	}
	return id, intent
}

// absentStampPresent reports whether the row's payload has a fanout_origin key.
func absentStampPresent(t *testing.T, ctx context.Context, db *sql.DB, id string) bool {
	t.Helper()
	var present bool
	if err := db.QueryRowContext(ctx, `SELECT payload ? 'fanout_origin' FROM fact_work_items WHERE work_item_id = $1`, id).Scan(&present); err != nil {
		t.Fatal(err)
	}
	return present
}

// TestReopenStampsAnUnstampedInFlightHolderWithTheIncomingSetOnlyLive: a
// claimed, unflagged holder with no stamp (missing, then JSON null) is flagged
// and stamped with exactly the incoming set, never the sentinel or a set-less
// stamp; after its own ack the flag returns it to pending, and its rerun runs
// scoped to that set.
func TestReopenStampsAnUnstampedInFlightHolderWithTheIncomingSetOnlyLive(t *testing.T) {
	for _, tc := range []struct{ name, stamp string }{{"missing", ""}, {"json null", "null"}} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, db := fanOutLiveDB(t)
			id, intent := absentClaimed(t, ctx, db, "h-absent-unflagged", tc.stamp, false)
			reopen, err := fanOutLiveStore(db).ReopenHolderDetailed(ctx, "h-absent-unflagged", widenOriginB)
			if err != nil || reopen.Outcome != supplychaincore.FanOutReplayFlagged {
				t.Fatalf("reopen = %+v err=%v, want replay_flagged", reopen, err)
			}
			row := widenRead(t, ctx, db, id)
			if want := `["` + widenCVEB + `"]`; !row.flag || row.cves != want || row.scopeID != widenOriginB.SourceScopeID {
				t.Fatalf("row = %+v, want flagged with exactly %s from %s", row, want, widenOriginB.SourceScopeID)
			}
			queue := fanOutLiveQueue(db)
			if err := queue.Ack(ctx, intent, reducercontract.Result{}); err != nil {
				t.Fatalf("ack: %v", err)
			}
			rerun := lateClaim(t, ctx, db, id)
			if got := supplychaincore.ScopedPassCVEIDs(rerun); !reflect.DeepEqual(got, []string{widenCVEB}) {
				t.Fatalf("rerun scope = %v (attempt %d), want [%s]", got, rerun.AttemptCount, widenCVEB)
			}
		})
	}
}

// TestReopenLeavesAFlaggedUnstampedInFlightHolderUntouchedLive: a claimed
// holder whose flag is already on and that has no stamp is not written at all:
// same row version, still no fanout_origin, outcome pending_noop. Writing the
// incoming stamp there would narrow its genuine full rerun to a scoped one hop.
func TestReopenLeavesAFlaggedUnstampedInFlightHolderUntouchedLive(t *testing.T) {
	for _, tc := range []struct{ name, stamp string }{{"missing", ""}, {"json null", "null"}} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, db := fanOutLiveDB(t)
			id, _ := absentClaimed(t, ctx, db, "h-absent-flagged", tc.stamp, true)
			before := widenRead(t, ctx, db, id)
			reopen, err := fanOutLiveStore(db).ReopenHolderDetailed(ctx, "h-absent-flagged", widenOriginB)
			if err != nil || reopen.Outcome != supplychaincore.FanOutPendingNoop {
				t.Fatalf("reopen = %+v err=%v, want pending_noop", reopen, err)
			}
			after := widenRead(t, ctx, db, id)
			if after.version != before.version || !after.flag {
				t.Fatalf("row version %s -> %s flag=%v: a flagged unstamped holder was written", before.version, after.version, after.flag)
			}
			if present := absentStampPresent(t, ctx, db, id); present != (tc.stamp != "") || after.cves != "" {
				t.Fatalf("fanout_origin present=%v cves=%q, want the stored stamp left as it was", present, after.cves)
			}
		})
	}
}

// TestReopenKeepsAnUnboundedStoredStampUnboundedLive: a claimed holder whose
// stored stamp is set-less or the sentinel stays unbounded after a reopen for
// {B}, with the flag off (flagged and merged) and on (left alone).
func TestReopenKeepsAnUnboundedStoredStampUnboundedLive(t *testing.T) {
	cases := []struct {
		name, stamp string
		flag        bool
		want        supplychaincore.FanOutOutcome
	}{
		{"set-less, flag off", `{"scope_id": "scope:a"}`, false, supplychaincore.FanOutReplayFlagged},
		{"set-less, flag on", `{"scope_id": "scope:a"}`, true, supplychaincore.FanOutPendingNoop},
		{"sentinel, flag off", `{"scope_id": "scope:a", "cves": ["*"]}`, false, supplychaincore.FanOutReplayFlagged},
		{"sentinel, flag on", `{"scope_id": "scope:a", "cves": ["*"]}`, true, supplychaincore.FanOutPendingNoop},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, db := fanOutLiveDB(t)
			id, _ := absentClaimed(t, ctx, db, "h-unbounded", tc.stamp, tc.flag)
			reopen, err := fanOutLiveStore(db).ReopenHolderDetailed(ctx, "h-unbounded", widenOriginB)
			if err != nil || reopen.Outcome != tc.want {
				t.Fatalf("reopen = %+v err=%v, want %s", reopen, err, tc.want)
			}
			cves, present := fanOutStampCVEs(t, ctx, db, id)
			if present && !slices.Contains(cves, supplychaincore.FanOutStampSentinel) && len(cves) > 0 {
				t.Fatalf("stored cves = %v after the reopen, want the stamp still unbounded (set-less or the sentinel)", cves)
			}
			if row := widenRead(t, ctx, db, id); !row.flag {
				t.Fatalf("row = %+v, want the replay flag on", row)
			}
		})
	}
}

// TestReopenWidensAPendingHolderToSetLessInTheFenceTransactionLive pins the
// commit-skew fence path (ReopenSupplyChainImpactHolderInTransaction, a
// set-less origin): a pending holder stamped {A} is widened to the set-less
// stamp, so its next pass runs full, with status, attempt_count, updated_at,
// and visible_at unchanged; a pending unstamped holder is covered and keeps
// its row version.
func TestReopenWidensAPendingHolderToSetLessInTheFenceTransactionLive(t *testing.T) {
	ctx, db := fanOutLiveDB(t)
	stamped := widenSeedPending(t, ctx, db, "h-fence-stamped")
	fanOutSeedScope(t, ctx, db, "h-fence-unstamped", "g1")
	unstamped := fanOutSeedItem(t, ctx, db, "h-fence-unstamped", "g1")
	beforeStamped := widenRead(t, ctx, db, stamped)
	beforeUnstamped := widenRead(t, ctx, db, unstamped)

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	now := time.Date(2026, 11, 24, 0, 0, 0, 0, time.UTC)
	for _, holder := range []string{"h-fence-stamped", "h-fence-unstamped"} {
		if err := postgres.ReopenSupplyChainImpactHolderInTransaction(ctx, postgres.SQLTx{Tx: tx}, holder, "scope:fence", "gen-fence", now); err != nil {
			t.Fatalf("fence reopen %s: %v", holder, err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	after := widenRead(t, ctx, db, stamped)
	if _, present := fanOutStampCVEs(t, ctx, db, stamped); present || after.scopeID != "scope:fence" {
		t.Fatalf("stamped holder = %+v, want the set-less fence stamp", after)
	}
	if after.status != beforeStamped.status || after.attempts != beforeStamped.attempts ||
		!after.updatedAt.Equal(beforeStamped.updatedAt) || after.visibleAt != beforeStamped.visibleAt {
		t.Fatalf("stamped holder %+v -> %+v, want only the stamp written", beforeStamped, after)
	}
	if got := widenRead(t, ctx, db, unstamped); got.version != beforeUnstamped.version {
		t.Fatalf("unstamped holder version %s -> %s, want untouched", beforeUnstamped.version, got.version)
	}
}
