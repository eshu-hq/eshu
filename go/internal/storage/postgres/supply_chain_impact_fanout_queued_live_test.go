// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"sort"
	"testing"

	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// queuedHolderStates names every holder shape the prefilter must classify the
// way the reopen does. queued is the documented expectation; the differential
// below checks the same shapes against the shipped reopen's own classification.
var queuedHolderStates = []struct {
	scope  string
	queued bool
}{
	{"h-pending", true},
	{"h-retrying", true},
	{"h-two-pending", true},
	// A failed row is outside the reopen's matched status set, so the matched
	// rows are the pending one alone: the reopen classifies the holder
	// pending_noop and the prefilter drops it.
	{"h-pending-and-failed", true},
	{"h-claimed", false},
	{"h-running", false},
	{"h-succeeded", false},
	{"h-dead-letter", false},
	{"h-pending-and-succeeded", false},
	{"h-no-item", false},
	{"h-failed-only", false},
	{"h-old-generation-pending", false},
	// Pending at an old generation but succeeded at the active one: only the
	// active generation's item counts, and it is not queued, so the holder is
	// kept and the reopen returns it to pending.
	{"h-old-pending-active-succeeded", false},
}

// queuedActiveItemID is the stored work_item_id of a holder's seeded item at its
// active generation g2. fanOutSeedItem stores the item under the scoped
// generation id (fanOutGen), and fanOutItemID hashes that stored id, so the id
// must be built from fanOutGen(scope, "g2"): the short name "g2" names no row.
func queuedActiveItemID(scope string) string {
	return fanOutItemID(scope, fanOutGen(scope, "g2"))
}

// seedQueuedHolderStates creates one scope per queuedHolderStates entry and puts
// its active-generation items in that state.
func seedQueuedHolderStates(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	settle := func(id, status string) {
		t.Helper()
		res, err := db.ExecContext(ctx, `
UPDATE fact_work_items
SET status = $2,
    lease_owner = CASE WHEN $2 IN ('claimed','running') THEN 'w' END,
    claim_until = CASE WHEN $2 IN ('claimed','running') THEN now() + interval '1 hour' END,
    last_attempt_at = CASE WHEN $2 IN ('claimed','running') THEN now() END,
    next_attempt_at = CASE WHEN $2 = 'retrying' THEN now() + interval '1 minute' END,
    failure_class = CASE WHEN $2 IN ('dead_letter','failed') THEN 'synthetic' END
WHERE work_item_id = $1`, id, status)
		if err != nil {
			t.Fatalf("settle %s %s: %v", id, status, err)
		}
		// An UPDATE that matches no row is not an error. Without this check a
		// wrong id leaves the item pending and every holder looks queued.
		if n, err := res.RowsAffected(); err != nil || n != 1 {
			t.Fatalf("settle %s %s updated %d rows (err %v), want 1", id, status, n, err)
		}
	}
	extra := func(scope, status string) {
		t.Helper()
		id := queuedActiveItemID(scope) + "_second"
		if _, err := db.ExecContext(ctx, `
INSERT INTO fact_work_items (work_item_id, scope_id, generation_id, stage, domain, conflict_domain, conflict_key,
  status, attempt_count, payload, created_at, updated_at)
VALUES ($1, $2, $3, 'reducer', 'supply_chain_impact', 'scope', $1, 'pending', 0,
  jsonb_build_object('entity_key', $1::text), now(), now())`, id, scope, fanOutGen(scope, "g2")); err != nil {
			t.Fatalf("seed second item for %s: %v", scope, err)
		}
		settle(id, status)
	}
	for _, state := range queuedHolderStates {
		fanOutSeedScope(t, ctx, db, state.scope, "g2", "g1")
		switch state.scope {
		case "h-no-item":
		case "h-old-generation-pending":
			fanOutSeedItem(t, ctx, db, state.scope, "g1")
		case "h-old-pending-active-succeeded":
			fanOutSeedItem(t, ctx, db, state.scope, "g1")
			fanOutSeedItem(t, ctx, db, state.scope, "g2")
		default:
			fanOutSeedItem(t, ctx, db, state.scope, "g2")
		}
	}
	settle(queuedActiveItemID("h-retrying"), "retrying")
	settle(queuedActiveItemID("h-claimed"), "claimed")
	settle(queuedActiveItemID("h-running"), "running")
	settle(queuedActiveItemID("h-succeeded"), "succeeded")
	// The old-generation item stays pending; only the active one is settled.
	settle(queuedActiveItemID("h-old-pending-active-succeeded"), "succeeded")
	settle(queuedActiveItemID("h-dead-letter"), "dead_letter")
	settle(queuedActiveItemID("h-failed-only"), "failed")
	extra("h-two-pending", "pending")
	extra("h-pending-and-failed", "failed")
	extra("h-pending-and-succeeded", "succeeded")
}

func queuedHolderScopes() []string {
	out := make([]string, 0, len(queuedHolderStates))
	for _, state := range queuedHolderStates {
		out = append(out, state.scope)
	}
	sort.Strings(out)
	return out
}

// TestFindQueuedHolderScopesMatchesThePerHolderClassificationLive is the
// differential: for every holder shape, the prefilter returns exactly the
// holders the shipped reopen statement would classify as nothing-to-write (every
// matched row pending or retrying, and at least one). The classification runs
// the reopen's own target CTE, derived from the shipped text.
func TestFindQueuedHolderScopesMatchesThePerHolderClassificationLive(t *testing.T) {
	ctx, db := fanOutLiveDB(t)
	seedQueuedHolderStates(t, ctx, db)
	classify, ok := deriveReopenClassification(postgres.ReopenSupplyChainImpactHolderQueryForTest)
	if !ok {
		t.Fatal("cannot derive the reopen classification from the shipped statement")
	}

	var naive []string
	for _, scope := range queuedHolderScopes() {
		rows, err := db.QueryContext(ctx, classify, scope)
		if err != nil {
			t.Fatalf("classify %s: %v", scope, err)
		}
		matched, allQueued := 0, true
		for rows.Next() {
			var status string
			if err := rows.Scan(&status); err != nil {
				_ = rows.Close()
				t.Fatal(err)
			}
			matched++
			if status != "pending" && status != "retrying" {
				allQueued = false
			}
		}
		if err := rows.Close(); err != nil {
			t.Fatal(err)
		}
		if matched > 0 && allQueued {
			naive = append(naive, scope)
		}
	}

	got, err := fanOutLiveStore(db).FindQueuedHolderScopes(ctx, queuedHolderScopes(), nil)
	if err != nil {
		t.Fatalf("FindQueuedHolderScopes() error = %v", err)
	}
	if !reflect.DeepEqual(got, naive) {
		t.Fatalf("prefilter = %v, per-holder classification = %v", got, naive)
	}
	var want []string
	for _, state := range queuedHolderStates {
		if state.queued {
			want = append(want, state.scope)
		}
	}
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("prefilter = %v, want %v", got, want)
	}
	// A holder outside the asked list is never returned, an empty list reads
	// nothing, and an unknown scope is not queued.
	only, err := fanOutLiveStore(db).FindQueuedHolderScopes(ctx, []string{"h-pending", "h-unknown"}, nil)
	if err != nil || !reflect.DeepEqual(only, []string{"h-pending"}) {
		t.Fatalf("bounded list = %v, %v, want [h-pending]", only, err)
	}
	if none, err := fanOutLiveStore(db).FindQueuedHolderScopes(ctx, nil, nil); err != nil || len(none) != 0 {
		t.Fatalf("empty list = %v, %v", none, err)
	}
}

func queuedRowVersions(t *testing.T, ctx context.Context, db *sql.DB) map[string]string {
	t.Helper()
	rows, err := db.QueryContext(ctx, `SELECT work_item_id, xmin::text FROM fact_work_items`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	out := map[string]string{}
	for rows.Next() {
		var id, version string
		if err := rows.Scan(&id, &version); err != nil {
			t.Fatal(err)
		}
		out[id] = version
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// TestFindQueuedHolderScopesPrefilteredHoldersWriteNoRowVersionLive runs the
// fan-out loop the way the handler does: the prefilter read, then one reopen
// per kept holder. Pending and retrying holders must keep the row version
// (xmin) they had, which is the write the prefilter exists to avoid. The late
// claim of a kept holder is proven through a real claim in
// supply_chain_impact_fanout_queued_late_claim_live_test.go.
func TestFindQueuedHolderScopesPrefilteredHoldersWriteNoRowVersionLive(t *testing.T) {
	ctx, db := fanOutLiveDB(t)
	seedQueuedHolderStates(t, ctx, db)
	store := fanOutLiveStore(db)

	queued, err := store.FindQueuedHolderScopes(ctx, queuedHolderScopes(), nil)
	if err != nil {
		t.Fatal(err)
	}
	dropped := map[string]bool{}
	for _, scope := range queued {
		dropped[scope] = true
	}
	before := queuedRowVersions(t, ctx, db)
	// Every kept holder must still reach the per-holder statement; the loop
	// below runs it for the holders whose reopen this fixture can complete. A
	// failed-only holder has no matched item, so its reopen would enqueue a new
	// intent that collides with the fixture's failed row: that enqueue path is
	// covered by the reopen tests, not by this one.
	kept := []string{"h-claimed", "h-running", "h-succeeded"}
	for _, scope := range kept {
		if dropped[scope] {
			t.Fatalf("holder %s was dropped by the prefilter, want kept", scope)
		}
		if _, err := store.ReopenHolderDetailed(ctx, scope, fanOutLiveOrigin); err != nil {
			t.Fatalf("reopen %s: %v", scope, err)
		}
	}
	after := queuedRowVersions(t, ctx, db)
	for _, scope := range queued {
		// The primary item must exist in the snapshot, or the check proves nothing.
		if _, ok := before[queuedActiveItemID(scope)]; !ok {
			t.Fatalf("prefiltered holder %s has no seeded item %s in the version snapshot", scope, queuedActiveItemID(scope))
		}
		for _, id := range []string{queuedActiveItemID(scope), queuedActiveItemID(scope) + "_second"} {
			if beforeVersion, ok := before[id]; ok && after[id] != beforeVersion {
				t.Fatalf("prefiltered holder row %s changed version %s -> %s", id, beforeVersion, after[id])
			}
		}
	}
	if len(dropped) == 0 {
		t.Fatal("the prefilter dropped no holders: the test proves nothing")
	}
}

// TestFindQueuedHolderScopesTimesOutUnderALockedTableLive: the prefilter read has
// the fan-out read's bounded timeout and typed error.
func TestFindQueuedHolderScopesTimesOutUnderALockedTableLive(t *testing.T) {
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
	store.ReadTimeout = 150_000_000
	if _, err := store.FindQueuedHolderScopes(ctx, []string{"h-pending"}, nil); !errors.Is(err, supplychaincore.ErrFanOutReadTimeout) {
		t.Fatalf("error = %v, want ErrFanOutReadTimeout", err)
	}
}
