// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/recovery"
	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// deadLetterNow is the fixed reopen clock of the #11147 proofs, truncated to
// the microsecond so a stored timestamptz compares equal.
var deadLetterNow = time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)

func deadLetterStore(db *sql.DB) postgres.SupplyChainImpactFanOutStore {
	store := fanOutLiveStore(db)
	store.Now = func() time.Time { return deadLetterNow }
	return store
}

// deadLetterOldStamp is an origin stamp an earlier fan-out wrote; a reopened
// dead letter must not keep it, or its next claim would run as a one hop.
const deadLetterOldStamp = `{"scope_id":"scope:old","generation_id":"g-old","intent_id":"i-old","cve_ids":["CVE-2026-0001"]}`

// deadLetterSeed turns a seeded item into a dead letter that spent its budget
// and still owes holders, the shape the QA census found (#11147).
func deadLetterSeed(t *testing.T, ctx context.Context, db *sql.DB, id, class string) {
	t.Helper()
	var classArg any
	if class != "" {
		classArg = class
	}
	if _, err := db.ExecContext(ctx, `
UPDATE fact_work_items
SET status = 'dead_letter', attempt_count = 4, lease_owner = NULL, claim_until = NULL, visible_at = NULL,
    next_attempt_at = NULL, updated_at = now() - interval '1 hour',
    failure_class = $2, failure_message = 'canceling statement due to statement timeout', failure_details = 'SQLSTATE 57014',
    payload = payload || jsonb_build_object(
        'fanout_origin', $3::jsonb,
        'fanout_owed', TRUE,
        'fanout_remaining', '["scope:owed-a","scope:owed-b"]'::jsonb)
WHERE work_item_id = $1`, id, classArg, deadLetterOldStamp); err != nil {
		t.Fatalf("seed dead letter %s: %v", id, err)
	}
}

type deadLetterRow struct {
	status, leaseOwner, failureClass, failureMessage, failureDetails, v2, v3 string
	attempts                                                                 int
	flag, hasStamp, owed                                                     bool
	claimUntil, visibleAt, reopenedAt, nextAttemptAt, updatedAt              sql.NullTime
	remaining                                                                []string
}

func deadLetterRead(t *testing.T, ctx context.Context, db *sql.DB, id string) deadLetterRow {
	t.Helper()
	var (
		row                                           deadLetterRow
		lease, class, message, details, remainingJSON sql.NullString
	)
	if err := db.QueryRowContext(ctx, `
SELECT status, attempt_count, lease_owner, claim_until, visible_at, reopened_at, next_attempt_at, updated_at,
       failure_class, failure_message, failure_details, cross_scope_replay_required,
       container_image_identity_v2_authorized_status, container_image_identity_v3_authorized_status,
       payload ? 'fanout_origin', COALESCE((payload->>'fanout_owed')::boolean, FALSE), payload->'fanout_remaining'
FROM fact_work_items WHERE work_item_id = $1`, id).Scan(
		&row.status, &row.attempts, &lease, &row.claimUntil, &row.visibleAt, &row.reopenedAt, &row.nextAttemptAt, &row.updatedAt,
		&class, &message, &details, &row.flag, &row.v2, &row.v3, &row.hasStamp, &row.owed, &remainingJSON); err != nil {
		t.Fatalf("read item %s: %v", id, err)
	}
	row.leaseOwner, row.failureClass, row.failureMessage, row.failureDetails = lease.String, class.String, message.String, details.String
	if remainingJSON.Valid {
		if err := json.Unmarshal([]byte(remainingJSON.String), &row.remaining); err != nil {
			t.Fatalf("decode fanout_remaining: %v", err)
		}
	}
	return row
}

// assertGenuineReopen checks the decision-1 shape of a reopened dead letter:
// pending at attempt 0, lease and failure cleared, visible and reopened now,
// identity authorization as the admin replay sets it, no stamp, owed and
// remaining holders kept.
func assertGenuineReopen(t *testing.T, got deadLetterRow) {
	t.Helper()
	if got.status != "pending" || got.attempts != 0 || got.leaseOwner != "" || got.claimUntil.Valid || got.nextAttemptAt.Valid {
		t.Fatalf("row = %+v, want pending, attempt 0, no lease, no scheduled retry", got)
	}
	if !got.visibleAt.Valid || !got.visibleAt.Time.Equal(deadLetterNow) || !got.reopenedAt.Valid || !got.reopenedAt.Time.Equal(deadLetterNow) {
		t.Fatalf("visible_at %v reopened_at %v, want both %v", got.visibleAt, got.reopenedAt, deadLetterNow)
	}
	if got.failureClass != "" || got.failureMessage != "" || got.failureDetails != "" || got.flag {
		t.Fatalf("row = %+v, want failure evidence and replay flag cleared", got)
	}
	if got.v2 != "" || got.v3 != "" {
		t.Fatalf("identity authorization v2=%q v3=%q, want '' (not required), as the admin replay sets it", got.v2, got.v3)
	}
	if got.hasStamp {
		t.Fatal("reopened dead letter still carries fanout_origin: its next claim would run as a one hop and hide the unfinished genuine pass")
	}
	if !got.owed || !reflect.DeepEqual(got.remaining, []string{"scope:owed-a", "scope:owed-b"}) {
		t.Fatalf("owed=%v remaining=%v, want the owed marker and both remaining holders kept", got.owed, got.remaining)
	}
}

// TestReopenHolderReopensActiveDeadLetterEveryClassLive proves decision 1 of
// the #11147 ruling for every class: a dead letter at the holder's active
// generation reopens as a full genuine pass unless its class is in the admin
// unsafe-replay set, which stays dead and untouched and reports
// dead_letter_skipped. A NULL class (a hand-set or legacy row) reopens.
func TestReopenHolderReopensActiveDeadLetterEveryClassLive(t *testing.T) {
	ctx, db := fanOutLiveDB(t)
	store := deadLetterStore(db)
	reopenable := []string{
		supplychaincore.SupplyChainImpactFanOutFailureClass,
		"reducer_handler_abandoned_at_shutdown",
		"reducer_handler_timeout",
		"retry_exhausted",
		"dependency_unavailable",
		"timeout",
		"",
	}
	for i, class := range reopenable {
		scope := "holder-dl-" + string(rune('a'+i))
		fanOutSeedScope(t, ctx, db, scope, "g1")
		id := fanOutSeedItem(t, ctx, db, scope, "g1")
		deadLetterSeed(t, ctx, db, id, class)
		got, err := store.ReopenHolderDetailed(ctx, scope, fanOutLiveOrigin)
		if err != nil {
			t.Fatalf("class %q: ReopenHolderDetailed() error = %v", class, err)
		}
		if got.Outcome != supplychaincore.FanOutDeadLetterReopened || got.PriorFailureClass != class {
			t.Fatalf("class %q: reopen = %+v, want dead_letter_reopened naming the prior class", class, got)
		}
		assertGenuineReopen(t, deadLetterRead(t, ctx, db, id))
	}
	for i, class := range recovery.UnsafeReplayFailureClasses() {
		scope := "holder-unsafe-" + string(rune('a'+i))
		fanOutSeedScope(t, ctx, db, scope, "g1")
		id := fanOutSeedItem(t, ctx, db, scope, "g1")
		deadLetterSeed(t, ctx, db, id, class)
		before := deadLetterRead(t, ctx, db, id)
		got, err := store.ReopenHolderDetailed(ctx, scope, fanOutLiveOrigin)
		if err != nil {
			t.Fatalf("unsafe class %q: ReopenHolderDetailed() error = %v", class, err)
		}
		if got.Outcome != supplychaincore.FanOutDeadLetterSkipped || got.PriorFailureClass != class {
			t.Fatalf("unsafe class %q: reopen = %+v, want dead_letter_skipped naming the class", class, got)
		}
		if after := deadLetterRead(t, ctx, db, id); !reflect.DeepEqual(after, before) {
			t.Fatalf("unsafe class %q: row changed:\nbefore %+v\nafter  %+v", class, before, after)
		}
	}
}

// TestReopenHolderLeavesOlderGenerationDeadLetterLive proves a dead letter at
// a superseded generation is never reopened: the active generation's own item
// is reopened (or enqueued when it has none) and the old dead letter stays
// exactly as it was.
func TestReopenHolderLeavesOlderGenerationDeadLetterLive(t *testing.T) {
	ctx, db := fanOutLiveDB(t)
	store := deadLetterStore(db)

	fanOutSeedScope(t, ctx, db, "holder-old-enqueue", "g2", "g1")
	oldOnly := fanOutSeedItem(t, ctx, db, "holder-old-enqueue", "g1")
	deadLetterSeed(t, ctx, db, oldOnly, supplychaincore.SupplyChainImpactFanOutFailureClass)
	before := deadLetterRead(t, ctx, db, oldOnly)
	got, err := store.ReopenHolderDetailed(ctx, "holder-old-enqueue", fanOutLiveOrigin)
	if err != nil || got.Outcome != supplychaincore.FanOutEnqueued {
		t.Fatalf("reopen = %+v err=%v, want enqueued at the active generation", got, err)
	}
	if after := deadLetterRead(t, ctx, db, oldOnly); !reflect.DeepEqual(after, before) {
		t.Fatalf("older-generation dead letter changed:\nbefore %+v\nafter  %+v", before, after)
	}
	if active := fanOutItem(t, ctx, db, fanOutItemID("holder-old-enqueue", fanOutGen("holder-old-enqueue", "g2"))); active.status != "pending" {
		t.Fatalf("active generation item = %+v, want a new pending item", active)
	}

	fanOutSeedScope(t, ctx, db, "holder-old-reopen", "g2", "g1")
	oldDead := fanOutSeedItem(t, ctx, db, "holder-old-reopen", "g1")
	deadLetterSeed(t, ctx, db, oldDead, supplychaincore.SupplyChainImpactFanOutFailureClass)
	active := fanOutSeedItem(t, ctx, db, "holder-old-reopen", "g2")
	if _, err := db.ExecContext(ctx, `UPDATE fact_work_items SET status='succeeded' WHERE work_item_id=$1`, active); err != nil {
		t.Fatal(err)
	}
	before = deadLetterRead(t, ctx, db, oldDead)
	got, err = store.ReopenHolderDetailed(ctx, "holder-old-reopen", fanOutLiveOrigin)
	if err != nil || got.Outcome != supplychaincore.FanOutReopened {
		t.Fatalf("reopen = %+v err=%v, want the active succeeded item reopened", got, err)
	}
	if after := deadLetterRead(t, ctx, db, oldDead); !reflect.DeepEqual(after, before) {
		t.Fatalf("older-generation dead letter changed:\nbefore %+v\nafter  %+v", before, after)
	}
}

// TestReopenHolderDeadLetterNextClaimIsGenuineLive proves the reopened dead letter's
// next claim is a full genuine pass: it carries no origin stamp (so it is not a
// one hop), keeps the owed marker and remaining holders, and a second fan-out
// that reaches it while it is queued adds no stamp either.
func TestReopenHolderDeadLetterNextClaimIsGenuineLive(t *testing.T) {
	ctx, db := fanOutLiveDB(t)
	store := deadLetterStore(db)
	fanOutSeedScope(t, ctx, db, "holder-genuine", "g1")
	id := fanOutSeedItem(t, ctx, db, "holder-genuine", "g1")
	deadLetterSeed(t, ctx, db, id, "reducer_handler_abandoned_at_shutdown")
	if got, err := store.ReopenHolderDetailed(ctx, "holder-genuine", fanOutLiveOrigin); err != nil || got.Outcome != supplychaincore.FanOutDeadLetterReopened {
		t.Fatalf("first reopen = %+v err=%v, want dead_letter_reopened", got, err)
	}
	second := supplychaincore.FanOutOrigin{SourceScopeID: "scope:second", SourceGenerationID: "g", SourceIntentID: "i2", CVEIDs: []string{"CVE-2026-0002"}}
	if got, err := store.ReopenHolderDetailed(ctx, "holder-genuine", second); err != nil || got.Outcome != supplychaincore.FanOutPendingNoop {
		t.Fatalf("second reopen = %+v err=%v, want pending_noop on the queued row", got, err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE fact_work_items SET visible_at = now() - interval '1 second' WHERE work_item_id = $1`, id); err != nil {
		t.Fatal(err)
	}
	intent, ok, err := fanOutLiveQueue(db).Claim(ctx)
	if err != nil || !ok || intent.IntentID != id {
		t.Fatalf("Claim() = %q ok=%v err=%v, want the reopened item", intent.IntentID, ok, err)
	}
	if _, stamped := intent.Payload[supplychaincore.PayloadKeyFanOutOrigin]; stamped {
		t.Fatalf("claimed payload carries a stamp %v: the pass would run as a one hop", intent.Payload[supplychaincore.PayloadKeyFanOutOrigin])
	}
	if intent.AttemptCount != 1 {
		t.Fatalf("claimed attempt = %d, want 1 (a fresh budget)", intent.AttemptCount)
	}
	if owed, _ := intent.Payload[supplychaincore.PayloadKeyFanOutOwed].(bool); !owed {
		t.Fatalf("claimed payload %v lost the owed marker", intent.Payload)
	}
	if remaining, _ := intent.Payload[supplychaincore.PayloadKeyFanOutRemaining].([]any); len(remaining) != 2 {
		t.Fatalf("claimed payload %v lost the remaining holders", intent.Payload)
	}
}

// TestSupplyChainImpactActiveDeadLettersCountsByClassLive proves the #11147
// gauge reading: dead letters at the active generation, by class, with the
// subset that still owes a fan-out; a dead letter at a superseded generation,
// another domain's dead letter, and a live item are not counted.
func TestSupplyChainImpactActiveDeadLettersCountsByClassLive(t *testing.T) {
	ctx, db := fanOutLiveDB(t)
	seed := func(scope, class string, owing bool) string {
		fanOutSeedScope(t, ctx, db, scope, "g2", "g1")
		id := fanOutSeedItem(t, ctx, db, scope, "g2")
		deadLetterSeed(t, ctx, db, id, class)
		if !owing {
			if _, err := db.ExecContext(ctx, `UPDATE fact_work_items SET payload = payload - 'fanout_owed' - 'fanout_remaining' WHERE work_item_id = $1`, id); err != nil {
				t.Fatal(err)
			}
		}
		return id
	}
	seed("gauge-a", supplychaincore.SupplyChainImpactFanOutFailureClass, true)
	seed("gauge-b", supplychaincore.SupplyChainImpactFanOutFailureClass, false)
	seed("gauge-c", "reducer_handler_timeout", false)
	seed("gauge-d", "", true)
	old := fanOutSeedItem(t, ctx, db, "gauge-a", "g1")
	deadLetterSeed(t, ctx, db, old, supplychaincore.SupplyChainImpactFanOutFailureClass)
	other := seed("gauge-e", "reducer_handler_timeout", true)
	if _, err := db.ExecContext(ctx, `UPDATE fact_work_items SET domain = 'code_call_materialization' WHERE work_item_id = $1`, other); err != nil {
		t.Fatal(err)
	}
	fanOutSeedScope(t, ctx, db, "gauge-live", "g1")
	_ = fanOutSeedItem(t, ctx, db, "gauge-live", "g1")

	got, err := postgres.NewQueueObserverStore(postgres.SQLDB{DB: db}).SupplyChainImpactActiveDeadLetters(ctx)
	if err != nil {
		t.Fatalf("SupplyChainImpactActiveDeadLetters() error = %v", err)
	}
	// Rows come in stored-class order, so the NULL class (labeled
	// unclassified) sorts first.
	want := []telemetry.SupplyChainImpactDeadLetterCount{
		{FailureClass: "unclassified", Total: 1, OwingFanOut: 1},
		{FailureClass: "reducer_handler_timeout", Total: 1, OwingFanOut: 0},
		{FailureClass: supplychaincore.SupplyChainImpactFanOutFailureClass, Total: 2, OwingFanOut: 1},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("counts = %+v\nwant     %+v", got, want)
	}
}
