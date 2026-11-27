// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"fmt"
	"testing"
	"time"
)

// Scope-remainder horizon proofs for #11145. A bounded keyed tick that applies
// part of a scope row converts the unapplied remainder to fact rows and
// deletes the scope row in the same transaction. The watermark horizon is the
// earliest captured_at of the residual set minus 1us, so the converted rows
// must keep the scope row's captured_at: a fresh conversion-time capture moves
// the horizon past the original scope mark while the remainder's winners are
// still stale, and the in-transaction winners_watermark wake then reopens a
// waiter that depends on that activation before its winners are right.

// scopeRemainderScope writes a bumped g2 (priority + 1000) for scope, drops
// the fact marks so only the activation's scope row will be queued, activates
// g2, and returns the scope mark's capture time.
func scopeRemainderScope(h *keyedHarness, scope, prefix string, triples int) time.Time {
	h.t.Helper()
	h.addGeneration(scope, scope+"/g2", "pending")
	h.write(scope, scope+"/g2", chunkedFindings(prefix, triples, 1, 1000)...)
	h.exec(`DELETE FROM supply_chain_impact_winners_dirty_log WHERE kind = 'fact'`)
	h.exec(`DELETE FROM supply_chain_impact_winners_dirty_staging WHERE kind = 'fact'`)
	h.activate(scope, scope+"/g1", scope+"/g2")
	var mark time.Time
	if err := h.db.QueryRowContext(h.ctx, `SELECT min(captured_at) FROM (
    SELECT captured_at FROM supply_chain_impact_winners_dirty_log WHERE kind = 'scope' AND scope_id = $1
    UNION ALL
    SELECT captured_at FROM supply_chain_impact_winners_dirty_staging WHERE kind = 'scope' AND scope_id = $1) AS s`,
		scope).Scan(&mark); err != nil {
		h.t.Fatalf("read scope mark of %s: %v", scope, err)
	}
	return mark.UTC()
}

// scopeRemainderBase seeds g1 of every scope (prefixes[i], triples[i]) and
// commits the initial full resweep over all of them with empty queues.
func scopeRemainderBase(h *keyedHarness, scopes []string, prefixes []string, triples []int) {
	h.t.Helper()
	for i, scope := range scopes {
		h.seedScope(scope, scope+"/g1")
		h.write(scope, scope+"/g1", chunkedFindings(prefixes[i], triples[i], 1, 0)...)
	}
	h.exec(`DELETE FROM supply_chain_impact_winners_dirty_log`)
	h.exec(`DELETE FROM supply_chain_impact_winners_dirty_staging`)
	if _, err := h.store.ResweepWinners(h.ctx); err != nil {
		h.t.Fatalf("initial full resweep: %v", err)
	}
}

// staleWinners counts stored winners of prefix that still show g1's priority
// (g2 bumped every one by 1000).
func staleWinners(h *keyedHarness, prefix string) int {
	h.t.Helper()
	return h.count(`SELECT count(*) FROM supply_chain_impact_canonical_winners
WHERE package_id LIKE $1 AND priority_score < 1000`, "pkg:npm/"+prefix+"-%")
}

// parkWinnersWaiter parks one reducer work item on winners_watermark with the
// given enqueue time, the shape the advisory handler's Fail writes: a
// retrying queue row plus its wait row. The tick's in-transaction wake
// reopens it once the committed watermark reaches enqueuedAt.
func parkWinnersWaiter(h *keyedHarness, id, scope, generation string, enqueuedAt time.Time) {
	h.t.Helper()
	h.exec(`INSERT INTO fact_work_items (work_item_id, scope_id, generation_id, stage, domain,
    conflict_domain, conflict_key, status, attempt_count, payload, created_at, updated_at)
VALUES ($1, $2, $3, 'reducer', 'advisory_group_projection', 'scope', $1, 'retrying', 1,
    jsonb_build_object('entity_key', $1::text), $4, $4)`, id, scope, generation, enqueuedAt)
	h.exec(`INSERT INTO reducer_waiting_items (work_item_id, waits_for_kind, waits_for_key, scope_id,
    generation_id, domain, failure_class, first_deferred_at, updated_at)
VALUES ($1, 'winners_watermark', jsonb_build_object('enqueued_at', $2::text), $3, $4,
    'advisory_group_projection', 'advisory_group_winners_watermark_not_ready', $5, $5)`,
		id, enqueuedAt.Format(time.RFC3339Nano), scope, generation, enqueuedAt)
}

// waiterParked reports whether the waiter's wait row is still in place.
func waiterParked(h *keyedHarness, id string) bool {
	h.t.Helper()
	return h.count(`SELECT count(*) FROM reducer_waiting_items WHERE work_item_id = $1`, id) == 1
}

// scopeRemainderWaiterAfter sleeps past mark, reads the DB clock as the
// waiter's enqueue time, and sleeps again, so mark < enqueuedAt < the tick.
func scopeRemainderWaiterAfter(h *keyedHarness, mark time.Time) time.Time {
	h.t.Helper()
	time.Sleep(15 * time.Millisecond)
	at := horizonDBNow(h)
	time.Sleep(15 * time.Millisecond)
	if !at.After(mark) {
		h.t.Fatalf("waiter enqueue %s is not after scope mark %s", at.Format(time.RFC3339Nano), mark.Format(time.RFC3339Nano))
	}
	return at
}

// assertScopeHeld fails when a tick left the watermark at or past mark while
// winners of that scope are stale, or woke the waiter keyed after mark.
func assertScopeHeld(h *keyedHarness, label, prefix string, mark time.Time, waiter string) {
	h.t.Helper()
	stale := staleWinners(h, prefix)
	if stale == 0 {
		return
	}
	if wm := chunkedWatermark(h); !wm.Before(mark) {
		h.t.Errorf("%s: watermark %s is at or past scope mark %s while %d winners of the scope are stale",
			label, wm.Format(time.RFC3339Nano), mark.Format(time.RFC3339Nano), stale)
	}
	if waiter != "" && !waiterParked(h, waiter) {
		h.t.Errorf("%s: waiter %s enqueued after scope mark %s woke while %d winners of the scope are stale",
			label, waiter, mark.Format(time.RFC3339Nano), stale)
	}
}

// scopeRemainderMarkSQL inserts one fact mark for the g2 finding of package
// $3 into table, deriving the five-tuple the way the keyed identifier
// expressions do. The row source is the supports table: the scopes seed
// through the production writer, which upserts supports only and never writes
// finding fact rows, so fact_records holds no row to derive the mark from
// (#10880). The payload reads stay verbatim, so the staged mark matches the
// identifier the tick expands from the same support.
func scopeRemainderMarkSQL(table string) string {
	return `INSERT INTO ` + table + `
    (kind, source, package_id, repository_id, subject_digest, c0, advisory_id)
SELECT 'fact', 'fact_upsert', COALESCE(f.payload->>'package_id', ''), COALESCE(f.payload->>'repository_id', ''),
       COALESCE(f.payload->>'subject_digest', ''),
       COALESCE(NULLIF(f.payload->>'cve_id', ''), NULLIF(f.payload->>'advisory_id', ''), ''),
       COALESCE(f.payload->>'advisory_id', '')
FROM supply_chain_impact_finding_supports AS f
WHERE f.scope_id = $1 AND f.generation_id = $2 AND f.payload->>'package_id' = $3`
}

// TestSupplyChainImpactWinnersResweepKeyedScopeRemainderHoldsWatermarkLive is
// the lane-11119 M5b reproduction (#11145): eight winners re-ranked by one
// activation, three identifiers per tick. The first tick applies three and
// converts five; every tick until the scope's winners are all applied must
// keep the watermark under the scope mark and leave a waiter enqueued after
// the mark parked. Once drained, the waiter wakes and the winners equal a
// fresh full recompute.
func TestSupplyChainImpactWinnersResweepKeyedScopeRemainderHoldsWatermarkLive(t *testing.T) {
	h := newKeyedHarness(t, "#11145 scope-remainder watermark proof")
	scope := "vuln-intel://11145/scope"
	scopeRemainderBase(h, []string{scope}, []string{"act"}, []int{8})
	before := chunkedWatermark(h)
	mark := scopeRemainderScope(h, scope, "act", 8)
	waiterAt := scopeRemainderWaiterAfter(h, mark)
	parkWinnersWaiter(h, "wait-11145-one", scope, scope+"/g2", waiterAt)

	h.store.ChunkMaxIdentifiers = 3
	first := chunkedTick(h)
	if !first.Bounded {
		t.Fatalf("first tick = %+v, want Bounded", first)
	}
	if first.ScopeRemainderConverted != 5 {
		t.Errorf("first tick ScopeRemainderConverted = %d, want 5", first.ScopeRemainderConverted)
	}
	if n := h.count(`SELECT count(*) FROM supply_chain_impact_winners_dirty_log WHERE kind = 'fact'`); n != 5 {
		t.Fatalf("converted remainder rows = %d, want 5", n)
	}
	if n := h.count(`SELECT count(*) FROM supply_chain_impact_winners_dirty_log WHERE kind = 'scope'`); n != 0 {
		t.Fatalf("scope rows left = %d, want 0 (deleted with the conversion)", n)
	}
	if n := h.count(`SELECT count(*) FROM supply_chain_impact_winners_dirty_log WHERE kind = 'fact' AND captured_at <> $1`, mark); n != 0 {
		t.Errorf("%d converted rows do not carry the scope mark's captured_at %s", n, mark.Format(time.RFC3339Nano))
	}
	if wm := chunkedWatermark(h); !wm.After(before) {
		t.Errorf("bounded tick left the watermark at %s, want it advanced past %s", wm.Format(time.RFC3339Nano), before.Format(time.RFC3339Nano))
	}
	assertScopeHeld(h, "tick 1", "act", mark, "wait-11145-one")
	for i := 2; staleWinners(h, "act") > 0; i++ {
		if i > 10 {
			t.Fatalf("scope winners still stale after 10 ticks")
		}
		chunkedTick(h)
		assertScopeHeld(h, fmt.Sprintf("tick %d", i), "act", mark, "wait-11145-one")
	}
	chunkedDrain(h)
	if waiterParked(h, "wait-11145-one") {
		t.Errorf("waiter still parked after the scope's winners all applied and the log drained (watermark %s)",
			chunkedWatermark(h).Format(time.RFC3339Nano))
	}
	h.assertParity("scope-remainder drain")
}

// TestSupplyChainImpactWinnersResweepKeyedScopeRemainderEdgesLive covers the
// edges of #11145: several scope rows, a remainder that spans more than one
// bounded tick, a queued fact row for a remainder key captured after the
// scope mark (LEAST keeps the scope mark), and a staged mark for a remainder
// key that commits after the conversion.
func TestSupplyChainImpactWinnersResweepKeyedScopeRemainderEdgesLive(t *testing.T) {
	t.Run("several scope rows", func(t *testing.T) {
		h := newKeyedHarness(t, "#11145 several scope rows proof")
		s1, s2 := "vuln-intel://11145/s1", "vuln-intel://11145/s2"
		scopeRemainderBase(h, []string{s1, s2}, []string{"one", "two"}, []int{5, 5})
		m1 := scopeRemainderScope(h, s1, "one", 5)
		waiterAt := scopeRemainderWaiterAfter(h, m1)
		m2 := scopeRemainderScope(h, s2, "two", 5)
		parkWinnersWaiter(h, "wait-11145-s1", s1, s1+"/g2", waiterAt)
		h.store.ChunkMaxIdentifiers = 3
		for i := 1; staleWinners(h, "one")+staleWinners(h, "two") > 0; i++ {
			if i > 12 {
				t.Fatalf("scope winners still stale after 12 ticks")
			}
			chunkedTick(h)
			label := fmt.Sprintf("tick %d", i)
			assertScopeHeld(h, label+" s1", "one", m1, "wait-11145-s1")
			assertScopeHeld(h, label+" s2", "two", m2, "")
		}
		chunkedDrain(h)
		if waiterParked(h, "wait-11145-s1") {
			t.Error("waiter still parked after both scopes drained")
		}
		h.assertParity("several scope rows drain")
	})

	t.Run("remainder spans more than one bounded tick", func(t *testing.T) {
		h := newKeyedHarness(t, "#11145 multi-tick remainder proof")
		scope := "vuln-intel://11145/multi"
		scopeRemainderBase(h, []string{scope}, []string{"mt"}, []int{9})
		mark := scopeRemainderScope(h, scope, "mt", 9)
		waiterAt := scopeRemainderWaiterAfter(h, mark)
		parkWinnersWaiter(h, "wait-11145-multi", scope, scope+"/g2", waiterAt)
		h.store.ChunkMaxIdentifiers = 2
		ticks := 0
		for staleWinners(h, "mt") > 0 {
			ticks++
			if ticks > 12 {
				t.Fatalf("scope winners still stale after 12 ticks")
			}
			chunkedTick(h)
			assertScopeHeld(h, fmt.Sprintf("tick %d", ticks), "mt", mark, "wait-11145-multi")
		}
		if ticks < 4 {
			t.Fatalf("drained in %d ticks, want the remainder to span at least 3 ticks after conversion", ticks)
		}
		chunkedDrain(h)
		if waiterParked(h, "wait-11145-multi") {
			t.Error("waiter still parked after the remainder drained")
		}
		h.assertParity("multi-tick remainder drain")
	})

	t.Run("queued fact row captured after the scope mark", func(t *testing.T) {
		h := newKeyedHarness(t, "#11145 conflict LEAST proof")
		scope := "vuln-intel://11145/least"
		scopeRemainderBase(h, []string{scope}, []string{"lst"}, []int{8})
		mark := scopeRemainderScope(h, scope, "lst", 8)
		// Fold the staged scope mark into the log the way the collapse does,
		// so the scope row's seq precedes the queued fact row's and the chunk
		// tie-break (age, then seq, then tuple) keeps lst-07 out of the first
		// chunk of three.
		h.exec(`INSERT INTO supply_chain_impact_winners_dirty_log (kind, source, scope_id, generation_id, captured_at, dirty_version)
SELECT kind, source, scope_id, generation_id, min(captured_at), count(*)
FROM supply_chain_impact_winners_dirty_staging WHERE kind = 'scope' GROUP BY kind, source, scope_id, generation_id`)
		h.exec(`DELETE FROM supply_chain_impact_winners_dirty_staging WHERE kind = 'scope'`)
		time.Sleep(15 * time.Millisecond)
		// A queued fact row for the last key in tuple order captured after the
		// scope mark: the convert conflicts on it and must keep the earlier
		// capture, the scope mark.
		h.exec(scopeRemainderMarkSQL("supply_chain_impact_winners_dirty_log"), scope, scope+"/g2", "pkg:npm/lst-07")
		var later time.Time
		if err := h.db.QueryRowContext(h.ctx, `SELECT captured_at FROM supply_chain_impact_winners_dirty_log
WHERE kind = 'fact' AND package_id = 'pkg:npm/lst-07'`).Scan(&later); err != nil {
			t.Fatalf("read queued fact row: %v", err)
		}
		if !later.After(mark) {
			t.Fatalf("queued fact row capture %s is not after the scope mark %s", later.UTC().Format(time.RFC3339Nano), mark.Format(time.RFC3339Nano))
		}
		h.store.ChunkMaxIdentifiers = 3
		first := chunkedTick(h)
		if !first.Bounded {
			t.Fatalf("first tick = %+v, want Bounded", first)
		}
		if first.ScopeRemainderConverted != 5 {
			t.Errorf("first tick ScopeRemainderConverted = %d, want 5 (four inserted, one merged)", first.ScopeRemainderConverted)
		}
		var got time.Time
		var version int64
		if err := h.db.QueryRowContext(h.ctx, `SELECT captured_at, dirty_version FROM supply_chain_impact_winners_dirty_log
WHERE kind = 'fact' AND package_id = 'pkg:npm/lst-07'`).Scan(&got, &version); err != nil {
			t.Fatalf("read merged row: %v", err)
		}
		if !got.Equal(mark) {
			t.Errorf("merged row captured_at = %s, want the earlier scope mark %s", got.UTC().Format(time.RFC3339Nano), mark.Format(time.RFC3339Nano))
		}
		if version != 2 {
			t.Errorf("merged row dirty_version = %d, want 2 (the queued mark plus the converted one)", version)
		}
		assertScopeHeld(h, "tick 1", "lst", mark, "")
		chunkedDrain(h)
		h.assertParity("conflict LEAST drain")
	})

	t.Run("staged mark commits after the conversion", func(t *testing.T) {
		h := newKeyedHarness(t, "#11145 late staged mark proof")
		scope := "vuln-intel://11145/late"
		scopeRemainderBase(h, []string{scope}, []string{"lte"}, []int{8})
		mark := scopeRemainderScope(h, scope, "lte", 8)
		waiterAt := scopeRemainderWaiterAfter(h, mark)
		parkWinnersWaiter(h, "wait-11145-late", scope, scope+"/g2", waiterAt)
		late, err := h.db.BeginTx(h.ctx, nil)
		if err != nil {
			t.Fatalf("begin late writer: %v", err)
		}
		defer func() { _ = late.Rollback() }()
		if _, err := late.ExecContext(h.ctx, scopeRemainderMarkSQL("supply_chain_impact_winners_dirty_staging"),
			scope, scope+"/g2", "pkg:npm/lte-07"); err != nil {
			t.Fatalf("stage late mark: %v", err)
		}
		h.store.ChunkMaxIdentifiers = 3
		if first := chunkedTick(h); !first.Bounded {
			t.Fatalf("first tick = %+v, want Bounded", first)
		}
		assertScopeHeld(h, "tick 1 (late mark uncommitted)", "lte", mark, "wait-11145-late")
		if err := late.Commit(); err != nil {
			t.Fatalf("commit late mark: %v", err)
		}
		for i := 2; staleWinners(h, "lte") > 0; i++ {
			if i > 10 {
				t.Fatalf("scope winners still stale after 10 ticks")
			}
			chunkedTick(h)
			assertScopeHeld(h, fmt.Sprintf("tick %d", i), "lte", mark, "wait-11145-late")
		}
		chunkedDrain(h)
		if waiterParked(h, "wait-11145-late") {
			t.Error("waiter still parked after the remainder and the late mark drained")
		}
		if n := h.stagingDepth(); n != 0 {
			t.Errorf("staging still holds %d rows after the drain", n)
		}
		h.assertParity("late staged mark drain")
	})
}
