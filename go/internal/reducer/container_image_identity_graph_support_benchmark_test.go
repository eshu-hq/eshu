// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"fmt"
	"testing"
)

func BenchmarkContainerImageIdentityGraphRows(b *testing.B) {
	const repositoryID = "repository:performance"
	for _, size := range []int{1000, 5000} {
		decisions := make([]ContainerImageIdentityDecision, size)
		for index := range decisions {
			digest := fmt.Sprintf("sha256:%064x", index+1)
			decisions[index] = ContainerImageIdentityDecision{
				ImageRef:                     fmt.Sprintf("registry.example.com/performance/image-%d@%s", index, digest),
				Digest:                       digest,
				BuildProvenanceRepositoryIDs: []string{repositoryID},
				Outcome:                      ContainerImageIdentityExactDigest,
				CanonicalWrites:              1,
			}
		}
		decisions[0].BuildProvenanceRepositoryIDs = nil
		decisions[0].BaseImageForRepositoryIDs = []string{repositoryID}
		supports := containerImageIdentityGraphEquivalenceSupports(b, decisions)

		b.Run(fmt.Sprintf("built_from/decisions_%d", size), func(b *testing.B) {
			b.ReportAllocs()
			for range b.N {
				if rows := containerImageBuiltFromRows(decisions); len(rows) != size-1 {
					b.Fatalf("BUILT_FROM decision rows = %d, want %d", len(rows), size-1)
				}
			}
		})
		b.Run(fmt.Sprintf("built_from/effective_supports_%d", size), func(b *testing.B) {
			b.ReportAllocs()
			for range b.N {
				if rows := containerImageBuiltFromSupportRows(supports); len(rows) != size-1 {
					b.Fatalf("BUILT_FROM support rows = %d, want %d", len(rows), size-1)
				}
			}
		})
		b.Run(fmt.Sprintf("derived_from/decisions_%d", size), func(b *testing.B) {
			b.ReportAllocs()
			for range b.N {
				if rows := containerImageDerivedFromRows(decisions, repositoryID); len(rows) != size-1 {
					b.Fatalf("DERIVED_FROM decision rows = %d, want %d", len(rows), size-1)
				}
			}
		})
		b.Run(fmt.Sprintf("derived_from/effective_supports_%d", size), func(b *testing.B) {
			b.ReportAllocs()
			for range b.N {
				if rows := containerImageDerivedFromSupportRows(supports, repositoryID); len(rows) != size-1 {
					b.Fatalf("DERIVED_FROM support rows = %d, want %d", len(rows), size-1)
				}
			}
		})
	}
}
