// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"encoding/json"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts/supply/chain"
)

const (
	carrySplitLow = "CVE-2026-0300"
	carrySplitMid = "CVE-2026-0310"
	carrySplitB   = "GHSA-0300-b1"
	carrySplitS1  = "suppression-10408-low"
	carrySplitS2  = "suppression-10408-mid"
)

// seedCarrySplitPair seeds the split-orphan world (#10408, criterion 5d): one
// group {LOW, MID, B} with a lingering NVD-style record that still names the
// bridge id MID as its CVE after the bridge record is withdrawn. S1 names the
// surviving LOW id; S2 names the bridge MID id and is authored later, so it
// wins the pre-split finding.
func (c *rekeyProof) seedCarrySplitPair() {
	c.t.Helper()
	fanOutSeedScope(c.t, c.ctx, c.db, rekeyCommitter, "g1")
	c.upsert(
		c.envelope(rekeyCommitter, "g1", "fact:10408:slow", chain.VulnerabilityCVEFactKind, map[string]any{
			"advisory_id": carrySplitLow, "cve_id": carrySplitLow, "source": "ghsa", "cvss_score": 7.5, "aliases": []string{carrySplitLow, carrySplitMid},
		}),
		c.envelope(rekeyCommitter, "g1", "fact:10408:smid", chain.VulnerabilityCVEFactKind, map[string]any{
			"advisory_id": carrySplitMid, "cve_id": carrySplitMid, "source": "nvd", "cvss_score": 5.0, "aliases": []string{carrySplitMid, carrySplitLow, carrySplitB},
		}),
		c.envelope(rekeyCommitter, "g1", "fact:10408:sb", chain.VulnerabilityCVEFactKind, map[string]any{
			"advisory_id": carrySplitB, "source": "osv", "cvss_score": 4.0, "aliases": []string{carrySplitB, carrySplitMid},
		}),
		c.envelope(rekeyCommitter, "g1", "fact:10408:sling", chain.VulnerabilityCVEFactKind, map[string]any{
			"advisory_id": carrySplitB, "cve_id": carrySplitMid, "source": "osv", "cvss_score": 4.0, "aliases": []string{carrySplitB},
		}),
		c.affectedPackage(rekeyCommitter, "g1", "fact:10408:spkg", carrySplitLow, rekeyPackageA),
		c.affectedPackage(rekeyCommitter, "g1", "fact:10408:smpkg", carrySplitMid, rekeyPackageB),
		c.affectedPackage(rekeyCommitter, "g1", "fact:10408:sbpkg", carrySplitB, "pkg:npm/rekey-10408-c@1.0.0"),
		c.consumption(rekeyCommitter, "g1", "fact:10408:scons", rekeyPackageA, "repository:r_10408_c"),
		c.consumption(rekeyCommitter, "g1", "fact:10408:smcons", rekeyPackageB, "repository:r_10408_c"),
		c.consumption(rekeyCommitter, "g1", "fact:10408:sbcons", "pkg:npm/rekey-10408-c@1.0.0", "repository:r_10408_c"),
	)
	fanOutSeedItem(c.t, c.ctx, c.db, rekeyCommitter, "g1")
}

// rescanSplitBridge withdraws the bridge record and its affected-package
// row, and trims the cross-aliases: the group splits into {LOW} and {B} while
// the lingering record still names MID as its CVE.
func (c *rekeyProof) rescanSplitBridge() {
	c.t.Helper()
	c.copyGeneration(rekeyCommitter, "g1", "g2")
	if _, err := c.db.ExecContext(c.ctx, `
DELETE FROM fact_records WHERE scope_id = $1 AND generation_id = $2 AND fact_id IN ('fact:10408:smid:g2', 'fact:10408:smpkg:g2')`, rekeyCommitter, fanOutGen(rekeyCommitter, "g2")); err != nil {
		c.t.Fatalf("withdraw bridge record: %v", err)
	}
	trim := func(factID string, aliases []string) {
		encoded, err := json.Marshal(aliases)
		if err != nil {
			c.t.Fatalf("marshal aliases: %v", err)
		}
		if _, err := c.db.ExecContext(c.ctx, `
UPDATE fact_records SET payload = jsonb_set(payload, '{aliases}', $3::jsonb)
WHERE scope_id = $1 AND generation_id = $2 AND fact_id = $4`, rekeyCommitter, fanOutGen(rekeyCommitter, "g2"), string(encoded), factID+":g2"); err != nil {
			c.t.Fatalf("trim aliases: %v", err)
		}
	}
	trim("fact:10408:slow", []string{carrySplitLow})
	trim("fact:10408:sb", []string{carrySplitB})
}

// TestRekeyCarrySplitOrphanResurfacesLive is the criterion-5d proof: the
// split keeps the suppression-named LOW fragment suppressed, the other
// fragment is visible, and the orphaned-by-split sub-signal fires for the
// suppression whose dead bridge id matches no fragment anymore.
func TestRekeyCarrySplitOrphanResurfacesLive(t *testing.T) {
	c := newRekeyProof(t)
	c.seedCarrySplitPair()
	c.seedCarrySuppression(carrySplitS1, `"advisory_id":"`+carrySplitLow+`"`, "2026-10-31T11:00:00Z", "", "accepted_risk")
	c.seedCarrySuppression(carrySplitS2, `"cve_id":"`+carrySplitMid+`"`, "2026-10-31T11:30:00Z", "", "false_positive")
	c.activateCarryOperator()
	pre := c.drainCapturing()
	t.Logf("pre-split findings: %v", c.findingKeys())
	if got := c.suppressionStates(rekeyCommitter)[carrySplitLow]; got != "false_positive" {
		t.Fatalf("pre-split finding state = %q, want false_positive (later-authored bridge suppression wins)", got)
	}
	if got := c.suppressionIDs(rekeyCommitter)[carrySplitLow]; got != carrySplitS2 {
		t.Fatalf("pre-split suppression = %q, want %q (the bridge suppression carries the group)", got, carrySplitS2)
	}
	// Three package findings (one per affected package) all hide through the
	// bridge suppression's member anchor, so each counts as carried.
	if got := pre[rekeyCommitter+"@"+fanOutGen(rekeyCommitter, "g1")].SubSignals["suppression_carried_via_member"]; got != 3 {
		t.Fatalf("pre-split carried = %v, want 3 (one per package finding hidden through a member anchor)", got)
	}

	c.rescanSplitBridge()
	c.handled = nil
	post := c.drainCapturing()
	t.Logf("post-split findings: %v", c.findingKeys())
	t.Logf("post-split handled: %v", c.handled)
	if got := c.keysByScope(rekeyCommitter); len(got) != 2 || got[0] != carrySplitLow || got[1] != carrySplitB {
		t.Fatalf("committer keys = %v, want the split fragments [%s %s]", got, carrySplitLow, carrySplitB)
	}
	if got := c.suppressionStates(rekeyCommitter)[carrySplitLow]; got != "accepted_risk" {
		t.Fatalf("named fragment state = %q, want accepted_risk (S1 still suppresses it)", got)
	}
	if got := c.suppressionIDs(rekeyCommitter)[carrySplitLow]; got != carrySplitS1 {
		t.Fatalf("named fragment suppression = %q, want %q", got, carrySplitS1)
	}
	if got := c.suppressionStates(rekeyCommitter)[carrySplitB]; got != "active" {
		t.Fatalf("other fragment state = %q, want active (it resurfaced without the named id)", got)
	}
	split := post[rekeyCommitter+"@"+fanOutGen(rekeyCommitter, "g2")].SubSignals
	if got := split["suppression_orphaned_by_split"]; got != 1 {
		t.Fatalf("orphaned = %v, want 1 (the bridge suppression matches no fragment)", got)
	}
	if got := split["suppression_carried_via_member"]; got != 0 {
		t.Fatalf("carried = %v, want 0 (the surviving hide is canonical, not member-carried)", got)
	}
}
