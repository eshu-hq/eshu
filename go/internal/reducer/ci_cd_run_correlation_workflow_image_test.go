// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	cicdrunv1 "github.com/eshu-hq/eshu/sdk/go/factschema/cicdrun/v1"
)

// classifyCICDWorkflowImageEvidence is the SECOND caller of
// cicdImageMatchesForRepository, and it had no test file at all. Narrowing was
// a dead no-op here before #5766 (the identity payload's OCI repository_id
// could never equal a canonical repository:r_... id), so this call site always
// saw the unfiltered match set. It is now live, and it can flip a decision
// between ambiguous, derived, and exact -- which also changes CanonicalWrites.
// These tests pin that behavior at each arm.

func workflowImageEvidence(factID, repositoryID, imageRef string) *decodedCICDWorkflowImage {
	evidenceClass := "workflow_image_ref"
	return &decodedCICDWorkflowImage{
		envelope: facts.Envelope{FactID: factID},
		evidence: cicdrunv1.WorkflowImageEvidence{
			RepositoryID:  repositoryID,
			EvidenceClass: &evidenceClass,
			ImageRef:      &imageRef,
		},
	}
}

func TestClassifyCICDWorkflowImageEvidenceNarrowsMultipleRowsToExact(t *testing.T) {
	t.Parallel()

	const (
		repositoryID = "repository:r_builder"
		imageRef     = "ghcr.io/eshu-hq/demo:v1"
	)

	imageIndex := map[string][]cicdImageIdentity{
		"sha256:aaaa": {{
			factID:                       "identity-built-here",
			imageRef:                     imageRef,
			digest:                       "sha256:aaaa",
			sourceRepositoryIDs:          []string{repositoryID},
			buildProvenanceRepositoryIDs: []string{repositoryID},
		}},
		"sha256:bbbb": {{
			factID:                       "identity-built-elsewhere",
			imageRef:                     imageRef,
			digest:                       "sha256:bbbb",
			sourceRepositoryIDs:          []string{"repository:r_other"},
			buildProvenanceRepositoryIDs: []string{"repository:r_other"},
		}},
	}

	decision, handled := classifyCICDWorkflowImageEvidence(
		CICDRunCorrelationDecision{RepositoryID: repositoryID},
		[]*decodedCICDWorkflowImage{workflowImageEvidence("wf-1", repositoryID, imageRef)},
		true,
		imageIndex,
	)

	if !handled {
		t.Fatalf("classifyCICDWorkflowImageEvidence() did not handle a valid workflow_image_ref")
	}
	if decision.Outcome != CICDRunCorrelationExact {
		t.Fatalf("Outcome = %q, want exact: build-provenance narrowing reduces two ref matches to one", decision.Outcome)
	}
	if decision.ArtifactDigest != "sha256:aaaa" {
		t.Fatalf("ArtifactDigest = %q, want the row this repository built", decision.ArtifactDigest)
	}
	if decision.CanonicalWrites != 1 {
		t.Fatalf("CanonicalWrites = %d, want 1", decision.CanonicalWrites)
	}
}

// A repository that only REFERENCES the digest must not be narrowed to one row
// and promoted. Before #5823 this call site would have selected the
// reference-only row and written a canonical container_image target for a
// repository that never built the image.
func TestClassifyCICDWorkflowImageEvidenceStaysAmbiguousForReferenceOnly(t *testing.T) {
	t.Parallel()

	const (
		deployingRepo = "repository:r_deploys_only"
		imageRef      = "ghcr.io/eshu-hq/demo:v1"
	)

	imageIndex := map[string][]cicdImageIdentity{
		"sha256:aaaa": {{
			factID:                       "identity-built-elsewhere",
			imageRef:                     imageRef,
			digest:                       "sha256:aaaa",
			sourceRepositoryIDs:          []string{"repository:r_builder"},
			buildProvenanceRepositoryIDs: []string{"repository:r_builder"},
		}},
		"sha256:bbbb": {{
			factID: "identity-referenced-by-deployer",
			// The deploying repository appears as a source reference only; its
			// manifest names the digest, it did not build it.
			imageRef:            imageRef,
			digest:              "sha256:bbbb",
			sourceRepositoryIDs: []string{deployingRepo},
		}},
	}

	decision, handled := classifyCICDWorkflowImageEvidence(
		CICDRunCorrelationDecision{RepositoryID: deployingRepo},
		[]*decodedCICDWorkflowImage{workflowImageEvidence("wf-1", deployingRepo, imageRef)},
		true,
		imageIndex,
	)

	if !handled {
		t.Fatalf("classifyCICDWorkflowImageEvidence() did not handle a valid workflow_image_ref")
	}
	if decision.Outcome != CICDRunCorrelationAmbiguous {
		t.Fatalf("Outcome = %q, want ambiguous: a reference-only repository must not narrow to one row", decision.Outcome)
	}
	if decision.CanonicalWrites != 0 {
		t.Fatalf("CanonicalWrites = %d, want 0 for an ambiguous outcome", decision.CanonicalWrites)
	}
}

// The repository-wide fallback path (#5424) must stay derived, not exact, even
// when narrowing reduces the match set to one. Narrowing changes WHICH row is
// selected; it must not upgrade the confidence tier commitMatched governs.
func TestClassifyCICDWorkflowImageEvidenceFallbackStaysDerived(t *testing.T) {
	t.Parallel()

	const (
		repositoryID = "repository:r_builder"
		imageRef     = "ghcr.io/eshu-hq/demo:v1"
	)

	imageIndex := map[string][]cicdImageIdentity{
		"sha256:aaaa": {{
			factID:                       "identity-built-here",
			imageRef:                     imageRef,
			digest:                       "sha256:aaaa",
			buildProvenanceRepositoryIDs: []string{repositoryID},
		}},
		"sha256:bbbb": {{
			factID:                       "identity-built-elsewhere",
			imageRef:                     imageRef,
			digest:                       "sha256:bbbb",
			buildProvenanceRepositoryIDs: []string{"repository:r_other"},
		}},
	}

	decision, _ := classifyCICDWorkflowImageEvidence(
		CICDRunCorrelationDecision{RepositoryID: repositoryID},
		[]*decodedCICDWorkflowImage{workflowImageEvidence("wf-1", repositoryID, imageRef)},
		false,
		imageIndex,
	)

	if decision.Outcome != CICDRunCorrelationDerived {
		t.Fatalf("Outcome = %q, want derived: a repository-wide fallback is not commit-scoped", decision.Outcome)
	}
	if decision.ArtifactDigest != "sha256:aaaa" {
		t.Fatalf("ArtifactDigest = %q, want the narrowed row", decision.ArtifactDigest)
	}
}

// Legacy identity payloads carry no build-provenance key, so they are never
// selected here either. This is the second caller's half of the correction: an
// earlier revision fell back to source_repository_ids for legacy rows, which
// would have let a repository that merely references the digest narrow the set
// to one and take a canonical write. Legacy multi-row digests resolve ambiguous,
// which is exactly what origin/main does for this input.
func TestClassifyCICDWorkflowImageEvidenceStaysAmbiguousForLegacyPayloads(t *testing.T) {
	t.Parallel()

	const (
		repositoryID = "repository:r_builder"
		imageRef     = "ghcr.io/eshu-hq/demo:v1"
	)

	imageIndex := map[string][]cicdImageIdentity{
		"sha256:aaaa": {{
			factID:              "legacy-identity-built-here",
			imageRef:            imageRef,
			digest:              "sha256:aaaa",
			sourceRepositoryIDs: []string{repositoryID},
		}},
		"sha256:bbbb": {{
			factID:              "legacy-identity-built-elsewhere",
			imageRef:            imageRef,
			digest:              "sha256:bbbb",
			sourceRepositoryIDs: []string{"repository:r_other"},
		}},
	}

	decision, _ := classifyCICDWorkflowImageEvidence(
		CICDRunCorrelationDecision{RepositoryID: repositoryID},
		[]*decodedCICDWorkflowImage{workflowImageEvidence("wf-1", repositoryID, imageRef)},
		true,
		imageIndex,
	)

	if decision.Outcome != CICDRunCorrelationAmbiguous {
		t.Fatalf("Outcome = %q, want ambiguous: legacy rows carry no build evidence to narrow on", decision.Outcome)
	}
	if decision.CanonicalWrites != 0 {
		t.Fatalf("CanonicalWrites = %d, want 0", decision.CanonicalWrites)
	}
}

// An image ref with no identity row at all must report the no-match reason and
// write nothing canonical.
func TestClassifyCICDWorkflowImageEvidenceHandlesNoMatch(t *testing.T) {
	t.Parallel()

	const repositoryID = "repository:r_builder"

	decision, handled := classifyCICDWorkflowImageEvidence(
		CICDRunCorrelationDecision{RepositoryID: repositoryID},
		[]*decodedCICDWorkflowImage{workflowImageEvidence("wf-1", repositoryID, "ghcr.io/eshu-hq/absent:v1")},
		true,
		map[string][]cicdImageIdentity{},
	)

	if !handled {
		t.Fatalf("classifyCICDWorkflowImageEvidence() did not handle a workflow_image_ref with no match")
	}
	if decision.CanonicalWrites != 0 {
		t.Fatalf("CanonicalWrites = %d, want 0 when no identity row matches", decision.CanonicalWrites)
	}
	if decision.Reason != "workflow image ref has no matching container image identity row" {
		t.Fatalf("Reason = %q, want the no-match reason", decision.Reason)
	}
}
