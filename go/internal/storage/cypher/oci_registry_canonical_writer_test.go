// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

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
	if strings.Contains(manifest.Cypher, "MATCH (r:OciRegistryRepository {uid: row.repository_id})") {
		t.Fatalf("manifest Cypher = %q, must not require repository relationship anchor", manifest.Cypher)
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

func TestCanonicalNodeWriterOCIRegistrySkipsRelationshipWrites(t *testing.T) {
	t.Parallel()

	writer := NewCanonicalNodeWriter(&recordingExecutor{}, 2, nil)
	mat := projector.CanonicalMaterialization{
		ScopeID:      "oci-scope-1",
		GenerationID: "oci-generation-2",
		OCIRegistryRepository: &projector.OCIRegistryRepositoryRow{
			UID:              "oci-registry://registry.example.com/team/api",
			Provider:         "ghcr",
			Registry:         "registry.example.com",
			Repository:       "team/api",
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
	}

	statements := writer.buildOCIRegistryStatements(mat)
	for _, statement := range statements {
		if strings.Contains(statement.Cypher, "MERGE (r)-[rel:") {
			t.Fatalf("OCI registry Cypher must not write repository publication relationships:\n%s", statement.Cypher)
		}
		if strings.Contains(statement.Cypher, "PUBLISHES_") || strings.Contains(statement.Cypher, "OBSERVED_") {
			t.Fatalf("OCI registry Cypher must derive repository truth from node properties, not relationships:\n%s", statement.Cypher)
		}
	}
}

func TestCanonicalNodeWriterOCIRegistryKeepsImageFamilyLabels(t *testing.T) {
	t.Parallel()

	writer := NewCanonicalNodeWriter(&recordingExecutor{}, 2, nil)
	mat := projector.CanonicalMaterialization{
		ScopeID:      "oci-scope-1",
		GenerationID: "oci-generation-2",
		OCIRegistryRepository: &projector.OCIRegistryRepositoryRow{
			UID:              "oci-registry://registry.example.com/team/api",
			Provider:         "ghcr",
			Registry:         "registry.example.com",
			Repository:       "team/api",
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
		OCIImageIndexes: []projector.OCIImageIndexRow{{
			UID:           "oci-index://registry.example.com/team/api@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
			RepositoryID:  "oci-registry://registry.example.com/team/api",
			Digest:        "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
			MediaType:     "application/vnd.oci.image.index.v1+json",
			SourceFactID:  "oci-index-1",
			StableFactKey: "oci-index:1",
			SourceSystem:  "oci_registry",
			CollectorKind: "oci_registry",
		}},
		OCIImageDescriptors: []projector.OCIImageDescriptorRow{{
			UID:           "oci-descriptor://registry.example.com/team/api@sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
			RepositoryID:  "oci-registry://registry.example.com/team/api",
			Digest:        "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
			MediaType:     "application/vnd.oci.image.manifest.v1+json",
			SourceFactID:  "oci-descriptor-1",
			StableFactKey: "oci-descriptor:1",
			SourceSystem:  "oci_registry",
			CollectorKind: "oci_registry",
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
		OCIImageReferrers: []projector.OCIImageReferrerRow{{
			UID:               "oci-referrer-1",
			RepositoryID:      "oci-registry://registry.example.com/team/api",
			SubjectDigest:     "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			SubjectMediaType:  "application/vnd.oci.image.manifest.v1+json",
			ReferrerDigest:    "sha256:eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee",
			ReferrerMediaType: "application/vnd.dev.cosign.simplesigning.v1+json",
			SourceFactID:      "oci-referrer-1",
			StableFactKey:     "oci-referrer:1",
			SourceSystem:      "oci_registry",
			CollectorKind:     "oci_registry",
		}},
	}

	statements := writer.buildOCIRegistryStatements(mat)
	wants := map[string]string{
		"OciImageManifest":       "MERGE (m:ContainerImage:OciImageManifest {uid: row.uid})",
		"OciImageIndex":          "MERGE (i:ContainerImageIndex:OciImageIndex {uid: row.uid})",
		"OciImageDescriptor":     "MERGE (d:ContainerImageDescriptor:OciImageDescriptor {uid: row.uid})",
		"OciImageTagObservation": "MERGE (t:ContainerImageTagObservation:OciImageTagObservation {uid: row.uid})",
		"OciImageReferrer":       "MERGE (ref:OciImageReferrer {uid: row.uid})",
	}
	for _, statement := range statements {
		label, _ := statement.Parameters[StatementMetadataEntityLabelKey].(string)
		wantMerge, ok := wants[label]
		if !ok {
			continue
		}
		if !strings.Contains(statement.Cypher, wantMerge) {
			t.Fatalf("%s Cypher = %q, want %q", label, statement.Cypher, wantMerge)
		}
	}
}
