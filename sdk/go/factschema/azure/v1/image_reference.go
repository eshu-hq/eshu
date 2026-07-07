// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package v1

// ImageReference is the schema-version-1 typed payload for the
// "azure_image_reference" fact kind. It is digest-first evidence for a runtime
// image observed on an Azure compute resource.
type ImageReference struct {
	// OwningARMResourceID is the raw ARM identity of the resource running the
	// image. Required.
	OwningARMResourceID string `json:"owning_arm_resource_id"`

	// OwningNormalizedID is the normalized ARM identity of the owning resource.
	// Required.
	OwningNormalizedID string `json:"owning_normalized_id"`

	// OwningResourceType is the normalized ARM resource type. Required.
	OwningResourceType string `json:"owning_resource_type"`

	// TagDigestConfidence is "digest" or "tag"; it is required so consumers do
	// not treat tag-only evidence as digest-anchored.
	TagDigestConfidence string `json:"tag_digest_confidence"`

	// ImageReference is the observed image reference string.
	ImageReference *string `json:"image_reference,omitempty"`

	// ImageDigest is the observed content digest, when present.
	ImageDigest *string `json:"image_digest,omitempty"`

	// ContainerNameFingerprint is the keyed fingerprint of the container name.
	ContainerNameFingerprint *string `json:"container_name_fingerprint,omitempty"`

	// ProviderTime is the provider read/update time, serialized as RFC3339 text.
	ProviderTime *string `json:"provider_time,omitempty"`

	// RedactionPolicyVersion identifies the policy used for fingerprints.
	RedactionPolicyVersion *string `json:"redaction_policy_version,omitempty"`
}
