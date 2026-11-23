// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"
	"time"

	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
)

// The #10972 stamp-widening proofs. A holder whose item is pending (or
// claimed and already flagged) with a fan-out stamp for CVE A, reached by a
// fan-out for CVE B, must end with a stamp naming both, or its next pass
// derives A only and B is lost for that holder. Stored stamps use the shape
// FanOutStampJSON writes.

const (
	widenCVEA = "CVE-2026-7001"
	widenCVEB = "CVE-2026-7002"
)

// widenOriginA is the earlier change the holder's stamp already names.
var widenOriginA = supplychaincore.FanOutOrigin{
	SourceScopeID: "scope:source-a", SourceGenerationID: "gen-a", SourceIntentID: "intent-a",
	CVEIDs: []string{widenCVEA},
}

// widenOriginB is the later change that reaches the same holder.
var widenOriginB = supplychaincore.FanOutOrigin{
	SourceScopeID: "scope:source-b", SourceGenerationID: "gen-b", SourceIntentID: "intent-b",
	CVEIDs: []string{widenCVEB},
}

// widenStamp returns the stored stamp JSON for an origin, through the one
// stamp writer the store uses.
func widenStamp(t *testing.T, origin supplychaincore.FanOutOrigin) string {
	t.Helper()
	stamp, err := supplychaincore.FanOutStampJSON(origin)
	if err != nil {
		t.Fatalf("marshal stamp: %v", err)
	}
	return string(stamp)
}

// widenSetStamp writes stamp as the item's fanout_origin and returns the
// decoded map a claim would carry.
func widenSetStamp(t *testing.T, ctx context.Context, db *sql.DB, id, stamp string) map[string]any {
	t.Helper()
	res, err := db.ExecContext(ctx, `
UPDATE fact_work_items SET payload = jsonb_set(payload, '{fanout_origin}', $2::jsonb, TRUE)
WHERE work_item_id = $1`, id, stamp)
	if err != nil {
		t.Fatalf("stamp %s: %v", id, err)
	}
	if n, err := res.RowsAffected(); err != nil || n != 1 {
		t.Fatalf("stamp %s updated %d rows (err %v), want 1", id, n, err)
	}
	var seen map[string]any
	if err := json.Unmarshal([]byte(stamp), &seen); err != nil {
		t.Fatal(err)
	}
	return seen
}

// widenRow is the queue state a stamp widen must leave alone, plus the stamp.
type widenRow struct {
	status     string
	attempts   int
	flag       bool
	updatedAt  time.Time
	visibleAt  sql.NullTime
	leaseOwner sql.NullString
	claimUntil sql.NullTime
	failure    sql.NullString
	cves       string
	scopeID    string
	version    string
}

func widenRead(t *testing.T, ctx context.Context, db *sql.DB, id string) widenRow {
	t.Helper()
	var row widenRow
	var cves, scope sql.NullString
	if err := db.QueryRowContext(ctx, `
SELECT status, attempt_count, cross_scope_replay_required, updated_at, visible_at, lease_owner, claim_until,
       failure_class, (payload->'fanout_origin'->'cves')::text, payload->'fanout_origin'->>'scope_id', xmin::text
FROM fact_work_items WHERE work_item_id = $1`, id).Scan(&row.status, &row.attempts, &row.flag, &row.updatedAt,
		&row.visibleAt, &row.leaseOwner, &row.claimUntil, &row.failure, &cves, &scope, &row.version); err != nil {
		t.Fatalf("read %s: %v", id, err)
	}
	row.cves, row.scopeID = cves.String, scope.String
	return row
}

// widenSeedPending seeds a scope with one pending, attempt-0 item at its
// active generation, stamped for change A, and returns the item id.
func widenSeedPending(t *testing.T, ctx context.Context, db *sql.DB, scope string) string {
	t.Helper()
	fanOutSeedScope(t, ctx, db, scope, "g1")
	id := fanOutSeedItem(t, ctx, db, scope, "g1")
	widenSetStamp(t, ctx, db, id, widenStamp(t, widenOriginA))
	return id
}

// TestFindQueuedHolderScopesKeepsAPendingHolderStampedForAnotherChangeLive:
// a pending attempt-0 holder stamped {A} runs a one-hop pass scoped to A, so
// a fan-out for {B} must not drop it at the prefilter read. It has to reach
// the reopen, which widens its stamp.
func TestFindQueuedHolderScopesKeepsAPendingHolderStampedForAnotherChangeLive(t *testing.T) {
	ctx, db := fanOutLiveDB(t)
	widenSeedPending(t, ctx, db, "h-widen-keep")
	got, err := fanOutLiveStore(db).FindQueuedHolderScopes(ctx, []string{"h-widen-keep"})
	if err != nil {
		t.Fatalf("FindQueuedHolderScopes() error = %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("prefilter dropped %v: a pending holder stamped {%s} does not cover a change to %s", got, widenCVEA, widenCVEB)
	}
}

// TestReopenWidensAPendingHoldersStampLive: the reopen merges change B into
// the pending holder's stamp, sorted, and writes nothing else. updated_at and
// visible_at stay, because the claim orders by updated_at and a bump would
// push a busy holder to the back on every fan-out.
func TestReopenWidensAPendingHoldersStampLive(t *testing.T) {
	ctx, db := fanOutLiveDB(t)
	id := widenSeedPending(t, ctx, db, "h-widen-pending")
	before := widenRead(t, ctx, db, id)

	reopen, err := fanOutLiveStore(db).ReopenHolderDetailed(ctx, "h-widen-pending", widenOriginB)
	if err != nil {
		t.Fatalf("ReopenHolderDetailed() error = %v", err)
	}
	if string(reopen.Outcome) != "stamp_widened" {
		t.Fatalf("outcome = %q, want stamp_widened", reopen.Outcome)
	}
	after := widenRead(t, ctx, db, id)
	if want := `["` + widenCVEA + `", "` + widenCVEB + `"]`; after.cves != want {
		t.Fatalf("stamp cves = %s, want %s", after.cves, want)
	}
	if after.status != "pending" || after.attempts != 0 || after.flag {
		t.Fatalf("row = %+v, want pending, attempt 0, no replay flag", after)
	}
	if !after.updatedAt.Equal(before.updatedAt) || after.visibleAt != before.visibleAt {
		t.Fatalf("updated_at %v -> %v, visible_at %v -> %v: a widen must not move the claim order",
			before.updatedAt, after.updatedAt, before.visibleAt, after.visibleAt)
	}
	if after.leaseOwner.Valid || after.claimUntil.Valid || after.failure.Valid {
		t.Fatalf("row = %+v, want no lease and no failure fields", after)
	}
}

// TestReopenWidensAFlaggedInFlightHoldersStampLive: a claimed holder whose
// replay flag is already on, stamped {A}, reached by {B}, keeps its flag and
// lease and ends stamped [A, B], so its one rerun derives both. The running
// pass saw {A} at claim, so its conditional flag clear must now fail.
func TestReopenWidensAFlaggedInFlightHoldersStampLive(t *testing.T) {
	ctx, db := fanOutLiveDB(t)
	store := fanOutLiveStore(db)
	fanOutSeedScope(t, ctx, db, "h-widen-flagged", "g1")
	id := fanOutSeedItem(t, ctx, db, "h-widen-flagged", "g1")
	seen := widenSetStamp(t, ctx, db, id, widenStamp(t, widenOriginA))
	intent, ok, err := fanOutLiveQueue(db).Claim(ctx)
	if err != nil || !ok || intent.IntentID != id {
		t.Fatalf("claim = %q %v %v", intent.IntentID, ok, err)
	}
	// A first fan-out flagged the pass while it ran, leaving its stamp {A}.
	if _, err := db.ExecContext(ctx, `UPDATE fact_work_items SET cross_scope_replay_required = TRUE WHERE work_item_id = $1`, id); err != nil {
		t.Fatal(err)
	}
	before := widenRead(t, ctx, db, id)

	if _, err := store.ReopenHolderDetailed(ctx, "h-widen-flagged", widenOriginB); err != nil {
		t.Fatalf("ReopenHolderDetailed() error = %v", err)
	}
	after := widenRead(t, ctx, db, id)
	if want := `["` + widenCVEA + `", "` + widenCVEB + `"]`; after.cves != want {
		t.Fatalf("stamp cves = %s, want %s", after.cves, want)
	}
	if after.status != "claimed" || !after.flag || after.attempts != before.attempts {
		t.Fatalf("row = %+v, want claimed, flag on, attempts %d", after, before.attempts)
	}
	if after.leaseOwner != before.leaseOwner || after.claimUntil != before.claimUntil {
		t.Fatalf("lease %v/%v -> %v/%v, want untouched", before.leaseOwner, before.claimUntil, after.leaseOwner, after.claimUntil)
	}
	cleared, err := store.ClearReplayFlagIfStampSeen(ctx, intent, seen)
	if err != nil || cleared {
		t.Fatalf("clear with the claim-time stamp = %v err = %v, want false: the widened stamp must rerun the pass", cleared, err)
	}
}
