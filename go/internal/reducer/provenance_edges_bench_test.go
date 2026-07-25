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
		repoID := fmt.Sprintf("repo-%d", i)
		decisions = append(decisions, ContainerImageIdentityDecision{
			Digest:              fmt.Sprintf("sha256:%064d", i),
			SourceRepositoryIDs: []string{repoID},
			// BUILT_FROM (#5796) gates on BuildProvenanceRepositoryIDs, not the
			// broader SourceRepositoryIDs; this benchmark's decisions must carry
			// build evidence too, or containerImageBuiltFromRows emits zero rows
			// and the below != 5000 check fails every call (the exact class of
			// gap #5808 already fixed for the real evidence-extraction path).
			BuildProvenanceRepositoryIDs: []string{repoID},
			Outcome:                      ContainerImageIdentityExactDigest,
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

// benchContainerImageBaseLineageDecisions builds the realistic worst case for
// ONE identity intent (B-9 #3802 cost-budget evidence, issue #5460): the owning
// repository "repo-own" declares one base and builds n children, and the intent
// also sees 2n cross-scope noise decisions (other repositories' bases and
// children reached through the active fact load) that the owner-scoped builder
// must scan past without emitting. So the input is 2n+n+1 decisions and the
// output is n rows, all for repo-own.
func benchContainerImageBaseLineageDecisions(n int) []ContainerImageIdentityDecision {
	decisions := make([]ContainerImageIdentityDecision, 0, 3*n+1)
	decisions = append(decisions, ContainerImageIdentityDecision{
		Digest:                    "sha256:b" + fmt.Sprintf("%063d", 0),
		BaseImageForRepositoryIDs: []string{benchDerivedFromOwningRepo},
		Outcome:                   ContainerImageIdentityExactDigest,
	})
	for i := 0; i < n; i++ {
		decisions = append(decisions, ContainerImageIdentityDecision{
			Digest: fmt.Sprintf("sha256:c%063d", i),
			// Children of the owning repository carry build provenance, which is
			// what the DERIVED_FROM child gate keys on (#5460).
			SourceRepositoryIDs:          []string{benchDerivedFromOwningRepo},
			BuildProvenanceRepositoryIDs: []string{benchDerivedFromOwningRepo},
			Outcome:                      ContainerImageIdentityExactDigest,
		})
		// Cross-scope noise: another repository's base and child, which the
		// owner-scoped builder must skip.
		noiseRepo := fmt.Sprintf("repository:noise-%d", i)
		decisions = append(decisions, ContainerImageIdentityDecision{
			Digest:                    fmt.Sprintf("sha256:n%063d", i),
			BaseImageForRepositoryIDs: []string{noiseRepo},
			Outcome:                   ContainerImageIdentityExactDigest,
		})
		decisions = append(decisions, ContainerImageIdentityDecision{
			Digest:                       fmt.Sprintf("sha256:m%063d", i),
			SourceRepositoryIDs:          []string{noiseRepo},
			BuildProvenanceRepositoryIDs: []string{noiseRepo},
			Outcome:                      ContainerImageIdentityExactDigest,
		})
	}
	return decisions
}

const benchDerivedFromOwningRepo = "repository:repo-own"

// BenchmarkContainerImageDerivedFromRows measures the owner-scoped DERIVED_FROM
// row-building path over one intent's decision set (B-9 #3802 cost-budget
// evidence for issue #5460). The builder makes one pass to find the owning
// repository's single base and a second to emit its children, skipping all
// cross-scope noise.
func BenchmarkContainerImageDerivedFromRows(b *testing.B) {
	decisions := benchContainerImageBaseLineageDecisions(2500)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rows := containerImageDerivedFromRows(decisions, benchDerivedFromOwningRepo)
		if len(rows) != 2500 {
			b.Fatalf("rows = %d, want 2500", len(rows))
		}
	}
}
