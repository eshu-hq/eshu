// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// productRetractionFinding builds the one finding a product-keyed (CPE) group
// derives from the supplied vulnerability.cve envelopes plus the product, SBOM
// and image evidence that anchors it, with the retraction decision applied.
func productRetractionFinding(t *testing.T, cves ...facts.Envelope) SupplyChainImpactFinding {
	t.Helper()

	envelopes := append([]facts.Envelope{}, cves...)
	envelopes = append(envelopes,
		vulnerabilityAffectedProductFact("product-1", "CVE-2026-2100", testImpactProductCriteria, testImpactMatchCriteriaID, true),
		sbomComponentCPEImpactFact("component-1", "doc-1", testImpactProductCriteria),
		sbomAttachmentImpactFact("attachment-1", "doc-1", testImpactSubjectDigest),
		containerImageIdentityImpactFact("image-1", testImpactSubjectDigest, testImpactRepositoryID),
	)
	findings := BuildSupplyChainImpactFindings(envelopes)
	if len(findings) != 1 {
		t.Fatalf("findings = %d, want exactly 1: %#v", len(findings), findings)
	}
	return applyRetractionDecision(findings[0])
}

// productCVE builds a CVE record for the product group with a source-reported
// CVSS score and the given status.
func productCVE(factID, source, status string, score float64, label string) facts.Envelope {
	envelope := retractionCVEFact(factID, "CVE-2026-2100", source, "CVE-2026-2100", status)
	envelope.Payload["cvss_score"] = score
	envelope.Payload["severity_label"] = label
	return envelope
}

func cvssContribution(finding SupplyChainImpactFinding) (SupplyChainImpactPriorityContribution, bool) {
	for _, contribution := range finding.PriorityContributions {
		if contribution.Input == "cvss" {
			return contribution, true
		}
	}
	return SupplyChainImpactPriorityContribution{}, false
}

// TestRejectedOnlyProductFindingLendsNoSeverity pins #10764: a product-keyed
// finding whose only record is REJECTED and carries CVSS 9.8 must not score
// that CVSS. A retracted record lends no severity on the package and the
// product path alike. Before the fix the all-retracted fallback returned the
// rejected representative, so the finding carried CVSS 9.8 and a +60 cvss
// contribution: priority_score 100 (contributions summed to 120 and clamped)
// and priority_bucket critical. After it the finding carries no CVSS and
// scores the remaining evidence alone: priority_score 60, bucket high. The
// retraction annotation is unchanged (#10756 annotates, never moves).
func TestRejectedOnlyProductFindingLendsNoSeverity(t *testing.T) {
	t.Parallel()

	finding := productRetractionFinding(t, productCVE("nvd-cve", "nvd", "Rejected", 9.8, "CRITICAL"))

	if finding.CVSSScore != 0 || finding.SeverityVector != "" || finding.SeverityLabel != "" {
		t.Fatalf("rejected-only product finding carries severity score=%v vector=%q label=%q, want none",
			finding.CVSSScore, finding.SeverityVector, finding.SeverityLabel)
	}
	if contribution, ok := cvssContribution(finding); ok {
		t.Fatalf("rejected-only product finding scored CVSS %#v: a retracted record lends no severity", contribution)
	}
	if finding.PriorityScore != 60 {
		t.Fatalf("PriorityScore = %d, want 60 (remaining evidence only, no cvss contribution)", finding.PriorityScore)
	}
	assertPriorityNamesRetraction(t, finding, AdvisoryRetractionRejected)
}

func TestLiveProductRecordStillLendsSeverity(t *testing.T) {
	t.Parallel()

	finding := productRetractionFinding(t, productCVE("nvd-cve", "nvd", "Analyzed", 9.8, "CRITICAL"))

	if finding.CVSSScore != 9.8 {
		t.Fatalf("CVSSScore = %v, want 9.8 from the live record", finding.CVSSScore)
	}
	if _, ok := cvssContribution(finding); !ok {
		t.Fatalf("live product finding earned no cvss contribution: %#v", finding.PriorityContributions)
	}
}

func TestMixedProductGroupKeepsTheLiveRecordSeverity(t *testing.T) {
	t.Parallel()

	// The rejected record ranks first and names the finding; the live record
	// still lends its own, lower severity.
	finding := productRetractionFinding(t,
		productCVE("nvd-cve", "nvd", "Rejected", 9.8, "CRITICAL"),
		productCVE("osv-cve", "osv", "", 5.0, "MEDIUM"),
	)

	if got, want := finding.Suppression.State, SupplyChainSuppressionStateActive; got != want {
		t.Fatalf("state = %q, want %q: one live source keeps the group live", got, want)
	}
	if finding.CVSSScore != 5.0 {
		t.Fatalf("CVSSScore = %v, want 5.0 from the live record, not the rejected 9.8", finding.CVSSScore)
	}
}

// TestRestoredProductAdvisoryKeepsIdentityAndRegainsSeverity is the product
// twin of TestUnrejectedAdvisoryKeepsTheSameFindingIdentity: severity
// derivation is pure, so restoring the record returns the finding to the score
// and CVSS it had before the rejection, under the same identity.
func TestRestoredProductAdvisoryKeepsIdentityAndRegainsSeverity(t *testing.T) {
	t.Parallel()

	live := productRetractionFinding(t, productCVE("nvd-cve", "nvd", "Analyzed", 9.8, "CRITICAL"))
	rejected := productRetractionFinding(t, productCVE("nvd-cve", "nvd", "Rejected", 9.8, "CRITICAL"))
	restored := productRetractionFinding(t, productCVE("nvd-cve", "nvd", "Analyzed", 9.8, "CRITICAL"))

	if rejected.PriorityScore >= restored.PriorityScore {
		t.Fatalf("rejected score %d must be below the restored score %d by the CVSS contribution",
			rejected.PriorityScore, restored.PriorityScore)
	}
	if restored.PriorityScore != live.PriorityScore || restored.CVSSScore != live.CVSSScore {
		t.Fatalf("restored score/cvss = %d/%v, want the pre-rejection %d/%v",
			restored.PriorityScore, restored.CVSSScore, live.PriorityScore, live.CVSSScore)
	}
	if restored.AdvisoryRetraction != nil {
		t.Fatalf("restored finding still carries a retraction: %#v", restored.AdvisoryRetraction)
	}
	write := SupplyChainImpactWrite{IntentID: "intent", ScopeID: "scope", GenerationID: "gen"}
	for name, pair := range map[string][2]string{
		"finding_id": {supplyChainImpactFindingID(rejected), supplyChainImpactFindingID(restored)},
		"stable key": {supplyChainImpactStableFactKey(write, rejected), supplyChainImpactStableFactKey(write, restored)},
	} {
		if pair[0] != pair[1] {
			t.Fatalf("%s moved when the advisory was retracted: %q vs %q", name, pair[0], pair[1])
		}
	}
	if !equalStrings(rejected.EvidenceFactIDs, restored.EvidenceFactIDs) {
		t.Fatalf("evidence fact ids moved: %v vs %v", rejected.EvidenceFactIDs, restored.EvidenceFactIDs)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// scopedProductCVE is a productCVE copy held in its own scope and generation
// with a stated modified_at, the way per-window generations deliver several
// copies of one record.
func scopedProductCVE(factID, status, modifiedAt string) facts.Envelope {
	envelope := productCVE(factID, "nvd", status, 9.8, "CRITICAL")
	envelope.Payload["modified_at"] = modifiedAt
	envelope.ScopeID = "scope-" + factID
	envelope.GenerationID = "generation-" + factID
	return envelope
}

// TestRejectedNewestCopyOverStaleLiveCopyLendsNoSeverity pins the stale-copy
// edge of #10764: two copies of one NVD record, the older Analyzed at 9.8 and
// the newer Rejected, are one retracted record. The retraction annotation
// already reads the newest copy, so the severity read must too: the superseded
// live copy must not lend its CVSS to a finding the record has withdrawn. Both
// fact id orders run, because the ranked representative (lowest fact id among
// equal ranks) is the stale copy in one and the Rejected copy in the other.
func TestRejectedNewestCopyOverStaleLiveCopyLendsNoSeverity(t *testing.T) {
	t.Parallel()

	for name, ids := range map[string][2]string{
		"stale copy is the ranked representative":    {"nvd-a-old", "nvd-b-new"},
		"rejected copy is the ranked representative": {"nvd-b-old", "nvd-a-new"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			finding := productRetractionFinding(t,
				scopedProductCVE(ids[0], "Analyzed", "2026-01-01T00:00:00Z"),
				scopedProductCVE(ids[1], "Rejected", "2026-06-01T00:00:00Z"),
			)

			if finding.AdvisoryRetraction == nil {
				t.Fatalf("newest copy is Rejected but the finding carries no retraction: %#v", finding)
			}
			if finding.CVSSScore != 0 || finding.SeverityVector != "" || finding.SeverityLabel != "" {
				t.Fatalf("stale live copy lent severity score=%v vector=%q label=%q to a retracted record, want none",
					finding.CVSSScore, finding.SeverityVector, finding.SeverityLabel)
			}
			if contribution, ok := cvssContribution(finding); ok {
				t.Fatalf("retracted record scored CVSS from its superseded copy: %#v", contribution)
			}
		})
	}
}

// TestRestoredNewestCopyOverStaleRejectedCopyKeepsSeverity is the inverse: a
// newer live copy supersedes an older Rejected copy, so the record is live and
// scores its own CVSS.
func TestRestoredNewestCopyOverStaleRejectedCopyKeepsSeverity(t *testing.T) {
	t.Parallel()

	finding := productRetractionFinding(t,
		scopedProductCVE("nvd-old", "Rejected", "2026-01-01T00:00:00Z"),
		scopedProductCVE("nvd-new", "Analyzed", "2026-06-01T00:00:00Z"),
	)

	if finding.AdvisoryRetraction != nil {
		t.Fatalf("newest copy is live but the finding is retracted: %#v", finding.AdvisoryRetraction)
	}
	if finding.CVSSScore != 9.8 {
		t.Fatalf("CVSSScore = %v, want 9.8 from the live newest copy", finding.CVSSScore)
	}
}

// TestStaleLiveRepresentativeLendsNoSeverityBesideItsNewerLiveCopy pins the
// superseded-representative guard in severityObservation: two live copies of
// one NVD record, the older at 9.8 with the lower fact id (so it is the ranked
// representative) and the newer at 5.0, are one record whose newest copy says
// 5.0. The superseded representative must lend nothing, and the newest copy
// must supply the score, so the finding reads 5.0. A guard that left the stale
// representative's tier in place would keep the 9.8, and one that blanked it
// without giving the newest copy a turn would read 0.
func TestStaleLiveRepresentativeLendsNoSeverityBesideItsNewerLiveCopy(t *testing.T) {
	t.Parallel()

	stale := scopedProductCVE("nvd-a-old", "Analyzed", "2026-01-01T00:00:00Z")
	newer := scopedProductCVE("nvd-b-new", "Analyzed", "2026-06-01T00:00:00Z")
	newer.Payload["cvss_score"] = 5.0
	newer.Payload["severity_label"] = "MEDIUM"

	finding := productRetractionFinding(t, stale, newer)

	if finding.AdvisoryRetraction != nil {
		t.Fatalf("both copies are live but the finding is retracted: %#v", finding.AdvisoryRetraction)
	}
	if finding.CVSSScore != 5.0 {
		t.Fatalf("CVSSScore = %v, want 5.0 from the newest live copy, not the superseded representative's 9.8 or 0", finding.CVSSScore)
	}
}

// TestRejectedRecordBesideASiblingLendsNoSeverityInEveryState pins the #10773
// ruling for the product path. A rejected record at CVSS 9.8 lends no severity
// whatever its sibling is: the finding stays active (unannotated) while the
// sibling is quarantined or live, and is annotated only when every record is
// retracted. The quarantined case scores exactly what the all-retracted case
// scores (60), because the only readable number comes from a record that says
// the ID is not a vulnerability, and a quarantined fact's own score is never
// read. A live sibling supplies its own severity. The quarantined and rejected
// rows fail on the pre-#10764 product path, which kept the rejected record's
// 9.8 (score 100); the live row is a control that passes there too.
func TestRejectedRecordBesideASiblingLendsNoSeverityInEveryState(t *testing.T) {
	t.Parallel()

	rejected := func() facts.Envelope { return productCVE("nvd-rejected", "nvd", "Rejected", 9.8, "CRITICAL") }
	cases := []struct {
		name         string
		sibling      facts.Envelope
		wantState    SupplyChainSuppressionState
		wantRetract  bool
		wantCVSS     float64
		wantScore    int
		wantQuarants int
	}{
		{
			name:         "sibling quarantined",
			sibling:      quarantinedCVEFact("osv-quarantined", "CVE-2026-2100", "osv", "GHSA-live-qqqq-rrrr"),
			wantState:    SupplyChainSuppressionStateActive,
			wantCVSS:     0,
			wantScore:    60,
			wantQuarants: 1,
		},
		{
			name:      "sibling live",
			sibling:   productCVE("osv-live", "osv", "", 7.5, "HIGH"),
			wantState: SupplyChainSuppressionStateActive,
			wantCVSS:  7.5,
			wantScore: 95,
		},
		{
			name:        "sibling rejected",
			sibling:     productCVE("osv-rejected", "osv", "Rejected", 9.1, "CRITICAL"),
			wantState:   SupplyChainSuppressionStateAdvisoryRetracted,
			wantRetract: true,
			wantCVSS:    0,
			wantScore:   60,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			findings, quarantined, err := buildSupplyChainImpactFindingsWithQuarantine([]facts.Envelope{
				rejected(), tc.sibling,
				vulnerabilityAffectedProductFact("product-1", "CVE-2026-2100", testImpactProductCriteria, testImpactMatchCriteriaID, true),
				sbomComponentCPEImpactFact("component-1", "doc-1", testImpactProductCriteria),
				sbomAttachmentImpactFact("attachment-1", "doc-1", testImpactSubjectDigest),
				containerImageIdentityImpactFact("image-1", testImpactSubjectDigest, testImpactRepositoryID),
			})
			if err != nil || len(quarantined) != tc.wantQuarants || len(findings) != 1 {
				t.Fatalf("err=%v quarantined=%d findings=%d, want %d quarantined and one finding", err, len(quarantined), len(findings), tc.wantQuarants)
			}
			finding := applyRetractionDecision(findings[0])
			if finding.Suppression.State != tc.wantState {
				t.Fatalf("state = %q, want %q", finding.Suppression.State, tc.wantState)
			}
			if (finding.AdvisoryRetraction != nil) != tc.wantRetract {
				t.Fatalf("AdvisoryRetraction = %#v, want annotated=%v", finding.AdvisoryRetraction, tc.wantRetract)
			}
			if finding.CVSSScore != tc.wantCVSS || finding.PriorityScore != tc.wantScore {
				t.Fatalf("cvss=%v score=%d, want cvss=%v score=%d", finding.CVSSScore, finding.PriorityScore, tc.wantCVSS, tc.wantScore)
			}
			_, scored := cvssContribution(finding)
			if scored != (tc.wantCVSS > 0) {
				t.Fatalf("cvss contribution present=%v, want %v for cvss %v", scored, tc.wantCVSS > 0, tc.wantCVSS)
			}
			if tc.wantRetract {
				assertPriorityNamesRetraction(t, finding, AdvisoryRetractionRejected)
			}
		})
	}
}
