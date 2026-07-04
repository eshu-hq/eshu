// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package v1

import "encoding/json"

// CloudResource is the schema-version-1 typed payload for the
// "azure_cloud_resource" fact kind (Contract System v1 §3.1,
// docs/internal/design/contract-system-v1.md).
//
// azure_cloud_resource is a POLYMORPHIC generic envelope: one fact kind
// carries every ARM resource type observed via Resource Graph or ARM
// fallback, and each resource type's provider-specific extension metadata
// rides along in a nested "extension" object. A single typed struct
// therefore cannot fully type it without either the optional-everything
// anti-pattern (design §3.3 rejects it) or a per-resource-type redesign (out
// of scope for this issue). The struct instead types and validates the
// shared IDENTITY contract and the common fields the reducer reads, and
// passes every remaining top-level payload key through untyped in
// Attributes:
//
//   - Required (identity): ARMResourceID, ResourceType, SubscriptionID,
//     Location. The decode seam rejects a payload missing any of them with a
//     classified input_invalid error naming the field, matching the collector
//     emitter (azurecloud.NewResourceEnvelope), which validates arm_resource_id
//     non-empty and always derives resource_type, subscription_id (from ARM
//     identity parsing), and location (from the collection boundary). This is
//     the accuracy fix — these are exactly the fields whose silent absence
//     produced a wrong-but-plausible CloudResource uid
//     (cloudResourceUID(subscriptionID, location, resourceType, resourceID)).
//   - Optional (common): NormalizedResourceID, ResourceName, ProviderNamespace
//     — fields the node projector reads. NormalizedResourceID is preferred
//     over ARMResourceID for uid derivation when present (the reducer's
//     azureNormalizedResourceID fallback), but the emitter always derives one
//     from ARM identity parsing when absent from the observation, so an
//     absent value is rare, not a break.
//   - Optional (pass-through): Attributes carries every top-level payload key
//     with no named struct field above, UNTYPED, with JSON type fidelity
//     preserved through the round trip. Unlike the aws family, the Azure
//     emitter writes its remaining fields FLAT at the top level (resource_group,
//     kind, sku_class, tags, provider_time, extension, ...) rather than nested
//     under an "attributes" object, so a consumer reads them directly at
//     Attributes[key] — never Attributes["attributes"][key].
//
// The generated JSON Schema at
// sdk/go/factschema/schema/azure_cloud_resource.v1.schema.json mirrors this:
// its "required" array lists only the four identity fields, and Attributes is
// an open object (additionalProperties) so the schema stays honest about the
// untyped pass-through.
type CloudResource struct {
	// ARMResourceID is the raw provider ARM resource id, preserved verbatim
	// for exact reducer joins. Required.
	ARMResourceID string `json:"arm_resource_id"`

	// ResourceType is the ARM provider-defined resource type (for example
	// "microsoft.compute/virtualmachines"), normalized lower-case by ARM
	// identity parsing. Required.
	ResourceType string `json:"resource_type"`

	// SubscriptionID is the Azure subscription the resource belongs to.
	// Required.
	SubscriptionID string `json:"subscription_id"`

	// Location is the Azure region/location bucket the resource was
	// collected under. Required.
	Location string `json:"location"`

	// NormalizedResourceID is the lower-cased, normalized ARM resource id
	// parsed from ARMResourceID. Optional: the reducer prefers this for uid
	// derivation when present and falls back to ARMResourceID otherwise; the
	// emitter always derives one from ARM identity parsing, so an absent
	// value only occurs if the collector's own normalization failed.
	NormalizedResourceID *string `json:"normalized_resource_id,omitempty"`

	// ResourceName is the ARM resource's short name segment. Optional: the
	// node projector copies it onto the CloudResource node's name field.
	ResourceName *string `json:"resource_name,omitempty"`

	// ProviderNamespace is the ARM provider namespace segment (for example
	// "microsoft.compute"). Optional: the node projector copies it onto the
	// node's service_kind field.
	ProviderNamespace *string `json:"provider_namespace,omitempty"`

	// Attributes carries every top-level payload key with no named struct
	// field above, untyped and with JSON type fidelity preserved by the
	// custom UnmarshalJSON. In practice this includes resource_group,
	// collector_kind, collector_instance_id, tenant_id, scope_kind,
	// provider_scope_id, source_lane, kind, sku_class, tags, provider_time,
	// redaction_policy_version, and extension. Optional: a payload with no
	// unmodeled top-level keys leaves it nil.
	Attributes map[string]any `json:"-"`
}

// cloudResourceKnownKeys is the set of payload keys the named CloudResource
// fields cover. UnmarshalJSON removes them from the raw payload so Attributes
// captures only the remainder, and MarshalJSON re-emits the named fields while
// flattening Attributes back to top level. Keeping this list beside the
// struct means adding a named field is a one-line change in two places (the
// field tag and this set), both local to this file.
var cloudResourceKnownKeys = map[string]struct{}{
	"arm_resource_id":        {},
	"resource_type":          {},
	"subscription_id":        {},
	"location":               {},
	"normalized_resource_id": {},
	"resource_name":          {},
	"provider_namespace":     {},
}

// cloudResourceAlias is CloudResource without the custom JSON methods, so
// UnmarshalJSON and MarshalJSON can decode/encode the named fields with the
// standard library without recursing into themselves.
type cloudResourceAlias CloudResource

// UnmarshalJSON decodes the named identity/common fields normally and
// captures every remaining top-level payload key into Attributes with its
// JSON-native Go type preserved (float64 for numbers, bool, string, []any,
// map[string]any). This is what lets a service-specific consumer read
// resource.Attributes["kind"] with the same value and Go type the raw payload
// carried.
func (r *CloudResource) UnmarshalJSON(data []byte) error {
	var named cloudResourceAlias
	if err := json.Unmarshal(data, &named); err != nil {
		return err
	}
	*r = CloudResource(named)

	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	for key := range cloudResourceKnownKeys {
		delete(raw, key)
	}
	if len(raw) > 0 {
		r.Attributes = raw
	}
	return nil
}

// MarshalJSON emits the named fields (honoring their omitempty rules) and
// flattens Attributes back to top-level keys, so an encoded CloudResource
// produces the same flat payload shape the collector emits. A key present in
// Attributes never overrides a named field: named fields win, and
// cloudResourceKnownKeys entries are dropped from the Attributes copy before
// merging.
func (r CloudResource) MarshalJSON() ([]byte, error) {
	named, err := json.Marshal(cloudResourceAlias(r))
	if err != nil {
		return nil, err
	}
	if len(r.Attributes) == 0 {
		return named, nil
	}

	var merged map[string]any
	if err := json.Unmarshal(named, &merged); err != nil {
		return nil, err
	}
	for key, value := range r.Attributes {
		if _, isKnown := cloudResourceKnownKeys[key]; isKnown {
			continue
		}
		if _, alreadyNamed := merged[key]; alreadyNamed {
			continue
		}
		merged[key] = value
	}
	return json.Marshal(merged)
}
