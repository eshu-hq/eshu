// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

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

func TestBuildProjectionQueuesContainerImageIdentityForGCPImageReference(t *testing.T) {
	t.Parallel()

	scopeValue := scope.IngestionScope{
		ScopeID:      "gcp:project:demo:run:resource:global",
		ScopeKind:    "gcp_cloud",
		SourceSystem: "gcp",
	}
	generation := scope.ScopeGeneration{
		ScopeID:      scopeValue.ScopeID,
		GenerationID: "gcp-generation-1",
		ObservedAt:   time.Date(2026, time.June, 13, 11, 0, 0, 0, time.UTC),
		IngestedAt:   time.Date(2026, time.June, 13, 11, 0, 1, 0, time.UTC),
		Status:       scope.GenerationStatusPending,
	}

	projection, err := buildProjection(scopeValue, generation, []facts.Envelope{
		gcpImageReferenceEnvelope("fact-gcp-image-1", scopeValue.ScopeID, generation.GenerationID),
	})
	if err != nil {
		t.Fatalf("buildProjection() error = %v, want nil", err)
	}

	intent := requireContainerImageIdentityIntent(t, projection.reducerIntents)
	if got, want := intent.Domain, reducer.DomainContainerImageIdentity; got != want {
		t.Fatalf("intent.Domain = %q, want %q", got, want)
	}
	if got, want := intent.EntityKey, "container_image_identity:gcp:project:demo:run:resource:global"; got != want {
		t.Fatalf("intent.EntityKey = %q, want %q", got, want)
	}
	if got, want := intent.FactID, "fact-gcp-image-1"; got != want {
		t.Fatalf("intent.FactID = %q, want GCP image reference fact", got)
	}
	if got, want := intent.SourceSystem, "gcp"; got != want {
		t.Fatalf("intent.SourceSystem = %q, want %q", got, want)
	}
}

func TestBuildProjectionQueuesContainerImageIdentityForAzureImageReference(t *testing.T) {
	t.Parallel()

	scopeValue := scope.IngestionScope{
		ScopeID:      "azure:tenant:subscription:demo:containerapps:global:resources",
		ScopeKind:    "azure_cloud",
		SourceSystem: "azure",
	}
	generation := scope.ScopeGeneration{
		ScopeID:      scopeValue.ScopeID,
		GenerationID: "azure-generation-1",
		ObservedAt:   time.Date(2026, time.June, 13, 12, 0, 0, 0, time.UTC),
		IngestedAt:   time.Date(2026, time.June, 13, 12, 0, 1, 0, time.UTC),
		Status:       scope.GenerationStatusPending,
	}

	projection, err := buildProjection(scopeValue, generation, []facts.Envelope{
		azureImageReferenceEnvelope("fact-azure-image-1", scopeValue.ScopeID, generation.GenerationID),
	})
	if err != nil {
		t.Fatalf("buildProjection() error = %v, want nil", err)
	}

	intent := requireContainerImageIdentityIntent(t, projection.reducerIntents)
	if got, want := intent.Domain, reducer.DomainContainerImageIdentity; got != want {
		t.Fatalf("intent.Domain = %q, want %q", got, want)
	}
	if got, want := intent.EntityKey, "container_image_identity:azure:tenant:subscription:demo:containerapps:global:resources"; got != want {
		t.Fatalf("intent.EntityKey = %q, want %q", got, want)
	}
	if got, want := intent.FactID, "fact-azure-image-1"; got != want {
		t.Fatalf("intent.FactID = %q, want Azure image reference fact", got)
	}
	if got, want := intent.SourceSystem, "azure"; got != want {
		t.Fatalf("intent.SourceSystem = %q, want %q", got, want)
	}
}

func TestBuildProjectionDoesNotQueueContainerImageIdentityFromInvalidAWSRelationship(t *testing.T) {
	t.Parallel()

	scopeValue := scope.IngestionScope{
		ScopeID:      "aws:123456789012:us-east-1:ecs",
		ScopeKind:    "aws_cloud",
		SourceSystem: "aws",
	}
	generation := scope.ScopeGeneration{
		ScopeID:      scopeValue.ScopeID,
		GenerationID: "aws-generation-1",
		ObservedAt:   time.Date(2026, time.June, 13, 13, 0, 0, 0, time.UTC),
		IngestedAt:   time.Date(2026, time.June, 13, 13, 0, 1, 0, time.UTC),
		Status:       scope.GenerationStatusPending,
	}
	envelopes := []facts.Envelope{{
		FactID:        "fact-invalid-aws-image-relationship",
		ScopeID:       scopeValue.ScopeID,
		GenerationID:  generation.GenerationID,
		FactKind:      facts.AWSRelationshipFactKind,
		SchemaVersion: facts.AWSRelationshipSchemaVersion,
		CollectorKind: "aws_cloud",
		ObservedAt:    generation.ObservedAt,
		SourceRef: facts.Ref{
			SourceSystem: "aws",
		},
		Payload: map[string]any{
			"account_id":            "123456789012",
			"region":                "us-east-1",
			"relationship_type":     "REFERENCES_IMAGE",
			"source_resource_id":    "arn:aws:ecs:us-east-1:123456789012:task-definition/api:1",
			"target_resource_id":    "",
			"target_type":           "container_image",
			"target_arn":            "",
			"source_arn":            "arn:aws:ecs:us-east-1:123456789012:task-definition/api:1",
			"collector_instance_id": "aws-collector-1",
		},
	}}
	delete(envelopes[0].Payload, "target_resource_id")

	projection, err := buildProjection(scopeValue, generation, envelopes)
	if err != nil {
		t.Fatalf("buildProjection() error = %v, want nil", err)
	}
	for _, intent := range projection.reducerIntents {
		if intent.Domain == reducer.DomainContainerImageIdentity {
			t.Fatalf("unexpected container_image_identity intent from input_invalid relationship")
		}
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

func azureImageReferenceEnvelope(factID, scopeID, generationID string) facts.Envelope {
	return facts.Envelope{
		FactID:           factID,
		ScopeID:          scopeID,
		GenerationID:     generationID,
		FactKind:         facts.AzureImageReferenceFactKind,
		SchemaVersion:    facts.AzureImageReferenceSchemaVersion,
		CollectorKind:    "azure",
		SourceConfidence: facts.SourceConfidenceReported,
		ObservedAt:       time.Date(2026, time.June, 13, 12, 0, 0, 0, time.UTC),
		SourceRef: facts.Ref{
			SourceSystem: "azure",
		},
		Payload: map[string]any{
			"owning_arm_resource_id": "/subscriptions/demo/resourceGroups/rg/providers/Microsoft.App/containerApps/api",
			"owning_normalized_id":   "/subscriptions/demo/resourcegroups/rg/providers/microsoft.app/containerapps/api",
			"owning_resource_type":   "microsoft.app/containerapps",
			"image_reference":        "contoso.azurecr.io/team/api:prod",
			"image_digest":           "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			"tag_digest_confidence":  "digest",
		},
	}
}

func gcpImageReferenceEnvelope(factID, scopeID, generationID string) facts.Envelope {
	return facts.Envelope{
		FactID:           factID,
		ScopeID:          scopeID,
		GenerationID:     generationID,
		FactKind:         facts.GCPImageReferenceFactKind,
		SchemaVersion:    facts.GCPImageReferenceSchemaVersion,
		CollectorKind:    "gcp",
		SourceConfidence: facts.SourceConfidenceReported,
		ObservedAt:       time.Date(2026, time.June, 13, 11, 0, 0, 0, time.UTC),
		SourceRef: facts.Ref{
			SourceSystem: "gcp",
		},
		Payload: map[string]any{
			"owning_full_resource_name": "//run.googleapis.com/projects/demo/locations/us-central1/services/api",
			"owning_asset_type":         "run.googleapis.com/Service",
			"image_reference":           "us-docker.pkg.dev/team-artifacts/apps/api:prod",
			"image_digest":              "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			"tag_digest_confidence":     "digest",
		},
	}
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
