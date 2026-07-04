// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package v1

// IAMPolicyObservation is the schema-version-1 typed payload for the
// "gcp_iam_policy_observation" fact kind (Contract System v1 §3.1,
// docs/internal/design/contract-system-v1.md).
//
// The required set matches the collector emitter
// (gcpcloud.NewIAMPolicyObservationEnvelope), which fails closed on a missing
// FullResourceName, a missing AssetType, a missing Role, or zero fingerprinted
// members (iam_policy_observation.go:84-86 rejects an observation whose
// fingerprinted members slice is empty before the envelope is built). Members
// is therefore an unconditional invariant on every emitted fact — it is the
// only principal evidence the binding carries — so it is REQUIRED: the decode
// seam validates key presence (absent or explicit-null members dead-letters as
// input_invalid), which stops an external collector or fixture from omitting
// the sole principal evidence and still passing decode + schema conformance. A
// present-but-empty [] still decodes (the seam validates presence, not
// non-emptiness), but the emitter never produces one, so the meaningful contract
// boundary — the key must be present — is enforced.
//
// This kind is out of the secrets_iam family boundary: it is a GCP
// cloud-inventory IAM binding OBSERVATION (fingerprinted evidence only, no
// resolved principal/trust-chain graph write), distinct from
// gcp_iam_principal / gcp_iam_trust_policy / gcp_iam_permission_policy, which
// live in go/internal/facts/secrets_iam.go and are out of this family's scope.
type IAMPolicyObservation struct {
	// FullResourceName is the CAI full resource name the policy binding is
	// attached to. Required.
	FullResourceName string `json:"full_resource_name"`

	// AssetType is the CAI asset type of the resource. Required.
	AssetType string `json:"asset_type"`

	// Role is the bounded IAM role (for example "roles/storage.admin").
	// Required.
	Role string `json:"role"`

	// ProjectID is the GCP project derived from FullResourceName. Optional:
	// always emitted but may be empty.
	ProjectID *string `json:"project_id,omitempty"`

	// Members are the fingerprinted member bindings (each an object carrying
	// member_class and member_fingerprint; no raw member identity). Required:
	// the emitter fails closed on zero members, so it is the binding's
	// unconditional principal evidence. A required slice carries no omitempty by
	// the flat-struct convention, so an absent or null members key dead-letters
	// as input_invalid.
	Members []map[string]string `json:"members"`

	// ConditionPresent reports whether the binding carries an IAM condition.
	// Optional pointer so nil (unreported) stays distinct from an observed
	// false.
	ConditionPresent *bool `json:"condition_present,omitempty"`

	// ConditionFingerprint is a keyed fingerprint of the condition body, when
	// ConditionPresent is true. Optional: empty when no condition is present.
	ConditionFingerprint *string `json:"condition_fingerprint,omitempty"`

	// EtagFingerprint is a keyed fingerprint of the policy etag, when the
	// collector observed one. Optional: absent when no etag was observed.
	EtagFingerprint *string `json:"etag_fingerprint,omitempty"`
}
