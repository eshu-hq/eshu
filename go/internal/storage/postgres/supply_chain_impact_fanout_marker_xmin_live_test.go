// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"testing"

	"github.com/eshu-hq/eshu-sdk/factschema"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/facts/supply/chain"
	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
)

// TestUpdateFanOutStateMarkerKeepsRowsOutsideTheStampLive is the live half of
// the #10972 D2 bound. A holder holds rows for two CVEs; its genuine pass left
// a fresh full-pass marker. A stamped one-hop rerun for one CVE must run
// scoped and leave the other CVE's row at its xmin. Before the marker, the
// #10415 fallback ran that rerun full on every reopen because the holder held
// a row outside the stamp.
func TestUpdateFanOutStateMarkerKeepsRowsOutsideTheStampLive(t *testing.T) {
	const holder, ownCVE = "scope:10972:d2:multi", "CVE-2026-73110"
	c := newConvergence(t, false)
	fanOutSeedScope(t, c.ctx, c.db, holder, "g1")
	gen := fanOutGen(holder, "g1")
	consume := func(id, pkg string) facts.Envelope {
		return c.envelope(holder, gen, id, factschema.FactKindReducerPackageConsumptionCorrelation, map[string]any{
			"package_id": pkg, "relationship_kind": "consumption", "repository_id": "repository:r_d2_multi",
			"dependency_range": "1.0.0", "canonical_writes": 1, "evidence_fact_ids": []string{id + ":manifest"},
		})
	}
	c.upsert(
		c.envelope(holder, gen, "fact:d2:own:cve", chain.VulnerabilityCVEFactKind, map[string]any{
			"advisory_id": ownCVE, "cve_id": ownCVE, "source": "nvd", "cvss_score": 7.5, "status": "Analyzed",
		}),
		c.envelope(holder, gen, "fact:d2:own:affected", chain.VulnerabilityAffectedPackageFactKind, map[string]any{
			"advisory_id": ownCVE, "cve_id": ownCVE, "package_id": "pkg:npm/d2-own@1.0.0", "ecosystem": "npm",
			"package_name": "d2-own", "affected_versions": []string{"1.0.0"}, "fixed_versions": []string{"1.1.0"},
		}),
		consume("fact:d2:own:consumes", "pkg:npm/d2-own@1.0.0"),
		consume("fact:d2:source:consumes", convergencePackage),
	)
	fanOutSeedItem(t, c.ctx, c.db, holder, "g1")
	c.drain()

	xmin := func(cve string) string {
		t.Helper()
		var v string
		if err := c.db.QueryRowContext(c.ctx, `
SELECT xmin::text FROM supply_chain_impact_finding_supports WHERE scope_id = $1 AND cve_id = $2`,
			holder, cve).Scan(&v); err != nil {
			t.Fatalf("fixture: the holder holds no active row for %s: %v", cve, err)
		}
		return v
	}
	ownBefore := xmin(ownCVE)
	_ = xmin(convergenceCVE)

	origin := supplychaincore.FanOutOrigin{SourceScopeID: convergenceSource, SourceGenerationID: fanOutGen(convergenceSource, "g1"), SourceIntentID: "d2-xmin", CVEIDs: []string{convergenceCVE}}
	if _, err := c.fanOut.ReopenHolderDetailed(c.ctx, holder, origin); err != nil {
		t.Fatalf("reopen: %v", err)
	}
	intent := c.raceClaim(holder)
	result, err := c.handler.Handle(c.ctx, intent)
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	if err := c.queue.Ack(c.ctx, intent, result); err != nil {
		t.Fatalf("ack: %v", err)
	}
	if got := result.SubSignals["scoped_cve_count"]; got != 1 {
		t.Errorf("rerun scoped_cve_count = %v, want 1: with a fresh full-pass marker a multi-CVE holder's stamped rerun stays scoped", got)
	}
	if after := xmin(ownCVE); after != ownBefore {
		t.Errorf("row for %s outside the stamp went from xmin %s to %s: a scoped rerun must leave it byte-identical", ownCVE, ownBefore, after)
	}
}
