// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package v1

// Descriptor is a nested, digest-addressed OCI descriptor carried inside a
// manifest's config/layers or an index's manifests list (Contract System v1
// §3.1, docs/internal/design/contract-system-v1.md).
//
// It is NOT a fact-kind payload on its own; it is the typed shape of the
// nested objects the projector reads out of OCIImageManifest.Config,
// OCIImageManifest.Layers, and OCIImageIndex.Manifests. Only Digest is read
// for graph truth today (the manifest/index rows collect nested digests); the
// remaining fields are typed for round-trip fidelity so an encode of a decoded
// manifest reproduces the collector's payload. Every field is optional: a
// nested descriptor with no digest is simply skipped by the projector's digest
// collector, so absence is a valid, non-materializable value, never a
// dead-letter.
type Descriptor struct {
	// Digest is the descriptor's content digest (for example
	// "sha256:..."). Optional: a nested descriptor missing a digest is
	// skipped by the projector's digest collector rather than dead-lettered.
	Digest *string `json:"digest,omitempty"`

	// MediaType is the descriptor's OCI media type. Optional.
	MediaType *string `json:"media_type,omitempty"`

	// SizeBytes is the descriptor's content size in bytes. Optional pointer so
	// nil (unreported) stays distinct from an observed zero.
	SizeBytes *int64 `json:"size_bytes,omitempty"`

	// ArtifactType is the descriptor's OCI artifact type, when the collector
	// observed one. Optional.
	ArtifactType *string `json:"artifact_type,omitempty"`

	// Annotations are the descriptor's redaction-safe annotations. Optional: a
	// descriptor with no retained annotations omits this.
	Annotations map[string]string `json:"annotations,omitempty"`

	// Platform is the descriptor's platform selector (os/architecture/variant),
	// when the collector observed one. Optional.
	Platform map[string]string `json:"platform,omitempty"`
}
