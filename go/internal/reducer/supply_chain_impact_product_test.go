// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

const (
	testImpactProductCriteria = "cpe:2.3:a:example:server:1.4.2:*:*:*:*:*:*:*"
	testImpactMatchCriteriaID = "b5ec4c98-0000-4000-9000-000000000001"
)

func TestBuildSupplyChainImpactFindingsSkipsProductOnlyEvidenceWithoutOwnedSBOM(t *testing.T) {
	t.Parallel()

	findings := BuildSupplyChainImpactFindings([]facts.Envelope{
		vulnerabilityCVEFact("cve-product", "CVE-2026-0100", 8.6),
		vulnerabilityAffectedProductFact(
			"product-1",
			"CVE-2026-0100",
			testImpactProductCriteria,
			testImpactMatchCriteriaID,
			true,
		),
	})

	if got := len(findings); got != 0 {
		t.Fatalf("len(findings) = %d, want 0 for product-only source intelligence: %#v", got, findings)
	}
}

func TestBuildSupplyChainImpactFindingsDerivesProductImpactFromSBOMCPE(t *testing.T) {
	t.Parallel()

	findings := BuildSupplyChainImpactFindings([]facts.Envelope{
		vulnerabilityCVEFact("cve-product", "CVE-2026-0101", 8.6),
		vulnerabilityAffectedProductFact(
			"product-1",
			"CVE-2026-0101",
			testImpactProductCriteria,
			testImpactMatchCriteriaID,
			true,
		),
		sbomComponentCPEImpactFact("component-1", "doc-1", testImpactProductCriteria),
		sbomAttachmentImpactFact("attachment-1", "doc-1", testImpactSubjectDigest),
		containerImageIdentityImpactFact("image-1", testImpactSubjectDigest, testImpactRepositoryID),
	})

	if got, want := len(findings), 1; got != want {
		t.Fatalf("len(findings) = %d, want %d: %#v", got, want, findings)
	}
	got := findings[0]
	assertSupplyChainImpactStatus(t, got, SupplyChainImpactAffectedDerived)
	if got.Confidence != "derived_product" {
		t.Fatalf("Confidence = %q, want derived_product", got.Confidence)
	}
	if got.RuntimeReachability != "image_sbom" {
		t.Fatalf("RuntimeReachability = %q, want image_sbom", got.RuntimeReachability)
	}
	if got.SubjectDigest != testImpactSubjectDigest {
		t.Fatalf("SubjectDigest = %q, want %q", got.SubjectDigest, testImpactSubjectDigest)
	}
	if got.RepositoryID != testImpactRepositoryID {
		t.Fatalf("RepositoryID = %q, want %q", got.RepositoryID, testImpactRepositoryID)
	}
	if got.ObservedVersion != "1.4.2" {
		t.Fatalf("ObservedVersion = %q, want 1.4.2 from SBOM component", got.ObservedVersion)
	}
	for _, want := range []string{
		facts.VulnerabilityAffectedProductFactKind,
		facts.SBOMComponentFactKind,
		sbomAttestationAttachmentFactKind,
		containerImageIdentityFactKind,
	} {
		if !strings.Contains(strings.Join(got.EvidencePath, " -> "), want) {
			t.Fatalf("EvidencePath = %#v, want %q", got.EvidencePath, want)
		}
	}
}

func TestBuildSupplyChainImpactFindingsSkipsNonVulnerableNVDProductCriteria(t *testing.T) {
	t.Parallel()

	findings := BuildSupplyChainImpactFindings([]facts.Envelope{
		vulnerabilityCVEFact("cve-product", "CVE-2026-0105", 8.6),
		vulnerabilityAffectedProductFact(
			"product-1",
			"CVE-2026-0105",
			testImpactProductCriteria,
			testImpactMatchCriteriaID,
			false,
		),
		sbomComponentCPEImpactFact("component-1", "doc-1", testImpactProductCriteria),
		sbomAttachmentImpactFact("attachment-1", "doc-1", testImpactSubjectDigest),
		containerImageIdentityImpactFact("image-1", testImpactSubjectDigest, testImpactRepositoryID),
	})

	if got := len(findings); got != 0 {
		t.Fatalf("len(findings) = %d, want 0 for non-vulnerable product evidence: %#v", got, findings)
	}
}

func TestSupplyChainImpactFilterSkipsNonVulnerableProductCriteria(t *testing.T) {
	t.Parallel()

	filter := supplyChainImpactFilter([]facts.Envelope{
		vulnerabilityAffectedProductFact(
			"product-1",
			"CVE-2026-0106",
			testImpactProductCriteria,
			testImpactMatchCriteriaID,
			false,
		),
	})

	if got, want := strings.Join(filter.CVEIDs, ","), "CVE-2026-0106"; got != want {
		t.Fatalf("CVEIDs = %q, want %q", got, want)
	}
	if got := strings.Join(filter.ProductCriteria, ","); got != "" {
		t.Fatalf("ProductCriteria = %q, want blank for non-vulnerable product evidence", got)
	}
}

func TestBuildSupplyChainImpactFindingsRanksPackageEvidenceAboveProductEvidence(t *testing.T) {
	t.Parallel()

	findings := BuildSupplyChainImpactFindings([]facts.Envelope{
		vulnerabilityCVEFact("cve-product", "CVE-2026-0102", 8.6),
		vulnerabilityAffectedProductFact(
			"product-1",
			"CVE-2026-0102",
			testImpactProductCriteria,
			testImpactMatchCriteriaID,
			true,
		),
		vulnerabilityAffectedPackageFact(
			"affected-1",
			"CVE-2026-0102",
			testImpactPackageID,
			"npm",
			"example",
			"1.2.3",
			"1.3.0",
		),
		packageConsumptionFactWithRange("consume-1", testImpactPackageID, testImpactRepositoryID, "1.2.3"),
	})

	if got, want := len(findings), 1; got != want {
		t.Fatalf("len(findings) = %d, want %d: %#v", got, want, findings)
	}
	got := findings[0]
	assertSupplyChainImpactStatus(t, got, SupplyChainImpactAffectedExact)
	if got.ProductCriteria != "" {
		t.Fatalf("ProductCriteria = %q, want blank when package evidence wins", got.ProductCriteria)
	}
	if !strings.Contains(strings.Join(got.EvidencePath, " -> "), facts.VulnerabilityAffectedPackageFactKind) {
		t.Fatalf("EvidencePath = %#v, want affected_package evidence", got.EvidencePath)
	}
}

func TestSupplyChainImpactHandlerLoadsActiveCPEEvidenceForProductFacts(t *testing.T) {
	t.Parallel()

	loader := &stubSupplyChainImpactFactLoader{
		scopeFacts: []facts.Envelope{
			vulnerabilityCVEFact("cve-product", "CVE-2026-0103", 8.6),
			vulnerabilityAffectedProductFact(
				"product-1",
				"CVE-2026-0103",
				testImpactProductCriteria,
				testImpactMatchCriteriaID,
				true,
			),
		},
		active: []facts.Envelope{
			sbomComponentCPEImpactFact("component-1", "doc-1", testImpactProductCriteria),
			sbomAttachmentImpactFact("attachment-1", "doc-1", testImpactSubjectDigest),
			containerImageIdentityImpactFact("image-1", testImpactSubjectDigest, testImpactRepositoryID),
		},
	}
	writer := &recordingSupplyChainImpactWriter{}
	handler := SupplyChainImpactHandler{FactLoader: loader, Writer: writer}

	result, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-impact",
		ScopeID:      "vuln-intel://nvd/CVE-2026-0103",
		GenerationID: "generation-impact",
		SourceSystem: "vulnerability_intelligence",
		Domain:       DomainSupplyChainImpact,
		Cause:        "vulnerability evidence observed",
	})
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}
	if got, want := strings.Join(loader.filters[0].ProductCriteria, ","), testImpactProductCriteria; got != want {
		t.Fatalf("active product criteria = %q, want %q", got, want)
	}
	if result.CanonicalWrites != 1 {
		t.Fatalf("CanonicalWrites = %d, want 1", result.CanonicalWrites)
	}
	got := writer.write.Findings[0]
	assertSupplyChainImpactStatus(t, got, SupplyChainImpactAffectedDerived)
}

func TestSupplyChainImpactStableFactKeyIncludesProductCriteria(t *testing.T) {
	t.Parallel()

	write := SupplyChainImpactWrite{
		ScopeID:      "scope-1",
		GenerationID: "generation-1",
	}
	left := supplyChainImpactStableFactKey(write, SupplyChainImpactFinding{
		CVEID:           "CVE-2026-0104",
		ProductCriteria: "cpe:2.3:a:example:left:1.0:*:*:*:*:*:*:*",
		MatchCriteriaID: "left-match",
	})
	right := supplyChainImpactStableFactKey(write, SupplyChainImpactFinding{
		CVEID:           "CVE-2026-0104",
		ProductCriteria: "cpe:2.3:a:example:right:1.0:*:*:*:*:*:*:*",
		MatchCriteriaID: "right-match",
	})
	if left == right {
		t.Fatalf("stable fact keys collapsed distinct product criteria: %q", left)
	}
	if !strings.Contains(left, ":cpe:2.3:a:example:left:1.0:*:*:*:*:*:*:*:") {
		t.Fatalf("stable fact key = %q, want product criteria identity segment", left)
	}
}

func vulnerabilityAffectedProductFact(
	factID string,
	cveID string,
	criteria string,
	matchCriteriaID string,
	vulnerable bool,
) facts.Envelope {
	return facts.Envelope{
		FactID:   factID,
		FactKind: facts.VulnerabilityAffectedProductFactKind,
		Payload: map[string]any{
			"cve_id":            cveID,
			"criteria":          criteria,
			"match_criteria_id": matchCriteriaID,
			"vulnerable":        vulnerable,
			"source":            "nvd",
		},
	}
}

func sbomComponentCPEImpactFact(factID string, documentID string, cpe string) facts.Envelope {
	return facts.Envelope{
		FactID:   factID,
		FactKind: facts.SBOMComponentFactKind,
		Payload: map[string]any{
			"document_id": documentID,
			"cpe":         cpe,
			"name":        "example server",
			"version":     "1.4.2",
		},
	}
}
