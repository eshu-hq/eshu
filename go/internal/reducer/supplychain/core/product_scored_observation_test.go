// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// productScoredObservationFacts builds the SBOM-CPE product path for one CVE
// with the given CVE observations ahead of the shared product and SBOM
// evidence.
func productScoredObservationFacts(cves ...facts.Envelope) []facts.Envelope {
	out := append([]facts.Envelope(nil), cves...)
	return append(out,
		vulnerabilityAffectedProductFact("product-1", "CVE-2026-0110", testImpactProductCriteria, testImpactMatchCriteriaID, true),
		sbomComponentCPEImpactFact("component-1", "doc-1", testImpactProductCriteria),
		sbomAttachmentImpactFact("attachment-1", "doc-1", testImpactSubjectDigest),
		containerImageIdentityImpactFact("image-1", testImpactSubjectDigest, testImpactRepositoryID),
	)
}

// R9 (eshu-hq/eshu#10717): the CVE List ranks 5, above NVD at 6, so an
// unscored cve_list row wins the product path's representative pick. The
// finding must still carry NVD's score, the way the package path's severity
// fallback finds it.
func TestProductFindingCarriesNVDScoreWhenCVEListRowIsUnscored(t *testing.T) {
	t.Parallel()

	findings := BuildSupplyChainImpactFindings(productScoredObservationFacts(
		vulnerabilityCVEFactWithProvenance("cve-a-list", "CVE-2026-0110", "cve_list", "CVE-2026-0110", 0, "", "", "2026-02-01T00:00:00Z"),
		vulnerabilityCVEFactWithProvenance("cve-b-nvd", "CVE-2026-0110", "nvd", "CVE-2026-0110", 7.5, "", "HIGH", "2026-02-01T00:00:00Z"),
	))

	if len(findings) != 1 {
		t.Fatalf("len(findings) = %d, want 1: %#v", len(findings), findings)
	}
	if got := findings[0].CVSSScore; got != 7.5 {
		t.Fatalf("CVSSScore = %v, want NVD's 7.5 behind the unscored cve_list row", got)
	}
	if got := findings[0].SeverityLabel; got != "HIGH" {
		t.Fatalf("SeverityLabel = %q, want NVD's HIGH", got)
	}
}

// Taking the score from another observation must not move the finding's
// identity: advisory_id and the fact ids still come from the ranked
// representative, so finding_id and suppression matching stay stable.
func TestProductScoredObservationKeepsFindingIdentity(t *testing.T) {
	t.Parallel()

	listRow := vulnerabilityCVEFactWithProvenance("cve-a-list", "CVE-2026-0110", "cve_list", "CVE-2026-0110", 0, "", "", "2026-02-01T00:00:00Z")
	nvdRow := vulnerabilityCVEFactWithProvenance("cve-b-nvd", "CVE-2026-0110", "nvd", "CVE-2026-0110", 7.5, "", "HIGH", "2026-02-01T00:00:00Z")
	nvdRow.Payload["advisory_id"] = "NVD-ONLY-ADVISORY"

	withNVD := BuildSupplyChainImpactFindings(productScoredObservationFacts(listRow, nvdRow))
	listOnly := BuildSupplyChainImpactFindings(productScoredObservationFacts(listRow))
	if len(withNVD) != 1 || len(listOnly) != 1 {
		t.Fatalf("findings = %d and %d, want 1 each", len(withNVD), len(listOnly))
	}
	if withNVD[0].AdvisoryID != listOnly[0].AdvisoryID {
		t.Fatalf("AdvisoryID = %q, want the ranked representative's %q", withNVD[0].AdvisoryID, listOnly[0].AdvisoryID)
	}
	if got, want := supplyChainImpactFindingID(withNVD[0]), supplyChainImpactFindingID(listOnly[0]); got != want {
		t.Fatalf("finding id = %q, want unchanged %q", got, want)
	}
}

// A scored representative still decides alone: the fallback only fills a gap.
func TestProductScoredRepresentativeKeepsItsOwnScore(t *testing.T) {
	t.Parallel()

	findings := BuildSupplyChainImpactFindings(productScoredObservationFacts(
		vulnerabilityCVEFactWithProvenance("cve-a-list", "CVE-2026-0110", "cve_list", "CVE-2026-0110", 9.1, "", "CRITICAL", "2026-02-01T00:00:00Z"),
		vulnerabilityCVEFactWithProvenance("cve-b-nvd", "CVE-2026-0110", "nvd", "CVE-2026-0110", 7.5, "", "HIGH", "2026-02-01T00:00:00Z"),
	))
	if len(findings) != 1 || findings[0].CVSSScore != 9.1 {
		t.Fatalf("findings = %#v, want one finding with the CNA's 9.1", findings)
	}
}

// A withdrawn scored row must not lend its score to the finding.
func TestProductScoredObservationIgnoresWithdrawnRow(t *testing.T) {
	t.Parallel()

	findings := BuildSupplyChainImpactFindings(productScoredObservationFacts(
		vulnerabilityCVEFactWithProvenance("cve-a-list", "CVE-2026-0110", "cve_list", "CVE-2026-0110", 0, "", "", "2026-02-01T00:00:00Z"),
		vulnerabilityCVEFactWithdrawn("cve-b-nvd", "CVE-2026-0110", "nvd", "CVE-2026-0110", 7.5, "HIGH", "2026-02-01T00:00:00Z", "2026-03-01T00:00:00Z"),
	))
	if len(findings) != 1 || findings[0].CVSSScore != 0 {
		t.Fatalf("findings = %#v, want one finding with no score from a withdrawn row", findings)
	}
}
