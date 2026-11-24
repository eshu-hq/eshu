// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"encoding/json"
	"fmt"
	"testing"
)

// drainBoundHolders is the continuation list size: over the 256 reopen cap, so
// a drain that issued reopen statements instead of prefiltering would need a
// second claim and would store a new list.
const drainBoundHolders = 300

// TestFanOutDrainOfAnAllPendingListRunsOnceAndRefillsNothingLive is the
// continuation-drain bound (#10972 reality audit, risk 3). The source's item
// carries the QA shape: fanout_owed plus a 300-entry fanout_remaining list,
// every holder pending with an unstamped item (covered: its next pass is full),
// not yet visible to a claim. The source's own facts are unchanged since its
// last pass. One drain claim must settle it: exactly one source execution, no
// reopen statement on any holder (row version unchanged), the list and the
// owed marker gone, the source acknowledged to succeeded with nothing left to
// claim. A refill (a new list, a reopen, or a second source claim) fails.
//
// Expected on main: GREEN, through the #10972 PR-2 queued-holder prefilter
// (it drops covered pending holders before the cap). A RED means drains
// re-add work again.
func TestFanOutDrainOfAnAllPendingListRunsOnceAndRefillsNothingLive(t *testing.T) {
	c := newConvergence(t, false)
	c.drain()
	holders := make([]string, 0, drainBoundHolders)
	ids := make(map[string]string, drainBoundHolders)
	for i := 0; i < drainBoundHolders; i++ {
		scope := fmt.Sprintf("scope:10972:drain:%03d", i)
		fanOutSeedScope(t, c.ctx, c.db, scope, "g1")
		ids[scope] = fanOutSeedItem(t, c.ctx, c.db, scope, "g1")
		holders = append(holders, scope)
	}
	// Pending but not claimable during the drain: the claim must pick the source.
	if _, err := c.db.ExecContext(c.ctx, `UPDATE fact_work_items SET visible_at = now() + interval '1 hour'
WHERE scope_id LIKE 'scope:10972:drain:%'`); err != nil {
		t.Fatal(err)
	}
	before := make(map[string]string, len(ids))
	for scope, id := range ids {
		before[scope] = workItemXmin(t, c.ctx, c.db, id)
	}
	list, err := json.Marshal(holders)
	if err != nil {
		t.Fatal(err)
	}
	c.rerun(convergenceSource)
	if _, err := c.db.ExecContext(c.ctx, `UPDATE fact_work_items
SET payload = payload || jsonb_build_object('fanout_remaining', $2::jsonb, 'fanout_owed', true)
WHERE scope_id = $1 AND domain = 'supply_chain_impact' AND status = 'pending'`, convergenceSource, string(list)); err != nil {
		t.Fatal(err)
	}

	src := c.claimTarget(convergenceSource)
	result, err := c.handler.Handle(c.ctx, src)
	if err != nil {
		t.Fatalf("drain claim: %v", err)
	}
	if err := c.queue.Ack(c.ctx, src, result); err != nil {
		t.Fatal(err)
	}
	if got := result.SubSignals["fanout_reopened"] + result.SubSignals["fanout_replay_flagged"] + result.SubSignals["fanout_stamp_widened"]; got != 0 {
		t.Fatalf("drain reopened, flagged or widened %v holders (subsignals %v), want 0: every holder was pending and covered", got, result.SubSignals)
	}
	if got := result.SubSignals["fanout_prefiltered_pending"]; got != drainBoundHolders {
		t.Fatalf("fanout_prefiltered_pending = %v, want %d", got, drainBoundHolders)
	}
	for scope, id := range ids {
		if after := workItemXmin(t, c.ctx, c.db, id); after != before[scope] {
			t.Fatalf("holder %s item xmin %s -> %s: the drain wrote a holder it had to drop", scope, before[scope], after)
		}
	}
	var remaining, owed bool
	var status string
	if err := c.db.QueryRowContext(c.ctx, `
SELECT status,
       CASE WHEN jsonb_typeof(payload->'fanout_remaining') = 'array' THEN jsonb_array_length(payload->'fanout_remaining') > 0 ELSE false END,
       COALESCE(payload->>'fanout_owed', '') = 'true'
FROM fact_work_items WHERE work_item_id = $1`, src.IntentID).Scan(&status, &remaining, &owed); err != nil {
		t.Fatal(err)
	}
	if status != "succeeded" || remaining || owed {
		t.Fatalf("source after the drain: status=%s remaining=%v owed=%v, want succeeded with no list and no marker", status, remaining, owed)
	}
	if _, ok, err := c.queue.Claim(c.ctx); err != nil || ok {
		t.Fatalf("claim after the drain = %v %v, want nothing claimable: the drain re-added work", ok, err)
	}
}
