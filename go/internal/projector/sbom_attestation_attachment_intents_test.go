package projector

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

func TestBuildProjectionQueuesSBOMAttestationAttachmentForSBOMDocument(t *testing.T) {
	t.Parallel()

	scopeValue := scope.IngestionScope{
		ScopeID:      "sbom://remote-e2e/team-api",
		SourceSystem: "sbom_attestation",
	}
	generation := scope.ScopeGeneration{
		ScopeID:      scopeValue.ScopeID,
		GenerationID: "generation-sbom",
	}
	projection, err := buildProjection(scopeValue, generation, []facts.Envelope{{
		FactID:        "fact-sbom-doc",
		ScopeID:       scopeValue.ScopeID,
		GenerationID:  generation.GenerationID,
		FactKind:      facts.SBOMDocumentFactKind,
		SchemaVersion: facts.SBOMAttestationSchemaVersionV1,
		SourceRef: facts.Ref{
			SourceSystem: "sbom_attestation",
		},
		Payload: map[string]any{
			"document_id":     "doc-team-api",
			"document_digest": "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
			"subject_digest":  "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		},
	}})
	if err != nil {
		t.Fatalf("buildProjection() error = %v, want nil", err)
	}

	intent := requireSBOMAttestationAttachmentIntent(t, projection.reducerIntents)
	if got, want := intent.ScopeID, scopeValue.ScopeID; got != want {
		t.Fatalf("ScopeID = %q, want %q", got, want)
	}
	if got, want := intent.GenerationID, generation.GenerationID; got != want {
		t.Fatalf("GenerationID = %q, want %q", got, want)
	}
	if got, want := intent.FactID, "fact-sbom-doc"; got != want {
		t.Fatalf("FactID = %q, want %q", got, want)
	}
	if got, want := intent.SourceSystem, "sbom_attestation"; got != want {
		t.Fatalf("SourceSystem = %q, want %q", got, want)
	}
}

func TestBuildProjectionQueuesSBOMAttestationAttachmentForAttestationStatement(t *testing.T) {
	t.Parallel()

	scopeValue := scope.IngestionScope{
		ScopeID:      "attestation://remote-e2e/team-api",
		SourceSystem: "sbom_attestation",
	}
	generation := scope.ScopeGeneration{
		ScopeID:      scopeValue.ScopeID,
		GenerationID: "generation-attestation",
	}
	projection, err := buildProjection(scopeValue, generation, []facts.Envelope{{
		FactID:        "fact-attestation-statement",
		ScopeID:       scopeValue.ScopeID,
		GenerationID:  generation.GenerationID,
		FactKind:      facts.AttestationStatementFactKind,
		SchemaVersion: facts.SBOMAttestationSchemaVersionV1,
		CollectorKind: "sbom_attestation",
		Payload: map[string]any{
			"statement_id":     "statement-team-api",
			"statement_digest": "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
			"subject_digests": []any{
				"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			},
		},
	}})
	if err != nil {
		t.Fatalf("buildProjection() error = %v, want nil", err)
	}

	intent := requireSBOMAttestationAttachmentIntent(t, projection.reducerIntents)
	if got, want := intent.Domain, reducer.DomainSBOMAttestationAttachment; got != want {
		t.Fatalf("Domain = %q, want %q", got, want)
	}
	if got, want := intent.EntityKey, "sbom_attestation_attachment:attestation://remote-e2e/team-api"; got != want {
		t.Fatalf("EntityKey = %q, want %q", got, want)
	}
	if got, want := intent.SourceSystem, "sbom_attestation"; got != want {
		t.Fatalf("SourceSystem = %q, want %q", got, want)
	}
}

func TestBuildProjectionQueuesSBOMAttestationAttachmentForOCIReferrer(t *testing.T) {
	t.Parallel()

	scopeValue := scope.IngestionScope{
		ScopeID:      "oci-registry://registry.example.com/team/api",
		SourceSystem: "oci_registry",
	}
	generation := scope.ScopeGeneration{
		ScopeID:      scopeValue.ScopeID,
		GenerationID: "generation-oci",
	}

	projection, err := buildProjection(scopeValue, generation, []facts.Envelope{
		ociRegistryReferrerEnvelope("fact-oci-referrer-1", scopeValue.ScopeID, generation.GenerationID),
	})
	if err != nil {
		t.Fatalf("buildProjection() error = %v, want nil", err)
	}
	for _, intent := range projection.reducerIntents {
		if intent.Domain == reducer.DomainSBOMAttestationAttachment {
			if got, want := intent.FactID, "fact-oci-referrer-1"; got != want {
				t.Fatalf("FactID = %q, want %q", got, want)
			}
			if got, want := intent.SourceSystem, "oci_registry"; got != want {
				t.Fatalf("SourceSystem = %q, want %q", got, want)
			}
			return
		}
	}
	t.Fatalf("sbom_attestation_attachment intent missing from %#v", projection.reducerIntents)
}

func TestBuildSBOMAttestationAttachmentReducerIntentSkipsComponentOnlyEvidence(t *testing.T) {
	t.Parallel()

	scopeValue := scope.IngestionScope{ScopeID: "sbom://remote-e2e/team-api"}
	generation := scope.ScopeGeneration{ScopeID: scopeValue.ScopeID, GenerationID: "generation-sbom"}
	_, ok := buildSBOMAttestationAttachmentReducerIntent(scopeValue, generation, []facts.Envelope{{
		FactID:       "component-only",
		ScopeID:      scopeValue.ScopeID,
		GenerationID: generation.GenerationID,
		FactKind:     facts.SBOMComponentFactKind,
	}})
	if ok {
		t.Fatal("buildSBOMAttestationAttachmentReducerIntent() ok = true, want false for component-only evidence")
	}
}

func requireSBOMAttestationAttachmentIntent(t *testing.T, intents []ReducerIntent) ReducerIntent {
	t.Helper()
	for _, intent := range intents {
		if intent.Domain == reducer.DomainSBOMAttestationAttachment {
			return intent
		}
	}
	t.Fatalf("sbom_attestation_attachment intent missing from %#v", intents)
	return ReducerIntent{}
}
