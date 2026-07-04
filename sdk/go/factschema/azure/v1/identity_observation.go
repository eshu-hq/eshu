// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package v1

// IdentityObservation is the schema-version-1 typed payload for the
// "azure_identity_observation" fact kind (Contract System v1 §3.1,
// docs/internal/design/contract-system-v1.md).
//
// A fully typed, CLOSED schema: the collector emitter
// (azurecloud.NewIdentityObservationEnvelope) fingerprints every principal
// GUID before emission, so the payload's full shape is already known — there
// is no untyped remainder to carry in an Attributes pass-through. It is
// policy evidence only; it creates no identity graph node.
//
// Required fields mirror what the emitter validates: ARMResourceID and
// IdentityType are always present (the emitter rejects an unknown
// IdentityType and a blank ARMResourceID). At least one of the four
// fingerprint fields is present on any given fact (the emitter requires at
// least one raw principal id), but which one varies per observation, so none
// of the four fingerprints can be required on its own — requiring one would
// dead-letter a valid fact identified only by another.
type IdentityObservation struct {
	// ARMResourceID is the raw ARM identity carrying the identity/assignment.
	// Required.
	ARMResourceID string `json:"arm_resource_id"`

	// IdentityType is the bounded identity/assignment type (system_assigned,
	// user_assigned, service_principal, or role_assignment). Required: the
	// emitter validates it against this enum and rejects an unknown value
	// before emission.
	IdentityType string `json:"identity_type"`

	// NormalizedResourceID is the normalized form of ARMResourceID. Optional:
	// the reducer prefers this for uid resolution when present.
	NormalizedResourceID *string `json:"normalized_resource_id,omitempty"`

	// ResourceType is the carrying resource's ARM resource type. Optional
	// metadata.
	ResourceType *string `json:"resource_type,omitempty"`

	// RoleClass is the bounded role/action class (for example owner,
	// contributor). Optional: blank for identity types that are not role
	// assignments.
	RoleClass *string `json:"role_class,omitempty"`

	// AssignmentScope is the ARM scope of a role assignment, preserved
	// verbatim. Optional: blank for identity types that are not role
	// assignments.
	AssignmentScope *string `json:"assignment_scope,omitempty"`

	// PrincipalFingerprint is the keyed fingerprint of the raw principal GUID.
	// Optional: at least one of the four fingerprint fields is present per
	// the emitter's either-or validation, but never all four are guaranteed;
	// none can be required alone.
	PrincipalFingerprint *string `json:"principal_fingerprint,omitempty"`

	// ClientFingerprint is the keyed fingerprint of the raw client GUID.
	// Optional; see PrincipalFingerprint.
	ClientFingerprint *string `json:"client_fingerprint,omitempty"`

	// ObjectFingerprint is the keyed fingerprint of the raw object GUID.
	// Optional; see PrincipalFingerprint.
	ObjectFingerprint *string `json:"object_fingerprint,omitempty"`

	// TenantFingerprint is the keyed fingerprint of the raw tenant GUID.
	// Optional; see PrincipalFingerprint.
	TenantFingerprint *string `json:"tenant_fingerprint,omitempty"`

	// ProviderTime is the observation's read/update time. Optional: absent
	// when the provider did not report one.
	ProviderTime *string `json:"provider_time,omitempty"`

	// RedactionPolicyVersion is the redaction policy version the collector
	// fingerprinted principal GUIDs under. Optional metadata.
	RedactionPolicyVersion *string `json:"redaction_policy_version,omitempty"`
}
