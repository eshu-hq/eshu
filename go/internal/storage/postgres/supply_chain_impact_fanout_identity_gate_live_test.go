// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"fmt"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts/supply/chain"
)

// The D7 identity gate (#10972, arbiter ruling arbiter-pr4-d7.md (b)): a pass
// originates a fan-out for a row only when the row's identity digest changed,
// unless the pass's own generation holds EPSS or KEV facts. These proofs pin
// the KEV half of the RED/GREEN pair, a mixed change, and a scope that holds
// both advisory and EPSS facts. The EPSS pair is
// supply_chain_impact_fanout_epss_echo_live_test.go.

// newIdentityGateFleet is the convergence harness plus echoExtraHolders EPSS
// scopes that each hold a copy of the CVE, drained to quiescence.
func newIdentityGateFleet(t *testing.T, seed func(c *convergence)) *convergence {
	t.Helper()
	c := newConvergence(t, false)
	for i := 0; i < echoExtraHolders; i++ {
		scope := fmt.Sprintf("scope:10972:gate:%02d", i)
		c.seedNoRowHolder(scope)
		fanOutSeedItem(t, c.ctx, c.db, scope, "g1")
	}
	if seed != nil {
		seed(c)
	}
	c.drainEchoFleet(40 * (echoExtraHolders + 2))
	return c
}

// runGatePass reruns scope's item and runs its pass to the acknowledgement.
func (c *convergence) runGatePass(scope string) map[string]float64 {
	c.t.Helper()
	c.rerun(scope)
	intent := c.claimTarget(scope)
	result, err := c.handler.Handle(c.ctx, intent)
	if err != nil {
		c.t.Fatalf("pass %s: %v", scope, err)
	}
	if err := c.queue.Ack(c.ctx, intent, result); err != nil {
		c.t.Fatal(err)
	}
	return result.SubSignals
}

func gateReached(signals map[string]float64) float64 {
	return signals["fanout_reopened"] + signals["fanout_replay_flagged"] + signals["fanout_stamp_widened"]
}

// TestFanOutIdentityGateKEVListingSeenByANonEnrichmentScopeReopensNobodyLive is
// the KEV twin of the EPSS echo (RED on main): a first KEV listing becomes
// visible in a scope with no pending pass, and the NVD scope's rerun derives
// known_exploited on its copy. That is an enrichment-only change, so the
// rerun rewrites its own row but must reopen nobody.
func TestFanOutIdentityGateKEVListingSeenByANonEnrichmentScopeReopensNobodyLive(t *testing.T) {
	c := newIdentityGateFleet(t, nil)
	const kevScope = "scope:10972:gate:kev-unrun"
	fanOutSeedScope(t, c.ctx, c.db, kevScope, "g1")
	c.upsert(c.envelope(kevScope, fanOutGen(kevScope, "g1"), "fact:10972:gate:kev", chain.VulnerabilityKnownExploitedFactKind, map[string]any{
		"cve_id": convergenceCVE, "date_added": "2026-11-01",
	}))
	before := c.gateFindingDigests(convergenceSource)
	signals := c.runGatePass(convergenceSource)
	if changed := gateChangedKeys(before, c.gateFindingDigests(convergenceSource)); len(changed) == 0 {
		t.Fatal("fixture: the NVD scope's copy did not change with the KEV listing, so the case proves nothing")
	}
	if reached, open := gateReached(signals), openSupplyChainImpactItems(t, c, convergenceSource); reached != 0 || open != 0 {
		t.Fatalf("a KEV-only rerun of the NVD scope reached %v holders and left %d items open (subsignals %v), want 0 and 0: the KEV scope's own pass carries that change", reached, open, signals)
	}
}

// TestFanOutIdentityGateKEVScopesOwnPassReopensEveryHolderLive is the GREEN
// twin: the KEV scope's own first pass is an enrichment origin, so it fans out
// on the full digest and reopens every other holder of the CVE.
func TestFanOutIdentityGateKEVScopesOwnPassReopensEveryHolderLive(t *testing.T) {
	c := newIdentityGateFleet(t, nil)
	c.seedKEVScope()
	signals := c.runGatePass(enrichment9878KEVScope)
	if want := float64(echoExtraHolders + 2); signals["fanout_reopened"] != want {
		t.Fatalf("KEV scope reopened %v holders (subsignals %v), want %v (the NVD scope, the EPSS scope, and every extra holder)", signals["fanout_reopened"], signals, want)
	}
}

// TestFanOutIdentityGateMixedChangeStillFansOutLive: one non-enrichment pass
// sees both a newer EPSS date and a real advisory change (the record is
// rejected). The identity digest changed, so it fans out for the advisory ids.
func TestFanOutIdentityGateMixedChangeStillFansOutLive(t *testing.T) {
	c := newEPSSEchoFleet(t)
	c.setSourceStatus("Rejected")
	signals := c.runGatePass(convergenceSource)
	if want := float64(echoExtraHolders + 1); gateReached(signals) != want {
		t.Fatalf("a rejection plus an EPSS date move reached %v holders (subsignals %v), want %v: a real advisory change must fan out", gateReached(signals), signals, want)
	}
}

// TestFanOutIdentityGateScopeHoldingEPSSFansOutOnTheFullDigestLive: a scope
// whose own generation holds advisory and EPSS facts is an enrichment origin.
// An EPSS-only change in it fans out on the full digest, as before D7.
func TestFanOutIdentityGateScopeHoldingEPSSFansOutOnTheFullDigestLive(t *testing.T) {
	c := newIdentityGateFleet(t, func(c *convergence) {
		c.upsert(c.envelope(convergenceSource, fanOutGen(convergenceSource, "g1"), "fact:10972:gate:source-epss", chain.VulnerabilityEPSSScoreFactKind, map[string]any{
			"cve_id": convergenceCVE, "probability": "0.5", "percentile": "0.9", "score_date": "2026-10-31",
		}))
	})
	if _, err := c.db.ExecContext(c.ctx, `UPDATE fact_records
SET payload = jsonb_set(payload, '{score_date}', '"2026-11-02"') WHERE fact_id = 'fact:10972:gate:source-epss'`); err != nil {
		t.Fatal(err)
	}
	signals := c.runGatePass(convergenceSource)
	if want := float64(echoExtraHolders + 1); gateReached(signals) != want {
		t.Fatalf("an EPSS-only change in a scope holding EPSS facts reached %v holders (subsignals %v), want %v: an enrichment origin fans out on the full digest", gateReached(signals), signals, want)
	}
}
