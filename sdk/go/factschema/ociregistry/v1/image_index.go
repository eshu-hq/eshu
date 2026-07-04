// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package v1

// ImageIndex is the schema-version-1 typed payload for the
// "oci_registry.image_index" fact kind (Contract System v1 §3.1,
// docs/internal/design/contract-system-v1.md).
//
// Required identity fields are RepositoryID and Digest, for the same reason as
// ImageManifest: the descriptor UID folds both. DescriptorID stays optional
// (synthesized from RepositoryID and Digest when absent). Manifests,
// MediaType, SizeBytes, ArtifactType, and CorrelationAnchors are optional
// common properties.
type ImageIndex struct {
	// RepositoryID is the owning repository identity. Required.
	RepositoryID string `json:"repository_id"`

	// Digest is the index's content digest. Required.
	Digest string `json:"digest"`

	// DescriptorID is the collector-resolved descriptor UID, when supplied.
	// Optional: synthesized from (RepositoryID, Digest) when absent.
	DescriptorID *string `json:"descriptor_id,omitempty"`

	// MediaType is the index's OCI media type. Optional.
	MediaType *string `json:"media_type,omitempty"`

	// SizeBytes is the index's content size in bytes. Optional pointer so nil
	// stays distinct from an observed zero.
	SizeBytes *int64 `json:"size_bytes,omitempty"`

	// ArtifactType is the index's OCI artifact type, when observed. Optional.
	ArtifactType *string `json:"artifact_type,omitempty"`

	// Manifests are the index's child manifest descriptors. Optional: the
	// projector collects Manifests[].Digest for the index row's manifest
	// digests.
	Manifests []Descriptor `json:"manifests,omitempty"`

	// CorrelationAnchors are the redaction-safe anchors the collector published.
	// Optional.
	CorrelationAnchors []string `json:"correlation_anchors,omitempty"`

	// CollectorInstanceID is the emitting collector instance's boundary id.
	// Optional control-plane metadata.
	CollectorInstanceID *string `json:"collector_instance_id,omitempty"`
}
