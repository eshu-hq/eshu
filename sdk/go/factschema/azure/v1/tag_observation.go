// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package v1

// TagObservation is the schema-version-1 typed payload for the
// "azure_tag_observation" fact kind. Tag values are fingerprinted by the
// collector; raw tag values never enter durable facts.
type TagObservation struct {
	// ARMResourceID is the raw ARM identity carrying the observed tags. Required.
	ARMResourceID string `json:"arm_resource_id"`

	// NormalizedResourceID is the normalized ARM identity used for joins.
	// Required because the collector derives it before emitting the fact.
	NormalizedResourceID string `json:"normalized_resource_id"`

	// ResourceType is the normalized ARM provider resource type. Required.
	ResourceType string `json:"resource_type"`

	// TagValueFingerprints maps tag key to keyed value fingerprint. Required:
	// the emitter fails closed when no usable tags exist.
	TagValueFingerprints map[string]string `json:"tag_value_fingerprints"`

	// SubscriptionID is the Azure subscription the resource belongs to.
	SubscriptionID *string `json:"subscription_id,omitempty"`

	// ResourceGroup is the ARM resource group segment, when present.
	ResourceGroup *string `json:"resource_group,omitempty"`

	// ProviderNamespace is the ARM provider namespace segment.
	ProviderNamespace *string `json:"provider_namespace,omitempty"`

	// ResourceName is the ARM resource's short name segment.
	ResourceName *string `json:"resource_name,omitempty"`

	// TagKeys is the sorted set of tag keys whose values were fingerprinted.
	TagKeys []string `json:"tag_keys,omitempty"`

	// TagCount counts the fingerprinted tag values.
	TagCount *int `json:"tag_count,omitempty"`

	// TagTruncated reports whether the collector bounded the tag set.
	TagTruncated *bool `json:"tag_truncated,omitempty"`

	// ProviderTime is the provider read/update time, serialized as RFC3339 text.
	ProviderTime *string `json:"provider_time,omitempty"`

	// RedactionPolicyVersion identifies the policy used for value fingerprints.
	RedactionPolicyVersion *string `json:"redaction_policy_version,omitempty"`
}
