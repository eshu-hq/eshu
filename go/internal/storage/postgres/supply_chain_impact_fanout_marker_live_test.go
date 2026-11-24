// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"reflect"
	"testing"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
)

// TestUpdateFanOutStateStoresTheFullPassMarkerUnderTheLeaseLive pins the
// storage half of the #10972 D2 bound: the pass's own fenced state update
// stores the full-pass marker beside the stamp it consumes, a nil marker
// leaves the stored one alone, the marker survives a reopen and the ack, and
// an update that lost its lease stores nothing.
func TestUpdateFanOutStateStoresTheFullPassMarkerUnderTheLeaseLive(t *testing.T) {
	ctx, db := fanOutLiveDB(t)
	fanOutSeedScope(t, ctx, db, "holder", "g1")
	id := fanOutSeedItem(t, ctx, db, "holder", "g1")
	store := fanOutLiveStore(db)
	queue := fanOutLiveQueue(db)
	marker := map[string]any{
		supplychaincore.FullPassMarkerGenerationKey:      fanOutGen("holder", "g1"),
		supplychaincore.FullPassMarkerScopedSinceFullKey: 0,
		supplychaincore.FullPassMarkerFullAtKey:          "2026-11-24T01:00:00Z",
	}
	stored := map[string]any{
		supplychaincore.FullPassMarkerGenerationKey:      fanOutGen("holder", "g1"),
		supplychaincore.FullPassMarkerScopedSinceFullKey: float64(0),
		supplychaincore.FullPassMarkerFullAtKey:          "2026-11-24T01:00:00Z",
	}

	first, ok, err := queue.Claim(ctx)
	if err != nil || !ok || first.IntentID != id {
		t.Fatalf("claim = %q %v %v", first.IntentID, ok, err)
	}
	if err := store.UpdateFanOutState(ctx, first, supplychaincore.FanOutState{ClearOwed: true, FullPassMarker: marker}); err != nil {
		t.Fatalf("UpdateFanOutState(marker) error = %v", err)
	}
	if got := fanOutStoredPayload(t, db, id)[supplychaincore.PayloadKeyFullPassMarker]; !reflect.DeepEqual(got, stored) {
		t.Fatalf("stored marker = %v, want %v", got, stored)
	}
	if err := queue.Ack(ctx, first, reducercontract.Result{}); err != nil {
		t.Fatalf("ack: %v", err)
	}

	// A reopen stamps the item and keeps the marker; the next claim sees it.
	origin := supplychaincore.FanOutOrigin{SourceScopeID: "source", SourceGenerationID: "g", SourceIntentID: "i", CVEIDs: []string{"CVE-2026-73070"}}
	if _, err := store.ReopenHolderDetailed(ctx, "holder", origin); err != nil {
		t.Fatalf("reopen: %v", err)
	}
	second, ok, err := queue.Claim(ctx)
	if err != nil || !ok {
		t.Fatalf("second claim = %v %v", ok, err)
	}
	if got := second.Payload[supplychaincore.PayloadKeyFullPassMarker]; !reflect.DeepEqual(got, stored) {
		t.Fatalf("claimed marker after a reopen = %v, want %v", got, stored)
	}
	expected := second.Payload[supplychaincore.PayloadKeyFanOutOrigin].(map[string]any)
	if err := store.UpdateFanOutState(ctx, second, supplychaincore.FanOutState{ConsumeOrigin: true, ExpectedOrigin: expected}); err != nil {
		t.Fatalf("UpdateFanOutState(no marker) error = %v", err)
	}
	payload := fanOutStoredPayload(t, db, id)
	if got := payload[supplychaincore.PayloadKeyFullPassMarker]; !reflect.DeepEqual(got, stored) {
		t.Fatalf("marker after a nil-marker update = %v, want it left alone: %v", got, stored)
	}
	if _, has := payload[supplychaincore.PayloadKeyFanOutOrigin]; has {
		t.Fatal("the stamp was not consumed beside the marker")
	}
	if err := queue.Ack(ctx, second, reducercontract.Result{}); err != nil {
		t.Fatalf("second ack: %v", err)
	}

	// The lease is gone: the fenced update stores nothing.
	newer := map[string]any{
		supplychaincore.FullPassMarkerGenerationKey:      fanOutGen("holder", "g1"),
		supplychaincore.FullPassMarkerScopedSinceFullKey: 0,
		supplychaincore.FullPassMarkerFullAtKey:          "2026-11-24T02:00:00Z",
	}
	if err := store.UpdateFanOutState(ctx, second, supplychaincore.FanOutState{FullPassMarker: newer}); err == nil {
		t.Fatal("UpdateFanOutState() error = nil after the ack, want a lost-lease error")
	}
	if got := fanOutStoredPayload(t, db, id)[supplychaincore.PayloadKeyFullPassMarker]; !reflect.DeepEqual(got, stored) {
		t.Fatalf("marker after a lost-lease update = %v, want the old one %v", got, stored)
	}
}
