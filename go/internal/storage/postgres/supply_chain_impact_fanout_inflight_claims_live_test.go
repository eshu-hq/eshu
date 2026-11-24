// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"errors"
	"reflect"
	"sort"
	"testing"
	"time"

	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
)

// TestFindInFlightClaimsLive pins the two claim reads of #10972 D4b against
// the shipped in-flight read. Set B returns the same scopes as
// FindInFlightNonMarkerScopes, each with the item's stored last_attempt_at.
// Set A also names an item claimed at an older or not-yet-active generation,
// so a claim made before its generation activated is never mistaken for a new
// one.
func TestFindInFlightClaimsLive(t *testing.T) {
	ctx, db := fanOutLiveDB(t)
	for _, scope := range []string{"own", "claimed", "running", "pending", "old-generation", "marker"} {
		fanOutSeedScope(t, ctx, db, scope, "g2", "g1")
		fanOutSeedItem(t, ctx, db, scope, "g2")
	}
	fanOutSeedItem(t, ctx, db, "old-generation", "g1")
	claimedAt := time.Date(2026, 11, 24, 1, 2, 3, 456789000, time.UTC)
	for scope, status := range map[string]string{"own": "claimed", "claimed": "claimed", "running": "running", "marker": "claimed"} {
		if _, err := db.ExecContext(ctx, `UPDATE fact_work_items SET status = $2, lease_owner = 'w',
claim_until = now() + interval '1 hour', last_attempt_at = $4
WHERE scope_id = $1 AND generation_id = $3`, scope, status, fanOutGen(scope, "g2"), claimedAt); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.ExecContext(ctx, `UPDATE fact_work_items SET payload = payload || '{"trigger":"manifest_dependency"}'::jsonb WHERE scope_id = 'marker'`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE fact_work_items SET status='running', lease_owner='w', claim_until=now()+interval '1 hour', last_attempt_at=$2
WHERE scope_id='old-generation' AND generation_id = $1`, fanOutGen("old-generation", "g1"), claimedAt); err != nil {
		t.Fatal(err)
	}
	store := fanOutLiveStore(db)
	scopes, err := store.FindInFlightNonMarkerScopes(ctx, "own")
	if err != nil {
		t.Fatal(err)
	}
	setB, err := store.FindInFlightNonMarkerClaims(ctx, "own")
	if err != nil {
		t.Fatalf("FindInFlightNonMarkerClaims() error = %v", err)
	}
	if got := claimScopes(setB); !reflect.DeepEqual(got, scopes) {
		t.Fatalf("set B scopes = %v, want the shipped in-flight read's %v", got, scopes)
	}
	for _, claim := range setB {
		if !claim.ClaimedAt.Equal(claimedAt) {
			t.Fatalf("set B claim %+v, want ClaimedAt %v (the stored last_attempt_at, microseconds kept)", claim, claimedAt)
		}
	}
	setA, err := store.SnapshotInFlightNonMarkerClaims(ctx, "own")
	if err != nil {
		t.Fatalf("SnapshotInFlightNonMarkerClaims() error = %v", err)
	}
	if got, want := claimScopes(setA), []string{"claimed", "old-generation", "running"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("set A scopes = %v, want %v (any generation, no marker, not own)", got, want)
	}
}

// TestFindInFlightClaimsTimeOutUnderALockedTableLive: both claim reads have the
// holder read's bounded timeout and typed error, so the load-end read can fail
// closed on it.
func TestFindInFlightClaimsTimeOutUnderALockedTableLive(t *testing.T) {
	ctx, db := fanOutLiveDB(t)
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tx.Rollback() })
	if _, err := tx.ExecContext(ctx, `LOCK TABLE fact_work_items IN ACCESS EXCLUSIVE MODE`); err != nil {
		t.Fatal(err)
	}
	store := fanOutLiveStore(db)
	store.ReadTimeout = 150 * time.Millisecond
	if _, err := store.SnapshotInFlightNonMarkerClaims(ctx, "own"); !errors.Is(err, supplychaincore.ErrFanOutReadTimeout) {
		t.Fatalf("set A error = %v, want ErrFanOutReadTimeout", err)
	}
	if _, err := store.FindInFlightNonMarkerClaims(ctx, "own"); !errors.Is(err, supplychaincore.ErrFanOutReadTimeout) {
		t.Fatalf("set B error = %v, want ErrFanOutReadTimeout", err)
	}
}

func claimScopes(claims []supplychaincore.InFlightClaim) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, claim := range claims {
		if _, ok := seen[claim.ScopeID]; !ok {
			seen[claim.ScopeID] = struct{}{}
			out = append(out, claim.ScopeID)
		}
	}
	sort.Strings(out)
	return out
}
