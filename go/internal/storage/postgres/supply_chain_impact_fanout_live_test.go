// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"context"
	"database/sql"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

const (
	fanOutLiveOperatorScope = "operator:vulnerability_suppressions"
	fanOutLiveOwner         = "fanout-worker"
)

var fanOutLiveOrigin = supplychaincore.FanOutOrigin{
	SourceScopeID: "vuln-intel://nvd/cve", SourceGenerationID: "gen-src", SourceIntentID: "intent-src",
}

func fanOutLiveDB(t *testing.T) (context.Context, *sql.DB) {
	t.Helper()
	// Route the proof through the family switch explicitly; the disposable
	// database itself resolves inside the opener.
	_ = supplyChainImpactLiveDSN(t, "#9695 holder fan-out proof")
	ctx, cancel := context.WithTimeout(context.Background(), 400*time.Second)
	t.Cleanup(cancel)
	db := openAdvisorySuppressionIsolatedDB(t)
	db.SetMaxOpenConns(24)
	t.Cleanup(func() { _ = db.Close() })
	return ctx, db
}

func fanOutLiveStore(db *sql.DB) postgres.SupplyChainImpactFanOutStore {
	return postgres.SupplyChainImpactFanOutStore{
		Beginner: postgres.SQLDB{DB: db}, DB: postgres.SQLDB{DB: db}, LeaseOwner: fanOutLiveOwner,
	}
}

func fanOutLiveQueue(db *sql.DB) postgres.ReducerQueue {
	queue := postgres.NewReducerQueue(postgres.SQLDB{DB: db}, fanOutLiveOwner, time.Minute)
	queue.RetryDelay = time.Second
	queue.MaxAttempts = 5
	queue.ClaimDomains = []reducercontract.Domain{reducercontract.DomainSupplyChainImpact}
	return queue
}

// fanOutGen is the stored generation id for a short name; generation ids are
// unique across scopes, so the scope is part of it.
func fanOutGen(scope, generation string) string { return scope + "/" + generation }

// fanOutSeedScope creates a scope whose active generation is active, plus any
// older generations as superseded.
func fanOutSeedScope(t *testing.T, ctx context.Context, db *sql.DB, scope, active string, older ...string) {
	t.Helper()
	now := time.Date(2026, 10, 31, 0, 0, 0, 0, time.UTC)
	if _, err := db.ExecContext(ctx, `
INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind,
  partition_key, observed_at, ingested_at, status, payload)
VALUES ($1, 'vulnerability_intelligence', 'synthetic', $1, 'synthetic', $1, $2, $2, 'active', '{}'::jsonb)`, scope, now); err != nil {
		t.Fatalf("seed scope %s: %v", scope, err)
	}
	for _, short := range older {
		id := fanOutGen(scope, short)
		if _, err := db.ExecContext(ctx, `
INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at, payload)
VALUES ($1, $2, 'synthetic', $3, $3, 'superseded', $3, '{}'::jsonb)`, id, scope, now); err != nil {
			t.Fatalf("seed generation %s: %v", id, err)
		}
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at, payload)
VALUES ($1, $2, 'synthetic', $3, $3, 'active', $3, '{}'::jsonb)`, fanOutGen(scope, active), scope, now); err != nil {
		t.Fatalf("seed active generation %s: %v", active, err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE ingestion_scopes SET active_generation_id = $2 WHERE scope_id = $1`, scope, fanOutGen(scope, active)); err != nil {
		t.Fatalf("activate %s: %v", active, err)
	}
}

// fanOutItemID mirrors the queue's stable work-item identity
// (scope, generation, domain, entity key with ":" and "/" replaced by "_"), so a
// seeded item and the one the enqueue path would create collide the way a real
// one does.
func fanOutItemID(scope, generation string) string {
	clean := func(part string) string { return strings.NewReplacer(":", "_", "/", "_").Replace(part) }
	return "reducer_" + strings.Join([]string{
		clean(scope), clean(generation), "supply_chain_impact", clean("supply_chain_impact:" + scope),
	}, "_")
}

// fanOutSeedItem inserts a pending supply_chain_impact item and returns its id.
func fanOutSeedItem(t *testing.T, ctx context.Context, db *sql.DB, scope, generation string) string {
	t.Helper()
	return fanOutSeedItemAt(t, ctx, db, scope, fanOutGen(scope, generation))
}

// fanOutSeedItemAt inserts a pending item at an exact stored generation id.
func fanOutSeedItemAt(t *testing.T, ctx context.Context, db *sql.DB, scope, generation string) string {
	t.Helper()
	id := fanOutItemID(scope, generation)
	if _, err := db.ExecContext(ctx, `
INSERT INTO fact_work_items (work_item_id, scope_id, generation_id, stage, domain, conflict_domain, conflict_key,
  status, attempt_count, payload, created_at, updated_at)
VALUES ($1, $2, $3, 'reducer', 'supply_chain_impact', 'scope', $2, 'pending', 0,
  jsonb_build_object('entity_key', $1::text, 'reason', 'fanout proof', 'fact_id', $1::text, 'source_system', 'vuln'),
  now() - interval '1 minute', now() - interval '1 minute')`, id, scope, generation); err != nil {
		t.Fatalf("seed item %s: %v", id, err)
	}
	return id
}

// fanOutSeedFinding inserts one finding row for a CVE.
func fanOutSeedFinding(t *testing.T, ctx context.Context, db *sql.DB, scope, generation, cve string, tombstone bool) {
	t.Helper()
	generation = fanOutGen(scope, generation)
	id := fmt.Sprintf("finding_%s_%s_%v", generation, cve, tombstone)
	if _, err := db.ExecContext(ctx, `
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, schema_version,
  source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload)
VALUES ($1, $2, $3, $4, $1, '1.0.0', 'synthetic', $1, now(), now(), $5, jsonb_build_object('cve_id', $6::text))`,
		id, scope, generation, facts.ReducerSupplyChainImpactFindingFactKind, tombstone, cve); err != nil {
		t.Fatalf("seed finding %s: %v", id, err)
	}
}

type fanOutItemState struct {
	status    string
	flag      bool
	attempts  int
	reopened  bool
	originSrc string
}

func fanOutItem(t *testing.T, ctx context.Context, db *sql.DB, id string) fanOutItemState {
	t.Helper()
	var (
		state    fanOutItemState
		reopened sql.NullTime
		origin   sql.NullString
	)
	if err := db.QueryRowContext(ctx, `
SELECT status, cross_scope_replay_required, attempt_count, reopened_at, payload->'fanout_origin'->>'scope_id'
FROM fact_work_items WHERE work_item_id = $1`, id).Scan(&state.status, &state.flag, &state.attempts, &reopened, &origin); err != nil {
		t.Fatalf("read item %s: %v", id, err)
	}
	state.reopened, state.originSrc = reopened.Valid, origin.String
	return state
}

// TestFindHolderScopesReturnsOperatorSuppressionScopeLive pins the holder set: the
// scopes that hold an ACTIVE finding row at their ACTIVE generation for a
// requested CVE, the operator suppression scope included, and never the asking
// scope, a tombstoned copy, a superseded generation's copy, or another CVE.
func TestFindHolderScopesReturnsOperatorSuppressionScopeLive(t *testing.T) {
	ctx, db := fanOutLiveDB(t)
	for _, scope := range []string{"own", "holder-a", fanOutLiveOperatorScope, "tombstoned", "superseded", "other-cve"} {
		fanOutSeedScope(t, ctx, db, scope, "gen-2", "gen-1")
	}
	fanOutSeedFinding(t, ctx, db, "own", "gen-2", "CVE-2026-0001", false)
	fanOutSeedFinding(t, ctx, db, "holder-a", "gen-2", "CVE-2026-0001", false)
	fanOutSeedFinding(t, ctx, db, "holder-a", "gen-2", "CVE-2026-0002", false) // a second row must not duplicate the scope
	fanOutSeedFinding(t, ctx, db, fanOutLiveOperatorScope, "gen-2", "CVE-2026-0001", false)
	fanOutSeedFinding(t, ctx, db, "tombstoned", "gen-2", "CVE-2026-0001", true)
	fanOutSeedFinding(t, ctx, db, "superseded", "gen-1", "CVE-2026-0001", false)
	fanOutSeedFinding(t, ctx, db, "other-cve", "gen-2", "CVE-2026-0009", false)

	got, err := fanOutLiveStore(db).FindHolderScopes(ctx, "own", []string{"CVE-2026-0001", "CVE-2026-0002"})
	if err != nil {
		t.Fatalf("FindHolderScopes() error = %v", err)
	}
	want := []string{"holder-a", fanOutLiveOperatorScope}
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("holders = %v, want %v (operator scope in; own, tombstoned, superseded, other CVE out)", got, want)
	}
}

// TestReopenHolderResolvesOperatorScopeActiveGenerationLive proves the reopen
// targets the scope's CURRENT active generation: the operator scope has a stale
// pending item at an old generation and a succeeded item at the active one, and
// only the active one moves.
func TestReopenHolderResolvesOperatorScopeActiveGenerationLive(t *testing.T) {
	ctx, db := fanOutLiveDB(t)
	fanOutSeedScope(t, ctx, db, fanOutLiveOperatorScope, "gen-2", "gen-1")
	stale := fanOutSeedItem(t, ctx, db, fanOutLiveOperatorScope, "gen-1")
	active := fanOutSeedItem(t, ctx, db, fanOutLiveOperatorScope, "gen-2")
	if _, err := db.ExecContext(ctx, `UPDATE fact_work_items SET status='succeeded', lease_owner=NULL, claim_until=NULL, visible_at=NULL WHERE work_item_id = $1`, active); err != nil {
		t.Fatalf("settle active item: %v", err)
	}

	outcome, err := fanOutLiveStore(db).ReopenHolder(ctx, fanOutLiveOperatorScope, fanOutLiveOrigin)
	if err != nil {
		t.Fatalf("ReopenHolder() error = %v", err)
	}
	if outcome != supplychaincore.FanOutReopened {
		t.Fatalf("outcome = %q, want reopened", outcome)
	}
	if got := fanOutItem(t, ctx, db, active); got.status != "pending" || got.attempts != 0 || !got.reopened || got.originSrc != fanOutLiveOrigin.SourceScopeID {
		t.Fatalf("active item = %+v, want pending, attempts 0, reopened, stamped", got)
	}
	if got := fanOutItem(t, ctx, db, stale); got.reopened || got.originSrc != "" {
		t.Fatalf("stale generation item = %+v, want untouched", got)
	}
}

// TestReopenHolderTransitionsLive walks the queue states a holder can be in. The
// statement must reopen a succeeded item, mark a claimed one so its own
// acknowledgement returns it to pending once, leave a queued one alone without
// writing a row version, enqueue when the active generation has no item, and
// leave a dead letter to the operator.
func TestReopenHolderTransitionsLive(t *testing.T) {
	t.Run("succeeded is reopened", func(t *testing.T) {
		ctx, db := fanOutLiveDB(t)
		store := fanOutLiveStore(db)
		fanOutSeedScope(t, ctx, db, "holder-s", "g1")
		id := fanOutSeedItem(t, ctx, db, "holder-s", "g1")
		// A holder reopened many times must not creep toward its attempt budget:
		// the reopen starts a fresh cycle at attempt 0.
		if _, err := db.ExecContext(ctx, `UPDATE fact_work_items SET status='succeeded', attempt_count=3 WHERE work_item_id=$1`, id); err != nil {
			t.Fatal(err)
		}
		outcome, err := store.ReopenHolder(ctx, "holder-s", fanOutLiveOrigin)
		if err != nil || outcome != supplychaincore.FanOutReopened {
			t.Fatalf("outcome=%q err=%v, want reopened", outcome, err)
		}
		if got := fanOutItem(t, ctx, db, id); got.status != "pending" || got.attempts != 0 || !got.reopened || got.originSrc == "" {
			t.Fatalf("item = %+v", got)
		}
	})

	t.Run("claimed is flagged and reruns exactly once", func(t *testing.T) {
		ctx, db := fanOutLiveDB(t)
		store := fanOutLiveStore(db)
		fanOutSeedScope(t, ctx, db, "holder-c", "g1")
		id := fanOutSeedItem(t, ctx, db, "holder-c", "g1")
		queue := fanOutLiveQueue(db)
		intent, ok, err := queue.Claim(ctx)
		if err != nil || !ok || intent.IntentID != id {
			t.Fatalf("claim = %v %v %v", intent.IntentID, ok, err)
		}
		outcome, err := store.ReopenHolder(ctx, "holder-c", fanOutLiveOrigin)
		if err != nil || outcome != supplychaincore.FanOutReplayFlagged {
			t.Fatalf("outcome=%q err=%v, want replay_flagged", outcome, err)
		}
		if got := fanOutItem(t, ctx, db, id); got.status != "claimed" || !got.flag {
			t.Fatalf("flagged item = %+v", got)
		}
		again, err := store.ReopenHolder(ctx, "holder-c", fanOutLiveOrigin)
		if err != nil || again != supplychaincore.FanOutPendingNoop {
			t.Fatalf("second outcome=%q err=%v, want pending_noop while flagged", again, err)
		}
		if err := queue.Ack(ctx, intent, reducercontract.Result{}); err != nil {
			t.Fatalf("ack: %v", err)
		}
		if got := fanOutItem(t, ctx, db, id); got.status != "pending" || got.flag || got.attempts != 0 {
			t.Fatalf("after its own ack = %+v, want pending, flag cleared", got)
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
	})

	t.Run("pending is untouched and writes no row version", func(t *testing.T) {
		ctx, db := fanOutLiveDB(t)
		store := fanOutLiveStore(db)
		fanOutSeedScope(t, ctx, db, "holder-p", "g1")
		id := fanOutSeedItem(t, ctx, db, "holder-p", "g1")
		var before string
		if err := db.QueryRowContext(ctx, `SELECT xmin::text FROM fact_work_items WHERE work_item_id=$1`, id).Scan(&before); err != nil {
			t.Fatal(err)
		}
		outcome, err := store.ReopenHolder(ctx, "holder-p", fanOutLiveOrigin)
		if err != nil || outcome != supplychaincore.FanOutPendingNoop {
			t.Fatalf("outcome=%q err=%v, want pending_noop", outcome, err)
		}
		var after string
		if err := db.QueryRowContext(ctx, `SELECT xmin::text FROM fact_work_items WHERE work_item_id=$1`, id).Scan(&after); err != nil {
			t.Fatal(err)
		}
		if before != after {
			t.Fatalf("xmin %s -> %s: a queued holder was rewritten", before, after)
		}
	})

	t.Run("no item at the active generation is enqueued", func(t *testing.T) {
		ctx, db := fanOutLiveDB(t)
		store := fanOutLiveStore(db)
		fanOutSeedScope(t, ctx, db, "holder-e", "g2", "g1")
		stale := fanOutSeedItem(t, ctx, db, "holder-e", "g1")
		outcome, err := store.ReopenHolder(ctx, "holder-e", fanOutLiveOrigin)
		if err != nil || outcome != supplychaincore.FanOutEnqueued {
			t.Fatalf("outcome=%q err=%v, want enqueued", outcome, err)
		}
		var status, origin string
		if err := db.QueryRowContext(ctx, `SELECT status, COALESCE(payload->'fanout_origin'->>'scope_id','') FROM fact_work_items
			WHERE scope_id='holder-e' AND generation_id='holder-e/g2' AND domain='supply_chain_impact'`).Scan(&status, &origin); err != nil {
			t.Fatalf("read enqueued item: %v", err)
		}
		if status != "pending" || origin != fanOutLiveOrigin.SourceScopeID {
			t.Fatalf("enqueued item status=%q origin=%q, want pending and stamped", status, origin)
		}
		if got := fanOutItem(t, ctx, db, stale); got.reopened || got.originSrc != "" {
			t.Fatalf("stale item = %+v, want untouched", got)
		}
		again, err := store.ReopenHolder(ctx, "holder-e", fanOutLiveOrigin)
		if err != nil || again != supplychaincore.FanOutPendingNoop {
			t.Fatalf("second outcome=%q err=%v, want pending_noop", again, err)
		}
	})

	t.Run("dead letter is left to the operator", func(t *testing.T) {
		ctx, db := fanOutLiveDB(t)
		store := fanOutLiveStore(db)
		fanOutSeedScope(t, ctx, db, "holder-d", "g1")
		id := fanOutSeedItem(t, ctx, db, "holder-d", "g1")
		if _, err := db.ExecContext(ctx, `UPDATE fact_work_items SET status='dead_letter', failure_class='x', failure_message='y' WHERE work_item_id=$1`, id); err != nil {
			t.Fatal(err)
		}
		outcome, err := store.ReopenHolder(ctx, "holder-d", fanOutLiveOrigin)
		if err != nil || outcome != supplychaincore.FanOutPendingNoop {
			t.Fatalf("outcome=%q err=%v, want pending_noop", outcome, err)
		}
		if got := fanOutItem(t, ctx, db, id); got.status != "dead_letter" {
			t.Fatalf("item = %+v, want the dead letter unchanged", got)
		}
	})

	t.Run("a later fan-out overwrites the stamp", func(t *testing.T) {
		ctx, db := fanOutLiveDB(t)
		store := fanOutLiveStore(db)
		fanOutSeedScope(t, ctx, db, "holder-o", "g1")
		id := fanOutSeedItem(t, ctx, db, "holder-o", "g1")
		for _, src := range []string{"first", "second"} {
			if _, err := db.ExecContext(ctx, `UPDATE fact_work_items SET status='succeeded' WHERE work_item_id=$1`, id); err != nil {
				t.Fatal(err)
			}
			origin := fanOutLiveOrigin
			origin.SourceScopeID = src
			if _, err := store.ReopenHolder(ctx, "holder-o", origin); err != nil {
				t.Fatal(err)
			}
		}
		if got := fanOutItem(t, ctx, db, id); got.originSrc != "second" {
			t.Fatalf("stamp = %q, want the latest origin", got.originSrc)
		}
	})
}

// TestReopenHolderWaitsForARunningAckAndReopensLive proves the CASE runs on the
// locked row: a fan-out that arrives while the holder's own acknowledgement is
// uncommitted waits, then sees succeeded and reopens rather than flagging a
// row that has already finished.
func TestReopenHolderWaitsForARunningAckAndReopensLive(t *testing.T) {
	ctx, db := fanOutLiveDB(t)
	fanOutSeedScope(t, ctx, db, "holder-l", "g1")
	id := fanOutSeedItem(t, ctx, db, "holder-l", "g1")
	queue := fanOutLiveQueue(db)
	if _, ok, err := queue.Claim(ctx); err != nil || !ok {
		t.Fatalf("claim = %v, %v", ok, err)
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tx.Rollback() })
	if _, err := tx.ExecContext(ctx, `UPDATE fact_work_items SET status='succeeded', lease_owner=NULL, claim_until=NULL, visible_at=NULL WHERE work_item_id=$1`, id); err != nil {
		t.Fatal(err)
	}
	type result struct {
		outcome supplychaincore.FanOutOutcome
		err     error
		waited  time.Duration
	}
	done := make(chan result, 1)
	go func() {
		started := time.Now()
		outcome, err := fanOutLiveStore(db).ReopenHolder(ctx, "holder-l", fanOutLiveOrigin)
		done <- result{outcome, err, time.Since(started)}
	}()
	time.Sleep(400 * time.Millisecond)
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	got := <-done
	if got.err != nil || got.outcome != supplychaincore.FanOutReopened {
		t.Fatalf("outcome=%q err=%v, want reopened after the ack committed", got.outcome, got.err)
	}
	if got.waited < 300*time.Millisecond {
		t.Fatalf("fan-out returned after %v: it did not wait for the row lock", got.waited)
	}
}
