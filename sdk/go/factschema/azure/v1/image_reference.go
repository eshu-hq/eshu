// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package v1

// ImageReference is the schema-version-1 typed payload for the
// "azure_image_reference" fact kind (Contract System v1 §3.1,
// docs/internal/design/contract-system-v1.md).
//
// A fully typed, CLOSED schema: the collector emitter
// (azurecloud.NewImageReferenceEnvelope) fingerprints the container name
// before emission, so the payload's full shape is already known. It is
// digest-first: TagDigestConfidence is "digest" when a content digest is
// present (or embedded in the reference) and "tag" otherwise, so a missing
// digest is explicit lower confidence.
//
// Required fields mirror what the emitter validates: OwningARMResourceID
// (always non-empty) and TagDigestConfidence (always derived, never absent).
// ImageReference and ImageDigest are each individually optional because the
// emitter validates only that at least one of the two is non-empty — neither
// can be required alone without dead-lettering a valid digest-only or
// reference-only fact.
type ImageReference struct {
	// OwningARMResourceID is the raw ARM identity of the resource running the
	// image. Required.
	OwningARMResourceID string `json:"owning_arm_resource_id"`

	// TagDigestConfidence classifies whether the reference is digest-anchored
	// ("digest") or tag-only ("tag"). Required: the emitter always derives
	// this value.
	TagDigestConfidence string `json:"tag_digest_confidence"`

	// OwningNormalizedID is the normalized form of OwningARMResourceID.
	// Optional metadata.
	OwningNormalizedID *string `json:"owning_normalized_id,omitempty"`

	// OwningResourceType is the owning resource's ARM resource type. Optional
	// metadata.
	OwningResourceType *string `json:"owning_resource_type,omitempty"`

	// ImageReference is the image reference string (registry/repo:tag or
	// @digest). Optional: the emitter requires ImageReference OR ImageDigest,
	// so this may be empty when only a digest was observed.
	ImageReference *string `json:"image_reference,omitempty"`

	// ImageDigest is the content digest when present. Optional: the emitter
	// requires ImageReference OR ImageDigest, so this may be empty when only
	// a tag reference was observed.
	ImageDigest *string `json:"image_digest,omitempty"`

	// ContainerNameFingerprint is the keyed fingerprint of the raw container
	// name. Optional: absent when the provider did not report a container
	// name.
	ContainerNameFingerprint *string `json:"container_name_fingerprint,omitempty"`

	// ProviderTime is the read time. Optional: absent when the provider did
	// not report one.
	ProviderTime *string `json:"provider_time,omitempty"`

	// RedactionPolicyVersion is the redaction policy version the collector
	// fingerprinted the container name under. Optional metadata.
	RedactionPolicyVersion *string `json:"redaction_policy_version,omitempty"`
}
