// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// This file holds the fact-kind vocabulary, outcome vocabulary, and
// operator-facing summary helpers for container_image_identity. Split out of
// container_image_identity.go, which otherwise passed the repository's
// 500-line file cap when the #5874 fencing-token-issuer wiring was added; the
// handler and its evidence loaders stay in container_image_identity.go.

func containerImageIdentityFactKinds() []string {
	return []string{
		factKindContentEntity,
		factKindRepository,
		facts.CICDWorkflowImageEvidenceFactKind,
		facts.CICDRunFactKind,
		facts.CICDArtifactFactKind,
		facts.AWSRelationshipFactKind,
		facts.AWSImageReferenceFactKind,
		facts.AzureImageReferenceFactKind,
		facts.GCPImageReferenceFactKind,
		facts.OCIImageTagObservationFactKind,
		facts.OCIImageManifestFactKind,
		facts.OCIImageIndexFactKind,
		facts.OCIImageReferrerFactKind,
		facts.AttestationStatementFactKind,
		facts.AttestationSLSAProvenanceFactKind,
		facts.AttestationSignatureVerificationFactKind,
	}
}

func containerImageIdentityOutcomes() []ContainerImageIdentityOutcome {
	return []ContainerImageIdentityOutcome{
		ContainerImageIdentityExactDigest,
		ContainerImageIdentityTagResolved,
		ContainerImageIdentityAmbiguousTag,
		ContainerImageIdentityUnresolved,
		ContainerImageIdentityStaleTag,
	}
}

func containerImageIdentityCounts(
	decisions []ContainerImageIdentityDecision,
) map[ContainerImageIdentityOutcome]int {
	counts := make(map[ContainerImageIdentityOutcome]int, len(containerImageIdentityOutcomes()))
	for _, decision := range decisions {
		counts[decision.Outcome]++
	}
	return counts
}

// containerImageIdentitySummary renders the operator-facing evidence line for
// one handled intent: the decision counts this pass evaluated, and how many of
// them the writer published durably.
func containerImageIdentitySummary(
	evaluated int,
	counts map[ContainerImageIdentityOutcome]int,
	canonicalWrites int,
) string {
	return fmt.Sprintf(
		"container image identity evaluated=%d exact_digest=%d tag_resolved=%d ambiguous_tag=%d unresolved=%d stale_tag=%d canonical_writes=%d",
		evaluated,
		counts[ContainerImageIdentityExactDigest],
		counts[ContainerImageIdentityTagResolved],
		counts[ContainerImageIdentityAmbiguousTag],
		counts[ContainerImageIdentityUnresolved],
		counts[ContainerImageIdentityStaleTag],
		canonicalWrites,
	)
}

func containerImageIdentityCanonicalDecisions(
	decisions []ContainerImageIdentityDecision,
) []ContainerImageIdentityDecision {
	out := make([]ContainerImageIdentityDecision, 0, len(decisions))
	for _, decision := range decisions {
		if decision.CanonicalWrites <= 0 {
			continue
		}
		out = append(out, decision)
	}
	return out
}
