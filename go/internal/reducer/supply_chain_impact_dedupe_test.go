// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestBuildSupplyChainImpactFindingsDedupesPURLOnlyAffectedPackage(t *testing.T) {
	t.Parallel()

	purlOnlyAffected := affectedPackageWithPURLFact(
		"affected-purl-only",
		"CVE-2026-3176",
		"",
		crossScopeAffectedPURL,
		"npm",
		"lodash",
		"4.17.11",
		"4.17.21",
	)
	findings := BuildSupplyChainImpactFindings([]facts.Envelope{
		vulnerabilityCVEFact("cve-1", "CVE-2026-3176", 8.0),
		affectedPackageWithPURLFact(
			"affected-canonical",
			"CVE-2026-3176",
			crossScopeAffectedPackageID,
			crossScopeAffectedPURL,
			"npm",
			"lodash",
			"4.17.11",
			"4.17.21",
		),
		purlOnlyAffected,
		sbomComponentImpactFactWithPackageID("component-1", "doc-1", crossScopeComponentPURL, crossScopeAffectedPackageID),
		sbomAttachmentImpactFact("attachment-1", "doc-1", testImpactSubjectDigest),
		containerImageIdentityImpactFact("image-1", testImpactSubjectDigest, testImpactRepositoryID),
	})

	if got, want := len(findings), 1; got != want {
		t.Fatalf("len(findings) = %d, want %d: %#v", got, want, findings)
	}
	got := findings[0]
	if got.PackageID != crossScopeAffectedPackageID {
		t.Fatalf("PackageID = %q, want canonical package id %q", got.PackageID, crossScopeAffectedPackageID)
	}
	assertContainsString(t, got.EvidenceFactIDs, "affected-canonical")
	assertContainsString(t, got.EvidenceFactIDs, "affected-purl-only")
}
