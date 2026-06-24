// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"slices"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestBuildContainerImageIdentityDecisionsUsesOCIConfigSourceLabel(t *testing.T) {
	t.Parallel()

	decisions := BuildContainerImageIdentityDecisions([]facts.Envelope{
		repositoryRemoteFact("repo://acme/payments-api", "https://github.com/acme/payments-api.git"),
		ociManifestWithConfigLabels("oci-manifest", testContainerDigest, map[string]string{
			"org.opencontainers.image.source":   "https://github.com/acme/payments-api",
			"org.opencontainers.image.revision": "0123456789abcdef0123456789abcdef01234567",
		}),
	})

	got := decisionsByRef(decisions)["registry.example.com/team/api@"+testContainerDigest]
	assertContainerImageDecision(t, got, ContainerImageIdentityExactDigest, testContainerDigest, 1)
	if !slices.Contains(got.SourceRepositoryIDs, "repo://acme/payments-api") {
		t.Fatalf("SourceRepositoryIDs = %#v, want repo://acme/payments-api", got.SourceRepositoryIDs)
	}
	if got.SourceRevision != "0123456789abcdef0123456789abcdef01234567" {
		t.Fatalf("SourceRevision = %q", got.SourceRevision)
	}
	if got.IdentityStrength != "oci_config_source_label_with_digest" {
		t.Fatalf("IdentityStrength = %q, want oci_config_source_label_with_digest", got.IdentityStrength)
	}
}

func TestBuildContainerImageIdentityDecisionsRejectsMissingConflictingAndMalformedOCIConfigSourceLabels(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		labels map[string]string
	}{
		{
			name:   "missing labels",
			labels: nil,
		},
		{
			name: "conflicting source labels",
			labels: map[string]string{
				"org.opencontainers.image.source": "https://github.com/acme/payments-api",
				"org.label-schema.vcs-url":        "https://github.com/acme/other-api",
			},
		},
		{
			name: "malformed source label",
			labels: map[string]string{
				"org.opencontainers.image.source": "not a url",
			},
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			decisions := BuildContainerImageIdentityDecisions([]facts.Envelope{
				repositoryRemoteFact("repo://acme/payments-api", "https://github.com/acme/payments-api.git"),
				ociManifestWithConfigLabels("oci-manifest", testContainerDigest, tc.labels),
			})
			if got := len(decisions); got != 0 {
				t.Fatalf("decisions = %#v, want no label-proven image identity", decisions)
			}
		})
	}
}

func TestSBOMAttachmentInheritsRepositoryAnchorFromLabelProvenImageIdentity(t *testing.T) {
	t.Parallel()

	imageDecision := BuildContainerImageIdentityDecisions([]facts.Envelope{
		repositoryRemoteFact("repo://acme/payments-api", "https://github.com/acme/payments-api.git"),
		ociManifestWithConfigLabels("oci-manifest", testContainerDigest, map[string]string{
			"org.opencontainers.image.source": "https://github.com/acme/payments-api",
		}),
	})[0]

	attachmentDecisions := BuildSBOMAttestationAttachmentDecisions([]facts.Envelope{
		sbomDocumentFact("doc", "doc", testContainerDigest, "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc", "parsed", "verified"),
		containerImageIdentityReducerFact("identity", testContainerDigest, imageDecision.SourceRepositoryIDs),
	})

	got := sbomAttachmentDecisionsByDocument(attachmentDecisions)["doc"]
	if !slices.Contains(got.RepositoryIDs, "repo://acme/payments-api") {
		t.Fatalf("RepositoryIDs = %#v, want repo://acme/payments-api", got.RepositoryIDs)
	}
	if got.AttachmentScope != "subject_only_unanchored" {
		t.Fatalf("AttachmentScope = %q, want subject_only_unanchored without OCI referrer", got.AttachmentScope)
	}
}

func repositoryRemoteFact(repositoryID string, remoteURL string) facts.Envelope {
	return facts.Envelope{
		FactID:           "repository:" + repositoryID,
		ScopeID:          "git-repository-scope:" + repositoryID,
		GenerationID:     "generation-git",
		FactKind:         factKindRepository,
		CollectorKind:    "git",
		SourceConfidence: facts.SourceConfidenceObserved,
		Payload: map[string]any{
			"repo_id":    repositoryID,
			"graph_id":   repositoryID,
			"remote_url": remoteURL,
		},
	}
}

func ociManifestWithConfigLabels(factID string, digest string, labels map[string]string) facts.Envelope {
	envelope := ociManifestFact(factID, digest)
	if labels != nil {
		envelope.Payload["config_labels"] = labels
	}
	return envelope
}

func containerImageIdentityReducerFact(factID string, digest string, repositoryIDs []string) facts.Envelope {
	return facts.Envelope{
		FactID:   factID,
		FactKind: containerImageIdentityFactKind,
		Payload: map[string]any{
			"digest":                digest,
			"outcome":               string(ContainerImageIdentityExactDigest),
			"canonical_writes":      1,
			"source_repository_ids": stringsToAny(repositoryIDs),
		},
	}
}
