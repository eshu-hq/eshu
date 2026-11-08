// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"context"
	"database/sql"
	"reflect"
	"testing"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
)

// stampOriginWithCVEs is a fan-out origin that changed two CVEs, listed
// unsorted with a duplicate, so the tests pin the sort/de-duplication.
func stampOriginWithCVEs() supplychaincore.FanOutOrigin {
	return supplychaincore.FanOutOrigin{
		SourceScopeID:      "scope:src",
		SourceGenerationID: "gen-src",
		SourceIntentID:     "intent-src",
		CVEIDs:             []string{"CVE-2026-9002", "CVE-2026-9001", "CVE-2026-9002"},
	}
}

// fanOutStampCVEs reads the stamped CVE set back from an item.
func fanOutStampCVEs(t *testing.T, ctx context.Context, db *sql.DB, id string) (cves []string, present bool) {
	t.Helper()
	var raw []byte
	var isNull bool
	if err := db.QueryRowContext(ctx, `SELECT payload->'fanout_origin'->'cves', payload->'fanout_origin'->'cves' IS NULL FROM fact_work_items WHERE work_item_id = $1`, id).Scan(&raw, &isNull); err != nil {
		t.Fatalf("read stamp cves: %v", err)
	}
	if isNull {
		return nil, false
	}
	// Decode through the same jsonb array the writer stored.
	rows, err := db.QueryContext(ctx, `SELECT x FROM jsonb_array_elements_text($1::jsonb) AS x ORDER BY x`, string(raw))
	if err != nil {
		t.Fatalf("decode stamp cves: %v", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scan stamp cve: %v", err)
		}
		cves = append(cves, id)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate stamp cves: %v", err)
	}
	return cves, true
}

// TestReopenHolderStampsSortedCVEIDsLive pins the #9740 stamp: a reopen
// carries the changed CVE ids sorted and de-duplicated on fanout_origin.
func TestReopenHolderStampsSortedCVEIDsLive(t *testing.T) {
	ctx, db := fanOutLiveDB(t)
	store := fanOutLiveStore(db)
	fanOutSeedScope(t, ctx, db, "holder-stamp", "g1")
	id := fanOutSeedItem(t, ctx, db, "holder-stamp", "g1")
	if _, err := db.ExecContext(ctx, `UPDATE fact_work_items SET status='succeeded' WHERE work_item_id=$1`, id); err != nil {
		t.Fatalf("settle item: %v", err)
	}
	if _, err := store.ReopenHolder(ctx, "holder-stamp", stampOriginWithCVEs()); err != nil {
		t.Fatalf("ReopenHolder() error = %v", err)
	}
	got, present := fanOutStampCVEs(t, ctx, db, id)
	if !present {
		t.Fatal("no cves key on the reopened stamp")
	}
	want := []string{"CVE-2026-9001", "CVE-2026-9002"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("stamp cves = %v, want sorted de-duplicated %v", got, want)
	}
}

// TestEnqueueMissingHolderStampsNoCVEIDsLive pins the establishing-pass rule:
// an enqueued holder item stamps the origin without its CVE set, so the first
// pass of the generation always runs full.
func TestEnqueueMissingHolderStampsNoCVEIDsLive(t *testing.T) {
	ctx, db := fanOutLiveDB(t)
	store := fanOutLiveStore(db)
	fanOutSeedScope(t, ctx, db, "holder-enq", "g1")
	outcome, err := store.ReopenHolder(ctx, "holder-enq", stampOriginWithCVEs())
	if err != nil || outcome != supplychaincore.FanOutEnqueued {
		t.Fatalf("outcome=%q err=%v, want enqueued", outcome, err)
	}
	id := fanOutItemID("holder-enq", fanOutGen("holder-enq", "g1"))
	if _, present := fanOutStampCVEs(t, ctx, db, id); present {
		t.Fatal("enqueued stamp carries a cves key: the establishing pass must run full")
	}
	if got := fanOutItem(t, ctx, db, id); got.originSrc != "scope:src" {
		t.Fatalf("enqueued stamp source = %q, want scope:src", got.originSrc)
	}
}

// TestClearReplayFlagLive walks the #9740 conditional clear: the seen stamp
// clears the flag, a newer stamp turns the same seen value into a no-op that
// keeps the flag, and a lost lease is an error.
func TestClearReplayFlagLive(t *testing.T) {
	ctx, db := fanOutLiveDB(t)
	store := fanOutLiveStore(db)
	fanOutSeedScope(t, ctx, db, "holder-clr", "g1")
	id := fanOutSeedItem(t, ctx, db, "holder-clr", "g1")
	queue := fanOutLiveQueue(db)
	intent, ok, err := queue.Claim(ctx)
	if err != nil || !ok || intent.IntentID != id {
		t.Fatalf("claim = %v %v %v", intent.IntentID, ok, err)
	}
	origin := stampOriginWithCVEs()
	if _, err := store.ReopenHolder(ctx, "holder-clr", origin); err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if got := fanOutItem(t, ctx, db, id); !got.flag {
		t.Fatalf("flagged item = %+v, want the replay flag set", got)
	}
	seen := map[string]any{
		"scope_id": "scope:src", "generation_id": "gen-src", "intent_id": "intent-src",
		"cves": []any{"CVE-2026-9001", "CVE-2026-9002"},
	}
	cleared, err := store.ClearReplayFlagIfStampSeen(ctx, intent, seen)
	if err != nil || !cleared {
		t.Fatalf("clear = %v err = %v, want the seen stamp to clear", cleared, err)
	}
	if got := fanOutItem(t, ctx, db, id); got.flag {
		t.Fatalf("cleared item = %+v, want the replay flag gone and the item still claimed", got)
	}
	if got, present := fanOutStampCVEs(t, ctx, db, id); !present || !reflect.DeepEqual(got, []string{"CVE-2026-9001", "CVE-2026-9002"}) {
		t.Fatalf("stamp after clear = %v present = %v, want the stamp intact for the consume step", got, present)
	}

	// A newer change lands between the compare and the clear: the same seen
	// value must not clear, so the pass reruns once under the newer stamp.
	origin.SourceIntentID = "intent-src-2"
	origin.CVEIDs = []string{"CVE-2026-9003"}
	if _, err := store.ReopenHolder(ctx, "holder-clr", origin); err != nil {
		t.Fatalf("second reopen: %v", err)
	}
	cleared, err = store.ClearReplayFlagIfStampSeen(ctx, intent, seen)
	if err != nil || cleared {
		t.Fatalf("clear with a stale stamp = %v err = %v, want a no-op that keeps the flag", cleared, err)
	}
	if got := fanOutItem(t, ctx, db, id); !got.flag {
		t.Fatalf("restamped item = %+v, want the replay flag kept", got)
	}

	// The kept flag still reruns the pass once: the ack returns the item to
	// pending, and the rerun consumes the newer stamp and succeeds.
	if err := queue.Ack(ctx, intent, reducercontract.Result{}); err != nil {
		t.Fatalf("ack: %v", err)
	}
	if got := fanOutItem(t, ctx, db, id); got.status != "pending" || got.flag {
		t.Fatalf("after its own ack = %+v, want pending with the flag consumed into the rerun", got)
	}
	rerun, ok, err := queue.Claim(ctx)
	if err != nil || !ok || rerun.AttemptCount != 1 {
		t.Fatalf("rerun claim = %v %v attempt=%d", ok, err, rerun.AttemptCount)
	}
	if err := queue.Ack(ctx, rerun, reducercontract.Result{}); err != nil {
		t.Fatalf("rerun ack: %v", err)
	}
	if got := fanOutItem(t, ctx, db, id); got.status != "succeeded" {
		t.Fatalf("after the rerun = %+v, want succeeded", got)
	}

	// A lost lease is an error, not a silent no-op: another worker owns the
	// item and will redo the pass.
	other := intent
	other.Payload = map[string]any{}
	if _, err := db.ExecContext(ctx, `UPDATE fact_work_items SET lease_owner='someone-else' WHERE work_item_id=$1`, id); err != nil {
		t.Fatalf("steal lease: %v", err)
	}
	if _, err := store.ClearReplayFlagIfStampSeen(ctx, other, seen); err == nil {
		t.Fatal("clear with a lost lease = nil, want the lease error")
	}
}
