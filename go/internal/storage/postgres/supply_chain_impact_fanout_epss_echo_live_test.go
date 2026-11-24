// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"fmt"
	"testing"
)

// drainEchoFleet plays the worker until nothing is claimable, up to maxClaims,
// like convergence.drain with a bound sized for a fleet.
func (c *convergence) drainEchoFleet(maxClaims int) {
	c.t.Helper()
	for i := 0; i < maxClaims; i++ {
		intent, ok, err := c.queue.Claim(c.ctx)
		if err != nil {
			c.t.Fatalf("claim: %v", err)
		}
		if !ok {
			return
		}
		c.handled = append(c.handled, fmt.Sprintf("%s#%d", intent.ScopeID, intent.AttemptCount))
		result, err := c.handler.Handle(c.ctx, intent)
		if err != nil {
			c.t.Fatalf("handle %s: %v", intent.ScopeID, err)
		}
		if err := c.queue.Ack(c.ctx, intent, result); err != nil {
			c.t.Fatalf("ack %s: %v", intent.ScopeID, err)
		}
	}
	c.t.Fatalf("queue still had work after %d claims", maxClaims)
}

// echoExtraHolders is how many extra scopes hold a copy of the CVE besides the
// source and the EPSS scope.
const echoExtraHolders = 20

// newEPSSEchoFleet is the convergence harness plus echoExtraHolders EPSS
// scopes that each hold a copy of the CVE, drained to quiescence, then the EPSS
// scope's score date moved to a newer vintage the way a new EPSS collection
// becomes visible before the EPSS scope's own pass runs.
func newEPSSEchoFleet(t *testing.T) *convergence {
	t.Helper()
	c := newConvergence(t, false)
	for i := 0; i < echoExtraHolders; i++ {
		scope := fmt.Sprintf("scope:10972:echo:%02d", i)
		c.seedNoRowHolder(scope)
		fanOutSeedItem(t, c.ctx, c.db, scope, "g1")
	}
	c.drainEchoFleet(40 * (echoExtraHolders + 2))
	if _, err := c.db.ExecContext(c.ctx, `UPDATE fact_records
SET payload = jsonb_set(payload, '{score_date}', '"2026-11-01"') WHERE fact_id = 'fact:9695:epss'`); err != nil {
		t.Fatal(err)
	}
	return c
}

// openSupplyChainImpactItems counts the findings items other than scope that
// are not settled.
func openSupplyChainImpactItems(t *testing.T, c *convergence, except string) int {
	t.Helper()
	var open int
	if err := c.db.QueryRowContext(c.ctx, `SELECT count(*) FROM fact_work_items
WHERE domain = 'supply_chain_impact' AND scope_id <> $1 AND status <> 'succeeded'`, except).Scan(&open); err != nil {
		t.Fatal(err)
	}
	return open
}

// TestEPSSDateOnlyRerunOfANonEnrichmentScopeReopensNobodyLive is the D7
// contract the arbiter sketched (#10972 reality audit, risk 4): a
// non-enrichment scope's genuine rerun that only sees a newer EPSS score date
// must not originate a fan-out. The EPSS scope's own pass is the documented
// origin of that change (HOLDER_FANOUT.md "Enrichment refresh"), so an echo
// from every other holder only re-adds work: on QA one EPSS refresh fanned out
// once per copy.
//
// Expected on main: RED. The content digest includes the EPSS-derived fields
// (writer_changed.go contentDigestOf; provenance.selected_epss_score_date), so
// the source's rerun reports a change and reopens every other holder of the
// CVE. This is a real re-add defect, not a fixture artifact. D7 is not ruled
// yet; the test states the intended contract and must not be weakened to pass.
func TestEPSSDateOnlyRerunOfANonEnrichmentScopeReopensNobodyLive(t *testing.T) {
	c := newEPSSEchoFleet(t)
	before := c.gateFindingDigests(convergenceSource)
	watermark := c.gateStagingWatermark()
	c.rerun(convergenceSource)
	src := c.claimTarget(convergenceSource)
	result, err := c.handler.Handle(c.ctx, src)
	if err != nil {
		t.Fatalf("source rerun: %v", err)
	}
	if err := c.queue.Ack(c.ctx, src, result); err != nil {
		t.Fatal(err)
	}
	reached := result.SubSignals["fanout_reopened"] + result.SubSignals["fanout_replay_flagged"] + result.SubSignals["fanout_stamp_widened"]
	if reached != 0 || openSupplyChainImpactItems(t, c, convergenceSource) != 0 {
		t.Fatalf("an EPSS-date-only rerun of the NVD scope reached %v holders and left %d items open (subsignals %v), want 0 and 0: the enrichment scope's own pass carries that change",
			reached, openSupplyChainImpactItems(t, c, convergenceSource), result.SubSignals)
	}
	// The gate is on the fan-out origin only: the rerun still rewrites its own
	// copy on the full digest and stages its winners dirty mark.
	after := c.gateFindingDigests(convergenceSource)
	if changed := gateChangedKeys(before, after); len(changed) == 0 {
		t.Errorf("the source's own copy kept its full content digest (%v): the newer EPSS date must still be written to it", after)
	}
	if staged := c.gateStagedFactMarks(watermark); staged == 0 {
		t.Errorf("the source's rerun staged no winners dirty fact mark: the row write and the dirty capture stay on the full digest")
	}
}

// TestEPSSScopesOwnPassStillReopensEveryHolderLive is the twin: the EPSS
// scope's own pass on the newer vintage must still reopen every other holder
// of the CVE, so a fix for the echo cannot drop the real propagation path.
//
// Expected on main: GREEN.
func TestEPSSScopesOwnPassStillReopensEveryHolderLive(t *testing.T) {
	c := newEPSSEchoFleet(t)
	c.rerun(convergenceHolder)
	holder := c.claimTarget(convergenceHolder)
	result, err := c.handler.Handle(c.ctx, holder)
	if err != nil {
		t.Fatalf("EPSS scope pass: %v", err)
	}
	if err := c.queue.Ack(c.ctx, holder, result); err != nil {
		t.Fatal(err)
	}
	want := float64(echoExtraHolders + 1) // the source plus every extra holder
	if got := result.SubSignals["fanout_reopened"]; got != want {
		t.Fatalf("EPSS scope reopened %v holders (subsignals %v), want %v", got, result.SubSignals, want)
	}
	c.handled = nil
	c.drainEchoFleet(4 * (echoExtraHolders + 2))
	if runs := len(c.handled); runs != echoExtraHolders+1 {
		t.Fatalf("drain ran %d passes (%v), want exactly one rerun per reopened holder", runs, c.handled)
	}
}
