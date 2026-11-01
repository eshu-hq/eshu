// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/facts/supply/chain"
)

// TestUnreadableLiveAliasRecordDoesNotLetAWithdrawalRetract is N1: a live NVD
// alias record fails typed decode (a score of the wrong JSON type) and is
// quarantined. The withdrawn GHSA record and its package are readable. A group
// with an unreadable member is not provably retracted, so the finding stays
// active exactly as it does when the alias is readable.
func TestUnreadableLiveAliasRecordDoesNotLetAWithdrawalRetract(t *testing.T) {
	t.Parallel()

	withdrawn := vulnerabilityCVEFactWithdrawn(
		"gh-cve", "CVE-2026-5001", "osv", "GHSA-wdrn-aaaa-bbbb", 7.5, "HIGH",
		"2026-05-18T10:00:00Z", "2026-05-22T08:00:00Z",
	)
	nvd := retractionCVEFact("nvd-cve", "CVE-2026-5001", "nvd", "CVE-2026-5001", "")
	nvd.Payload["cvss_score"] = "9.8"
	envelopes := []facts.Envelope{
		nvd,
		withdrawn,
		retractionPackageFact("gh-pkg", "CVE-2026-5001", "osv", "GHSA-wdrn-aaaa-bbbb", "1.0.1"),
		retractionConsumption(),
	}
	findings, quarantined, err := buildSupplyChainImpactFindingsWithQuarantine(envelopes)
	if err != nil || len(quarantined) != 1 || len(findings) != 1 {
		t.Fatalf("err=%v quarantined=%d findings=%d, want the one NVD fact quarantined and one finding", err, len(quarantined), len(findings))
	}
	finding := applyRetractionDecision(findings[0])
	if got := finding.Suppression.State; got != SupplyChainSuppressionStateActive {
		t.Fatalf("state = %q, want active: an unreadable record may still assert the vulnerability", got)
	}
	// Control: the same evidence with the alias readable stays active.
	readable := buildRetractionFinding(t,
		retractionCVEFact("nvd-cve", "CVE-2026-5001", "nvd", "CVE-2026-5001", ""),
		withdrawn,
		retractionPackageFact("gh-pkg", "CVE-2026-5001", "osv", "GHSA-wdrn-aaaa-bbbb", "1.0.1"),
		retractionConsumption(),
	)
	if readable.Suppression.State != SupplyChainSuppressionStateActive {
		t.Fatalf("control state = %q, want active", readable.Suppression.State)
	}
}

// TestUnreadableLiveRecordKeepsAProductFindingActive is the product-path mirror:
// the rejected NVD record is readable and carries the CPE match; a live GHSA
// record for the same CVE is quarantined.
func TestUnreadableLiveRecordKeepsAProductFindingActive(t *testing.T) {
	t.Parallel()

	findings, quarantined, err := buildSupplyChainImpactFindingsWithQuarantine([]facts.Envelope{
		retractionCVEFact("nvd-cve", "CVE-2026-5011", "nvd", "CVE-2026-5011", "Rejected"),
		quarantinedCVEFact("ghsa-cve", "CVE-2026-5011", "osv", "GHSA-live-mmmm-nnnn"),
		vulnerabilityAffectedProductFact("product-1", "CVE-2026-5011", testImpactProductCriteria, testImpactMatchCriteriaID, true),
		sbomComponentCPEImpactFact("component-1", "doc-1", testImpactProductCriteria),
		sbomAttachmentImpactFact("attachment-1", "doc-1", testImpactSubjectDigest),
		containerImageIdentityImpactFact("image-1", testImpactSubjectDigest, testImpactRepositoryID),
	})
	if err != nil || len(quarantined) != 1 || len(findings) != 1 {
		t.Fatalf("err=%v quarantined=%d findings=%d, want one quarantined fact and one finding", err, len(quarantined), len(findings))
	}
	if got := applyRetractionDecision(findings[0]).Suppression.State; got != SupplyChainSuppressionStateActive {
		t.Fatalf("state = %q, want active while a record for the CVE is unreadable", got)
	}
}

// TestIdentityFollowsTheHighestPriorityScoredSource pins the standalone
// identity loop (N2): with two live sources that each report a score, the
// higher-priority source names the finding. The lower-priority source must not
// win by being last, and an operator suppression scoped to the advisory the
// finding really carries must keep matching.
func TestIdentityFollowsTheHighestPriorityScoredSource(t *testing.T) {
	t.Parallel()

	ghsa := retractionCVEFact("ghsa-cve", "CVE-2026-6001", "osv", "GHSA-aaaa-bbbb-cccc", "")
	ghsa.Payload["cvss_score"] = 9.0
	nvd := retractionCVEFact("nvd-cve", "CVE-2026-6001", "nvd", "CVE-2026-6001", "")
	nvd.Payload["cvss_score"] = 7.0
	for name, order := range map[string][]facts.Envelope{"ghsa first": {ghsa, nvd}, "nvd first": {nvd, ghsa}} {
		finding := buildRetractionFinding(t, append(append([]facts.Envelope{}, order...),
			retractionPackageFact("pkg", "CVE-2026-6001", "osv", "GHSA-aaaa-bbbb-cccc", "1.0.1"),
			retractionConsumption(),
		)...)
		if finding.AdvisoryID != "GHSA-aaaa-bbbb-cccc" {
			t.Fatalf("%s: advisory_id = %q, want the higher-priority GHSA record", name, finding.AdvisoryID)
		}
		if finding.SeveritySource != "ghsa" {
			t.Fatalf("%s: severity source = %q, want ghsa", name, finding.SeveritySource)
		}
		decision := EvaluateSupplyChainSuppression(finding, []vulnerabilitySuppression{{
			SuppressionID: "op-1",
			Source:        chain.VulnerabilitySuppressionSourcePolicy,
			Justification: chain.VulnerabilitySuppressionJustificationAcceptedRisk,
			AuthoredAt:    time.Unix(10, 0).UTC(),
			Scope:         vulnerabilitySuppressionScope{AdvisoryID: "GHSA-aaaa-bbbb-cccc", PackageID: retractionPackageID},
		}}, time.Unix(20, 0).UTC())
		if decision.State != SupplyChainSuppressionStateAcceptedRisk {
			t.Fatalf("%s: suppression state = %q, want accepted_risk", name, decision.State)
		}
	}
}

// TestIdentitySkipsAWithdrawnTopSourceAndKeepsALowerOne pins the withdrawn
// guard of the identity loop (N2) in both positions: a withdrawn top-priority
// source hands identity to the next one, and a withdrawn lower-priority source
// leaves the top one in place.
func TestIdentitySkipsAWithdrawnTopSourceAndKeepsALowerOne(t *testing.T) {
	t.Parallel()

	build := func(ghsaWithdrawn, nvdWithdrawn bool) string {
		ghsa := retractionCVEFact("ghsa-cve", "CVE-2026-6101", "osv", "GHSA-aaaa-bbbb-cccc", "")
		ghsa.Payload["cvss_score"] = 9.0
		nvd := retractionCVEFact("nvd-cve", "CVE-2026-6101", "nvd", "CVE-2026-6101", "")
		nvd.Payload["cvss_score"] = 7.0
		// A third live record keeps the group from retracting whichever source is withdrawn.
		live := retractionCVEFact("gl-cve", "CVE-2026-6101", "gitlab", "GLSA-live-1", "")
		live.Payload["cvss_score"] = 5.0
		if ghsaWithdrawn {
			ghsa.Payload["withdrawn_at"] = "2026-05-22T08:00:00Z"
		}
		if nvdWithdrawn {
			nvd.Payload["withdrawn_at"] = "2026-05-22T08:00:00Z"
		}
		finding := buildRetractionFinding(t, ghsa, nvd, live,
			retractionPackageFact("pkg", "CVE-2026-6101", "osv", "GHSA-aaaa-bbbb-cccc", "1.0.1"),
			retractionConsumption(),
		)
		return finding.AdvisoryID
	}
	if got := build(false, false); got != "GHSA-aaaa-bbbb-cccc" {
		t.Fatalf("nothing withdrawn: advisory_id = %q, want the GHSA record", got)
	}
	if got := build(true, false); got != "CVE-2026-6101" {
		t.Fatalf("top source withdrawn: advisory_id = %q, want identity to fall to the NVD record", got)
	}
	if got := build(false, true); got != "GHSA-aaaa-bbbb-cccc" {
		t.Fatalf("lower source withdrawn: advisory_id = %q, want the GHSA record to stay", got)
	}
}

// TestSameSourceRetractedRecordsAreListedInFullAndInOrder kills the
// explanation survivors: two withdrawn records of one source class, with the
// same timestamp so only the advisory id tells them apart, are both
// named, in advisory-id order, in the reason and in the serialized sources,
// and each source row carries its withdrawn timestamp.
func TestSameSourceRetractedRecordsAreListedInFullAndInOrder(t *testing.T) {
	t.Parallel()

	first := vulnerabilityCVEFactWithdrawn(
		"gh-cve-b", "CVE-2026-6201", "osv", "GHSA-bbbb-bbbb-bbbb", 7.5, "HIGH",
		"2026-05-18T10:00:00Z", "2026-05-22T08:00:00Z",
	)
	second := vulnerabilityCVEFactWithdrawn(
		"gh-cve-a", "CVE-2026-6201", "osv", "GHSA-aaaa-aaaa-aaaa", 7.5, "HIGH",
		"2026-05-18T10:00:00Z", "2026-05-22T08:00:00Z",
	)
	finding := buildRetractionFinding(t, first, second,
		retractionPackageFact("pkg-a", "CVE-2026-6201", "osv", "GHSA-aaaa-aaaa-aaaa", "1.0.1"),
		retractionPackageFact("pkg-b", "CVE-2026-6201", "osv", "GHSA-bbbb-bbbb-bbbb", "1.0.1"),
		retractionConsumption(),
	)
	want := "advisory retracted by its source: ghsa withdrawn GHSA-aaaa-aaaa-aaaa; ghsa withdrawn GHSA-bbbb-bbbb-bbbb"
	if finding.AdvisoryRetraction.Reason != want {
		t.Fatalf("Reason = %q, want %q", finding.AdvisoryRetraction.Reason, want)
	}
	payload := supplyChainImpactTypedPayload(SupplyChainImpactWrite{IntentID: "i", ScopeID: "s", GenerationID: "g"}, finding)
	block, _ := payload.Provenance["advisory_retraction"].(map[string]any)
	rows, _ := block["sources"].([]map[string]any)
	if len(rows) != 2 || rows[0]["advisory_id"] != "GHSA-aaaa-aaaa-aaaa" || rows[1]["advisory_id"] != "GHSA-bbbb-bbbb-bbbb" {
		t.Fatalf("serialized sources = %#v, want both records in advisory-id order", rows)
	}
	if rows[0]["withdrawn_at"] != "2026-05-22T08:00:00Z" || rows[1]["withdrawn_at"] != "2026-05-22T08:00:00Z" {
		t.Fatalf("withdrawn_at = %v, %v, want the shared timestamp on each row", rows[0]["withdrawn_at"], rows[1]["withdrawn_at"])
	}
}

// TestDuplicatePackageFactForAWithdrawnAdvisoryFailsTowardActive documents the
// safe direction of an unmatched, statusless package observation: a second
// package fact for a withdrawn advisory matches no CVE fact, so it counts as a
// live source and the finding stays active. The OSV collector merges every
// affected entry per (advisory, package, source) into one fact, so a collector
// cannot produce this shape; the reducer-side behavior is covered here only.
func TestDuplicatePackageFactForAWithdrawnAdvisoryFailsTowardActive(t *testing.T) {
	t.Parallel()

	withdrawn := vulnerabilityCVEFactWithdrawn(
		"gh-cve", "CVE-2026-6301", "osv", "GHSA-wdrn-dddd-eeee", 7.5, "HIGH",
		"2026-05-18T10:00:00Z", "2026-05-22T08:00:00Z",
	)
	finding := buildRetractionFinding(t, withdrawn,
		retractionPackageFact("pkg-1", "CVE-2026-6301", "osv", "GHSA-wdrn-dddd-eeee", "1.0.1"),
		retractionPackageFact("pkg-2", "CVE-2026-6301", "osv", "GHSA-wdrn-dddd-eeee", "1.0.2"),
		retractionConsumption(),
	)
	if got := finding.Suppression.State; got != SupplyChainSuppressionStateActive {
		t.Fatalf("state = %q, want active: the second package fact has no retracting record", got)
	}
}

// TestUnreadableRecordGuardUsesTheGroupKey pins the key the unreadable-record
// guard records against the key the CVE groups use, FirstNonBlank(cve_id,
// advisory_id): a record with no cve_id keys by advisory id, a padded cve_id keys
// by its trimmed value, and a cve_id of the wrong JSON type falls back to the
// advisory id. In each shape the withdrawn record and its package share the
// group with an unreadable live record, so the finding must stay active.
func TestUnreadableRecordGuardUsesTheGroupKey(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		groupKey  string
		unreadabl func(payload map[string]any)
	}{
		{"keyed by advisory id, no cve_id", "GHSA-keyd-aaaa-bbbb", func(p map[string]any) { delete(p, "cve_id") }},
		{"padded cve_id", "CVE-2026-5101", func(p map[string]any) { p["cve_id"] = "  CVE-2026-5101  " }},
		{"cve_id of the wrong type", "CVE-2026-5102", func(p map[string]any) { p["cve_id"] = 5102 }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			withdrawn := vulnerabilityCVEFactWithdrawn(
				"gh-cve", tc.groupKey, "osv", tc.groupKey, 7.5, "HIGH",
				"2026-05-18T10:00:00Z", "2026-05-22T08:00:00Z",
			)
			live := retractionCVEFact("nvd-cve", tc.groupKey, "nvd", tc.groupKey, "")
			live.Payload["cvss_score"] = "9.8"
			tc.unreadabl(live.Payload)
			if tc.name == "keyed by advisory id, no cve_id" {
				delete(withdrawn.Payload, "cve_id")
			}
			findings, quarantined, err := buildSupplyChainImpactFindingsWithQuarantine([]facts.Envelope{
				live, withdrawn,
				retractionPackageFact("gh-pkg", tc.groupKey, "osv", tc.groupKey, "1.0.1"),
				retractionConsumption(),
			})
			if err != nil || len(quarantined) != 1 || len(findings) != 1 {
				t.Fatalf("err=%v quarantined=%d findings=%d, want one quarantined fact and one finding", err, len(quarantined), len(findings))
			}
			if got := applyRetractionDecision(findings[0]).Suppression.State; got != SupplyChainSuppressionStateActive {
				t.Fatalf("state = %q, want active: the unreadable record belongs to this group", got)
			}
		})
	}
}
