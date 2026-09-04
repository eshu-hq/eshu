// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package containerimage

import (
	"slices"
	"testing"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/sbomattest"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/factload"
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
	assertContainerImageDecision(t, got, reducercontract.ContainerImageIdentityExactDigest, testContainerDigest, 1)
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

// TestBuildContainerImageIdentityDecisionsSourceLabelSurvivesDuplicateRepositoryFacts
// pins the #5801 fix: matchOCIConfigSourceRepository must dedupe by repository
// identity before applying its exactly-one rule, so a repository legitimately
// carrying two active `repository` facts (more than one scope or collector
// observing it) does not make an otherwise-unambiguous OCI config source label
// look ambiguous. Before the fix, the second fact alone made
// matchOCIConfigSourceRepository count two raw fact matches and reject the
// label, keeping the oci_config_source_label_with_digest identity tier
// unreachable in any corpus with duplicate repository facts.
func TestBuildContainerImageIdentityDecisionsSourceLabelSurvivesDuplicateRepositoryFacts(t *testing.T) {
	t.Parallel()

	decisions := BuildContainerImageIdentityDecisions([]facts.Envelope{
		repositoryRemoteFact("repo://acme/payments-api", "https://github.com/acme/payments-api.git"),
		repositoryRemoteFact("repo://acme/payments-api", "https://github.com/acme/payments-api.git"),
		ociManifestWithConfigLabels("oci-manifest-dup-label", testContainerDigest, map[string]string{
			"org.opencontainers.image.source": "https://github.com/acme/payments-api",
		}),
	})

	got := decisionsByRef(decisions)["registry.example.com/team/api@"+testContainerDigest]
	if !slices.Contains(got.SourceRepositoryIDs, "repo://acme/payments-api") {
		t.Fatalf("SourceRepositoryIDs = %#v, want repo://acme/payments-api: duplicate active repository facts must not break the source-label match", got.SourceRepositoryIDs)
	}
	if got.IdentityStrength != "oci_config_source_label_with_digest" {
		t.Fatalf("IdentityStrength = %q, want oci_config_source_label_with_digest", got.IdentityStrength)
	}
}

// TestBuildContainerImageIdentityDecisionsSourceLabelStaysAmbiguousForTwoDistinctRepositories
// is the negative half of the #5801 fix: when two DISTINCT repositories
// genuinely claim the same remote URL, the label must still resolve to
// neither — deduping by repository identity must never loosen real ambiguity
// into a guess.
func TestBuildContainerImageIdentityDecisionsSourceLabelStaysAmbiguousForTwoDistinctRepositories(t *testing.T) {
	t.Parallel()

	const remoteURL = "https://github.com/acme/payments-api"

	decisions := BuildContainerImageIdentityDecisions([]facts.Envelope{
		repositoryRemoteFact("repo://acme/payments-api-fork-one", remoteURL+".git"),
		repositoryRemoteFact("repo://acme/payments-api-fork-two", remoteURL+".git"),
		ociManifestWithConfigLabels("oci-manifest-ambiguous-label", testContainerDigest, map[string]string{
			"org.opencontainers.image.source": remoteURL,
		}),
	})

	got := decisionsByRef(decisions)["registry.example.com/team/api@"+testContainerDigest]
	if slices.Contains(got.SourceRepositoryIDs, "repo://acme/payments-api-fork-one") ||
		slices.Contains(got.SourceRepositoryIDs, "repo://acme/payments-api-fork-two") {
		t.Fatalf("SourceRepositoryIDs = %#v, want neither distinct repository from an ambiguous label match", got.SourceRepositoryIDs)
	}
	if got.IdentityStrength == "oci_config_source_label_with_digest" {
		t.Fatalf("IdentityStrength = %q, want anything but the label tier: two distinct repositories claiming one remote must stay ambiguous", got.IdentityStrength)
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

	attachmentDecisions := sbomattest.BuildSBOMAttestationAttachmentDecisions([]facts.Envelope{
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
		FactKind:         factload.FactKindRepository,
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
		FactKind: reducercontract.ContainerImageIdentityFactKind,
		Payload: map[string]any{
			"digest":                digest,
			"outcome":               string(reducercontract.ContainerImageIdentityExactDigest),
			"canonical_writes":      1,
			"source_repository_ids": stringsToAny(repositoryIDs),
		},
	}
}

// sbomDocumentFact, sbomAttachmentDecisionsByDocument, and stringsToAny are
// local copies of the sbom_attestation family's test fixture helpers (see
// sbom_attestation_attachment_test.go and sbom_attestation_attachment_anchors_test.go
// in internal/reducer/sbomattest). They carry no sbom_attestation-specific
// logic and Go test files cannot share unexported symbols across package
// boundaries, so they are duplicated here rather than exported cross-package
// for test-only use.

func sbomDocumentFact(
	factID string,
	documentID string,
	subjectDigest string,
	documentDigest string,
	parseStatus string,
	verificationStatus string,
) facts.Envelope {
	return facts.Envelope{
		FactID:   factID,
		FactKind: facts.SBOMDocumentFactKind,
		Payload: map[string]any{
			"document_id":         documentID,
			"document_digest":     documentDigest,
			"subject_digest":      subjectDigest,
			"parse_status":        parseStatus,
			"verification_status": verificationStatus,
			"format":              "cyclonedx",
			"spec_version":        "1.6",
		},
	}
}

func sbomAttachmentDecisionsByDocument(
	decisions []sbomattest.SBOMAttestationAttachmentDecision,
) map[string]sbomattest.SBOMAttestationAttachmentDecision {
	out := make(map[string]sbomattest.SBOMAttestationAttachmentDecision, len(decisions))
	for _, decision := range decisions {
		out[decision.DocumentID] = decision
	}
	return out
}

func stringsToAny(values []string) []any {
	out := make([]any, 0, len(values))
	for _, value := range values {
		out = append(out, value)
	}
	return out
}
