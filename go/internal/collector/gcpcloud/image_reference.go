// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/redact"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
	gcpv1 "github.com/eshu-hq/eshu/sdk/go/factschema/gcp/v1"
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

// ImageReferenceObservation is one runtime image reference observed on a GCP
// compute resource (GKE, Cloud Run, Compute Engine, ...). The collector keeps the
// image reference and digest as evidence and fingerprints the container name; it
// records digest-vs-tag confidence so the reducer can require a digest anchor.
type ImageReferenceObservation struct {
	// Boundary carries the scope and generation contract fields.
	Boundary Boundary
	// OwningFullResourceName is the CAI full resource name running the image.
	OwningFullResourceName string
	// ImageReference is the image reference string (registry/repo:tag or @digest).
	ImageReference string
	// ImageDigest is the content digest when present.
	ImageDigest string
	// ContainerName is the raw container name; it is fingerprinted, never raw.
	ContainerName string
	// UpdateTime is the read/update time.
	UpdateTime time.Time
	// SourceRecordID overrides the default record id.
	SourceRecordID string
	// SourceURI is the bounded source URI.
	SourceURI string
}

// NewImageReferenceEnvelope builds the durable gcp_image_reference fact for one
// runtime image reference. It is digest-first: the tag/digest confidence is
// `digest` when a content digest is present (or embedded in the reference) and
// `tag` otherwise, so a missing digest is explicit lower confidence rather than a
// fabricated match. The container name is fingerprinted with the redaction key.
// The stable fact key is derived from the owning resource identity and the image
// reference/digest (hashed by facts.StableID, never exposed).
//
// It fails closed on a missing owning resource name, no image reference and no
// digest, or a zero redaction key.
func NewImageReferenceEnvelope(obs ImageReferenceObservation, key redact.Key) (facts.Envelope, error) {
	if err := validateBoundary(obs.Boundary); err != nil {
		return facts.Envelope{}, err
	}
	if key.IsZero() {
		return facts.Envelope{}, fmt.Errorf("gcp image reference observation requires a redaction key")
	}
	owningName := strings.TrimSpace(obs.OwningFullResourceName)
	if owningName == "" {
		return facts.Envelope{}, fmt.Errorf("gcp image reference observation requires owning full_resource_name")
	}
	imageReference := strings.TrimSpace(obs.ImageReference)
	imageDigest := strings.TrimSpace(obs.ImageDigest)
	if imageReference == "" && imageDigest == "" {
		return facts.Envelope{}, fmt.Errorf("gcp image reference observation requires an image reference or digest")
	}

	confidence := ImageConfidenceTag
	if imageDigest != "" || strings.Contains(imageReference, "@sha256:") {
		confidence = ImageConfidenceDigest
	}

	stableKey := facts.StableID(facts.GCPImageReferenceFactKind, map[string]any{
		"owning_full_resource_name": owningName,
		"image_reference":           imageReference,
		"image_digest":              imageDigest,
		"content_family":            obs.Boundary.ContentFamily,
	})

	projectID := strings.TrimSpace(ProjectIDFromFullName(owningName))
	payload, err := factschema.EncodeGCPImageReference(gcpv1.ImageReference{
		OwningFullResourceName: owningName,
		TagDigestConfidence:    confidence,
		OwningProjectID:        &projectID,
		ImageReference:         &imageReference,
		ImageDigest:            &imageDigest,
		RedactionPolicyVersion: stringPtr(RedactionPolicyVersion),
	})
	if err != nil {
		return facts.Envelope{}, fmt.Errorf("encode gcp_image_reference payload: %w", err)
	}
	addGCPBoundaryPayload(payload, obs.Boundary)
	payload["read_time"] = timeOrNil(obs.Boundary.ReadTime)
	payload["update_time"] = timeOrNil(obs.UpdateTime.UTC())
	if container := strings.TrimSpace(obs.ContainerName); container != "" {
		payload["container_name_fingerprint"] = redact.String(container, "gcp_image_container", "gcp_image_container", key).Marker
	}

	return newEnvelope(
		obs.Boundary,
		facts.GCPImageReferenceFactKind,
		facts.GCPImageReferenceSchemaVersion,
		stableKey,
		sourceRecordID(obs.SourceRecordID, owningName+"|"+imageReference+"|"+imageDigest),
		obs.SourceURI,
		payload,
	), nil
}
