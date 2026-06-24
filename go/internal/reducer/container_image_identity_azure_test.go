// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"slices"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

const testAzureImageRepository = "contoso.azurecr.io/team/api"

func TestContainerImageIdentityFactKindsIncludesAzureImageReferences(t *testing.T) {
	t.Parallel()

	if !slices.Contains(containerImageIdentityFactKinds(), facts.AzureImageReferenceFactKind) {
		t.Fatalf("containerImageIdentityFactKinds() missing %q", facts.AzureImageReferenceFactKind)
	}
}

func TestBuildContainerImageIdentityDecisionsConsumesAzureDigestReference(t *testing.T) {
	t.Parallel()

	decisions := BuildContainerImageIdentityDecisions([]facts.Envelope{
		azureImageReferenceFact(
			"azure-image-digest",
			testAzureImageRepository+":prod",
			testContainerDigest,
			"digest",
		),
		ociImageFactForRepository(
			"azure-oci-manifest",
			facts.OCIImageManifestFactKind,
			"contoso.azurecr.io",
			"team/api",
			testContainerDigest,
			nil,
		),
	})

	got := decisionsByRef(decisions)
	digestRef := testAzureImageRepository + "@" + testContainerDigest
	decision, ok := got[digestRef]
	if !ok {
		t.Fatalf("decisions missing digest reference %q: %#v", digestRef, decisions)
	}
	assertContainerImageDecision(t, decision, ContainerImageIdentityExactDigest, testContainerDigest, 1)
	if slices.Contains(decision.SourceRepositoryIDs, "azure-container-app") {
		t.Fatalf("SourceRepositoryIDs = %#v, want no invented repository anchor", decision.SourceRepositoryIDs)
	}
	if !slices.Contains(decision.EvidenceFactIDs, "azure-image-digest") ||
		!slices.Contains(decision.EvidenceFactIDs, "azure-oci-manifest") {
		t.Fatalf("EvidenceFactIDs = %#v, want Azure and OCI evidence", decision.EvidenceFactIDs)
	}
}

func TestBuildContainerImageIdentityDecisionsResolvesAzureTagOnlyWithRegistryEvidence(t *testing.T) {
	t.Parallel()

	decisions := BuildContainerImageIdentityDecisions([]facts.Envelope{
		azureImageReferenceFact("azure-image-tag", testAzureImageRepository+":prod", "", "tag"),
		ociImageFactForRepository(
			"azure-oci-tag",
			facts.OCIImageTagObservationFactKind,
			"contoso.azurecr.io",
			"team/api",
			testContainerDigest,
			map[string]any{
				"tag":             "prod",
				"resolved_digest": testContainerDigest,
			},
		),
	})

	got := decisionsByRef(decisions)
	decision := got[testAzureImageRepository+":prod"]
	assertContainerImageDecision(t, decision, ContainerImageIdentityTagResolved, testContainerDigest, 1)
	if !slices.Contains(decision.EvidenceFactIDs, "azure-image-tag") ||
		!slices.Contains(decision.EvidenceFactIDs, "azure-oci-tag") {
		t.Fatalf("EvidenceFactIDs = %#v, want Azure and OCI tag evidence", decision.EvidenceFactIDs)
	}
}

func azureImageReferenceFact(
	factID string,
	imageRef string,
	imageDigest string,
	confidence string,
) facts.Envelope {
	return facts.Envelope{
		FactID:           factID,
		ScopeID:          "azure:tenant:subscription:demo:containerapps:global:resources",
		GenerationID:     "generation-azure",
		FactKind:         facts.AzureImageReferenceFactKind,
		SchemaVersion:    facts.AzureImageReferenceSchemaVersion,
		CollectorKind:    "azure",
		SourceConfidence: facts.SourceConfidenceReported,
		ObservedAt:       time.Date(2026, time.June, 13, 12, 0, 0, 0, time.UTC),
		SourceRef: facts.Ref{
			SourceSystem: "azure",
			SourceURI:    "azure://container-apps/demo-api",
		},
		Payload: map[string]any{
			"owning_arm_resource_id":     "/subscriptions/demo/resourceGroups/rg/providers/Microsoft.App/containerApps/api",
			"owning_normalized_id":       "/subscriptions/demo/resourcegroups/rg/providers/microsoft.app/containerapps/api",
			"owning_resource_type":       "microsoft.app/containerapps",
			"image_reference":            imageRef,
			"image_digest":               imageDigest,
			"tag_digest_confidence":      confidence,
			"container_name_fingerprint": "sha256:container",
			"provider_time":              "2026-06-13T12:00:00Z",
		},
	}
}
