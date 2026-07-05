// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package v1

// ImageManifest is the schema-version-1 typed payload for the
// "oci_registry.image_manifest" fact kind (Contract System v1 §3.1,
// docs/internal/design/contract-system-v1.md).
//
// Required identity fields are RepositoryID and Digest: the projector's
// descriptor UID is oci-descriptor://<registry/repository>@<digest>, folded
// from RepositoryID and Digest, and the container-image-identity reducer keys
// its digest observations on (RepositoryID, Digest). Absent either key today
// yields a manifest row whose descriptor UID is built from an empty-string
// identity segment (or is dropped with no operator signal); requiring both
// makes an absent key a classified input_invalid dead-letter. A present-but-
// empty value stays a valid decode the projector's own identity gate still
// drops.
//
// DescriptorID is OPTIONAL: the projector synthesizes it from
// (RepositoryID, Digest) when absent (ociDescriptorFields), so its absence must
// stay a valid decode. MediaType, SizeBytes, ArtifactType, SourceTag, Config,
// Layers, CorrelationAnchors, and CollectorInstanceID are common properties the
// node row copies but never forms identity from, so all are optional.
type ImageManifest struct {
	// RepositoryID is the owning repository identity. Required — half the
	// descriptor UID.
	RepositoryID string `json:"repository_id"`

	// Digest is the manifest's content digest. Required — the other half of
	// the descriptor UID and the container-image join key.
	Digest string `json:"digest"`

	// DescriptorID is the collector-resolved descriptor UID, when supplied.
	// Optional: the projector synthesizes it from (RepositoryID, Digest) when
	// absent, so an absent value is a valid decode, not a dead-letter.
	DescriptorID *string `json:"descriptor_id,omitempty"`

	// MediaType is the manifest's OCI media type. Optional.
	MediaType *string `json:"media_type,omitempty"`

	// SizeBytes is the manifest's content size in bytes. Optional pointer so
	// nil stays distinct from an observed zero.
	SizeBytes *int64 `json:"size_bytes,omitempty"`

	// ArtifactType is the manifest's OCI artifact type, when observed. Optional.
	ArtifactType *string `json:"artifact_type,omitempty"`

	// SourceTag is the tag the manifest was observed under, when the collector
	// reached it by tag. Optional.
	SourceTag *string `json:"source_tag,omitempty"`

	// Config is the manifest's config descriptor. Optional: the projector reads
	// only Config.Digest for the node row's config_digest.
	Config *Descriptor `json:"config,omitempty"`

	// ConfigLabels are redaction-safe labels extracted from the image config.
	// Optional; carried for round-trip fidelity.
	ConfigLabels map[string]string `json:"config_labels,omitempty"`

	// Layers are the manifest's layer descriptors. Optional: the projector
	// collects Layers[].Digest for the node row's layer digests.
	Layers []Descriptor `json:"layers,omitempty"`

	// CorrelationAnchors are the redaction-safe anchors the collector published.
	// Optional.
	CorrelationAnchors []string `json:"correlation_anchors,omitempty"`

	// CollectorInstanceID is the emitting collector instance's boundary id.
	// Optional control-plane metadata.
	CollectorInstanceID *string `json:"collector_instance_id,omitempty"`
}
