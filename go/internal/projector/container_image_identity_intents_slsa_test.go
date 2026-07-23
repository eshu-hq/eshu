// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// TestBuildProjectionQueuesContainerImageIdentityForSLSAProvenance is the
// #5456 PR #5707 P1-b reachability guard, mirroring
// TestBuildProjectionQueuesContainerImageIdentityForCICDContainerArtifact: an
// attestation.slsa_provenance fact must trigger a container_image_identity
// intent for the SBOM-attestation scope it landed in, so a refresh actually
// runs and (via the reducer's cross-scope active loaders) can join it to an
// OCI-scope image identity decision. Without this trigger, SLSA provenance
// landing with no OTHER new identity evidence in the same generation would
// never cause the reducer to re-derive the affected image's decision.
func TestBuildProjectionQueuesContainerImageIdentityForSLSAProvenance(t *testing.T) {
	t.Parallel()

	scopeValue := scope.IngestionScope{
		ScopeID:      "sbom_attestation:supply-chain-demo:supply-chain-demo",
		ScopeKind:    "sbom_attestation",
		SourceSystem: "sbom_attestation",
	}
	generation := scope.ScopeGeneration{
		ScopeID:      scopeValue.ScopeID,
		GenerationID: "sbom-attestation-generation-1",
		ObservedAt:   time.Date(2026, time.July, 23, 10, 0, 0, 0, time.UTC),
		IngestedAt:   time.Date(2026, time.July, 23, 10, 0, 1, 0, time.UTC),
		Status:       scope.GenerationStatusPending,
	}

	projection, err := buildProjection(scopeValue, generation, []facts.Envelope{
		attestationSLSAProvenanceEnvelope("fact-slsa-provenance-1", scopeValue.ScopeID, generation.GenerationID),
	})
	if err != nil {
		t.Fatalf("buildProjection() error = %v, want nil", err)
	}

	intent := requireContainerImageIdentityIntent(t, projection.reducerIntents)
	if got, want := intent.FactID, "fact-slsa-provenance-1"; got != want {
		t.Fatalf("intent.FactID = %q, want the attestation.slsa_provenance fact", got)
	}
	if got, want := intent.SourceSystem, "sbom_attestation"; got != want {
		t.Fatalf("intent.SourceSystem = %q, want %q", got, want)
	}
}

// TestBuildProjectionQueuesContainerImageIdentityForSignatureVerification
// mirrors the SLSA provenance guard above for
// attestation.signature_verification: a verification result can land in a
// LATER generation than its statement/provenance (an async re-verification
// pass), so it must independently trigger a refresh too — otherwise the
// #5456 PR #5707 P1-a verification gate could never flip a decision from
// unverified to verified after the fact.
func TestBuildProjectionQueuesContainerImageIdentityForSignatureVerification(t *testing.T) {
	t.Parallel()

	scopeValue := scope.IngestionScope{
		ScopeID:      "sbom_attestation:supply-chain-demo:supply-chain-demo",
		ScopeKind:    "sbom_attestation",
		SourceSystem: "sbom_attestation",
	}
	generation := scope.ScopeGeneration{
		ScopeID:      scopeValue.ScopeID,
		GenerationID: "sbom-attestation-generation-2",
		ObservedAt:   time.Date(2026, time.July, 23, 11, 0, 0, 0, time.UTC),
		IngestedAt:   time.Date(2026, time.July, 23, 11, 0, 1, 0, time.UTC),
		Status:       scope.GenerationStatusPending,
	}

	projection, err := buildProjection(scopeValue, generation, []facts.Envelope{
		attestationSignatureVerificationEnvelope("fact-slsa-verification-1", scopeValue.ScopeID, generation.GenerationID),
	})
	if err != nil {
		t.Fatalf("buildProjection() error = %v, want nil", err)
	}

	intent := requireContainerImageIdentityIntent(t, projection.reducerIntents)
	if got, want := intent.FactID, "fact-slsa-verification-1"; got != want {
		t.Fatalf("intent.FactID = %q, want the attestation.signature_verification fact", got)
	}
	if got, want := intent.SourceSystem, "sbom_attestation"; got != want {
		t.Fatalf("intent.SourceSystem = %q, want %q", got, want)
	}
}

func attestationSLSAProvenanceEnvelope(factID, scopeID, generationID string) facts.Envelope {
	return facts.Envelope{
		FactID:           factID,
		ScopeID:          scopeID,
		GenerationID:     generationID,
		FactKind:         facts.AttestationSLSAProvenanceFactKind,
		SchemaVersion:    facts.SBOMAttestationSchemaVersionV1,
		CollectorKind:    "sbom_attestation",
		SourceConfidence: facts.SourceConfidenceReported,
		ObservedAt:       time.Date(2026, time.July, 23, 10, 0, 0, 0, time.UTC),
		SourceRef: facts.Ref{
			SourceSystem: "sbom_attestation",
		},
		Payload: map[string]any{
			"statement_id":   "stmt-slsa-trigger",
			"predicate_type": "https://slsa.dev/provenance/v1",
			"builder_id":     "https://github.com/eshu-hq/supply-chain-demo/actions/runner@v1",
		},
	}
}

func attestationSignatureVerificationEnvelope(factID, scopeID, generationID string) facts.Envelope {
	return facts.Envelope{
		FactID:           factID,
		ScopeID:          scopeID,
		GenerationID:     generationID,
		FactKind:         facts.AttestationSignatureVerificationFactKind,
		SchemaVersion:    facts.SBOMAttestationSchemaVersionV1,
		CollectorKind:    "sbom_attestation",
		SourceConfidence: facts.SourceConfidenceReported,
		ObservedAt:       time.Date(2026, time.July, 23, 11, 0, 0, 0, time.UTC),
		SourceRef: facts.Ref{
			SourceSystem: "sbom_attestation",
		},
		Payload: map[string]any{
			"statement_id":         "stmt-slsa-trigger",
			"verification_result":  "passed",
			"verification_status":  "passed",
			"verification_policy":  "cosign-keyless",
			"verification_subject": reducerTestSubjectDigest,
		},
	}
}

const reducerTestSubjectDigest = "sha256:2b3c4d5e6f708192a3b4c5d6e7f8091a2b3c4d5e6f708192a3b4c5d6e7f80901a"
