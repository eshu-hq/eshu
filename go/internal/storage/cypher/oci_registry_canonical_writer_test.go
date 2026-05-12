package cypher

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/projector"
)

func TestCanonicalNodeWriterBuildsOCIRegistryStatements(t *testing.T) {
	t.Parallel()

	writer := NewCanonicalNodeWriter(&recordingExecutor{}, 2, nil)
	mat := projector.CanonicalMaterialization{
		ScopeID:      "oci-scope-1",
		GenerationID: "oci-generation-1",
		OCIRegistryRepository: &projector.OCIRegistryRepositoryRow{
			UID:              "oci-registry://registry.example.com/team/api",
			Provider:         "ghcr",
			Registry:         "registry.example.com",
			Repository:       "team/api",
			Visibility:       "private",
			AuthMode:         "credentialed",
			SourceFactID:     "oci-repository-1",
			StableFactKey:    "oci-registry://registry.example.com/team/api",
			SourceSystem:     "oci_registry",
			SourceRecordID:   "oci-registry://registry.example.com/team/api",
			SourceConfidence: facts.SourceConfidenceReported,
			CollectorKind:    "oci_registry",
		},
		OCIImageManifests: []projector.OCIImageManifestRow{{
			UID:                  "oci-descriptor://registry.example.com/team/api@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			RepositoryID:         "oci-registry://registry.example.com/team/api",
			Digest:               "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			MediaType:            "application/vnd.oci.image.manifest.v1+json",
			SizeBytes:            1024,
			ArtifactType:         "application/vnd.example.release",
			SourceTag:            "prod",
			ConfigDigest:         "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
			LayerDigests:         []string{"sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"},
			SourceFactID:         "oci-manifest-1",
			StableFactKey:        "oci-descriptor://registry.example.com/team/api@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			SourceSystem:         "oci_registry",
			SourceRecordID:       "oci-descriptor://registry.example.com/team/api@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			SourceConfidence:     facts.SourceConfidenceReported,
			CollectorKind:        "oci_registry",
			CorrelationAnchors:   []string{"oci-registry://registry.example.com/team/api"},
			CollectorInstanceID:  "oci-collector-1",
			ResolvedDescriptorID: "oci-descriptor://registry.example.com/team/api@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		}},
		OCIImageTagObservations: []projector.OCIImageTagObservationRow{{
			UID:                   "oci-tag-observation-1",
			RepositoryID:          "oci-registry://registry.example.com/team/api",
			ImageRef:              "registry.example.com/team/api:prod",
			Tag:                   "prod",
			ResolvedDigest:        "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			ResolvedDescriptorUID: "oci-descriptor://registry.example.com/team/api@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			MediaType:             "application/vnd.oci.image.manifest.v1+json",
			IdentityStrength:      "weak_tag",
			SourceFactID:          "oci-tag-1",
			StableFactKey:         "oci-tag:prod",
			SourceSystem:          "oci_registry",
			SourceConfidence:      facts.SourceConfidenceReported,
			CollectorKind:         "oci_registry",
		}},
	}

	statements := writer.buildOCIRegistryStatements(mat)
	if got, want := len(statements), 3; got != want {
		t.Fatalf("buildOCIRegistryStatements() count = %d, want %d", got, want)
	}

	repository := statements[0]
	if !strings.Contains(repository.Cypher, "MERGE (r:OciRegistryRepository {uid: row.uid})") {
		t.Fatalf("repository Cypher = %q, want OciRegistryRepository uid merge", repository.Cypher)
	}
	if got, want := repository.Parameters[StatementMetadataPhaseKey], canonicalPhaseOCIRegistry; got != want {
		t.Fatalf("repository phase = %#v, want %#v", got, want)
	}

	manifest := statements[1]
	if !strings.Contains(manifest.Cypher, "MATCH (r:OciRegistryRepository {uid: row.repository_id})") {
		t.Fatalf("manifest Cypher = %q, want repository uid anchor", manifest.Cypher)
	}
	if !strings.Contains(manifest.Cypher, "MERGE (m:ContainerImage:OciImageManifest {uid: row.uid})") {
		t.Fatalf("manifest Cypher = %q, want ContainerImage/OciImageManifest uid merge", manifest.Cypher)
	}
	if strings.Contains(manifest.Cypher, "{tag: row.source_tag}") {
		t.Fatalf("manifest Cypher = %q, must not merge image identity from a tag", manifest.Cypher)
	}

	tagObservation := statements[2]
	if !strings.Contains(tagObservation.Cypher, "MERGE (t:ContainerImageTagObservation:OciImageTagObservation {uid: row.uid})") {
		t.Fatalf("tag Cypher = %q, want ContainerImageTagObservation/OciImageTagObservation uid merge", tagObservation.Cypher)
	}
	rows := tagObservation.Parameters["rows"].([]map[string]any)
	if got, want := rows[0]["image_ref"], "registry.example.com/team/api:prod"; got != want {
		t.Fatalf("image_ref = %q, want %q", got, want)
	}
	if got, want := rows[0]["identity_strength"], "weak_tag"; got != want {
		t.Fatalf("identity_strength = %q, want %q", got, want)
	}
	if got, want := rows[0]["resolved_descriptor_uid"], mat.OCIImageTagObservations[0].ResolvedDescriptorUID; got != want {
		t.Fatalf("resolved_descriptor_uid = %q, want %q", got, want)
	}
}
