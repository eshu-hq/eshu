// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package v1

// ImageDescriptor is the schema-version-1 typed payload for the
// "oci_registry.image_descriptor" fact kind (Contract System v1 §3.1,
// docs/internal/design/contract-system-v1.md).
//
// A reusable digest-addressed descriptor. Required identity fields are
// RepositoryID and Digest (the descriptor UID folds both); DescriptorID stays
// optional (synthesized when absent). MediaType, SizeBytes, and ArtifactType
// are optional common properties.
type ImageDescriptor struct {
	// RepositoryID is the owning repository identity. Required.
	RepositoryID string `json:"repository_id"`

	// Digest is the descriptor's content digest. Required.
	Digest string `json:"digest"`

	// DescriptorID is the collector-resolved descriptor UID, when supplied.
	// Optional: synthesized from (RepositoryID, Digest) when absent.
	DescriptorID *string `json:"descriptor_id,omitempty"`

	// MediaType is the descriptor's OCI media type. Optional.
	MediaType *string `json:"media_type,omitempty"`

	// SizeBytes is the descriptor's content size in bytes. Optional pointer so
	// nil stays distinct from an observed zero.
	SizeBytes *int64 `json:"size_bytes,omitempty"`

	// ArtifactType is the descriptor's OCI artifact type, when observed.
	// Optional.
	ArtifactType *string `json:"artifact_type,omitempty"`

	// CollectorInstanceID is the emitting collector instance's boundary id.
	// Optional control-plane metadata.
	CollectorInstanceID *string `json:"collector_instance_id,omitempty"`
}
