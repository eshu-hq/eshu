// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"fmt"
	"testing"
)

// benchPackageOwnershipDecisions builds n exact-outcome package-ownership
// decisions with distinct package/repository ids, for B-9 (#3802)
// credential-free micro-benchmarking of the row-building path.
func benchPackageOwnershipDecisions(n int) []PackageSourceCorrelationDecision {
	decisions := make([]PackageSourceCorrelationDecision, 0, n)
	for i := 0; i < n; i++ {
		decisions = append(decisions, PackageSourceCorrelationDecision{
			PackageID:    fmt.Sprintf("pkg-%d", i),
			RepositoryID: fmt.Sprintf("repo-%d", i),
			Outcome:      PackageSourceCorrelationExact,
		})
	}
	return decisions
}

func benchPackagePublicationDecisions(n int) []PackagePublicationDecision {
	decisions := make([]PackagePublicationDecision, 0, n)
	for i := 0; i < n; i++ {
		decisions = append(decisions, PackagePublicationDecision{
			PackageID:    fmt.Sprintf("pkg-%d", i),
			VersionID:    fmt.Sprintf("pkg-%d@1.0.0", i),
			RepositoryID: fmt.Sprintf("repo-%d", i),
			Outcome:      PackageSourceCorrelationExact,
		})
	}
	return decisions
}

func benchContainerImageIdentityDecisions(n int) []ContainerImageIdentityDecision {
	decisions := make([]ContainerImageIdentityDecision, 0, n)
	for i := 0; i < n; i++ {
		decisions = append(decisions, ContainerImageIdentityDecision{
			Digest:              fmt.Sprintf("sha256:%064d", i),
			SourceRepositoryIDs: []string{fmt.Sprintf("repo-%d", i)},
			Outcome:             ContainerImageIdentityExactDigest,
		})
	}
	return decisions
}

// BenchmarkPackageOwnershipPublishesRows measures the PUBLISHES row-building
// filter over package-ownership decisions (B-9 #3802 cost-budget evidence for
// issue #5457).
func BenchmarkPackageOwnershipPublishesRows(b *testing.B) {
	decisions := benchPackageOwnershipDecisions(5000)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rows := packageOwnershipPublishesRows(decisions)
		if len(rows) != 5000 {
			b.Fatalf("rows = %d, want 5000", len(rows))
		}
	}
}

// BenchmarkPackagePublicationPublishesRows measures the PUBLISHES row-building
// filter over package-publication decisions (B-9 #3802 cost-budget evidence
// for issue #5457).
func BenchmarkPackagePublicationPublishesRows(b *testing.B) {
	decisions := benchPackagePublicationDecisions(5000)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rows := packagePublicationPublishesRows(decisions)
		if len(rows) != 5000 {
			b.Fatalf("rows = %d, want 5000", len(rows))
		}
	}
}

// BenchmarkContainerImageBuiltFromRows measures the BUILT_FROM row-building
// filter over container-image-identity decisions (B-9 #3802 cost-budget
// evidence for issue #5457).
func BenchmarkContainerImageBuiltFromRows(b *testing.B) {
	decisions := benchContainerImageIdentityDecisions(5000)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rows := containerImageBuiltFromRows(decisions)
		if len(rows) != 5000 {
			b.Fatalf("rows = %d, want 5000", len(rows))
		}
	}
}
