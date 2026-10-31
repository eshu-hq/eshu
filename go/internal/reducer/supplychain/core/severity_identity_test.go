// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// Issue #9693 review round 3 (F1): deriving a score from a vector or reading a
// label must change severity only. A finding's advisory_id feeds its
// finding_id, its canonical key, and advisory-scoped suppression matching, so
// the same evidence must keep the identity it had before severity was derived.
// Only a source-reported score has ever chosen the advisory identity (the
// pre-#9693 rule); a derived score or a label never does.

const identityCVE = "CVE-2026-93050"

func identityCVEFact(factID, advisoryID, source string, extra map[string]any) facts.Envelope {
	payload := map[string]any{
		"cve_id":      identityCVE,
		"advisory_id": advisoryID,
		"source":      source,
		"aliases":     []any{identityCVE},
	}
	for key, value := range extra {
		payload[key] = value
	}
	return facts.Envelope{FactID: factID, FactKind: "vulnerability.cve", Payload: payload}
}

func identityFinding(t *testing.T, cves ...facts.Envelope) SupplyChainImpactFinding {
	t.Helper()
	envelopes := append([]facts.Envelope{}, cves...)
	envelopes = append(envelopes,
		vulnerabilityAffectedPackageFact("affected-identity", identityCVE, testImpactPackageID, "npm", "example", "1.2.3", "1.3.0"),
		packageConsumptionFactWithChain("consume-identity", testImpactPackageID, testImpactRepositoryID, "1.2.3", []string{"example"}, 1, true),
	)
	findings := BuildSupplyChainImpactFindings(envelopes)
	if len(findings) != 1 {
		t.Fatalf("len(findings) = %d, want 1: %#v", len(findings), findings)
	}
	return findings[0]
}

func TestSeverityTiersDoNotMoveFindingIdentity(t *testing.T) {
	t.Parallel()

	const ghsa = "GHSA-aaaa-bbbb-cccc"
	shapes := map[string][2]map[string]any{
		// {GHSA observation extras, second observation extras}; the second
		// observation's advisory id and source come from secondSource below.
		"ghsa label, nvd vector":   {{"severity_label": "low"}, {"cvss_vector": criticalV31Vector}},
		"ghsa nothing, nvd vector": {nil, {"cvss_vector": criticalV31Vector}},
		"ghsa nothing, nvd label":  {nil, {"severity_label": "HIGH"}},
	}
	for name, extras := range shapes {
		withSeverity := identityFinding(t,
			identityCVEFact("ghsa", ghsa, "osv", extras[0]),
			identityCVEFact("nvd", identityCVE, "nvd", extras[1]),
		)
		control := identityFinding(t,
			identityCVEFact("ghsa", ghsa, "osv", nil),
			identityCVEFact("nvd", identityCVE, "nvd", nil),
		)
		if withSeverity.AdvisoryID != ghsa || control.AdvisoryID != ghsa {
			t.Errorf("%s: advisory_id = %q (control %q), want %q: severity evidence must not choose the advisory identity",
				name, withSeverity.AdvisoryID, control.AdvisoryID, ghsa)
		}
		if got, want := supplyChainImpactFindingID(withSeverity), supplyChainImpactFindingID(control); got != want {
			t.Errorf("%s: finding_id = %s, want the severity-free evidence's %s", name, got, want)
		}
		suppression := vulnerabilitySuppression{Scope: vulnerabilitySuppressionScope{AdvisoryID: ghsa, PackageID: testImpactPackageID}}
		if !suppressionScopeMatchesFinding(withSeverity, suppression) {
			t.Errorf("%s: an advisory-scoped suppression for %s stopped matching", name, ghsa)
		}
		// Severity itself still derives from the second observation.
		if withSeverity.SeveritySource != "nvd" {
			t.Errorf("%s: SeveritySource = %q, want nvd (severity still follows the derivation tiers)", name, withSeverity.SeveritySource)
		}
	}
}

// TestSourceReportedScoreStillChoosesTheAdvisoryIdentity pins the pre-#9693
// rule that tier 1 keeps: the advisory id follows the source that reported
// the selected score. Same evidence, same id as before the change.
func TestSourceReportedScoreStillChoosesTheAdvisoryIdentity(t *testing.T) {
	t.Parallel()

	nvd := identityCVEFact("nvd", identityCVE, "nvd", map[string]any{"cvss_score": 8.1})
	finding := identityFinding(t, identityCVEFact("ghsa", "GHSA-aaaa-bbbb-cccc", "osv", map[string]any{"cvss_vector": criticalV31Vector}), nvd)
	if finding.AdvisoryID != identityCVE {
		t.Fatalf("advisory_id = %q, want %q: the reported-score source chooses the identity", finding.AdvisoryID, identityCVE)
	}
}

// TestVectorOnlyFindingKeepsTheFirstAdvisoryIdentity states the identity an
// OSV vector-only finding gets. Before #9693 it had no score winner, so its
// advisory id was the first advisory observation in source-priority order. It
// still is.
func TestVectorOnlyFindingKeepsTheFirstAdvisoryIdentity(t *testing.T) {
	t.Parallel()

	finding := identityFinding(t, identityCVEFact("ghsa", "GHSA-aaaa-bbbb-cccc", "osv", map[string]any{"cvss_vector": criticalV31Vector}))
	if finding.AdvisoryID != "GHSA-aaaa-bbbb-cccc" || finding.CVSSScore != 9.8 {
		t.Fatalf("advisory_id = %q score = %v, want GHSA-aaaa-bbbb-cccc with the derived 9.8", finding.AdvisoryID, finding.CVSSScore)
	}
}
