// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package v1

// ImageReferrer is the schema-version-1 typed payload for the
// "oci_registry.image_referrer" fact kind (Contract System v1 §3.1,
// docs/internal/design/contract-system-v1.md).
//
// A subject/referrer descriptor observation. Required identity fields are
// RepositoryID, SubjectDigest, and ReferrerDigest: the projector's referrer
// UID folds all three. Absent any one today yields a UID built from an
// empty-string segment (or a silently dropped row); requiring them makes an
// absent key a classified input_invalid dead-letter. The remaining fields are
// optional common properties.
type ImageReferrer struct {
	// RepositoryID is the owning repository identity. Required.
	RepositoryID string `json:"repository_id"`

	// SubjectDigest is the digest of the subject the referrer refers to.
	// Required.
	SubjectDigest string `json:"subject_digest"`

	// ReferrerDigest is the digest of the referrer descriptor itself. Required.
	ReferrerDigest string `json:"referrer_digest"`

	// SubjectMediaType is the subject descriptor's media type, when observed.
	// Optional.
	SubjectMediaType *string `json:"subject_media_type,omitempty"`

	// ReferrerMediaType is the referrer descriptor's media type, when observed.
	// Optional.
	ReferrerMediaType *string `json:"referrer_media_type,omitempty"`

	// ArtifactType is the referrer's OCI artifact type, when observed. Optional.
	ArtifactType *string `json:"artifact_type,omitempty"`

	// SizeBytes is the referrer descriptor's content size in bytes. Optional
	// pointer so nil stays distinct from an observed zero.
	SizeBytes *int64 `json:"size_bytes,omitempty"`

	// SourceAPIPath is the referrers-API path the referrer was observed on,
	// when applicable. Optional.
	SourceAPIPath *string `json:"source_api_path,omitempty"`

	// Provider is the registry provider kind. Optional.
	Provider *string `json:"provider,omitempty"`

	// Registry is the registry host. Optional.
	Registry *string `json:"registry,omitempty"`

	// Repository is the repository path within the registry. Optional.
	Repository *string `json:"repository,omitempty"`

	// Annotations are the referrer descriptor's redaction-safe annotations.
	// Optional.
	Annotations map[string]string `json:"annotations,omitempty"`

	// CollectorInstanceID is the emitting collector instance's boundary id.
	// Optional control-plane metadata.
	CollectorInstanceID *string `json:"collector_instance_id,omitempty"`

	// CorrelationAnchors are the redaction-safe anchors the collector published.
	// Optional.
	CorrelationAnchors []string `json:"correlation_anchors,omitempty"`
}
