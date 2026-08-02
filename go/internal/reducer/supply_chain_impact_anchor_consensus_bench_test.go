// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"fmt"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// buildSupplyChainImageIdentityBenchEnvelopes constructs a corpus-representative
// reducer_container_image_identity batch: digestCount distinct digests, each
// carrying rowsPerDigest agreeing rows (mirrors the live corpus's 20-row,
// mostly-agreeing shape from issue #5887's digest sha256:abcdef...ab).
func buildSupplyChainImageIdentityBenchEnvelopes(digestCount, rowsPerDigest int) []facts.Envelope {
	envelopes := make([]facts.Envelope, 0, digestCount*rowsPerDigest)
	for d := 0; d < digestCount; d++ {
		digest := fmt.Sprintf("sha256:bench%060d", d)
		repositoryID := fmt.Sprintf("repository:r_bench_%d", d)
		for r := 0; r < rowsPerDigest; r++ {
			envelopes = append(envelopes, containerImageIdentityImpactFactWithSourceRepositoryIDs(
				fmt.Sprintf("identity-bench-%d-%d", d, r), digest,
				fmt.Sprintf("oci-registry://registry.example/bench-app-%d", d), repositoryID,
			))
		}
	}
	return envelopes
}

// bareBestSupplyChainImageIdentitiesByDigest is the PRE-#5887 fold: the same
// loop bestSupplyChainImageIdentitiesByDigest ran before this issue's fix,
// using the unchanged, still-live preferSupplyChainImageIdentity directly
// instead of the consensus-aware wrapper. Kept only in this benchmark file
// (not the production path) to measure the #5887 fix's added cost against
// the real function it replaced, not a hand-reconstructed stand-in.
func bareBestSupplyChainImageIdentitiesByDigest(envelopes []facts.Envelope) map[string]supplyChainImageIdentity {
	winners := make(map[string]supplyChainImageIdentity)
	for _, envelope := range envelopes {
		if envelope.FactKind != containerImageIdentityFactKind {
			continue
		}
		image := supplyChainImageIdentityFromEnvelope(envelope)
		if image.digest == "" {
			continue
		}
		if existing, ok := winners[image.digest]; ok {
			image = preferSupplyChainImageIdentity(existing, image)
		}
		winners[image.digest] = image
	}
	return winners
}

// BenchmarkBestSupplyChainImageIdentitiesByDigestBare measures the PRE-#5887
// baseline (bare factID tie-break, no consensus pass) at a scale wider than
// the live corpus (50 digests x 20 rows = 1000 envelopes, vs. the live
// corpus's single digest with 20 rows) so any per-envelope cost is visible
// well above noise.
func BenchmarkBestSupplyChainImageIdentitiesByDigestBare(b *testing.B) {
	envelopes := buildSupplyChainImageIdentityBenchEnvelopes(50, 20)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = bareBestSupplyChainImageIdentitiesByDigest(envelopes)
	}
}

// BenchmarkBestSupplyChainImageIdentitiesByDigestConsensus measures the
// #5887 fix (buildSupplyChainImageIdentityConsensus pre-pass +
// preferSupplyChainImageIdentityConsensus fold) on the identical input as
// the bare-baseline benchmark above, for a direct before/after comparison
// via `go test -bench . -benchmem -run ^$`.
func BenchmarkBestSupplyChainImageIdentitiesByDigestConsensus(b *testing.B) {
	envelopes := buildSupplyChainImageIdentityBenchEnvelopes(50, 20)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = bestSupplyChainImageIdentitiesByDigest(envelopes)
	}
}
