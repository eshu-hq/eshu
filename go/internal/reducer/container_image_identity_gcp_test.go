// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"slices"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

const testGCPImageRepository = "us-docker.pkg.dev/team-artifacts/apps/api"

func TestContainerImageIdentityFactKindsIncludesGCPImageReferences(t *testing.T) {
	t.Parallel()

	if !slices.Contains(containerImageIdentityFactKinds(), facts.GCPImageReferenceFactKind) {
		t.Fatalf("containerImageIdentityFactKinds() missing %q", facts.GCPImageReferenceFactKind)
	}
}

func TestBuildContainerImageIdentityDecisionsConsumesGCPDigestReference(t *testing.T) {
	t.Parallel()

	decisions := BuildContainerImageIdentityDecisions([]facts.Envelope{
		gcpImageReferenceFact(
			"gcp-image-digest",
			testGCPImageRepository+":prod",
			testContainerDigest,
			"digest",
		),
		ociImageFactForRepository(
			"gcp-oci-manifest",
			facts.OCIImageManifestFactKind,
			"us-docker.pkg.dev",
			"team-artifacts/apps/api",
			testContainerDigest,
			nil,
		),
	})

	got := decisionsByRef(decisions)
	digestRef := testGCPImageRepository + "@" + testContainerDigest
	decision, ok := got[digestRef]
	if !ok {
		t.Fatalf("decisions missing digest reference %q: %#v", digestRef, decisions)
	}
	assertContainerImageDecision(t, decision, ContainerImageIdentityExactDigest, testContainerDigest, 1)
	if slices.Contains(decision.SourceRepositoryIDs, "gcp-cloud-run-service") {
		t.Fatalf("SourceRepositoryIDs = %#v, want no invented repository anchor", decision.SourceRepositoryIDs)
	}
	if !slices.Contains(decision.EvidenceFactIDs, "gcp-image-digest") ||
		!slices.Contains(decision.EvidenceFactIDs, "gcp-oci-manifest") {
		t.Fatalf("EvidenceFactIDs = %#v, want GCP and OCI evidence", decision.EvidenceFactIDs)
	}
	if _, ok := got[testGCPImageRepository+":prod"]; ok {
		t.Fatalf("decisions included mutable tag ref despite digest evidence: %#v", decisions)
	}
}

func TestBuildContainerImageIdentityDecisionsResolvesGCPTagOnlyWithRegistryEvidence(t *testing.T) {
	t.Parallel()

	decisions := BuildContainerImageIdentityDecisions([]facts.Envelope{
		gcpImageReferenceFact("gcp-image-tag", testGCPImageRepository+":prod", "", "tag"),
		ociImageFactForRepository(
			"gcp-oci-tag",
			facts.OCIImageTagObservationFactKind,
			"us-docker.pkg.dev",
			"team-artifacts/apps/api",
			testContainerDigest,
			map[string]any{
				"tag":             "prod",
				"resolved_digest": testContainerDigest,
			},
		),
	})

	got := decisionsByRef(decisions)
	decision := got[testGCPImageRepository+":prod"]
	assertContainerImageDecision(t, decision, ContainerImageIdentityTagResolved, testContainerDigest, 1)
	if !slices.Contains(decision.EvidenceFactIDs, "gcp-image-tag") ||
		!slices.Contains(decision.EvidenceFactIDs, "gcp-oci-tag") {
		t.Fatalf("EvidenceFactIDs = %#v, want GCP and OCI tag evidence", decision.EvidenceFactIDs)
	}
}

func gcpImageReferenceFact(
	factID string,
	imageRef string,
	imageDigest string,
	confidence string,
) facts.Envelope {
	return facts.Envelope{
		FactID:           factID,
		ScopeID:          "gcp:project:demo:run:resource:global",
		GenerationID:     "generation-gcp",
		FactKind:         facts.GCPImageReferenceFactKind,
		SchemaVersion:    facts.GCPImageReferenceSchemaVersion,
		CollectorKind:    "gcp",
		SourceConfidence: facts.SourceConfidenceReported,
		ObservedAt:       time.Date(2026, time.June, 13, 11, 0, 0, 0, time.UTC),
		SourceRef: facts.Ref{
			SourceSystem: "gcp",
			SourceURI:    "gcp://cloud-run/demo-service",
		},
		Payload: map[string]any{
			"owning_full_resource_name":  "//run.googleapis.com/projects/demo/locations/us-central1/services/api",
			"owning_asset_type":          "run.googleapis.com/Service",
			"image_reference":            imageRef,
			"image_digest":               imageDigest,
			"tag_digest_confidence":      confidence,
			"container_name_fingerprint": "sha256:container",
			"read_time":                  "2026-06-13T11:00:00Z",
		},
	}
}

func ociImageFactForRepository(
	factID string,
	kind string,
	registry string,
	repository string,
	digest string,
	extra map[string]any,
) facts.Envelope {
	payload := map[string]any{
		"registry":      registry,
		"repository":    repository,
		"repository_id": "oci-registry://" + registry + "/" + repository,
		"digest":        digest,
		"media_type":    "application/vnd.oci.image.manifest.v1+json",
	}
	for key, value := range extra {
		payload[key] = value
	}
	return facts.Envelope{
		FactID:           factID,
		ScopeID:          "oci-registry://" + registry + "/" + repository,
		GenerationID:     "generation-oci",
		FactKind:         kind,
		SchemaVersion:    "1.0.0",
		CollectorKind:    "oci_registry",
		SourceConfidence: facts.SourceConfidenceReported,
		ObservedAt:       time.Date(2026, time.June, 13, 11, 0, 0, 0, time.UTC),
		SourceRef: facts.Ref{
			SourceSystem: "oci_registry",
		},
		Payload: payload,
	}
}
