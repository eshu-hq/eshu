package ociregistry

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestRepositoryObservationBuildsReportedRepositoryEnvelope(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 5, 12, 12, 0, 0, 0, time.UTC)
	observation := RepositoryObservation{
		Identity: RepositoryIdentity{
			Provider:   ProviderECR,
			Registry:   "https://123456789012.dkr.ecr.us-east-1.amazonaws.com/",
			Repository: "Team/API-Service",
		},
		GenerationID:        "generation-1",
		CollectorInstanceID: "ecr-prod",
		FencingToken:        17,
		ObservedAt:          observedAt,
		Visibility:          VisibilityPrivate,
		AuthMode:            AuthModeCredentialed,
		SourceURI:           "https://user:secret@123456789012.dkr.ecr.us-east-1.amazonaws.com/v2/team/api-service?token=secret",
	}

	envelope, err := NewRepositoryEnvelope(observation)
	if err != nil {
		t.Fatalf("NewRepositoryEnvelope() error = %v", err)
	}

	assertOCIEnvelope(t, envelope, facts.OCIRegistryRepositoryFactKind, facts.OCIRegistryRepositorySchemaVersion)
	if envelope.ScopeID != "oci-registry://123456789012.dkr.ecr.us-east-1.amazonaws.com/team/api-service" {
		t.Fatalf("ScopeID = %q", envelope.ScopeID)
	}
	if envelope.FencingToken != 17 {
		t.Fatalf("FencingToken = %d, want 17", envelope.FencingToken)
	}
	if !envelope.ObservedAt.Equal(observedAt) {
		t.Fatalf("ObservedAt = %s, want %s", envelope.ObservedAt, observedAt)
	}
	if envelope.SourceRef.SourceURI != "https://123456789012.dkr.ecr.us-east-1.amazonaws.com/v2/team/api-service" {
		t.Fatalf("SourceRef.SourceURI = %q", envelope.SourceRef.SourceURI)
	}

	wantPayload := map[string]any{
		"collector_instance_id": "ecr-prod",
		"provider":              string(ProviderECR),
		"registry":              "123456789012.dkr.ecr.us-east-1.amazonaws.com",
		"repository":            "team/api-service",
		"repository_id":         "oci-registry://123456789012.dkr.ecr.us-east-1.amazonaws.com/team/api-service",
		"visibility":            string(VisibilityPrivate),
		"auth_mode":             string(AuthModeCredentialed),
	}
	for key, want := range wantPayload {
		if got := envelope.Payload[key]; got != want {
			t.Fatalf("Payload[%q] = %#v, want %#v; payload=%#v", key, got, want, envelope.Payload)
		}
	}
	assertStringSlice(t, envelope.Payload["correlation_anchors"], []string{
		"oci-registry://123456789012.dkr.ecr.us-east-1.amazonaws.com/team/api-service",
	})
}

func TestTagObservationBuildsWeakTagEnvelopeWithDigestIdentity(t *testing.T) {
	t.Parallel()

	observation := TagObservation{
		Repository: RepositoryIdentity{
			Provider:   ProviderJFrog,
			Registry:   "https://jfrog.example/artifactory/api/docker/prod",
			Repository: "team/api",
		},
		Tag:                 "Latest",
		Digest:              sha256Digest,
		MediaType:           MediaTypeOCIImageManifest,
		PreviousDigest:      "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		Mutated:             true,
		GenerationID:        "generation-tags",
		CollectorInstanceID: "jfrog-docker",
		ObservedAt:          time.Date(2026, 5, 12, 12, 5, 0, 0, time.UTC),
		SourceURI:           "https://jfrog.example/artifactory/api/docker/prod/v2/team/api/tags/list?access_token=secret",
	}

	envelope, err := NewTagObservationEnvelope(observation)
	if err != nil {
		t.Fatalf("NewTagObservationEnvelope() error = %v", err)
	}

	assertOCIEnvelope(t, envelope, facts.OCIImageTagObservationFactKind, facts.OCIImageTagObservationSchemaVersion)
	if got := envelope.Payload["tag"]; got != "Latest" {
		t.Fatalf("tag = %#v", got)
	}
	if got := envelope.Payload["resolved_digest"]; got != sha256Digest {
		t.Fatalf("resolved_digest = %#v", got)
	}
	if got := envelope.Payload["identity_strength"]; got != IdentityStrengthWeakTag {
		t.Fatalf("identity_strength = %#v", got)
	}
	if got := envelope.Payload["mutated"]; got != true {
		t.Fatalf("mutated = %#v", got)
	}
	if envelope.SourceRef.SourceURI != "https://jfrog.example/artifactory/api/docker/prod/v2/team/api/tags/list" {
		t.Fatalf("SourceRef.SourceURI = %q", envelope.SourceRef.SourceURI)
	}
	assertStringSlice(t, envelope.Payload["correlation_anchors"], []string{
		"oci-registry://jfrog.example/artifactory/api/docker/prod/team/api",
		sha256Digest,
	})

	sameObservation := observation
	sameObservation.Repository.Registry = "jfrog.example/artifactory/api/docker/prod/"
	second, err := NewTagObservationEnvelope(sameObservation)
	if err != nil {
		t.Fatalf("NewTagObservationEnvelope(sameObservation) error = %v", err)
	}
	if envelope.StableFactKey != second.StableFactKey {
		t.Fatalf("StableFactKey differs for normalized repository identity: %q != %q", envelope.StableFactKey, second.StableFactKey)
	}

	nextGeneration := observation
	nextGeneration.GenerationID = "generation-tags-next"
	third, err := NewTagObservationEnvelope(nextGeneration)
	if err != nil {
		t.Fatalf("NewTagObservationEnvelope(nextGeneration) error = %v", err)
	}
	if envelope.StableFactKey != third.StableFactKey {
		t.Fatalf("StableFactKey changed across generations: %q != %q", envelope.StableFactKey, third.StableFactKey)
	}
	if envelope.FactID == third.FactID {
		t.Fatalf("FactID did not include generation boundary: %q", envelope.FactID)
	}
}

func TestDescriptorEnvelopesPreserveDigestIdentityAndRedactUnknownAnnotations(t *testing.T) {
	t.Parallel()

	observation := ManifestObservation{
		Repository: RepositoryIdentity{
			Provider:   ProviderECR,
			Registry:   "123456789012.dkr.ecr.us-east-1.amazonaws.com",
			Repository: "team/api",
		},
		Descriptor: Descriptor{
			Digest:       sha256Digest,
			MediaType:    MediaTypeOCIImageManifest,
			SizeBytes:    2048,
			ArtifactType: "application/vnd.example.scan",
			Annotations: map[string]string{
				"org.opencontainers.image.source":  "https://github.com/example/api",
				"com.example.private.build.secret": "do-not-leak",
			},
		},
		Config: Descriptor{
			Digest:    "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
			MediaType: "application/vnd.oci.image.config.v1+json",
			SizeBytes: 512,
		},
		Layers: []Descriptor{{
			Digest:    "sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd",
			MediaType: "application/vnd.oci.image.layer.v1.tar+gzip",
			SizeBytes: 4096,
		}},
		SourceTag:           "latest",
		GenerationID:        "generation-manifest",
		CollectorInstanceID: "ecr-prod",
		ObservedAt:          time.Date(2026, 5, 12, 12, 10, 0, 0, time.UTC),
		SourceURI:           "https://123456789012.dkr.ecr.us-east-1.amazonaws.com/v2/team/api/manifests/latest",
	}

	envelope, err := NewManifestEnvelope(observation)
	if err != nil {
		t.Fatalf("NewManifestEnvelope() error = %v", err)
	}

	assertOCIEnvelope(t, envelope, facts.OCIImageManifestFactKind, facts.OCIImageManifestSchemaVersion)
	if got := envelope.Payload["digest"]; got != sha256Digest {
		t.Fatalf("digest = %#v", got)
	}
	if got := envelope.Payload["source_tag"]; got != "latest" {
		t.Fatalf("source_tag = %#v", got)
	}
	annotations, ok := envelope.Payload["annotations"].(map[string]string)
	if !ok {
		t.Fatalf("annotations = %#v", envelope.Payload["annotations"])
	}
	if got := annotations["org.opencontainers.image.source"]; got != "https://github.com/example/api" {
		t.Fatalf("known annotation = %q", got)
	}
	if got := annotations["com.example.private.build.secret"]; got != RedactedValue {
		t.Fatalf("unknown annotation = %q, want redacted", got)
	}

	sameDigestDifferentTag := observation
	sameDigestDifferentTag.SourceTag = "release-2026-05-12"
	second, err := NewManifestEnvelope(sameDigestDifferentTag)
	if err != nil {
		t.Fatalf("NewManifestEnvelope(sameDigestDifferentTag) error = %v", err)
	}
	if envelope.StableFactKey != second.StableFactKey {
		t.Fatalf("StableFactKey included mutable tag: %q != %q", envelope.StableFactKey, second.StableFactKey)
	}
}

func TestIndexDescriptorAndReferrerEnvelopeBuilders(t *testing.T) {
	t.Parallel()

	repository := RepositoryIdentity{
		Provider:   ProviderJFrog,
		Registry:   "jfrog.example/artifactory/api/docker/prod",
		Repository: "team/api",
	}
	indexDigest := "sha256:eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
	manifestDigest := "sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"
	attestationDigest := "sha256:1111111111111111111111111111111111111111111111111111111111111111"

	indexEnvelope, err := NewImageIndexEnvelope(IndexObservation{
		Repository: repository,
		Descriptor: Descriptor{
			Digest:    indexDigest,
			MediaType: MediaTypeOCIImageIndex,
			SizeBytes: 1024,
		},
		Manifests: []Descriptor{{
			Digest:    manifestDigest,
			MediaType: MediaTypeOCIImageManifest,
			SizeBytes: 2048,
			Platform:  Platform{OS: "linux", Architecture: "amd64"},
		}},
		GenerationID:        "generation-index",
		CollectorInstanceID: "jfrog-docker",
	})
	if err != nil {
		t.Fatalf("NewImageIndexEnvelope() error = %v", err)
	}
	assertOCIEnvelope(t, indexEnvelope, facts.OCIImageIndexFactKind, facts.OCIImageIndexSchemaVersion)

	descriptorEnvelope, err := NewDescriptorEnvelope(DescriptorObservation{
		Repository: repository,
		Descriptor: Descriptor{
			Digest:       attestationDigest,
			MediaType:    "application/vnd.in-toto+json",
			SizeBytes:    333,
			ArtifactType: "application/vnd.in-toto+json",
		},
		GenerationID:        "generation-descriptor",
		CollectorInstanceID: "jfrog-docker",
	})
	if err != nil {
		t.Fatalf("NewDescriptorEnvelope() error = %v", err)
	}
	assertOCIEnvelope(t, descriptorEnvelope, facts.OCIImageDescriptorFactKind, facts.OCIImageDescriptorSchemaVersion)

	referrerEnvelope, err := NewReferrerEnvelope(ReferrerObservation{
		Repository: repository,
		Subject: Descriptor{
			Digest:    manifestDigest,
			MediaType: MediaTypeOCIImageManifest,
			SizeBytes: 2048,
		},
		Referrer: Descriptor{
			Digest:       attestationDigest,
			MediaType:    "application/vnd.in-toto+json",
			SizeBytes:    333,
			ArtifactType: "application/vnd.in-toto+json",
		},
		SourceAPIPath:       "/v2/team/api/referrers/" + manifestDigest,
		GenerationID:        "generation-referrer",
		CollectorInstanceID: "jfrog-docker",
	})
	if err != nil {
		t.Fatalf("NewReferrerEnvelope() error = %v", err)
	}
	assertOCIEnvelope(t, referrerEnvelope, facts.OCIImageReferrerFactKind, facts.OCIImageReferrerSchemaVersion)
	if got := referrerEnvelope.Payload["subject_digest"]; got != manifestDigest {
		t.Fatalf("subject_digest = %#v", got)
	}
	if got := referrerEnvelope.Payload["referrer_digest"]; got != attestationDigest {
		t.Fatalf("referrer_digest = %#v", got)
	}
}

func assertOCIEnvelope(t *testing.T, envelope facts.Envelope, factKind, schemaVersion string) {
	t.Helper()

	if envelope.FactKind != factKind {
		t.Fatalf("FactKind = %q, want %q", envelope.FactKind, factKind)
	}
	if envelope.SchemaVersion != schemaVersion {
		t.Fatalf("SchemaVersion = %q, want %q", envelope.SchemaVersion, schemaVersion)
	}
	if envelope.CollectorKind != CollectorKind {
		t.Fatalf("CollectorKind = %q, want %q", envelope.CollectorKind, CollectorKind)
	}
	if envelope.SourceConfidence != facts.SourceConfidenceReported {
		t.Fatalf("SourceConfidence = %q, want %q", envelope.SourceConfidence, facts.SourceConfidenceReported)
	}
	if envelope.SourceRef.SourceSystem != CollectorKind {
		t.Fatalf("SourceRef.SourceSystem = %q, want %q", envelope.SourceRef.SourceSystem, CollectorKind)
	}
	if envelope.StableFactKey == "" || envelope.FactID == "" {
		t.Fatalf("stable identifiers must not be blank: %#v", envelope)
	}
}

func assertStringSlice(t *testing.T, got any, want []string) {
	t.Helper()

	values, ok := got.([]string)
	if !ok {
		t.Fatalf("value = %#v, want []string", got)
	}
	if len(values) != len(want) {
		t.Fatalf("len = %d, want %d: %#v", len(values), len(want), values)
	}
	for i := range want {
		if values[i] != want[i] {
			t.Fatalf("values[%d] = %q, want %q; values=%#v", i, values[i], want[i], values)
		}
	}
}
