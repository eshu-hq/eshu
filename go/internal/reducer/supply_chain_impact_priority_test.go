// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"slices"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestSupplyChainImpactPriorityModelScoresRequiredSignals(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		envelopes  []facts.Envelope
		cveID      string
		wantBucket string
		wantCodes  []string
	}{
		{
			name: "high CVSS",
			envelopes: []facts.Envelope{
				vulnerabilityCVEFactWithDates(
					"cve-high-cvss",
					"CVE-2026-1001",
					9.8,
					"CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:H",
					"2026-05-01T00:00:00Z",
					"2026-05-10T00:00:00Z",
				),
				vulnerabilityAffectedPackageFact("affected-high-cvss", "CVE-2026-1001", testImpactPackageID, "npm", "example", "1.2.3", "1.3.0"),
				packageConsumptionFactWithChain("consume-high-cvss", testImpactPackageID, testImpactRepositoryID, "1.2.3", []string{"example"}, 1, true),
			},
			cveID:      "CVE-2026-1001",
			wantBucket: "critical",
			wantCodes: []string{
				"advisory_age_recent",
				"cvss_v3_critical",
				"direct_dependency",
				"exact_version_evidence",
				"fixed_version_available",
			},
		},
		{
			name: "CVSS v4",
			envelopes: []facts.Envelope{
				vulnerabilityCVEFactWithDates(
					"cve-cvss-v4",
					"CVE-2026-1006",
					9.1,
					"CVSS:4.0/AV:N/AC:L/AT:N/PR:N/UI:N/VC:H/VI:H/VA:H/SC:H/SI:H/SA:H",
					"2026-05-01T00:00:00Z",
					"2026-05-10T00:00:00Z",
				),
				vulnerabilityAffectedPackageFact("affected-cvss-v4", "CVE-2026-1006", testImpactPackageID, "npm", "example", "1.2.3", ""),
				packageConsumptionFactWithChain("consume-cvss-v4", testImpactPackageID, testImpactRepositoryID, "1.2.3", []string{"example"}, 1, true),
			},
			cveID:      "CVE-2026-1006",
			wantBucket: "high",
			wantCodes:  []string{"cvss_v4_critical"},
		},
		{
			name: "high EPSS",
			envelopes: []facts.Envelope{
				vulnerabilityCVEFact("cve-high-epss", "CVE-2026-1002", 4.3),
				vulnerabilityAffectedPackageFact("affected-high-epss", "CVE-2026-1002", testImpactPackageID, "npm", "example", "1.2.3", ""),
				vulnerabilityEPSSFact("epss-high", "CVE-2026-1002", "0.72", "0.99"),
				packageConsumptionFactWithChain("consume-high-epss", testImpactPackageID, testImpactRepositoryID, "1.2.3", []string{"example"}, 1, true),
			},
			cveID:      "CVE-2026-1002",
			wantBucket: "high",
			wantCodes:  []string{"epss_high", "epss_percentile_high"},
		},
		{
			name: "KEV listed",
			envelopes: []facts.Envelope{
				vulnerabilityCVEFact("cve-kev", "CVE-2026-1003", 5.0),
				vulnerabilityAffectedPackageFact("affected-kev", "CVE-2026-1003", testImpactPackageID, "npm", "example", "1.2.3", ""),
				vulnerabilityKEVFact("kev-listed", "CVE-2026-1003"),
				packageConsumptionFactWithRange("consume-kev", testImpactPackageID, testImpactRepositoryID, "1.2.3"),
			},
			cveID:      "CVE-2026-1003",
			wantBucket: "high",
			wantCodes:  []string{"cisa_kev"},
		},
		{
			name: "runtime reachable image",
			envelopes: []facts.Envelope{
				vulnerabilityCVEFact("cve-runtime", "CVE-2026-1004", 6.8),
				vulnerabilityAffectedPackageFact("affected-runtime", "CVE-2026-1004", testImpactPackageID, "npm", "example", "1.2.3", ""),
				sbomComponentImpactFact("component-runtime", "doc-runtime", testImpactPURL),
				sbomAttachmentImpactFact("attachment-runtime", "doc-runtime", testImpactSubjectDigest),
				containerImageIdentityImpactFact("image-runtime", testImpactSubjectDigest, testImpactRepositoryID),
			},
			cveID:      "CVE-2026-1004",
			wantBucket: "high",
			wantCodes:  []string{"runtime_reachable", "sbom_image_evidence"},
		},
		{
			name: "deployed workload evidence",
			envelopes: []facts.Envelope{
				vulnerabilityCVEFact("cve-workload", "CVE-2026-1005", 7.1),
				vulnerabilityAffectedPackageFact("affected-workload", "CVE-2026-1005", testImpactPackageID, "npm", "example", "1.2.3", ""),
				sbomComponentImpactFact("component-workload", "doc-workload", testImpactPURL),
				sbomAttachmentImpactFact("attachment-workload", "doc-workload", testImpactSubjectDigest),
				containerImageIdentityImpactFactWithOutcome(
					"image-workload",
					testImpactSubjectDigest,
					testImpactRepositoryID,
					"registry.example/api@"+testImpactSubjectDigest,
					string(ContainerImageIdentityExactDigest),
				),
				cicdRunCorrelationImpactFact(
					"deploy-workload",
					testImpactSubjectDigest,
					"registry.example/api@"+testImpactSubjectDigest,
					testImpactRepositoryID,
					"prod",
					string(CICDRunCorrelationExact),
				),
				serviceCatalogCorrelationImpactFact(
					"catalog-workload",
					testImpactRepositoryID,
					"service:api",
					"workload:api",
					string(ServiceCatalogCorrelationExact),
					"matches",
					false,
				),
			},
			cveID:      "CVE-2026-1005",
			wantBucket: "high",
			wantCodes:  []string{"deployed_workload_evidence"},
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			finding := supplyChainImpactFindingsByCVE(BuildSupplyChainImpactFindings(tc.envelopes))[tc.cveID]
			if finding.PriorityBucket != tc.wantBucket {
				t.Fatalf("PriorityBucket = %q, want %q for %#v", finding.PriorityBucket, tc.wantBucket, finding)
			}
			if finding.PriorityScore <= 0 {
				t.Fatalf("PriorityScore = %d, want positive score", finding.PriorityScore)
			}
			for _, code := range tc.wantCodes {
				if !slices.Contains(finding.PriorityReasonCodes, code) {
					t.Fatalf("PriorityReasonCodes = %#v, want %q", finding.PriorityReasonCodes, code)
				}
			}
			if len(finding.PriorityContributions) == 0 {
				t.Fatal("PriorityContributions = nil, want explainable contributions")
			}
		})
	}
}

func TestSupplyChainImpactPriorityModelPenalizesLowerRiskInputsWithoutChangingImpactTruth(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		envelopes  []facts.Envelope
		cveID      string
		wantStatus SupplyChainImpactStatus
		wantBucket string
		wantCodes  []string
	}{
		{
			name: "dev-only dependency",
			envelopes: []facts.Envelope{
				vulnerabilityCVEFact("cve-dev", "CVE-2026-2001", 9.8),
				vulnerabilityAffectedPackageFact("affected-dev", "CVE-2026-2001", testImpactPackageID, "npm", "example", "1.2.3", ""),
				packageConsumptionFactWithScope("consume-dev", testImpactPackageID, testImpactRepositoryID, "1.2.3", "dev"),
			},
			cveID:      "CVE-2026-2001",
			wantStatus: SupplyChainImpactAffectedExact,
			wantBucket: "medium",
			wantCodes:  []string{"dependency_scope_dev", "exact_version_evidence"},
		},
		{
			name: "known fixed",
			envelopes: []facts.Envelope{
				vulnerabilityCVEFact("cve-fixed", "CVE-2026-2002", 9.8),
				vulnerabilityAffectedPackageFact("affected-fixed", "CVE-2026-2002", testImpactPackageID, "npm", "example", "1.2.3", "1.3.0"),
				packageConsumptionFactWithRange("consume-fixed", testImpactPackageID, testImpactRepositoryID, "1.3.0"),
			},
			cveID:      "CVE-2026-2002",
			wantStatus: SupplyChainImpactNotAffectedKnownFixed,
			wantBucket: "low",
			wantCodes:  []string{"observed_version_known_fixed", "fixed_version_available"},
		},
		{
			name: "missing exact version evidence stays possibly affected",
			envelopes: []facts.Envelope{
				vulnerabilityCVEFact("cve-missing", "CVE-2026-2003", 9.8),
				vulnerabilityAffectedPackageFact("affected-missing", "CVE-2026-2003", testImpactPackageID, "npm", "example", "1.2.3", ""),
				packageConsumptionFactWithRange("consume-missing", testImpactPackageID, testImpactRepositoryID, "^1.2.0"),
			},
			cveID:      "CVE-2026-2003",
			wantStatus: SupplyChainImpactPossiblyAffected,
			wantBucket: "high",
			wantCodes:  []string{"range_only_version_evidence", "missing_evidence_present"},
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			finding := supplyChainImpactFindingsByCVE(BuildSupplyChainImpactFindings(tc.envelopes))[tc.cveID]
			assertSupplyChainImpactStatus(t, finding, tc.wantStatus)
			if finding.PriorityBucket != tc.wantBucket {
				t.Fatalf("PriorityBucket = %q, want %q for %#v", finding.PriorityBucket, tc.wantBucket, finding)
			}
			for _, code := range tc.wantCodes {
				if !slices.Contains(finding.PriorityReasonCodes, code) {
					t.Fatalf("PriorityReasonCodes = %#v, want %q", finding.PriorityReasonCodes, code)
				}
			}
		})
	}
}

func vulnerabilityCVEFactWithDates(
	factID string,
	cveID string,
	cvssScore float64,
	cvssVector string,
	publishedAt string,
	modifiedAt string,
) facts.Envelope {
	envelope := vulnerabilityCVEFact(factID, cveID, cvssScore)
	envelope.Payload["cvss_vector"] = cvssVector
	envelope.Payload["published_at"] = publishedAt
	envelope.Payload["modified_at"] = modifiedAt
	return envelope
}

func packageConsumptionFactWithScope(
	factID string,
	packageID string,
	repositoryID string,
	dependencyRange string,
	dependencyScope string,
) facts.Envelope {
	envelope := packageConsumptionFactWithRange(factID, packageID, repositoryID, dependencyRange)
	envelope.Payload["dependency_scope"] = dependencyScope
	return envelope
}
