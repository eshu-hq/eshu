// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"database/sql"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
)

func fanOutStoredPayload(t *testing.T, db *sql.DB, id string) map[string]any {
	t.Helper()
	var raw []byte
	if err := db.QueryRow(`SELECT payload FROM fact_work_items WHERE work_item_id = $1`, id).Scan(&raw); err != nil {
		t.Fatalf("read payload %s: %v", id, err)
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("decode payload %s: %v", id, err)
	}
	return payload
}

// TestUpdateFanOutStateCarriesAContinuationAcrossClaimsLive proves the overflow
// state the handler relies on: the list is stored on the claimed item, the item
// is marked so its own acknowledgement returns it to pending, the next claim sees
// the list in its payload, and clearing it removes the key.
func TestUpdateFanOutStateCarriesAContinuationAcrossClaimsLive(t *testing.T) {
	ctx, db := fanOutLiveDB(t)
	fanOutSeedScope(t, ctx, db, "source", "g1")
	id := fanOutSeedItem(t, ctx, db, "source", "g1")
	store := fanOutLiveStore(db)
	queue := fanOutLiveQueue(db)

	first, ok, err := queue.Claim(ctx)
	if err != nil || !ok || first.IntentID != id {
		t.Fatalf("claim = %q %v %v", first.IntentID, ok, err)
	}
	if err := store.UpdateFanOutState(ctx, first, supplychaincore.FanOutState{ReplaceRemaining: true, Remaining: []string{"holder-b", "holder-c"}}); err != nil {
		t.Fatalf("UpdateFanOutState(list) error = %v", err)
	}
	if got := fanOutItem(t, ctx, db, id); got.status != "claimed" || !got.flag {
		t.Fatalf("item = %+v, want claimed and marked for one rerun", got)
	}
	if err := queue.Ack(ctx, first, reducercontract.Result{}); err != nil {
		t.Fatalf("ack: %v", err)
	}
	if got := fanOutItem(t, ctx, db, id); got.status != "pending" || got.flag {
		t.Fatalf("after ack = %+v, want pending with the mark consumed", got)
	}

	second, ok, err := queue.Claim(ctx)
	if err != nil || !ok {
		t.Fatalf("second claim = %v %v", ok, err)
	}
	if !reflect.DeepEqual(second.Payload[supplychaincore.PayloadKeyFanOutRemaining], []any{"holder-b", "holder-c"}) {
		t.Fatalf("second claim payload = %v, want the stored continuation list", second.Payload)
	}
	if err := store.UpdateFanOutState(ctx, second, supplychaincore.FanOutState{ReplaceRemaining: true}); err != nil {
		t.Fatalf("UpdateFanOutState(clear) error = %v", err)
	}
	if _, has := fanOutStoredPayload(t, db, id)[supplychaincore.PayloadKeyFanOutRemaining]; has {
		t.Fatal("the continuation list is still stored after clearing")
	}
	if got := fanOutItem(t, ctx, db, id); got.flag {
		t.Fatalf("item = %+v, want no rerun mark after clearing", got)
	}
	if err := queue.Ack(ctx, second, reducercontract.Result{}); err != nil {
		t.Fatalf("second ack: %v", err)
	}
	if got := fanOutItem(t, ctx, db, id); got.status != "succeeded" {
		t.Fatalf("final = %+v, want succeeded", got)
	}
}

// TestUpdateFanOutStateConsumesTheOriginStampLive proves a pass that honored the
// stamp removes it, so a later reopen by another path is judged fresh.
func TestUpdateFanOutStateConsumesTheOriginStampLive(t *testing.T) {
	ctx, db := fanOutLiveDB(t)
	fanOutSeedScope(t, ctx, db, "holder", "g1")
	id := fanOutSeedItem(t, ctx, db, "holder", "g1")
	if _, err := db.ExecContext(ctx, `UPDATE fact_work_items SET status='succeeded' WHERE work_item_id=$1`, id); err != nil {
		t.Fatal(err)
	}
	store := fanOutLiveStore(db)
	if outcome, err := store.ReopenHolder(ctx, "holder", fanOutLiveOrigin); err != nil || outcome != supplychaincore.FanOutReopened {
		t.Fatalf("reopen = %q %v", outcome, err)
	}
	queue := fanOutLiveQueue(db)
	intent, ok, err := queue.Claim(ctx)
	if err != nil || !ok {
		t.Fatalf("claim = %v %v", ok, err)
	}
	if _, has := intent.Payload[supplychaincore.PayloadKeyFanOutOrigin]; !has {
		t.Fatalf("claimed payload = %v, want the origin stamp", intent.Payload)
	}
	claimed, _ := intent.Payload[supplychaincore.PayloadKeyFanOutOrigin].(map[string]any)
	if err := store.UpdateFanOutState(ctx, intent, supplychaincore.FanOutState{ConsumeOrigin: true, ExpectedOrigin: claimed}); err != nil {
		t.Fatalf("UpdateFanOutState(consume) error = %v", err)
	}
	if _, has := fanOutStoredPayload(t, db, id)[supplychaincore.PayloadKeyFanOutOrigin]; has {
		t.Fatal("the origin stamp is still stored after being consumed")
	}
	if got := fanOutItem(t, ctx, db, id); got.flag {
		t.Fatalf("item = %+v: consuming the stamp must not mark the item for a rerun", got)
	}
}

// TestUpdateFanOutStateFailsWhenTheLeaseIsLostLive proves the state update is fenced
// like an acknowledgement: a worker that no longer holds the claim cannot write
// to the item, and the failure is an error so its pass fails instead of
// pretending the continuation was recorded.
func TestUpdateFanOutStateFailsWhenTheLeaseIsLostLive(t *testing.T) {
	ctx, db := fanOutLiveDB(t)
	fanOutSeedScope(t, ctx, db, "source", "g1")
	fanOutSeedItem(t, ctx, db, "source", "g1")
	store := fanOutLiveStore(db)
	queue := fanOutLiveQueue(db)
	intent, ok, err := queue.Claim(ctx)
	if err != nil || !ok {
		t.Fatalf("claim = %v %v", ok, err)
	}
	if err := queue.Ack(ctx, intent, reducercontract.Result{}); err != nil {
		t.Fatalf("ack: %v", err)
	}
	err = store.UpdateFanOutState(ctx, intent, supplychaincore.FanOutState{ReplaceRemaining: true, Remaining: []string{"holder-b"}})
	if err == nil {
		t.Fatal("UpdateFanOutState() error = nil, want a lost-lease error after the item was acknowledged")
	}
}

// TestFindHolderScopesTimesOutUnderALockedTableLive proves the bounded read: with
// the table held exclusively the statement cannot start, the statement timeout
// fires, and the failure is reported as the typed timeout error the handler
// counts as read_timeout.
func TestFindHolderScopesTimesOutUnderALockedTableLive(t *testing.T) {
	ctx, db := fanOutLiveDB(t)
	fanOutSeedScope(t, ctx, db, "holder-a", "g1")
	fanOutSeedFinding(t, ctx, db, "holder-a", "g1", "CVE-2026-0001", false)
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tx.Rollback() })
	if _, err := tx.ExecContext(ctx, `LOCK TABLE fact_records IN ACCESS EXCLUSIVE MODE`); err != nil {
		t.Fatalf("lock fact_records: %v", err)
	}
	store := fanOutLiveStore(db)
	store.ReadTimeout = 150 * time.Millisecond
	started := time.Now()
	_, err = store.FindHolderScopes(ctx, "own", []string{"CVE-2026-0001"})
	if !errors.Is(err, supplychaincore.ErrFanOutReadTimeout) {
		t.Fatalf("error = %v, want ErrFanOutReadTimeout", err)
	}
	if elapsed := time.Since(started); elapsed > 5*time.Second {
		t.Fatalf("the read held for %v: the timeout did not bound it", elapsed)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	holders, err := store.FindHolderScopes(ctx, "own", []string{"CVE-2026-0001"})
	if err != nil || !reflect.DeepEqual(holders, []string{"holder-a"}) {
		t.Fatalf("after the lock released: holders=%v err=%v", holders, err)
	}
}

// TestUpdateFanOutStateKeepsANewerStampLive proves the consume is conditional: a
// stamp another scope's fan-out wrote after this pass claimed the item is a
// different request and survives, so its replay is still judged one hop.
func TestUpdateFanOutStateKeepsANewerStampLive(t *testing.T) {
	ctx, db := fanOutLiveDB(t)
	fanOutSeedScope(t, ctx, db, "holder", "g1")
	id := fanOutSeedItem(t, ctx, db, "holder", "g1")
	if _, err := db.ExecContext(ctx, `UPDATE fact_work_items SET status='succeeded' WHERE work_item_id=$1`, id); err != nil {
		t.Fatal(err)
	}
	store := fanOutLiveStore(db)
	if _, err := store.ReopenHolder(ctx, "holder", fanOutLiveOrigin); err != nil {
		t.Fatal(err)
	}
	queue := fanOutLiveQueue(db)
	intent, ok, err := queue.Claim(ctx)
	if err != nil || !ok {
		t.Fatalf("claim = %v %v", ok, err)
	}
	claimed, _ := intent.Payload[supplychaincore.PayloadKeyFanOutOrigin].(map[string]any)
	newer := fanOutLiveOrigin
	newer.SourceScopeID = "scope:newer"
	if out, err := store.ReopenHolder(ctx, "holder", newer); err != nil || out != supplychaincore.FanOutReplayFlagged {
		t.Fatalf("newer stamp = %q %v, want replay_flagged on the running pass", out, err)
	}
	if err := store.UpdateFanOutState(ctx, intent, supplychaincore.FanOutState{ConsumeOrigin: true, ExpectedOrigin: claimed}); err != nil {
		t.Fatalf("UpdateFanOutState() error = %v", err)
	}
	stamp, _ := fanOutStoredPayload(t, db, id)[supplychaincore.PayloadKeyFanOutOrigin].(map[string]any)
	if stamp["scope_id"] != "scope:newer" {
		t.Fatalf("stored stamp = %v, want the newer stamp to survive", stamp)
	}
}

// TestUpdateFanOutStateRejectsAStaleAttemptLive proves the attempt fence. The
// lease owner is one shared constant, so it cannot tell two workers apart; the
// claimed-at value is what ties an update to the attempt that made it. An update
// from a superseded attempt of the same item must fail while the current attempt's
// succeeds.
func TestUpdateFanOutStateRejectsAStaleAttemptLive(t *testing.T) {
	ctx, db := fanOutLiveDB(t)
	fanOutSeedScope(t, ctx, db, "source", "g1")
	id := fanOutSeedItem(t, ctx, db, "source", "g1")
	store := fanOutLiveStore(db)
	queue := fanOutLiveQueue(db)
	first, ok, err := queue.Claim(ctx)
	if err != nil || !ok || first.IntentID != id {
		t.Fatalf("first claim = %v %v", ok, err)
	}
	if err := queue.Fail(ctx, first, raceRetryableErr{}); err != nil {
		t.Fatalf("fail: %v", err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE fact_work_items SET visible_at = now() - interval '1 second', next_attempt_at = NULL WHERE work_item_id = $1`, id); err != nil {
		t.Fatal(err)
	}
	second, ok, err := queue.Claim(ctx)
	if err != nil || !ok || second.AttemptCount != 2 {
		t.Fatalf("second claim = %v %v attempt=%d", ok, err, second.AttemptCount)
	}
	state := supplychaincore.FanOutState{ReplaceRemaining: true, Remaining: []string{"holder-b"}}
	if err := store.UpdateFanOutState(ctx, first, state); err == nil {
		t.Fatal("UpdateFanOutState() from the superseded attempt succeeded, want a fence error")
	}
	if err := store.UpdateFanOutState(ctx, second, state); err != nil {
		t.Fatalf("UpdateFanOutState() from the current attempt error = %v", err)
	}
}

// TestUpdateFanOutStateConsumeOnlyLeavesTheListAloneLive pins the explicit field at the
// SQL: an update that only consumes the stamp or clears the marker must not touch
// fanout_remaining, whatever else the item carries.
func TestUpdateFanOutStateConsumeOnlyLeavesTheListAloneLive(t *testing.T) {
	ctx, db := fanOutLiveDB(t)
	fanOutSeedScope(t, ctx, db, "source", "g1")
	id := fanOutSeedItem(t, ctx, db, "source", "g1")
	store := fanOutLiveStore(db)
	queue := fanOutLiveQueue(db)
	intent, ok, err := queue.Claim(ctx)
	if err != nil || !ok {
		t.Fatalf("claim = %v %v", ok, err)
	}
	if err := store.UpdateFanOutState(ctx, intent, supplychaincore.FanOutState{ReplaceRemaining: true, Remaining: []string{"holder-b"}}); err != nil {
		t.Fatal(err)
	}
	if err := store.UpdateFanOutState(ctx, intent, supplychaincore.FanOutState{ClearOwed: true}); err != nil {
		t.Fatal(err)
	}
	stored, _ := fanOutStoredPayload(t, db, id)[supplychaincore.PayloadKeyFanOutRemaining].([]any)
	if len(stored) != 1 || stored[0] != "holder-b" {
		t.Fatalf("stored list = %v, want it untouched by an update that did not replace it", stored)
	}
}
