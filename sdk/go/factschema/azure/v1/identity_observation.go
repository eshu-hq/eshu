// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package v1

// IdentityObservation is the schema-version-1 typed payload for the
// "azure_identity_observation" fact kind. Principal identifiers are keyed
// fingerprints; raw GUIDs never enter durable facts.
type IdentityObservation struct {
	// ARMResourceID is the raw ARM identity carrying the identity evidence.
	// Required.
	ARMResourceID string `json:"arm_resource_id"`

	// NormalizedResourceID is the normalized ARM identity used for joins.
	// Required.
	NormalizedResourceID string `json:"normalized_resource_id"`

	// ResourceType is the normalized ARM provider resource type. Required.
	ResourceType string `json:"resource_type"`

	// IdentityType is the bounded identity/assignment type. Required.
	IdentityType string `json:"identity_type"`

	// RoleClass is the bounded role/action class, when present.
	RoleClass *string `json:"role_class,omitempty"`

	// AssignmentScope is the ARM scope of an assignment, when present.
	AssignmentScope *string `json:"assignment_scope,omitempty"`

	// PrincipalFingerprint is the keyed fingerprint of the principal id.
	PrincipalFingerprint *string `json:"principal_fingerprint,omitempty"`

	// ClientFingerprint is the keyed fingerprint of the client id.
	ClientFingerprint *string `json:"client_fingerprint,omitempty"`

	// ObjectFingerprint is the keyed fingerprint of the object id.
	ObjectFingerprint *string `json:"object_fingerprint,omitempty"`

	// TenantFingerprint is the keyed fingerprint of the tenant id.
	TenantFingerprint *string `json:"tenant_fingerprint,omitempty"`

	// ProviderTime is the provider read/update time, serialized as RFC3339 text.
	ProviderTime *string `json:"provider_time,omitempty"`

	// RedactionPolicyVersion identifies the policy used for fingerprints.
	RedactionPolicyVersion *string `json:"redaction_policy_version,omitempty"`
}
