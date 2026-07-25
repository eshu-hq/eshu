// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"slices"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestBuildContainerImageIdentityDecisionsBuildProvenanceFromOCISourceLabel pins
// the build-evidence half of base-image lineage (#5460). DERIVED_FROM gates its
// child side on BuildProvenanceRepositoryIDs, so an image whose OCI config
// declares the repository that built it must carry that repository here -- even
// when the same image is ALSO merely referenced by that repository's Kubernetes
// manifest, which is how the golden-corpus fixture observes it.
//
// The k8s reference alone must never confer build provenance: that is exactly the
// third-party-image false positive the gate exists to stop.
func TestBuildContainerImageIdentityDecisionsBuildProvenanceFromOCISourceLabel(t *testing.T) {
	t.Parallel()

	const (
		repoID    = "repository:r_86b8b612"
		remoteURL = "https://github.com/acme/container-base-lineage"
	)

	decisions := BuildContainerImageIdentityDecisions([]facts.Envelope{
		repositoryRemoteFact(repoID, remoteURL),
		ociManifestWithConfigLabels("oci-manifest-child", testContainerDigest, map[string]string{
			"org.opencontainers.image.source": remoteURL,
		}),
	})

	var found bool
	for _, decision := range decisions {
		if decision.Digest != testContainerDigest {
			continue
		}
		found = true
		if !slices.Contains(decision.BuildProvenanceRepositoryIDs, repoID) {
			t.Fatalf(
				"BuildProvenanceRepositoryIDs = %#v, want %q: an OCI config source label naming the repository is build evidence, so DERIVED_FROM may treat this image as that repository's child",
				decision.BuildProvenanceRepositoryIDs, repoID,
			)
		}
	}
	if !found {
		t.Fatalf("no decision for digest %q in %#v", testContainerDigest, decisions)
	}
}

// TestBuildContainerImageIdentityDecisionsKubernetesReferenceIsNotBuildProvenance
// is the negative half: a repository whose Kubernetes manifest names a
// digest-pinned third-party image lands in SourceRepositoryIDs (the reference
// arrives on that repository's own content_entity fact and inherits its scope
// anchor), but must NOT gain build provenance -- claiming it built that image
// would fabricate CVE-inheritance lineage from its Dockerfile base (#5460).
func TestBuildContainerImageIdentityDecisionsKubernetesReferenceIsNotBuildProvenance(t *testing.T) {
	t.Parallel()

	const repoID = "repository:r_86b8b612"

	decisions := BuildContainerImageIdentityDecisions([]facts.Envelope{
		{
			FactID:       "content-entity-k8s",
			ScopeID:      "git-repository-scope:" + repoID,
			GenerationID: "gen-1",
			FactKind:     "content_entity",
			SourceRef:    facts.Ref{SourceSystem: "git"},
			Payload: map[string]any{
				"repo_id": repoID,
				"entity_metadata": map[string]any{
					"container_images": []any{"docker.io/library/postgres@" + testContainerDigest},
				},
			},
		},
	})

	for _, decision := range decisions {
		if slices.Contains(decision.BuildProvenanceRepositoryIDs, repoID) {
			t.Fatalf(
				"a Kubernetes image reference conferred build provenance on %q: BuildProvenanceRepositoryIDs = %#v",
				decision.ImageRef, decision.BuildProvenanceRepositoryIDs,
			)
		}
	}
}

// TestContainerImageDerivedFromRowsCorpusShape reproduces the exact golden-corpus
// rc-167 shape end to end at unit level: the repository, its Dockerfile-declared
// base, the built child image whose OCI config source label names that
// repository, and the Kubernetes manifest that also references the child. It is
// the unit-level twin of the live B-7 assertion, so a regression is caught
// without a Docker stack.
func TestContainerImageDerivedFromRowsCorpusShape(t *testing.T) {
	t.Parallel()

	const (
		repoID      = "repository:r_86b8b612"
		remoteURL   = "https://github.com/acme/container-base-lineage"
		childDigest = "sha256:c41d0000000000000000000000000000000000000000000000000000000000cd"
		baseDigest  = "sha256:ba5e0000000000000000000000000000000000000000000000000000000000ba"
		childRef    = "ghcr.io/eshu-hq/lineage-app@" + childDigest
	)

	envelopes := []facts.Envelope{
		repositoryRemoteFact(repoID, remoteURL),
		// The built child: its OCI config declares the repository that built it.
		ociManifestWithConfigLabels("oci-manifest-child", childDigest, map[string]string{
			"org.opencontainers.image.source": remoteURL,
		}),
		// The same child, also referenced by the repository's Kubernetes manifest.
		{
			FactID:       "content-entity-k8s",
			ScopeID:      "git-repository-scope:" + repoID,
			GenerationID: "gen-1",
			FactKind:     "content_entity",
			SourceRef:    facts.Ref{SourceSystem: "git"},
			Payload: map[string]any{
				"repo_id": repoID,
				"entity_metadata": map[string]any{
					"container_images": []any{childRef},
				},
			},
		},
	}

	decisions := BuildContainerImageIdentityDecisions(envelopes)

	var childProvenance []string
	for _, decision := range decisions {
		if decision.Digest == childDigest {
			childProvenance = append(childProvenance, decision.BuildProvenanceRepositoryIDs...)
		}
	}
	if !slices.Contains(childProvenance, repoID) {
		t.Fatalf(
			"the child image lost its build provenance when the Kubernetes reference merged with the OCI source label: got %#v, want %q",
			childProvenance, repoID,
		)
	}
}

// TestBuildProvenanceSurvivesDuplicateRepositoryFacts pins the deduping rule in
// matchOCIConfigSourceRepository. A repository legitimately carries several
// active facts (more than one scope or collector observing the same repo), and
// counting raw fact matches instead of distinct repositories rejected an
// unambiguous source label the moment a second fact existed -- silently
// disabling the build-evidence tier that base-image lineage's child attribution
// depends on (#5460). Two facts naming the SAME repository are one answer twice,
// not ambiguity.
func TestBuildProvenanceSurvivesDuplicateRepositoryFacts(t *testing.T) {
	t.Parallel()

	const (
		repoID    = "repository:r_86b8b612"
		remoteURL = "https://github.com/acme/container-base-lineage"
	)

	decisions := BuildContainerImageIdentityDecisions([]facts.Envelope{
		repositoryRemoteFact(repoID, remoteURL),
		repositoryRemoteFact(repoID, remoteURL),
		ociManifestWithConfigLabels("oci-manifest-dup", testContainerDigest, map[string]string{
			"org.opencontainers.image.source": remoteURL,
		}),
	})

	var provenance []string
	for _, decision := range decisions {
		if decision.Digest == testContainerDigest {
			provenance = append(provenance, decision.BuildProvenanceRepositoryIDs...)
		}
	}
	if !slices.Contains(provenance, repoID) {
		t.Fatalf("duplicate active repository facts for the same repo broke the source-label match: got %#v, want %q", provenance, repoID)
	}
}

// TestTwoDifferentRepositoriesClaimingOneRemoteStayAmbiguous keeps the dedupe
// from loosening real ambiguity: when two DISTINCT repository ids claim the same
// remote URL, the label resolves to neither.
func TestTwoDifferentRepositoriesClaimingOneRemoteStayAmbiguous(t *testing.T) {
	t.Parallel()

	const remoteURL = "https://github.com/acme/container-base-lineage"

	decisions := BuildContainerImageIdentityDecisions([]facts.Envelope{
		repositoryRemoteFact("repository:r_aaaaaaaa", remoteURL),
		repositoryRemoteFact("repository:r_bbbbbbbb", remoteURL),
		ociManifestWithConfigLabels("oci-manifest-ambig", testContainerDigest, map[string]string{
			"org.opencontainers.image.source": remoteURL,
		}),
	})

	for _, decision := range decisions {
		if len(decision.BuildProvenanceRepositoryIDs) > 0 {
			t.Fatalf("two distinct repositories claiming one remote must stay ambiguous, got provenance %#v", decision.BuildProvenanceRepositoryIDs)
		}
	}
}
