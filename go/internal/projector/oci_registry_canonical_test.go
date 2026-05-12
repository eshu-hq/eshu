package projector

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

func TestBuildCanonicalMaterializationExtractsOCIRegistryRows(t *testing.T) {
	t.Parallel()

	sc := ociRegistryScope()
	gen := ociRegistryGeneration()
	result := buildCanonicalMaterialization(sc, gen, ociRegistryFacts())

	if result.OCIRegistryRepository == nil {
		t.Fatal("OCIRegistryRepository = nil, want repository row")
	}
	repository := result.OCIRegistryRepository
	if got, want := repository.UID, "oci-registry://registry.example.com/team/api"; got != want {
		t.Fatalf("repository UID = %q, want %q", got, want)
	}
	if got, want := repository.Provider, "ghcr"; got != want {
		t.Fatalf("repository Provider = %q, want %q", got, want)
	}

	if got, want := len(result.OCIImageManifests), 1; got != want {
		t.Fatalf("len(OCIImageManifests) = %d, want %d", got, want)
	}
	manifest := result.OCIImageManifests[0]
	if got, want := manifest.UID, ociManifestDescriptorID(); got != want {
		t.Fatalf("manifest UID = %q, want %q", got, want)
	}
	if got, want := manifest.Digest, ociManifestDigest(); got != want {
		t.Fatalf("manifest Digest = %q, want %q", got, want)
	}
	if got, want := manifest.SourceTag, "prod"; got != want {
		t.Fatalf("manifest SourceTag = %q, want %q", got, want)
	}
	if got, want := manifest.ConfigDigest, ociConfigDigest(); got != want {
		t.Fatalf("manifest ConfigDigest = %q, want %q", got, want)
	}
	if got, want := manifest.LayerDigests[0], ociLayerDigest(); got != want {
		t.Fatalf("manifest LayerDigests[0] = %q, want %q", got, want)
	}

	if got, want := len(result.OCIImageIndexes), 1; got != want {
		t.Fatalf("len(OCIImageIndexes) = %d, want %d", got, want)
	}
	index := result.OCIImageIndexes[0]
	if got, want := index.ManifestDigests[0], ociManifestDigest(); got != want {
		t.Fatalf("index ManifestDigests[0] = %q, want %q", got, want)
	}

	if got, want := len(result.OCIImageTagObservations), 1; got != want {
		t.Fatalf("len(OCIImageTagObservations) = %d, want %d", got, want)
	}
	tag := result.OCIImageTagObservations[0]
	if got, want := tag.Tag, "prod"; got != want {
		t.Fatalf("tag Tag = %q, want %q", got, want)
	}
	if got, want := tag.ImageRef, "registry.example.com/team/api:prod"; got != want {
		t.Fatalf("tag ImageRef = %q, want %q", got, want)
	}
	if got, want := tag.ResolvedDigest, ociManifestDigest(); got != want {
		t.Fatalf("tag ResolvedDigest = %q, want %q", got, want)
	}
	if got, want := tag.IdentityStrength, "weak_tag"; got != want {
		t.Fatalf("tag IdentityStrength = %q, want %q", got, want)
	}
	if got, want := tag.ResolvedDescriptorUID, ociManifestDescriptorID(); got != want {
		t.Fatalf("tag ResolvedDescriptorUID = %q, want %q", got, want)
	}
}

func TestBuildCanonicalMaterializationSkipsTagOnlyOCIIdentity(t *testing.T) {
	t.Parallel()

	input := append(ociRegistryFacts(), facts.Envelope{
		FactID:        "oci-tag-only",
		ScopeID:       "oci-scope-1",
		GenerationID:  "oci-generation-1",
		FactKind:      facts.OCIImageTagObservationFactKind,
		SchemaVersion: facts.OCIImageTagObservationSchemaVersion,
		Payload: map[string]any{
			"provider":      "ghcr",
			"registry":      "registry.example.com",
			"repository":    "team/api",
			"repository_id": "oci-registry://registry.example.com/team/api",
			"tag":           "latest",
		},
	})

	result := buildCanonicalMaterialization(ociRegistryScope(), ociRegistryGeneration(), input)

	if got, want := len(result.OCIImageTagObservations), 1; got != want {
		t.Fatalf("len(OCIImageTagObservations) = %d, want %d", got, want)
	}
	if got, want := result.OCIImageTagObservations[0].Tag, "prod"; got != want {
		t.Fatalf("remaining tag = %q, want %q", got, want)
	}
}

func TestRuntimeProjectRejectsUnknownOCIRegistrySchemaVersion(t *testing.T) {
	t.Parallel()

	runtime := Runtime{
		CanonicalWriter: &recordingCanonicalWriter{},
		ContentWriter:   &recordingContentWriter{},
	}

	_, err := runtime.Project(
		context.Background(),
		ociRegistryScope(),
		ociRegistryGeneration(),
		[]facts.Envelope{{
			FactID:        "oci-manifest-1",
			ScopeID:       "oci-scope-1",
			GenerationID:  "oci-generation-1",
			FactKind:      facts.OCIImageManifestFactKind,
			SchemaVersion: "2.0.0",
			Payload: map[string]any{
				"repository_id": "oci-registry://registry.example.com/team/api",
				"descriptor_id": ociManifestDescriptorID(),
				"digest":        ociManifestDigest(),
			},
		}},
	)
	if err == nil {
		t.Fatal("Project() error = nil, want non-nil")
	}
}

func ociRegistryScope() scope.IngestionScope {
	return scope.IngestionScope{
		ScopeID:       "oci-scope-1",
		SourceSystem:  "oci_registry",
		ScopeKind:     scope.KindContainerRegistryRepository,
		CollectorKind: scope.CollectorOCIRegistry,
		PartitionKey:  "oci-registry://registry.example.com/team/api",
	}
}

func ociRegistryGeneration() scope.ScopeGeneration {
	return scope.ScopeGeneration{
		GenerationID: "oci-generation-1",
		ScopeID:      "oci-scope-1",
		ObservedAt:   time.Date(2026, time.May, 12, 15, 0, 0, 0, time.UTC),
		IngestedAt:   time.Date(2026, time.May, 12, 15, 1, 0, 0, time.UTC),
		Status:       scope.GenerationStatusPending,
		TriggerKind:  scope.TriggerKindSnapshot,
	}
}

func ociRegistryFacts() []facts.Envelope {
	observedAt := time.Date(2026, time.May, 12, 15, 0, 0, 0, time.UTC)
	return []facts.Envelope{
		{
			FactID:           "oci-repository-1",
			ScopeID:          "oci-scope-1",
			GenerationID:     "oci-generation-1",
			FactKind:         facts.OCIRegistryRepositoryFactKind,
			StableFactKey:    "oci-registry://registry.example.com/team/api",
			SchemaVersion:    facts.OCIRegistryRepositorySchemaVersion,
			CollectorKind:    "oci_registry",
			SourceConfidence: facts.SourceConfidenceReported,
			ObservedAt:       observedAt,
			Payload: map[string]any{
				"collector_instance_id": "oci-collector-1",
				"provider":              "ghcr",
				"registry":              "registry.example.com",
				"repository":            "team/api",
				"repository_id":         "oci-registry://registry.example.com/team/api",
				"visibility":            "private",
				"auth_mode":             "credentialed",
				"correlation_anchors":   []any{"oci-registry://registry.example.com/team/api"},
			},
			SourceRef: facts.Ref{
				SourceSystem:   "oci_registry",
				ScopeID:        "oci-scope-1",
				GenerationID:   "oci-generation-1",
				SourceRecordID: "oci-registry://registry.example.com/team/api",
			},
		},
		{
			FactID:           "oci-tag-1",
			ScopeID:          "oci-scope-1",
			GenerationID:     "oci-generation-1",
			FactKind:         facts.OCIImageTagObservationFactKind,
			StableFactKey:    "oci-tag:prod",
			SchemaVersion:    facts.OCIImageTagObservationSchemaVersion,
			CollectorKind:    "oci_registry",
			SourceConfidence: facts.SourceConfidenceReported,
			ObservedAt:       observedAt,
			Payload: map[string]any{
				"collector_instance_id": "oci-collector-1",
				"provider":              "ghcr",
				"registry":              "registry.example.com",
				"repository":            "team/api",
				"repository_id":         "oci-registry://registry.example.com/team/api",
				"tag":                   "prod",
				"resolved_digest":       ociManifestDigest(),
				"media_type":            "application/vnd.oci.image.manifest.v1+json",
				"identity_strength":     "weak_tag",
			},
		},
		{
			FactID:           "oci-manifest-1",
			ScopeID:          "oci-scope-1",
			GenerationID:     "oci-generation-1",
			FactKind:         facts.OCIImageManifestFactKind,
			StableFactKey:    ociManifestDescriptorID(),
			SchemaVersion:    facts.OCIImageManifestSchemaVersion,
			CollectorKind:    "oci_registry",
			SourceConfidence: facts.SourceConfidenceReported,
			ObservedAt:       observedAt,
			Payload: map[string]any{
				"collector_instance_id": "oci-collector-1",
				"provider":              "ghcr",
				"registry":              "registry.example.com",
				"repository":            "team/api",
				"repository_id":         "oci-registry://registry.example.com/team/api",
				"descriptor_id":         ociManifestDescriptorID(),
				"digest":                ociManifestDigest(),
				"media_type":            "application/vnd.oci.image.manifest.v1+json",
				"size_bytes":            int64(1024),
				"artifact_type":         "application/vnd.example.release",
				"source_tag":            "prod",
				"config": map[string]any{
					"digest":     ociConfigDigest(),
					"media_type": "application/vnd.oci.image.config.v1+json",
				},
				"layers": []any{
					map[string]any{
						"digest":     ociLayerDigest(),
						"media_type": "application/vnd.oci.image.layer.v1.tar+gzip",
					},
				},
				"correlation_anchors": []any{"oci-registry://registry.example.com/team/api", ociManifestDigest()},
			},
			SourceRef: facts.Ref{
				SourceSystem:   "oci_registry",
				ScopeID:        "oci-scope-1",
				GenerationID:   "oci-generation-1",
				SourceRecordID: ociManifestDescriptorID(),
			},
		},
		{
			FactID:           "oci-index-1",
			ScopeID:          "oci-scope-1",
			GenerationID:     "oci-generation-1",
			FactKind:         facts.OCIImageIndexFactKind,
			StableFactKey:    ociIndexDescriptorID(),
			SchemaVersion:    facts.OCIImageIndexSchemaVersion,
			CollectorKind:    "oci_registry",
			SourceConfidence: facts.SourceConfidenceReported,
			ObservedAt:       observedAt,
			Payload: map[string]any{
				"provider":      "ghcr",
				"registry":      "registry.example.com",
				"repository":    "team/api",
				"repository_id": "oci-registry://registry.example.com/team/api",
				"descriptor_id": ociIndexDescriptorID(),
				"digest":        ociIndexDigest(),
				"media_type":    "application/vnd.oci.image.index.v1+json",
				"manifests": []any{
					map[string]any{
						"digest":     ociManifestDigest(),
						"media_type": "application/vnd.oci.image.manifest.v1+json",
					},
				},
			},
		},
	}
}

func ociManifestDigest() string {
	return "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
}

func ociIndexDigest() string {
	return "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
}

func ociConfigDigest() string {
	return "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
}

func ociLayerDigest() string {
	return "sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"
}

func ociManifestDescriptorID() string {
	return "oci-descriptor://registry.example.com/team/api@" + ociManifestDigest()
}

func ociIndexDescriptorID() string {
	return "oci-descriptor://registry.example.com/team/api@" + ociIndexDigest()
}
