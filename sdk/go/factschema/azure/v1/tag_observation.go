// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package v1

// TagObservation is the schema-version-1 typed payload for the
// "azure_tag_observation" fact kind (Contract System v1 §3.1,
// docs/internal/design/contract-system-v1.md).
//
// Unlike CloudResource and CloudRelationship, TagObservation is a fully
// typed, CLOSED schema: the collector emitter (azurecloud.NewTagObservation-
// Envelope) fingerprints every tag value before emission, so the payload's
// full shape is already known and stable — there is no untyped
// service-specific remainder to carry in an Attributes pass-through.
//
// Required identity fields mirror what the emitter validates non-empty:
// ARMResourceID and ResourceType anchor the tag subject to the same
// CloudResource uid the resource fact would resolve to. TagValueFingerprints
// is a map field and, by this repo's flat-struct convention
// (TestPayloadStructShapeConvention), every map/slice field carries
// omitempty regardless of whether it is semantically required — the
// collector emitter (azurecloud.NewTagObservationEnvelope) independently
// refuses to build an envelope for a resource with zero usable tags, so an
// empty map here never actually reaches decode on the production path even
// though the schema does not enforce it as a required key.
type TagObservation struct {
	// ARMResourceID is the raw ARM identity of the tagged resource. Required
	// — it is the identity the reducer resolves against materialized
	// CloudResource facts.
	ARMResourceID string `json:"arm_resource_id"`

	// ResourceType is the tagged resource's ARM resource type. Required: the
	// reducer's CloudResource uid derivation needs it alongside the
	// identity.
	ResourceType string `json:"resource_type"`

	// TagValueFingerprints maps each observed tag key to its keyed value
	// fingerprint marker. Optional per the flat-struct map/slice convention
	// (see the struct doc comment): the collector emitter never builds an
	// envelope for a resource with zero usable tags, so this is effectively
	// always populated on the production path even though the schema does
	// not enforce it as required.
	TagValueFingerprints map[string]string `json:"tag_value_fingerprints,omitempty"`

	// NormalizedResourceID is the normalized form of ARMResourceID. Optional:
	// the reducer prefers this for uid resolution when present.
	NormalizedResourceID *string `json:"normalized_resource_id,omitempty"`

	// SubscriptionID is the Azure subscription the tagged resource belongs
	// to. Optional metadata.
	SubscriptionID *string `json:"subscription_id,omitempty"`

	// ResourceGroup is the ARM resource group segment. Optional metadata.
	ResourceGroup *string `json:"resource_group,omitempty"`

	// ProviderNamespace is the ARM provider namespace segment. Optional
	// metadata.
	ProviderNamespace *string `json:"provider_namespace,omitempty"`

	// ResourceName is the ARM resource's short name segment. Optional
	// metadata.
	ResourceName *string `json:"resource_name,omitempty"`

	// TagKeys lists the observed tag keys (without values), preserved as
	// correlation taxonomy. Optional.
	TagKeys []string `json:"tag_keys,omitempty"`

	// TagCount is the number of fingerprinted tags. Optional pointer so nil
	// (unreported) stays distinct from an observed zero.
	TagCount *int32 `json:"tag_count,omitempty"`

	// TagTruncated reports whether the tag set was truncated before
	// fingerprinting. Optional.
	TagTruncated *bool `json:"tag_truncated,omitempty"`

	// ProviderTime is the Resource Graph read/update time. Optional: absent
	// when the provider did not report one.
	ProviderTime *string `json:"provider_time,omitempty"`

	// RedactionPolicyVersion is the redaction policy version the collector
	// fingerprinted tag values under. Optional metadata.
	RedactionPolicyVersion *string `json:"redaction_policy_version,omitempty"`
}
