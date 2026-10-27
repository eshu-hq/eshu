// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// BenchmarkSBOMVersionGuard measures one package advisory and attached image
// through the reducer's in-memory classification path.
func BenchmarkSBOMVersionGuard(b *testing.B) {
	affected := vulnerabilityAffectedPackageMalformedRangeFact(
		"affected-bench", "CVE-2026-91913", crossScopeAffectedPackageID,
		"npm", "lodash", "4.17.11", "4.17.21",
	)
	affected.Payload["purl"] = crossScopeAffectedPURL
	component := sbomComponentImpactFactWithPackageID(
		"component-bench", "doc-bench", "pkg:npm/lodash@4.17.12", crossScopeAffectedPackageID,
	)
	component.Payload["version"] = "4.17.12"
	envelopes := []facts.Envelope{
		vulnerabilityCVEFact("cve-bench", "CVE-2026-91913", 8.0),
		affected,
		component,
		sbomAttachmentImpactFact("attachment-bench", "doc-bench", testImpactSubjectDigest),
		containerImageIdentityImpactFact("image-bench", testImpactSubjectDigest, testImpactRepositoryID),
	}
	b.ReportAllocs()
	for range b.N {
		findings := BuildSupplyChainImpactFindings(envelopes)
		if len(findings) != 1 {
			b.Fatalf("findings=%d, want 1", len(findings))
		}
	}
}
