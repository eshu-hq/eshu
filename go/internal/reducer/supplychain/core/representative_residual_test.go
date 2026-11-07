// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"maps"
	"slices"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/facts/supply/chain"
)

// TestJoinedRowWithNoDerivableFamilyStaysVisible pins the residual of the
// unsupported-matcher exemption (#9990) after #10034 item 2 emptied it: the
// fact joins an installed row and no purl names a version family, so the
// family is the row's (dpkg) and the version is compared under it. The fact
// carries no purl, so its line is unreadable and it holds no release
// evidence: the dpkg matcher reads os_release_unverified (#9945). The row is
// an owned anchor and the advisory names the package, so the finding stays,
// marked unverified.
// The join meets on the row's own package id, so an os:// id joins an os:// row. That
// is a synthetic row, because no collector writes an os:// purl; the next test holds the
// same id against a purl-keyed row.
func TestJoinedRowWithNoDerivableFamilyStaysVisible(t *testing.T) {
	t.Parallel()
	osRow := ubuntuCurl("22.04", repJoinVersion).envelope()
	osRow.Payload["purl"] = "os://ubuntu/curl@" + repJoinVersion
	for _, tc := range []struct {
		name, packageID string
		row             facts.Envelope
	}{
		{"purl package id", repJoinPackageID, ubuntuCurl("22.04", repJoinVersion).envelope()},
		{"os package id", "os://ubuntu/curl", osRow},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			findings := BuildSupplyChainImpactFindings([]facts.Envelope{
				vulnerabilityCVEFact("cve-9990", vendorLineCVE, 7.5),
				repJoinNoPURLFact(tc.packageID),
				tc.row,
			})
			if len(findings) != 1 {
				t.Fatalf("findings = %d, want 1: %#v", len(findings), findings)
			}
			got := findings[0]
			if got.Status != SupplyChainImpactPossiblyAffected || got.MatchReason != supplyChainVersionReasonOSReleaseUnverified {
				t.Fatalf("verdict = %s/%s, want possibly_affected/os_release_unverified", got.Status, got.MatchReason)
			}
			if !slices.Contains(got.EvidencePath, chain.VulnerabilityOSPackageFactKind) {
				t.Fatalf("evidence path %v, want the installed row on it", got.EvidencePath)
			}
			if len(got.MissingEvidence) == 0 {
				t.Fatal("missing evidence is empty, want the matcher reported missing")
			}
		})
	}
}

// TestOSPackageIDWithNoPURLDoesNotJoinAPURLKeyedRow pins the limit of that residual:
// installed rows are keyed by their purl, so an os:// id with no purl meets nothing,
// yields no finding, and is counted, not silent.
func TestOSPackageIDWithNoPURLDoesNotJoinAPURLKeyedRow(t *testing.T) {
	t.Parallel()
	findings, _, stats, err := buildSupplyChainImpactFindingsAndExclusions([]facts.Envelope{
		vulnerabilityCVEFact("cve-9990", vendorLineCVE, 7.5),
		repJoinNoPURLFact("os://ubuntu/curl"),
		ubuntuCurl("22.04", repJoinVersion).envelope(),
	})
	if err != nil {
		t.Fatalf("build findings: %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("findings = %d, want 0: %#v", len(findings), findings)
	}
	want := map[osRepresentativeOutcomeKey]int{{"ubuntu", osRepresentativeNoInstalledRow}: 1}
	if !maps.Equal(stats.osRepresentativeOutcomes, want) {
		t.Fatalf("outcomes = %v, want %v", stats.osRepresentativeOutcomes, want)
	}
}

// repJoinNoPURLFact is the host vendor's fact for the installed version, with a
// blank ecosystem and no purl to name a version family.
func repJoinNoPURLFact(packageID string) facts.Envelope {
	return facts.Envelope{FactID: "affected-no-purl", FactKind: chain.VulnerabilityAffectedPackageFactKind, Payload: map[string]any{
		"cve_id": vendorLineCVE, "advisory_id": "USN-9990-1", "source": "ubuntu",
		"package_id": packageID, "package_name": "curl", "affected_versions": []any{repJoinVersion},
	}}
}

// TestBlankEcosystemFactWithNoOSEvidenceIsStillDropped pins the other side of the
// exemption: a blank-ecosystem fact with no OS purl and no installed row has no
// version matcher and nothing to stand on, so the consumer alone does not keep it,
// as on the base.
func TestBlankEcosystemFactWithNoOSEvidenceIsStillDropped(t *testing.T) {
	t.Parallel()
	const packageID = "pkg:npm/example"
	findings := BuildSupplyChainImpactFindings([]facts.Envelope{
		vulnerabilityCVEFact("cve-9990", vendorLineCVE, 7.5),
		{FactID: "affected-blank", FactKind: chain.VulnerabilityAffectedPackageFactKind, Payload: map[string]any{
			"cve_id": vendorLineCVE, "advisory_id": "NVD-9990", "source": "nvd",
			"package_id": packageID, "package_name": "example", "affected_versions": []any{"1.0.0"},
		}},
		packageConsumptionFactWithRange("consume-9990", packageID, "repository:r_9990", "1.0.0"),
		ubuntuCurl("22.04", repJoinVersion).envelope(),
	})
	if len(findings) != 0 {
		t.Fatalf("findings = %d, want 0: %#v", len(findings), findings)
	}
}
