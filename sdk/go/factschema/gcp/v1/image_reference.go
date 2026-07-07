// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package v1

// ImageReference is the schema-version-1 typed payload for the
// "gcp_image_reference" fact kind. It is digest-first evidence for a runtime
// image observed on a GCP compute resource.
type ImageReference struct {
	// OwningFullResourceName is the CAI full resource name running the image.
	// Required.
	OwningFullResourceName string `json:"owning_full_resource_name"`

	// TagDigestConfidence is "digest" or "tag"; it is required so consumers do
	// not treat tag-only evidence as digest-anchored.
	TagDigestConfidence string `json:"tag_digest_confidence"`

	// OwningProjectID is the project derived from OwningFullResourceName.
	OwningProjectID *string `json:"owning_project_id,omitempty"`

	// ImageReference is the observed image reference string.
	ImageReference *string `json:"image_reference,omitempty"`

	// ImageDigest is the observed content digest, when present.
	ImageDigest *string `json:"image_digest,omitempty"`

	// ContainerNameFingerprint is the keyed fingerprint of the container name.
	ContainerNameFingerprint *string `json:"container_name_fingerprint,omitempty"`

	// RedactionPolicyVersion identifies the policy used for fingerprints.
	RedactionPolicyVersion *string `json:"redaction_policy_version,omitempty"`
}
