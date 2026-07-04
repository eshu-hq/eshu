// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package v1

// ResourceChange is the schema-version-1 typed payload for the
// "azure_resource_change" fact kind (Contract System v1 §3.1,
// docs/internal/design/contract-system-v1.md).
//
// A fully typed, CLOSED schema: the collector emitter
// (azurecloud.NewResourceChangeEnvelope) carries changed property PATHS only,
// never previous/new values, and fingerprints the actor identity before
// emission — the payload's full shape is already known. Change records are
// freshness evidence only; a "deleted" ChangeType is a tombstone candidate,
// not proof of final resource state, which the reducer must confirm via
// inventory.
//
// Required fields mirror what the emitter validates non-empty: TargetARM-
// ResourceID, ChangeType (validated against a bounded enum), and ChangeTime
// (the emitter rejects a zero time).
type ResourceChange struct {
	// TargetARMResourceID is the raw ARM identity the change applied to.
	// Required.
	TargetARMResourceID string `json:"target_arm_resource_id"`

	// ChangeType is the bounded change type (created, updated, or deleted).
	// Required: the emitter validates it against this enum.
	ChangeType string `json:"change_type"`

	// ChangeTime is the change timestamp. Required: the emitter rejects a
	// zero time.
	ChangeTime string `json:"change_time"`

	// TargetNormalizedID is the normalized form of TargetARMResourceID.
	// Optional metadata.
	TargetNormalizedID *string `json:"target_normalized_id,omitempty"`

	// TargetResourceType is the target's ARM resource type. Optional
	// metadata.
	TargetResourceType *string `json:"target_resource_type,omitempty"`

	// Operation is the bounded provider operation label. Optional metadata.
	Operation *string `json:"operation,omitempty"`

	// ClientType is the bounded client type label. Optional metadata.
	ClientType *string `json:"client_type,omitempty"`

	// ActorClass is the bounded actor class (for example user,
	// service_principal). Optional metadata.
	ActorClass *string `json:"actor_class,omitempty"`

	// ActorFingerprint is the keyed fingerprint of the raw actor identity.
	// Optional: absent when the provider did not report an actor.
	ActorFingerprint *string `json:"actor_fingerprint,omitempty"`

	// ChangedPropertyPaths lists changed property paths only, never values.
	// Optional.
	ChangedPropertyPaths []string `json:"changed_property_paths,omitempty"`

	// ChangedPropertyCount is the number of changed property paths before
	// truncation. Optional pointer so nil (unreported) stays distinct from an
	// observed zero.
	ChangedPropertyCount *int32 `json:"changed_property_count,omitempty"`

	// ChangedPropertyTruncated reports whether ChangedPropertyPaths was
	// truncated. Optional.
	ChangedPropertyTruncated *bool `json:"changed_property_truncated,omitempty"`

	// IsTombstoneCandidate reports whether this change record is a candidate
	// delete tombstone. Optional: a "deleted" ChangeType with this true means
	// the reducer must confirm final resource state via inventory rather than
	// projecting a delete directly from this fact.
	IsTombstoneCandidate *bool `json:"is_tombstone_candidate,omitempty"`

	// RedactionPolicyVersion is the redaction policy version the collector
	// fingerprinted the actor identity under. Optional metadata.
	RedactionPolicyVersion *string `json:"redaction_policy_version,omitempty"`
}
