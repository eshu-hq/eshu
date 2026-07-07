// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package azurecloud

import (
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/redact"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
	azurev1 "github.com/eshu-hq/eshu/sdk/go/factschema/azure/v1"
)

// Image reference confidence classifies how strongly an image reference is
// anchored. Digest-anchored evidence is required before the reducer admits image
// evidence into deployment or vulnerability paths.
const (
	// ImageConfidenceDigest means the reference is anchored to a content digest.
	ImageConfidenceDigest = "digest"
	// ImageConfidenceTag means the reference is tag-only (lower confidence).
	ImageConfidenceTag = "tag"
)

// ImageReferenceObservation is one runtime image reference observed on an Azure
// compute resource (AKS, Container Apps, App Service, VM scale set, ...). The
// collector keeps the image reference and digest as evidence and fingerprints the
// container name; it records digest-vs-tag confidence so the reducer can require
// a digest anchor before using the evidence.
type ImageReferenceObservation struct {
	// Boundary carries the scope and generation contract fields.
	Boundary Boundary
	// OwningARMResourceID is the raw ARM identity of the resource running the image.
	OwningARMResourceID string
	// ImageReference is the image reference string (registry/repo:tag or @digest).
	ImageReference string
	// ImageDigest is the content digest when present.
	ImageDigest string
	// ContainerName is the raw container name; it is fingerprinted, never raw.
	ContainerName string
	// ProviderTime is the read time, or nil when absent.
	ProviderTime *time.Time
	// SourceRecordID overrides the default record id.
	SourceRecordID string
	// SourceURI is the bounded source URI.
	SourceURI string
}

// NewImageReferenceEnvelope builds the durable azure_image_reference fact for one
// runtime image reference. It is digest-first: the tag/digest confidence is
// `digest` when a content digest is present (or embedded in the reference) and
// `tag` otherwise, so a missing digest is explicit lower confidence rather than a
// fabricated match. The container name is fingerprinted with the redaction key.
// The stable fact key is derived from the owning resource identity and the image
// reference/digest (hashed by facts.StableID, never exposed).
//
// It fails closed on a missing owning resource id, no image reference and no
// digest, or a zero redaction key.
func NewImageReferenceEnvelope(observation ImageReferenceObservation, key redact.Key) (facts.Envelope, error) {
	if err := validateBoundary(observation.Boundary); err != nil {
		return facts.Envelope{}, err
	}
	if key.IsZero() {
		return facts.Envelope{}, fmt.Errorf("azure image reference observation requires a redaction key")
	}
	owningID := strings.TrimSpace(observation.OwningARMResourceID)
	if owningID == "" {
		return facts.Envelope{}, fmt.Errorf("azure image reference observation requires owning_arm_resource_id")
	}
	imageReference := strings.TrimSpace(observation.ImageReference)
	imageDigest := strings.TrimSpace(observation.ImageDigest)
	if imageReference == "" && imageDigest == "" {
		return facts.Envelope{}, fmt.Errorf("azure image reference observation requires an image reference or digest")
	}

	owning, err := ParseARMIdentity(owningID)
	if err != nil {
		return facts.Envelope{}, fmt.Errorf("normalize owning arm identity: %w", err)
	}

	confidence := ImageConfidenceTag
	if imageDigest != "" || strings.Contains(imageReference, "@sha256:") {
		confidence = ImageConfidenceDigest
	}

	stableKey := facts.StableID(facts.AzureImageReferenceFactKind, map[string]any{
		"owning_normalized_id": owning.Normalized,
		"image_reference":      imageReference,
		"image_digest":         imageDigest,
		"source_lane":          observation.Boundary.SourceLane,
		"tenant_id":            observation.Boundary.TenantID,
	})

	payload, err := factschema.EncodeAzureImageReference(azurev1.ImageReference{
		OwningARMResourceID:    owningID,
		OwningNormalizedID:     owning.Normalized,
		OwningResourceType:     owning.ResourceType,
		TagDigestConfidence:    confidence,
		ImageReference:         &imageReference,
		ImageDigest:            &imageDigest,
		ProviderTime:           timeStringPtr(observation.ProviderTime),
		RedactionPolicyVersion: stringPtr(RedactionPolicyVersion),
	})
	if err != nil {
		return facts.Envelope{}, fmt.Errorf("encode azure_image_reference payload: %w", err)
	}
	addAzureBoundaryPayload(payload, observation.Boundary)
	payload["tenant_id"] = observation.Boundary.TenantID
	payload["scope_kind"] = observation.Boundary.ScopeKind
	payload["provider_scope_id"] = observation.Boundary.ProviderScopeID
	payload["source_lane"] = observation.Boundary.SourceLane
	if container := strings.TrimSpace(observation.ContainerName); container != "" {
		payload["container_name_fingerprint"] = redact.String(container, "azure_image_container", "azure_image_container", key).Marker
	}

	return newEnvelope(
		observation.Boundary,
		facts.AzureImageReferenceFactKind,
		facts.AzureImageReferenceSchemaVersion,
		stableKey,
		sourceRecordID(observation.SourceRecordID, owning.Normalized+"|"+imageReference+"|"+imageDigest),
		observation.SourceURI,
		payload,
	), nil
}
