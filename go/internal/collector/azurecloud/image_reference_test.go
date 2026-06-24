// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package azurecloud

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/redact"
)

func testImageReferenceObservation() ImageReferenceObservation {
	return ImageReferenceObservation{
		Boundary:            testBoundary(),
		OwningARMResourceID: "/subscriptions/11111111-1111-1111-1111-111111111111/resourceGroups/rg-app/providers/Microsoft.ContainerService/managedClusters/aks-prod",
		ImageReference:      "registry.example.com/team/api:1.4.2",
		ImageDigest:         "sha256:abc123",
		ContainerName:       "api",
	}
}

// TestNewImageReferenceEnvelopeDigestFirstConfidence proves the fact records
// digest confidence when a digest is present and fingerprints the container name.
func TestNewImageReferenceEnvelopeDigestFirstConfidence(t *testing.T) {
	obs := testImageReferenceObservation()
	key := testRedactionKey(t)

	env, err := NewImageReferenceEnvelope(obs, key)
	if err != nil {
		t.Fatalf("NewImageReferenceEnvelope error: %v", err)
	}
	if env.FactKind != facts.AzureImageReferenceFactKind {
		t.Fatalf("FactKind = %q", env.FactKind)
	}
	if env.Payload["tag_digest_confidence"] != ImageConfidenceDigest {
		t.Fatalf("tag_digest_confidence = %#v, want digest", env.Payload["tag_digest_confidence"])
	}
	if env.Payload["image_reference"] != obs.ImageReference {
		t.Fatalf("image_reference = %#v", env.Payload["image_reference"])
	}
	containerFp, _ := env.Payload["container_name_fingerprint"].(string)
	if containerFp == "" || containerFp == obs.ContainerName {
		t.Fatalf("container_name_fingerprint = %q, want non-raw marker", containerFp)
	}
}

// TestNewImageReferenceEnvelopeTagOnlyLowerConfidence proves a tag-only reference
// (no digest) records tag confidence rather than fabricating a digest match.
func TestNewImageReferenceEnvelopeTagOnlyLowerConfidence(t *testing.T) {
	obs := testImageReferenceObservation()
	obs.ImageDigest = ""
	obs.ImageReference = "registry.example.com/team/api:1.4.2"
	key := testRedactionKey(t)

	env, err := NewImageReferenceEnvelope(obs, key)
	if err != nil {
		t.Fatalf("NewImageReferenceEnvelope error: %v", err)
	}
	if env.Payload["tag_digest_confidence"] != ImageConfidenceTag {
		t.Fatalf("tag_digest_confidence = %#v, want tag", env.Payload["tag_digest_confidence"])
	}
}

// TestNewImageReferenceEnvelopeRejectsInvalid proves the builder fails closed on a
// missing owning resource, no reference and no digest, or a zero redaction key.
func TestNewImageReferenceEnvelopeRejectsInvalid(t *testing.T) {
	key := testRedactionKey(t)
	missingOwning := testImageReferenceObservation()
	missingOwning.OwningARMResourceID = ""
	if _, err := NewImageReferenceEnvelope(missingOwning, key); err == nil {
		t.Fatal("missing owning: error = nil, want non-nil")
	}
	noRef := testImageReferenceObservation()
	noRef.ImageReference = ""
	noRef.ImageDigest = ""
	if _, err := NewImageReferenceEnvelope(noRef, key); err == nil {
		t.Fatal("no ref/digest: error = nil, want non-nil")
	}
	if _, err := NewImageReferenceEnvelope(testImageReferenceObservation(), redact.Key{}); err == nil {
		t.Fatal("zero key: error = nil, want non-nil")
	}
}
