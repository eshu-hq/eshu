// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"fmt"
	"testing"

	"github.com/eshu-hq/eshu-sdk/factschema"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/facts/supply/chain"
	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
)

// d2MultiCVEScope holds three CVEs; a fan-out stamps only the first.
const d2MultiCVEScope = "scope:10972:d2:multi"

var d2CVEs = []string{"CVE-2026-97001", "CVE-2026-97002", "CVE-2026-97003"}

// seedMultiCVEScope gives one scope its own advisory, affected package and
// consumption for each CVE, so its findings pass derives one row per CVE and a
// stamp naming one CVE leaves two rows outside the stamp.
func (c *convergence) seedMultiCVEScope() {
	c.t.Helper()
	fanOutSeedScope(c.t, c.ctx, c.db, d2MultiCVEScope, "g1")
	gen := fanOutGen(d2MultiCVEScope, "g1")
	var envelopes []facts.Envelope
	for i, cve := range d2CVEs {
		pkg := fmt.Sprintf("pkg:npm/d2-multi-%d@1.0.0", i)
		id := func(part string) string { return fmt.Sprintf("fact:10972:d2:%d:%s", i, part) }
		envelopes = append(envelopes,
			c.envelope(d2MultiCVEScope, gen, id("cve"), chain.VulnerabilityCVEFactKind, map[string]any{
				"advisory_id": cve, "cve_id": cve, "source": "nvd", "cvss_score": 7.5, "status": "Analyzed",
			}),
			c.envelope(d2MultiCVEScope, gen, id("affected"), chain.VulnerabilityAffectedPackageFactKind, map[string]any{
				"advisory_id": cve, "cve_id": cve, "package_id": pkg, "ecosystem": "npm",
				"package_name": fmt.Sprintf("d2-multi-%d", i), "affected_versions": []string{"1.0.0"}, "fixed_versions": []string{"1.1.0"},
			}),
			c.envelope(d2MultiCVEScope, gen, id("consumption"), factschema.FactKindReducerPackageConsumptionCorrelation, map[string]any{
				"package_id": pkg, "relationship_kind": "consumption", "repository_id": "repository:r_10972_d2",
				"dependency_range": "1.0.0", "canonical_writes": 1, "evidence_fact_ids": []string{id("manifest")},
			}),
		)
	}
	c.upsert(envelopes...)
	fanOutSeedItem(c.t, c.ctx, c.db, d2MultiCVEScope, "g1")
}

// outOfStampRowVersions returns the xmin of every active finding row of the
// multi-CVE scope whose CVE the stamp does not name, read from the supports
// table (#10880 PR-C2). The writer's write-on-change guard leaves an
// untouched support at its xmin, so the byte-identity proof reads it there.
func (c *convergence) outOfStampRowVersions() map[string]string {
	c.t.Helper()
	rows, err := c.db.QueryContext(c.ctx, `
SELECT fact_id, xmin::text FROM supply_chain_impact_finding_supports
WHERE scope_id = $1 AND cve_id = ANY($2)`,
		d2MultiCVEScope, []string{d2CVEs[1], d2CVEs[2]})
	if err != nil {
		c.t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	out := map[string]string{}
	for rows.Next() {
		var id, xmin string
		if err := rows.Scan(&id, &xmin); err != nil {
			c.t.Fatal(err)
		}
		out[id] = xmin
	}
	if len(out) != 2 {
		c.t.Fatalf("out-of-stamp rows = %v, want the two rows of %v", out, d2CVEs[1:])
	}
	return out
}

// TestStampedRerunsOfAMultiCVEHolderStayScopedUntilTheReapLive is the live D2
// bound (#10972 reality audit, risk 9). A holder of three CVEs is reopened
// through the real reopen statement with a stamp naming one CVE, over and over,
// the way repeated fan-outs reopen it on QA. After its first full pass writes
// the full-pass marker, every stamped rerun must run scoped (scoped_cve_count
// 1) and leave the two rows outside the stamp byte-identical (xmin unchanged),
// until the 17th rerun, when 16 scoped reruns make a reap due and that rerun
// runs full; the rerun after it is scoped again.
//
// Expected on main: GREEN (#11336 D2 merged). A RED on the scoped arm means
// multi-CVE holders run full on every reopen again.
func TestStampedRerunsOfAMultiCVEHolderStayScopedUntilTheReapLive(t *testing.T) {
	c := newConvergence(t, false)
	c.seedMultiCVEScope()
	c.drainAll(50) // the establishing full pass writes the marker
	before := c.outOfStampRowVersions()
	origin := supplychaincore.FanOutOrigin{
		SourceScopeID: "scope:10972:d2:source", SourceGenerationID: "g", SourceIntentID: "i",
		CVEIDs: []string{d2CVEs[0]},
	}
	for rerun := 1; rerun <= 18; rerun++ {
		if _, err := c.fanOut.ReopenHolderDetailed(c.ctx, d2MultiCVEScope, origin); err != nil {
			t.Fatalf("rerun %d reopen: %v", rerun, err)
		}
		result := c.claimHandleAck(d2MultiCVEScope)
		scoped := result.SubSignals["scoped_cve_count"] > 0
		wantScoped := rerun != 17
		if scoped != wantScoped {
			t.Fatalf("rerun %d scoped=%v (scoped_cve_count=%v), want scoped=%v: reruns 1-16 scoped, 17 reaps in full, 18 scoped",
				rerun, scoped, result.SubSignals["scoped_cve_count"], wantScoped)
		}
		if !wantScoped {
			before = c.outOfStampRowVersions()
			continue
		}
		after := c.outOfStampRowVersions()
		for id, xmin := range before {
			if after[id] != xmin {
				t.Fatalf("rerun %d rewrote out-of-stamp row %s (xmin %s -> %s): a scoped pass must leave it byte-identical", rerun, id, xmin, after[id])
			}
		}
	}
	if _, ok, err := c.queue.Claim(c.ctx); err != nil || ok {
		t.Fatalf("claim after the reruns = %v %v, want nothing claimable", ok, err)
	}
}
