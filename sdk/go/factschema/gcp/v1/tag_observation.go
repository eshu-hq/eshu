// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package v1

// TagObservation is the schema-version-1 typed payload for the
// "gcp_tag_observation" fact kind. Tag values are fingerprinted by the
// collector; raw tag values never enter durable facts.
type TagObservation struct {
	// FullResourceName is the CAI full resource name carrying the tags.
	// Required.
	FullResourceName string `json:"full_resource_name"`

	// AssetType is the CAI asset type of the tagged resource. Required.
	AssetType string `json:"asset_type"`

	// TagValueFingerprints maps tag key to keyed value fingerprint. Required:
	// the emitter fails closed when no usable tags exist.
	TagValueFingerprints map[string]string `json:"tag_value_fingerprints"`

	// ProjectID is the project derived from FullResourceName.
	ProjectID *string `json:"project_id,omitempty"`

	// TagKeys is the sorted set of tag keys whose values were fingerprinted.
	TagKeys []string `json:"tag_keys,omitempty"`

	// SourceKind is the bounded tag source, such as direct or effective.
	SourceKind *string `json:"source_kind,omitempty"`

	// TagInheritanceState maps tag keys to direct or inherited for effective
	// tag evidence.
	TagInheritanceState map[string]string `json:"tag_inheritance_state,omitempty"`

	// RedactionPolicyVersion identifies the policy used for value fingerprints.
	RedactionPolicyVersion *string `json:"redaction_policy_version,omitempty"`
}
