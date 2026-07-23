// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// BenchmarkBuildContainerImageIdentityDecisionsCICDCompetition measures the pure
// in-memory decision-build cost on the #5423 competition shape (a ci.run +
// ci.artifact whose digest a deploy repo's content_entity also declares, plus
// the resolving OCI manifest). It isolates the reducer's decision path — no fact
// I/O, queue, or graph write — so the added per-decision ci-run digest lookup is
// measurable against the same input shape.
func BenchmarkBuildContainerImageIdentityDecisionsCICDCompetition(b *testing.B) {
	imageRef := "registry.example.com/team/api@" + testContainerDigest
	envelopes := []facts.Envelope{
		ciRunFact("run-image", "github_actions", "repository:r_build", "abc123def456"),
		ciArtifactFact("artifact-image", "run-image", testContainerDigest),
		gitImageRefFact("content-declares", imageRef),
		ociManifestFact("oci-manifest", testContainerDigest),
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = BuildContainerImageIdentityDecisions(envelopes)
	}
}
