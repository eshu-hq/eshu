// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"slices"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/facts/supply/chain"
	"github.com/eshu-hq/eshu/go/internal/reducer/supplychainmodel"
)

const (
	rpmListHostEVR  = "7.61.1-23.el8"
	rpmListFixedEVR = "7.61.1-30.el8"
)

// rpmListFact is an rpm affected_package fact that lists the host's exact
// version and carries no ECOSYSTEM range, the shape the legacy rpm list loop
// reads (#9945). source is the advisory source: a vendor, OSV or blank.
func rpmListFact(source string) facts.Envelope {
	return facts.Envelope{FactID: "affected-rpm-list-9945", FactKind: chain.VulnerabilityAffectedPackageFactKind, Payload: map[string]any{
		"cve_id": vendorLineCVE, "advisory_id": "ALSA-9945-1", "source": source,
		"package_id": "pkg:rpm/alma/curl", "ecosystem": "rpm", "package_name": "curl",
		"purl":              "pkg:rpm/alma/curl?distro=alma-8",
		"affected_versions": []any{rpmListHostEVR},
	}}
}

// TestRPMExactVersionListNeedsReleaseEvidence pins R2-2 (#9945). The host-line
// table has no rpm row, so an rpm fact never earns a verified release line,
// and a list that names the installed EVR is not affected_exact on its own: it
// reads possibly_affected with os_release_unverified, on the joined-row path
// and on the SBOM-only path, for a vendor fact and for a blank-source fact,
// in both fact orders. (A blank-source fact never joins an installed row.)
func TestRPMExactVersionListNeedsReleaseEvidence(t *testing.T) {
	t.Parallel()
	const digest = "sha256:ab000000000000000000000000000000000000000000000000000000000000ab"
	cve := vulnerabilityCVEFact("cve-9945", vendorLineCVE, 7.5)
	for _, source := range []string{"alma", ""} {
		affected := rpmListFact(source)
		if source != "" {
			t.Run("row/source="+source, func(t *testing.T) {
				t.Parallel()
				host := almaCurl("8", rpmListHostEVR).envelope()
				for _, order := range [][]facts.Envelope{{cve, affected, host}, {host, affected, cve}} {
					findings := BuildSupplyChainImpactFindings(slices.Clone(order))
					if len(findings) != 1 {
						t.Fatalf("findings = %d, want 1: %#v", len(findings), findings)
					}
					assertVendorLineVerdict(t, findings[0], SupplyChainImpactPossiblyAffected, supplyChainVersionReasonOSReleaseUnverified)
				}
			})
		}
		t.Run("sbom-only/source="+source, func(t *testing.T) {
			t.Parallel()
			component := sbomComponentImpactFactWithPackageIDVersion(
				"comp", "doc-1", "pkg:rpm/alma/curl@"+rpmListHostEVR+"?distro=alma-8", "pkg:rpm/alma/curl", rpmListHostEVR)
			component.Payload["name"] = "curl"
			attachment := sbomAttachmentImpactFact("att", "doc-1", digest)
			image := containerImageIdentityImpactFact("img", digest, "oci-registry://registry.example/app")
			for _, order := range [][]facts.Envelope{
				{cve, affected, component, attachment, image},
				{image, attachment, component, affected, cve},
			} {
				findings := BuildSupplyChainImpactFindings(slices.Clone(order))
				if len(findings) != 1 {
					t.Fatalf("findings = %d, want 1: %#v", len(findings), findings)
				}
				assertVendorLineVerdict(t, findings[0], SupplyChainImpactPossiblyAffected, supplyChainVersionReasonOSReleaseUnverified)
			}
		})
	}
}

// TestRPMExactVersionListWithReleaseEvidenceStaysExact is the control for
// TestRPMExactVersionListNeedsReleaseEvidence: a fact that carries a verified
// ReleaseLine still reads affected_exact, so the rule withholds exact only for
// a list with no release evidence.
func TestRPMExactVersionListWithReleaseEvidenceStaysExact(t *testing.T) {
	t.Parallel()
	pkg := supplychainmodel.AffectedPackage{AffectedVersions: []string{rpmListHostEVR}, ReleaseLine: "alma:8"}
	decision := evaluateRPMVersionMatch(rpmListHostEVR, "", []supplychainmodel.AffectedPackage{pkg})
	if decision.Status != SupplyChainImpactAffectedExact || decision.Reason != supplyChainVersionReasonRPMExactAffected {
		t.Fatalf("verified rpm list = %s/%s, want affected_exact/%s", decision.Status, decision.Reason, supplyChainVersionReasonRPMExactAffected)
	}
	pkg.ReleaseLine = ""
	decision = evaluateRPMVersionMatch(rpmListHostEVR, "", []supplychainmodel.AffectedPackage{pkg})
	if decision.Status != SupplyChainImpactPossiblyAffected || decision.Reason != supplyChainVersionReasonOSReleaseUnverified {
		t.Fatalf("unverified rpm list = %s/%s, want possibly_affected/%s", decision.Status, decision.Reason, supplyChainVersionReasonOSReleaseUnverified)
	}
	// A later unreadable entry still fails closed as malformed, not as the
	// unverified read the earlier match set.
	pkg.AffectedVersions = []string{rpmListHostEVR, ""}
	decision = evaluateRPMVersionMatch(rpmListHostEVR, "", []supplychainmodel.AffectedPackage{pkg})
	if decision.Status != SupplyChainImpactPossiblyAffected || decision.Reason != supplyChainVersionReasonMalformedRange {
		t.Fatalf("malformed rpm entry = %s/%s, want possibly_affected/%s", decision.Status, decision.Reason, supplyChainVersionReasonMalformedRange)
	}
}
