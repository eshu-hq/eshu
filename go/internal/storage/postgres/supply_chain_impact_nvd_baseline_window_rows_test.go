// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"testing"

	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
)

// TestNVDBaselineAndWindowRowsPerStableKey (R-B5b of eshu-hq/eshu#10735) pins
// the storage side of the newest-copy contract: the real impact loader
// (ListActiveSupplyChainImpactFacts) must surface every copy of one NVD
// advisory — the baseline copy and the later window copy, and each copy's
// affected_product rows — under the one stable key (cve_id), without
// collapsing or electing at load. The reducer elects in
// appendSupplyChainImpactIndexFindings; the loader only carries the copies.
//
// It runs hermetic against the routing fake (no live database): the fake
// serves the catalog branches' rows and payload pages while the test drives
// the production ListActiveSupplyChainImpactFacts path end to end.
//
// Fixtures use unzoned NVD-shaped modified_at values, the shape #10741 fixed
// the election to parse (closed).
func TestNVDBaselineAndWindowRowsPerStableKey(t *testing.T) {
	t.Parallel()

	const cveID = "CVE-2026-9201"
	const (
		baselineStamp = "2026-10-01T00:00:00.000"
		windowStamp   = "2026-11-01T00:00:00.000"
	)
	payload := func(body string) []byte { return []byte(body) }
	routed := &supplyChainImpactRoutingDB{
		branchRows: map[string][][2]string{
			"vulnerability_cve_id": {
				{"vuln-cve:10735-baseline", "vulnerability.cve"},
				{"vuln-cve:10735-window", "vulnerability.cve"},
			},
			"affected_product_criteria": {
				{"vuln-product:10735-baseline", "vulnerability.affected_product"},
				{"vuln-product:10735-window", "vulnerability.affected_product"},
			},
		},
		facts: map[string]string{
			"vuln-cve:10735-baseline":     "vulnerability.cve",
			"vuln-cve:10735-window":       "vulnerability.cve",
			"vuln-product:10735-baseline": "vulnerability.affected_product",
			"vuln-product:10735-window":   "vulnerability.affected_product",
		},
		payloads: map[string][]byte{
			"vuln-cve:10735-baseline":     payload(`{"cve_id":"` + cveID + `","advisory_id":"` + cveID + `","source":"nvd","cvss_score":7.5,"severity_label":"HIGH","modified_at":"` + baselineStamp + `"}`),
			"vuln-cve:10735-window":       payload(`{"cve_id":"` + cveID + `","advisory_id":"` + cveID + `","source":"nvd","cvss_score":9.8,"severity_label":"CRITICAL","modified_at":"` + windowStamp + `"}`),
			"vuln-product:10735-baseline": payload(`{"cve_id":"` + cveID + `","criteria":"cpe:2.3:a:example:alpha:1.0:*:*:*:*:*:*:*","match_criteria_id":"match-10735-alpha","vulnerable":true,"source":"nvd"}`),
			"vuln-product:10735-window":   payload(`{"cve_id":"` + cveID + `","criteria":"cpe:2.3:a:example:beta:2.0:*:*:*:*:*:*:*","match_criteria_id":"match-10735-beta","vulnerable":true,"source":"nvd"}`),
		},
	}

	loaded, truncated, err := NewFactStore(routed).ListActiveSupplyChainImpactFacts(context.Background(),
		supplychaincore.SupplyChainImpactFactFilter{
			CVEIDs: []string{cveID},
			ProductCriteria: []string{
				"cpe:2.3:a:example:alpha:1.0:*:*:*:*:*:*:*",
				"cpe:2.3:a:example:beta:2.0:*:*:*:*:*:*:*",
			},
		})
	if err != nil {
		t.Fatalf("ListActiveSupplyChainImpactFacts() error = %v, want nil", err)
	}
	if truncated {
		t.Fatal("truncated = true, want false for four rows far under every cap")
	}
	var cveRows, productRows int
	var stamps []string
	scopes := map[string]bool{}
	for _, envelope := range loaded {
		cve, _ := envelope.Payload["cve_id"].(string)
		if cve != cveID {
			t.Fatalf("loaded fact %q carries cve_id %q, want the one stable key %q", envelope.FactID, cve, cveID)
		}
		scopes[envelope.ScopeID] = true
		switch envelope.FactKind {
		case "vulnerability.cve":
			cveRows++
			stamp, _ := envelope.Payload["modified_at"].(string)
			stamps = append(stamps, stamp)
		case "vulnerability.affected_product":
			productRows++
		default:
			t.Fatalf("loaded fact %q has kind %q, want only the seeded cve and product rows", envelope.FactID, envelope.FactKind)
		}
	}
	if cveRows != 2 {
		t.Fatalf("cve rows = %d, want both the baseline and the window copy", cveRows)
	}
	if productRows != 2 {
		t.Fatalf("affected_product rows = %d, want each copy's product rows", productRows)
	}
	if len(scopes) != 4 {
		t.Fatalf("scopes = %d, want one scope per copy row (baseline and window stay distinct)", len(scopes))
	}
	wantStamps := map[string]bool{baselineStamp: true, windowStamp: true}
	for _, stamp := range stamps {
		delete(wantStamps, stamp)
	}
	if len(wantStamps) != 0 {
		t.Fatalf("missing modified_at stamps %v: the loader must not collapse the copies", wantStamps)
	}
}
