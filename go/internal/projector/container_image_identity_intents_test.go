package projector

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

func TestBuildProjectionQueuesSingleContainerImageIdentityIntentForOCIRegistryFacts(t *testing.T) {
	t.Parallel()

	scopeValue := scope.IngestionScope{
		ScopeID:      "oci-registry://registry.example.com/team/api",
		ScopeKind:    "container_registry_repository",
		SourceSystem: "oci_registry",
	}
	generation := scope.ScopeGeneration{
		ScopeID:      scopeValue.ScopeID,
		GenerationID: "oci-generation-1",
		ObservedAt:   time.Date(2026, time.May, 15, 10, 0, 0, 0, time.UTC),
		IngestedAt:   time.Date(2026, time.May, 15, 10, 0, 1, 0, time.UTC),
		Status:       scope.GenerationStatusPending,
	}
	envelopes := []facts.Envelope{
		ociRegistryManifestEnvelope("fact-oci-manifest-1", scopeValue.ScopeID, generation.GenerationID),
		ociRegistryTagEnvelope("fact-oci-tag-1", scopeValue.ScopeID, generation.GenerationID),
	}

	projection, err := buildProjection(scopeValue, generation, envelopes)
	if err != nil {
		t.Fatalf("buildProjection() error = %v, want nil", err)
	}
	var identityIntentCount int
	for _, intent := range projection.reducerIntents {
		if intent.Domain == reducer.DomainContainerImageIdentity {
			identityIntentCount++
		}
	}
	if got, want := identityIntentCount, 1; got != want {
		t.Fatalf("container image identity intents = %d, want %d", got, want)
	}
	intent := requireContainerImageIdentityIntent(t, projection.reducerIntents)
	if got, want := intent.Domain, reducer.DomainContainerImageIdentity; got != want {
		t.Fatalf("intent.Domain = %q, want %q", got, want)
	}
	if got, want := intent.EntityKey, "container_image_identity:oci-registry://registry.example.com/team/api"; got != want {
		t.Fatalf("intent.EntityKey = %q, want %q", got, want)
	}
	if got, want := intent.FactID, "fact-oci-manifest-1"; got != want {
		t.Fatalf("intent.FactID = %q, want first OCI identity fact", got)
	}
	if got, want := intent.SourceSystem, "oci_registry"; got != want {
		t.Fatalf("intent.SourceSystem = %q, want %q", got, want)
	}
}

func TestBuildProjectionQueuesContainerImageIdentityForOCIReferrer(t *testing.T) {
	t.Parallel()

	scopeValue := scope.IngestionScope{
		ScopeID:      "oci-registry://registry.example.com/team/api",
		ScopeKind:    "container_registry_repository",
		SourceSystem: "oci_registry",
	}
	generation := scope.ScopeGeneration{
		ScopeID:      scopeValue.ScopeID,
		GenerationID: "oci-generation-2",
		ObservedAt:   time.Date(2026, time.June, 6, 10, 0, 0, 0, time.UTC),
		IngestedAt:   time.Date(2026, time.June, 6, 10, 0, 1, 0, time.UTC),
		Status:       scope.GenerationStatusPending,
	}

	projection, err := buildProjection(scopeValue, generation, []facts.Envelope{
		ociRegistryReferrerEnvelope("fact-oci-referrer-1", scopeValue.ScopeID, generation.GenerationID),
	})
	if err != nil {
		t.Fatalf("buildProjection() error = %v, want nil", err)
	}

	intent := requireContainerImageIdentityIntent(t, projection.reducerIntents)
	if got, want := intent.Domain, reducer.DomainContainerImageIdentity; got != want {
		t.Fatalf("intent.Domain = %q, want %q", got, want)
	}
	if got, want := intent.FactID, "fact-oci-referrer-1"; got != want {
		t.Fatalf("intent.FactID = %q, want OCI referrer fact", got)
	}
	if got, want := intent.SourceSystem, "oci_registry"; got != want {
		t.Fatalf("intent.SourceSystem = %q, want %q", got, want)
	}
}

func requireContainerImageIdentityIntent(t *testing.T, intents []ReducerIntent) ReducerIntent {
	t.Helper()
	for _, intent := range intents {
		if intent.Domain == reducer.DomainContainerImageIdentity {
			return intent
		}
	}
	t.Fatalf("container_image_identity intent missing from %#v", intents)
	return ReducerIntent{}
}

func ociRegistryManifestEnvelope(factID, scopeID, generationID string) facts.Envelope {
	return facts.Envelope{
		FactID:           factID,
		ScopeID:          scopeID,
		GenerationID:     generationID,
		FactKind:         facts.OCIImageManifestFactKind,
		SchemaVersion:    facts.OCIImageManifestSchemaVersion,
		CollectorKind:    "oci_registry",
		SourceConfidence: facts.SourceConfidenceReported,
		ObservedAt:       time.Date(2026, time.May, 15, 10, 0, 0, 0, time.UTC),
		SourceRef: facts.Ref{
			SourceSystem: "oci_registry",
		},
		Payload: map[string]any{
			"repository_id": "oci-registry://registry.example.com/team/api",
			"registry":      "registry.example.com",
			"repository":    "team/api",
			"digest":        "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			"media_type":    "application/vnd.oci.image.manifest.v1+json",
		},
	}
}

func ociRegistryReferrerEnvelope(factID, scopeID, generationID string) facts.Envelope {
	return facts.Envelope{
		FactID:           factID,
		ScopeID:          scopeID,
		GenerationID:     generationID,
		FactKind:         facts.OCIImageReferrerFactKind,
		SchemaVersion:    facts.OCIImageReferrerSchemaVersion,
		CollectorKind:    "oci_registry",
		SourceConfidence: facts.SourceConfidenceReported,
		ObservedAt:       time.Date(2026, time.June, 6, 10, 0, 0, 0, time.UTC),
		SourceRef: facts.Ref{
			SourceSystem: "oci_registry",
		},
		Payload: map[string]any{
			"repository_id":       "oci-registry://registry.example.com/team/api",
			"registry":            "registry.example.com",
			"repository":          "team/api",
			"subject_digest":      "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			"subject_media_type":  "application/vnd.oci.image.manifest.v1+json",
			"referrer_digest":     "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
			"referrer_media_type": "application/vnd.cyclonedx+json",
			"artifact_type":       "application/vnd.cyclonedx+json",
		},
	}
}

func ociRegistryTagEnvelope(factID, scopeID, generationID string) facts.Envelope {
	return facts.Envelope{
		FactID:           factID,
		ScopeID:          scopeID,
		GenerationID:     generationID,
		FactKind:         facts.OCIImageTagObservationFactKind,
		SchemaVersion:    facts.OCIImageTagObservationSchemaVersion,
		CollectorKind:    "oci_registry",
		SourceConfidence: facts.SourceConfidenceReported,
		ObservedAt:       time.Date(2026, time.May, 15, 10, 0, 0, 0, time.UTC),
		SourceRef: facts.Ref{
			SourceSystem: "oci_registry",
		},
		Payload: map[string]any{
			"repository_id":   "oci-registry://registry.example.com/team/api",
			"registry":        "registry.example.com",
			"repository":      "team/api",
			"tag":             "prod",
			"resolved_digest": "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			"media_type":      "application/vnd.oci.image.manifest.v1+json",
		},
	}
}
