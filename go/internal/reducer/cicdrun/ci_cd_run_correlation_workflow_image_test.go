// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cicdrun

import (
	"strings"
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

func workflowImageEvidenceOfKind(factID, repositoryID, imageRef, commandKind string) *decodedCICDWorkflowImage {
	evidence := workflowImageEvidence(factID, repositoryID, imageRef)
	evidence.evidence.CommandKind = &commandKind
	return evidence
}

func singleIdentityIndex(imageRef, digest, buildRepo string) map[string][]cicdImageIdentity {
	return map[string][]cicdImageIdentity{
		digest: {{
			factID:                       "identity-" + digest,
			imageRef:                     imageRef,
			digest:                       digest,
			buildProvenanceRepositoryIDs: []string{buildRepo},
		}},
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
			buildProvenanceRepositoryIDs: []string{repositoryID},
		}},
		"sha256:bbbb": {{
			factID:                       "identity-built-elsewhere",
			imageRef:                     imageRef,
			digest:                       "sha256:bbbb",
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
			buildProvenanceRepositoryIDs: []string{"repository:r_builder"},
		}},
		"sha256:bbbb": {{
			factID: "identity-referenced-by-deployer",
			// The deploying repository appears as a source reference only; its
			// manifest names the digest, it did not build it, so it earns no
			// build provenance and can never be narrowed to.
			imageRef: imageRef,
			digest:   "sha256:bbbb",
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
			factID:   "legacy-identity-built-here",
			imageRef: imageRef,
			digest:   "sha256:aaaa",
		}},
		"sha256:bbbb": {{
			factID:   "legacy-identity-built-elsewhere",
			imageRef: imageRef,
			digest:   "sha256:bbbb",
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

// An image named by `jobs.<job>.with.image` is CONSUMED by the workflow, not
// produced by it: workflowimage.evidenceFromReusableWorkflow stamps those
// "reusable_workflow_input", and the value is typically a scanner, base, or
// tooling image. Calling that correlation exact asserts the run produced the
// image. That is not free: incidentCICDPromotionCandidates prefers a digest
// exact match over every other candidate and incidentCICDTruthLabel then stamps
// the incident's build/deploy and commit slots as exact truth, so an input-only
// image can win build attribution away from a genuine derived candidate.
func TestClassifyCICDWorkflowImageEvidenceDemotesReusableWorkflowInput(t *testing.T) {
	t.Parallel()

	const (
		repositoryID = "repository:r_builder"
		imageRef     = "ghcr.io/eshu-hq/scanner:v1"
	)

	decision, handled := classifyCICDWorkflowImageEvidence(
		CICDRunCorrelationDecision{RepositoryID: repositoryID},
		[]*decodedCICDWorkflowImage{
			workflowImageEvidenceOfKind("wf-1", repositoryID, imageRef, "reusable_workflow_input"),
		},
		true,
		singleIdentityIndex(imageRef, "sha256:aaaa", repositoryID),
	)

	if !handled {
		t.Fatalf("classifyCICDWorkflowImageEvidence() did not handle a valid workflow_image_ref")
	}
	if decision.Outcome != CICDRunCorrelationDerived {
		t.Fatalf("Outcome = %q, want derived: a reusable-workflow input image is consumed by "+
			"this workflow, not produced by it", decision.Outcome)
	}
	if !strings.Contains(decision.Reason, "reusable-workflow input") {
		t.Fatalf("Reason = %q, want it to name the input-only evidence", decision.Reason)
	}
}

// A docker build/push command genuinely denotes the run producing the image, so
// it keeps the exact promotion.
func TestClassifyCICDWorkflowImageEvidenceKeepsExactForProducedImages(t *testing.T) {
	t.Parallel()

	const (
		repositoryID = "repository:r_builder"
		imageRef     = "ghcr.io/eshu-hq/demo:v1"
	)

	for _, commandKind := range []string{"docker_build", "docker_buildx", "docker_push", "docker_tag"} {
		decision, _ := classifyCICDWorkflowImageEvidence(
			CICDRunCorrelationDecision{RepositoryID: repositoryID},
			[]*decodedCICDWorkflowImage{
				workflowImageEvidenceOfKind("wf-1", repositoryID, imageRef, commandKind),
			},
			true,
			singleIdentityIndex(imageRef, "sha256:aaaa", repositoryID),
		)
		if decision.Outcome != CICDRunCorrelationExact {
			t.Fatalf("command_kind %q: Outcome = %q, want exact", commandKind, decision.Outcome)
		}
	}
}

// command_kind is an optional free-string field. A fact that omits it, or
// carries a kind this reducer does not know, keeps the pre-existing behavior
// rather than being silently demoted: only the one kind proven to be input-only
// is denied, so a collector emitting a new produced-image kind is not degraded
// by a reducer that has not learned about it yet.
func TestClassifyCICDWorkflowImageEvidenceFailsOpenForUnknownCommandKind(t *testing.T) {
	t.Parallel()

	const (
		repositoryID = "repository:r_builder"
		imageRef     = "ghcr.io/eshu-hq/demo:v1"
	)

	absent, _ := classifyCICDWorkflowImageEvidence(
		CICDRunCorrelationDecision{RepositoryID: repositoryID},
		[]*decodedCICDWorkflowImage{workflowImageEvidence("wf-1", repositoryID, imageRef)},
		true,
		singleIdentityIndex(imageRef, "sha256:aaaa", repositoryID),
	)
	if absent.Outcome != CICDRunCorrelationExact {
		t.Fatalf("absent command_kind: Outcome = %q, want exact (fail open)", absent.Outcome)
	}

	unknown, _ := classifyCICDWorkflowImageEvidence(
		CICDRunCorrelationDecision{RepositoryID: repositoryID},
		[]*decodedCICDWorkflowImage{
			workflowImageEvidenceOfKind("wf-1", repositoryID, imageRef, "run"),
		},
		true,
		singleIdentityIndex(imageRef, "sha256:aaaa", repositoryID),
	)
	if unknown.Outcome != CICDRunCorrelationExact {
		t.Fatalf("unknown command_kind: Outcome = %q, want exact (fail open)", unknown.Outcome)
	}
}

// Produced-image evidence must win regardless of slice order, so a run that
// both consumes a scanner image and builds its own image is not decided by
// whichever fact happened to be indexed first.
func TestClassifyCICDWorkflowImageEvidencePrefersProducedOverInput(t *testing.T) {
	t.Parallel()

	const (
		repositoryID = "repository:r_builder"
		inputRef     = "ghcr.io/eshu-hq/scanner:v1"
		builtRef     = "ghcr.io/eshu-hq/demo:v1"
	)

	imageIndex := singleIdentityIndex(inputRef, "sha256:aaaa", repositoryID)
	for digest, rows := range singleIdentityIndex(builtRef, "sha256:bbbb", repositoryID) {
		imageIndex[digest] = rows
	}

	decision, _ := classifyCICDWorkflowImageEvidence(
		CICDRunCorrelationDecision{RepositoryID: repositoryID},
		[]*decodedCICDWorkflowImage{
			workflowImageEvidenceOfKind("wf-input", repositoryID, inputRef, "reusable_workflow_input"),
			workflowImageEvidenceOfKind("wf-built", repositoryID, builtRef, "docker_build"),
		},
		true,
		imageIndex,
	)

	if decision.Outcome != CICDRunCorrelationExact {
		t.Fatalf("Outcome = %q, want exact from the docker_build evidence", decision.Outcome)
	}
	if decision.ImageRef != builtRef {
		t.Fatalf("ImageRef = %q, want the built image %q, not the consumed one", decision.ImageRef, builtRef)
	}
}
