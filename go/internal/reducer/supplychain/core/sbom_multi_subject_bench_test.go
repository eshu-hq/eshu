// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"fmt"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// BenchmarkSBOMMultiSubjectFanout measures one advisory across one, two, and
// sixteen independently attached image subjects.
func BenchmarkSBOMMultiSubjectFanout(b *testing.B) {
	for _, count := range []int{1, 2, 16} {
		b.Run(fmt.Sprintf("subjects_%d", count), func(b *testing.B) {
			envelopes := []facts.Envelope{
				vulnerabilityCVEFact("cve-bench", "CVE-2026-91913", 8.0),
				vulnerabilityAffectedPackageMalformedRangeFact("affected-bench", "CVE-2026-91913", crossScopeAffectedPackageID, "npm", "lodash", "4.17.11", "4.17.21"),
			}
			for i := range count {
				id := fmt.Sprintf("bench-%d", i)
				digest := fmt.Sprintf("sha256:%064x", i+1)
				envelopes = append(envelopes,
					sbomComponentImpactFactWithPackageIDVersion("component-"+id, "doc-"+id, "pkg:npm/lodash@4.17.12", crossScopeAffectedPackageID, "4.17.12"),
					sbomAttachmentImpactFact("attachment-"+id, "doc-"+id, digest),
					containerImageIdentityImpactFact("image-"+id, digest, testImpactRepositoryID),
				)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				findings := BuildSupplyChainImpactFindings(envelopes)
				if len(findings) != count {
					b.Fatalf("findings=%d, want %d", len(findings), count)
				}
			}
		})
	}
}
