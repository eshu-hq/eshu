// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package v1

// ResourceChange is the schema-version-1 typed payload for the
// "azure_resource_change" fact kind. It carries changed property paths only,
// never previous or new values.
type ResourceChange struct {
	// TargetARMResourceID is the raw ARM identity the change applied to.
	// Required.
	TargetARMResourceID string `json:"target_arm_resource_id"`

	// TargetNormalizedID is the normalized ARM target identity. Required.
	TargetNormalizedID string `json:"target_normalized_id"`

	// TargetResourceType is the normalized ARM resource type. Required.
	TargetResourceType string `json:"target_resource_type"`

	// ChangeType is the bounded create/update/delete class. Required.
	ChangeType string `json:"change_type"`

	// ChangeTime is the provider change timestamp, serialized as RFC3339 text.
	// Required.
	ChangeTime string `json:"change_time"`

	// Operation is the bounded provider operation label.
	Operation *string `json:"operation,omitempty"`

	// ClientType is the bounded client type label.
	ClientType *string `json:"client_type,omitempty"`

	// ActorClass is the bounded actor class.
	ActorClass *string `json:"actor_class,omitempty"`

	// ActorFingerprint is the keyed fingerprint of the actor identity.
	ActorFingerprint *string `json:"actor_fingerprint,omitempty"`

	// ChangedPropertyPaths lists changed property paths only.
	ChangedPropertyPaths []string `json:"changed_property_paths,omitempty"`

	// ChangedPropertyCount counts paths carried after bounding.
	ChangedPropertyCount *int `json:"changed_property_count,omitempty"`

	// ChangedPropertyTruncated reports whether changed paths were bounded.
	ChangedPropertyTruncated *bool `json:"changed_property_truncated,omitempty"`

	// IsTombstoneCandidate marks delete changes that need inventory confirmation.
	IsTombstoneCandidate *bool `json:"is_tombstone_candidate,omitempty"`

	// RedactionPolicyVersion identifies the policy used for actor fingerprints.
	RedactionPolicyVersion *string `json:"redaction_policy_version,omitempty"`
}
