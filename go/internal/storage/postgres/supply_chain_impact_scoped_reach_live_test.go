// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu-sdk/factschema"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/facts/supply/chain"
	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
)

// The D3 guard differential of the #10972 re-run contract (arbiter RANK2
// section 3). Each case runs a stamped one-hop rerun of one holder and then a
// genuine full pass of the same holder on the same database. A sound rerun
// leaves the full pass nothing to change. These cases hold on main, where a
// rerun that reaches nothing runs full: they pin the contract any narrowing
// of the rerun (D3) must keep. The cases D3 itself turns green (an inert
// rerun, an unproven-stage holder) are not here; they land with D3.

// reachGen is the generation every reach fixture seeds and the source uses.
func reachGen(scope string) string { return fanOutGen(scope, "g1") }

// seedReachScope creates a scope with its envelopes and a pending item.
func (c *convergence) seedReachScope(scope string, envelopes ...facts.Envelope) {
	c.t.Helper()
	fanOutSeedScope(c.t, c.ctx, c.db, scope, "g1")
	c.upsert(envelopes...)
	fanOutSeedItem(c.t, c.ctx, c.db, scope, "g1")
}

// reachAdvisory returns a CVE record and one affected-package fact per
// package, all under advisoryID with cveID.
func (c *convergence) reachAdvisory(scope, advisoryID, cveID string, packages ...string) []facts.Envelope {
	out := []facts.Envelope{c.envelope(scope, reachGen(scope), "fact:"+scope+":"+advisoryID+":cve", chain.VulnerabilityCVEFactKind, map[string]any{
		"advisory_id": advisoryID, "cve_id": cveID, "source": "nvd", "cvss_score": 7.5, "status": "Analyzed",
	})}
	for _, pkg := range packages {
		out = append(out, c.envelope(scope, reachGen(scope), "fact:"+scope+":"+advisoryID+":"+pkg, chain.VulnerabilityAffectedPackageFactKind, map[string]any{
			"advisory_id": advisoryID, "cve_id": cveID, "package_id": "pkg:npm/" + pkg + "@1.0.0", "ecosystem": "npm",
			"package_name": pkg, "affected_versions": []string{"1.0.0"}, "fixed_versions": []string{"1.1.0"},
		}))
	}
	return out
}

// reachConsumption returns a consumption of one package by one repository.
func (c *convergence) reachConsumption(scope, pkg string) facts.Envelope {
	return c.envelope(scope, reachGen(scope), "fact:"+scope+":consumes:"+pkg, factschema.FactKindReducerPackageConsumptionCorrelation, map[string]any{
		"package_id": "pkg:npm/" + pkg + "@1.0.0", "relationship_kind": "consumption", "repository_id": "repository:r_" + pkg,
		"dependency_range": "1.0.0", "canonical_writes": 1, "evidence_fact_ids": []string{"fact:" + pkg + ":manifest"},
	})
}

// addSourceAdvisory lands a new advisory in the source scope's active
// generation, the change the stamp names.
func (c *convergence) addSourceAdvisory(advisoryID, cveID string, packages ...string) {
	c.t.Helper()
	c.upsert(c.reachAdvisory(convergenceSource, advisoryID, cveID, packages...)...)
}

// findingRows snapshots a scope's finding rows by what a pass asserts: the
// tombstone state and the writer's own content digest
// (provenance.content_digest, core/writer_changed.go:41-54). Not xmin and not
// the raw payload: the writer upserts every row a pass derives
// (factwrite.BatchInsertVersionedQuery has no content-equality guard), and
// the payload carries the pass's intent id and cause, so both move on every
// rewrite of an identical finding.
func (c *convergence) findingRows(scope string) map[string]string {
	c.t.Helper()
	rows := c.queryStringPairs(`
SELECT fact_id, is_tombstone::text || ':' || COALESCE(payload #>> '{provenance,content_digest}', '')
FROM fact_records WHERE scope_id = $1 AND fact_kind = $2`, scope, facts.ReducerSupplyChainImpactFindingFactKind)
	for id, value := range rows {
		// A row with no stored digest would compare equal to any other
		// digest-less row, so the guard would be blind to it.
		if strings.HasSuffix(value, ":") {
			c.t.Fatalf("finding row %s in %s carries no provenance.content_digest: the content comparison cannot judge it", id, scope)
		}
	}
	return rows
}

// activeFinding reports whether a scope holds an active row for an id.
func (c *convergence) activeFinding(scope, id string) bool {
	c.t.Helper()
	var n int
	if err := c.db.QueryRowContext(c.ctx, `
SELECT count(*) FROM fact_records WHERE scope_id = $1 AND fact_kind = $2 AND NOT is_tombstone
  AND (payload->>'cve_id' = $3 OR payload->>'advisory_id' = $3)`, scope, facts.ReducerSupplyChainImpactFindingFactKind, id).Scan(&n); err != nil {
		c.t.Fatalf("count findings: %v", err)
	}
	return n > 0
}

// rowIDFor returns the fact id of a scope's finding row for an id.
func (c *convergence) rowIDFor(scope, id string) string {
	c.t.Helper()
	var fact string
	if err := c.db.QueryRowContext(c.ctx, `
SELECT fact_id FROM fact_records WHERE scope_id = $1 AND fact_kind = $2 AND payload->>'cve_id' = $3 LIMIT 1`,
		scope, facts.ReducerSupplyChainImpactFindingFactKind, id).Scan(&fact); err != nil {
		c.t.Fatalf("find row for %s in %s: %v", id, scope, err)
	}
	return fact
}

// stampedRerun reopens a holder with a stamp naming ids and runs its one-hop
// rerun to its ack.
func (c *convergence) stampedRerun(meter quiescePassMeter, scope string, ids ...string) quiescePass {
	c.t.Helper()
	origin := supplychaincore.FanOutOrigin{SourceScopeID: convergenceSource, SourceGenerationID: reachGen(convergenceSource), SourceIntentID: "d3-differential", CVEIDs: ids}
	if _, err := c.fanOut.ReopenHolderDetailed(c.ctx, scope, origin); err != nil {
		c.t.Fatalf("reopen %s: %v", scope, err)
	}
	return c.handleCounted(meter, c.raceClaim(scope))
}

// fullRerunChanges runs a genuine full pass of the holder and returns the
// finding rows it changed.
func (c *convergence) fullRerunChanges(meter quiescePassMeter, scope string) []string {
	c.t.Helper()
	before := c.findingRows(scope)
	c.rerun(scope)
	c.handleCounted(meter, c.raceClaim(scope))
	return changedKeys(before, c.findingRows(scope))
}

// newReach is the convergence harness with the pass meter, the given holders
// seeded, and every first pass settled.
func newReach(t *testing.T, seed func(c *convergence)) (*convergence, quiescePassMeter) {
	t.Helper()
	c := newConvergence(t, false)
	meter := c.instrumentPassModes()
	seed(c)
	c.drainCounted(meter, quiesceDrainLimit)
	return c, meter
}

// wantNoFullPassChange is the differential: the full pass after the rerun
// changes nothing.
func wantNoFullPassChange(t *testing.T, c *convergence, meter quiescePassMeter, scope string) {
	t.Helper()
	if changed := c.fullRerunChanges(meter, scope); len(changed) > 0 {
		t.Errorf("a full pass after the stamped rerun still changed %v (tombstone or content digest): the rerun derived less than a full pass for the stamp", changed)
	}
}

// TestScopedReachDifferentialGainsAFindingLive is case 1: the holder has no
// row for the stamped id, and the source's new advisory names a package the
// holder consumes. The rerun must write the row.
func TestScopedReachDifferentialGainsAFindingLive(t *testing.T) {
	const holder = "scope:10972:d3:gain"
	c, meter := newReach(t, func(c *convergence) { c.seedReachScope(holder, c.reachConsumption(holder, "d3-gain")) })
	c.addSourceAdvisory("CVE-2026-73001", "CVE-2026-73001", "d3-gain")
	pass := c.stampedRerun(meter, holder, "CVE-2026-73001")
	if !c.activeFinding(holder, "CVE-2026-73001") {
		t.Errorf("rerun (mode %s) wrote no row for CVE-2026-73001: a holder that gains a finding for the stamp lost it", pass.mode)
	}
	wantNoFullPassChange(t, c, meter, holder)
}

// TestScopedReachDifferentialGainsThroughAnAdvisoryIDLive is case 2: the stamp
// spells the advisory id, and the CVE id differs. The rerun must still reach
// the holder through the advisory arm.
func TestScopedReachDifferentialGainsThroughAnAdvisoryIDLive(t *testing.T) {
	const holder = "scope:10972:d3:alias"
	c, meter := newReach(t, func(c *convergence) { c.seedReachScope(holder, c.reachConsumption(holder, "d3-alias")) })
	c.addSourceAdvisory("GHSA-d3a1-d3a1-d3a1", "CVE-2026-73002", "d3-alias")
	pass := c.stampedRerun(meter, holder, "GHSA-d3a1-d3a1-d3a1")
	if !c.activeFinding(holder, "CVE-2026-73002") {
		t.Errorf("rerun (mode %s) wrote no row for the advisory GHSA-d3a1-d3a1-d3a1 (CVE-2026-73002): an alias spelling was missed", pass.mode)
	}
	wantNoFullPassChange(t, c, meter, holder)
}

// TestScopedReachDifferentialLoopReachedRunsFullLive is case 4: the holder's
// own envelopes never name the stamped package. It arrives through the
// until-stable loop: a linker advisory in another scope names the holder's
// package and the stamped one, and the stamped package's consumer lives in a
// third scope. The rerun is never inert: it must reach the holder through the
// stamp-side closure or the stage exclusion, never through the closed bit alone.
func TestScopedReachDifferentialLoopReachedRunsFullLive(t *testing.T) {
	const holder, linker, consumer = "scope:10972:d3:loop", "scope:10972:d3:linker", "scope:10972:d3:consumer"
	c, meter := newReach(t, func(c *convergence) {
		c.seedReachScope(holder, append(c.reachAdvisory(holder, "CVE-2026-73040", "CVE-2026-73040", "d3-loop-own"), c.reachConsumption(holder, "d3-loop-own"))...)
		c.seedReachScope(linker, c.reachAdvisory(linker, "CVE-2026-73042", "CVE-2026-73042", "d3-loop-own", "d3-loop-far")...)
		c.seedReachScope(consumer, c.reachConsumption(consumer, "d3-loop-far"))
	})
	c.addSourceAdvisory("CVE-2026-73041", "CVE-2026-73041", "d3-loop-far")
	pass := c.stampedRerun(meter, holder, "CVE-2026-73041")
	if pass.mode == "scoped_inert" {
		t.Errorf("rerun ended inert for a holder the loop reaches: an expansion-reached holder must run full")
	}
	wantNoFullPassChange(t, c, meter, holder)
	if !c.activeFinding(holder, "CVE-2026-73041") {
		t.Fatal("fixture: a full pass of the holder derives no row for CVE-2026-73041, so the loop does not reach it and the case proves nothing")
	}
}

// TestScopedReachDifferentialRetractsInsideTheStampOnlyLive is case 5: the
// stamped id is no longer derivable and is retracted; rows outside the stamp
// keep their state and content, a stale one included (the byte-identical xmin
// half is TestUpdateFanOutStateMarkerKeepsRowsOutsideTheStampLive). It pins D2: under a fresh
// full-pass marker the rerun stays scoped, where the #10415 fallback used to
// run it full and reap the stale row too.
func TestScopedReachDifferentialRetractsInsideTheStampOnlyLive(t *testing.T) {
	const holder = "scope:10972:d3:retract"
	c, meter := newReach(t, func(c *convergence) {
		envelopes := append(c.reachAdvisory(holder, "CVE-2026-73051", "CVE-2026-73051", "d3-keep"), c.reachConsumption(holder, "d3-keep"))
		c.addSourceAdvisory("CVE-2026-73050", "CVE-2026-73050", "d3-drop")
		c.addSourceAdvisory("CVE-2026-73052", "CVE-2026-73052", "d3-stale")
		c.seedReachScope(holder, append(envelopes, c.reachConsumption(holder, "d3-drop"), c.reachConsumption(holder, "d3-stale"))...)
	})
	for _, id := range []string{"CVE-2026-73050", "CVE-2026-73051", "CVE-2026-73052"} {
		if !c.activeFinding(holder, id) {
			t.Fatalf("fixture: setup left no row for %s", id)
		}
	}
	if _, err := c.db.ExecContext(c.ctx, `UPDATE fact_records SET is_tombstone = true
WHERE scope_id = $1 AND fact_kind = $2 AND payload->>'cve_id' = ANY($3)`, convergenceSource, chain.VulnerabilityAffectedPackageFactKind,
		[]string{"CVE-2026-73050", "CVE-2026-73052"}); err != nil {
		t.Fatal(err)
	}
	outside := map[string]string{}
	for _, id := range []string{"CVE-2026-73051", "CVE-2026-73052"} {
		row := c.rowIDFor(holder, id)
		outside[row] = c.findingRows(holder)[row]
	}
	pass := c.stampedRerun(meter, holder, "CVE-2026-73050")
	if c.activeFinding(holder, "CVE-2026-73050") {
		t.Errorf("rerun (mode %s) kept CVE-2026-73050 active: a stamped id the holder no longer derives must be retracted", pass.mode)
	}
	after := c.findingRows(holder)
	for row, was := range outside {
		if after[row] != was {
			t.Errorf("rerun (mode %s) changed %s outside the stamp (%s -> %s): a row outside the stamp must keep its state and content", pass.mode, row, was, after[row])
		}
	}
	changed := c.fullRerunChanges(meter, holder)
	if want := []string{c.rowIDFor(holder, "CVE-2026-73052")}; fmt.Sprint(changed) != fmt.Sprint(want) {
		t.Errorf("full pass after the rerun changed %v, want only the stale row %v (the drift the reap bound leaves)", changed, want)
	}
}

// TestScopedReachDifferentialPartialEvidenceKeepsTheFlagLive is case 6: a
// truncated scoped load retracts nothing and does not clear the replay flag
// (core/fanout_stamp.go:140), so the item reruns and converges.
func TestScopedReachDifferentialPartialEvidenceKeepsTheFlagLive(t *testing.T) {
	const holder = "scope:10972:d3:partial"
	c, meter := newReach(t, func(c *convergence) {
		c.addSourceAdvisory("CVE-2026-73060", "CVE-2026-73060", "d3-partial")
		c.seedReachScope(holder, c.reachConsumption(holder, "d3-partial"))
	})
	origin := supplychaincore.FanOutOrigin{SourceScopeID: convergenceSource, SourceGenerationID: reachGen(convergenceSource), SourceIntentID: "d3-partial", CVEIDs: []string{"CVE-2026-73060"}}
	if _, err := c.fanOut.ReopenHolderDetailed(c.ctx, holder, origin); err != nil {
		t.Fatal(err)
	}
	intent := c.raceClaim(holder)
	if out, err := reopenOutcome(c.ctx, c.fanOut, holder, origin); err != nil || out != supplychaincore.FanOutReplayFlagged {
		t.Fatalf("second change while the rerun is claimed = %q %v, want replay_flagged", out, err)
	}
	before := c.findingRows(holder)
	truncated := c.handler
	truncated.EvidenceBudget = 1
	result, err := truncated.Handle(c.ctx, intent)
	if err != nil {
		t.Fatal(err)
	}
	if result.SubSignals["evidence_truncated_budget"] != 1 && result.SubSignals["evidence_truncated_rounds"] != 1 {
		t.Fatalf("fixture: the load was not truncated (sub-signals %v), so the case proves nothing", result.SubSignals)
	}
	if err := c.queue.Ack(c.ctx, intent, result); err != nil {
		t.Fatal(err)
	}
	after := c.findingRows(holder)
	for row, was := range before {
		wasTomb, _, _ := strings.Cut(was, ":")
		nowTomb, _, _ := strings.Cut(after[row], ":")
		if now := after[row]; nowTomb != wasTomb {
			t.Errorf("truncated rerun changed the tombstone state of %s (%s -> %s): a partial load must retract nothing", row, was, now)
		}
	}
	if st := fanOutItem(t, c.ctx, c.db, intent.IntentID); st.status != "pending" {
		t.Errorf("item after the truncated rerun = %+v, want pending: the replay flag must survive a partial load", st)
	}
	c.drainCounted(meter, quiesceDrainLimit)
	if !c.activeFinding(holder, "CVE-2026-73060") || c.openSupplyChainItems() != 0 {
		t.Errorf("the holder did not converge after the truncated rerun: row=%v open=%d", c.activeFinding(holder, "CVE-2026-73060"), c.openSupplyChainItems())
	}
}
