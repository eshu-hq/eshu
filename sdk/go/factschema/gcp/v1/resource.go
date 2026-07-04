// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package v1

import "encoding/json"

// Resource is the schema-version-1.1.0 typed payload for the
// "gcp_cloud_resource" fact kind (Contract System v1 §3.1,
// docs/internal/design/contract-system-v1.md).
//
// gcp_cloud_resource is a POLYMORPHIC generic envelope, mirroring
// awsv1.Resource: one fact kind carries every Cloud Asset Inventory resource
// type (Compute instances, Storage buckets, BigQuery datasets, ...), and
// per-asset-type extractors contribute their own fields through the bounded
// typed-depth Attributes map rather than a dedicated struct field per asset
// type. The struct types and validates the shared IDENTITY contract and the
// common fields the node projector reads, and passes the remainder through
// untyped in Attributes:
//
//   - Required (identity): FullResourceName, AssetType. The decode seam
//     rejects a payload missing either with a classified input_invalid error
//     naming the field, matching the collector emitter
//     (gcpcloud.NewCloudResourceEnvelope), which validates both non-empty, and
//     the reducer's own node-row gate (gcpCloudResourceNodeRow), which already
//     drops a resource lacking either rather than fabricating a node. This is
//     the accuracy fix: a collector regression that drops one of these two
//     keys now dead-letters instead of silently producing an empty-string
//     graph identity.
//   - Optional (common): ProjectID, Location, DisplayName, State,
//     AssetTypeFamily, CorrelationAnchors — fields the node projector reads.
//     Each is a pointer/slice/omitempty so an absent value stays distinct from
//     an observed zero. The emitter always writes ProjectID and Location (they
//     may be empty for a resource with no derivable project or location), so
//     an empty string is a valid observed value; only an absent key would
//     dead-letter, and neither is required because the reducer's own identity
//     gate does not require them.
//   - Optional (pass-through): Attributes carries every top-level payload key
//     with no named struct field above, UNTYPED, with JSON type fidelity
//     preserved through the round trip — this includes the collector's own
//     nested "attributes" object (the bounded typed-depth extraction map GCP's
//     1.1.0 schema bump added) plus boundary/control-plane metadata
//     (collector_instance_id, parent_scope_kind, ancestry, labels, extension,
//     ...). A service-specific consumer reads a nested attribute through the
//     decoded struct with the reducer's payloadAttributes(resource.Attributes)
//     helper (which returns resource.Attributes["attributes"] as a map) —
//     never env.Payload["attributes"][key] — mirroring awsv1.Resource.
//
// Typing per-asset-type attributes is deferred follow-up work (design §7,
// remaining families), matching the AWS per-resource-type deferral; it is a
// distinct, larger increment, not a gap in this struct's identity-accuracy
// goal, which is complete and uniform across every gcp_cloud_resource
// consumer.
//
// GCPCloudResourceSchemaVersion (go/internal/facts.gcp.go) is pinned at 1.1.0,
// one minor ahead of every other GCP fact kind in this family, because 1.1.0
// added the bounded typed-depth Attributes contract (mirroring the AWS
// resource attribute contract) as an additive, backward-compatible bump; this
// struct is that latest 1.1.0 shape.
type Resource struct {
	// FullResourceName is the globally-unique Cloud Asset Inventory resource
	// identity. Required — it anchors the CloudResource node and is the join
	// key the relationship edge projection resolves both endpoints against.
	FullResourceName string `json:"full_resource_name"`

	// AssetType is the CAI asset type (for example
	// "compute.googleapis.com/Instance"). Required — the node row's resource
	// type classification.
	AssetType string `json:"asset_type"`

	// ProjectID is the GCP project the resource belongs to, derived from
	// FullResourceName. Optional: always emitted by the collector but may be
	// empty for a resource whose full resource name carries no derivable
	// project segment.
	ProjectID *string `json:"project_id,omitempty"`

	// Location is the resource's region/zone/location string. Optional:
	// always emitted but may be empty for a global resource.
	Location *string `json:"location,omitempty"`

	// DisplayName is the provider-reported display name. Optional: absent when
	// the collector could not observe one.
	DisplayName *string `json:"display_name,omitempty"`

	// State is the provider-reported lifecycle state. Optional: a common field
	// the node projector copies onto the node; an absent value is a valid
	// unobserved state.
	State *string `json:"state,omitempty"`

	// AssetTypeFamily is the collector boundary's asset-type-family token (for
	// example "compute"). Optional: the node projector maps it onto
	// service_kind.
	AssetTypeFamily *string `json:"asset_type_family,omitempty"`

	// CorrelationAnchors are the redaction-safe name/URI anchors the collector
	// published for name-only join resolution. Optional: GCP relationship
	// edges resolve on FullResourceName today, so this is carried for parity
	// with the shared CloudResource node writer's SET clause rather than an
	// active join input.
	CorrelationAnchors []string `json:"correlation_anchors,omitempty"`

	// Attributes carries every top-level payload key with no named struct
	// field above, untyped and with JSON type fidelity preserved by the custom
	// UnmarshalJSON. This includes the nested "attributes" bounded typed-depth
	// map, "extension" control-plane metadata, ancestry, labels, and every
	// collector boundary key not named above. Optional: a payload with no
	// unmodeled top-level keys leaves it nil.
	Attributes map[string]any `json:"-"`
}

// resourceKnownKeys is the set of payload keys the named Resource fields
// cover. UnmarshalJSON removes them from the raw payload so Attributes
// captures only the remainder, and MarshalJSON re-emits the named fields while
// flattening Attributes back to top level. Mirrors awsv1.Resource's
// resourceKnownKeys.
var resourceKnownKeys = map[string]struct{}{
	"full_resource_name":  {},
	"asset_type":          {},
	"project_id":          {},
	"location":            {},
	"display_name":        {},
	"state":               {},
	"asset_type_family":   {},
	"correlation_anchors": {},
}

// resourceAlias is Resource without the custom JSON methods, so UnmarshalJSON
// and MarshalJSON can decode/encode the named fields with the standard
// library without recursing into themselves.
type resourceAlias Resource

// UnmarshalJSON decodes the named identity/common fields normally and captures
// every remaining top-level payload key into Attributes with its JSON-native
// Go type preserved (float64 for numbers, bool, string, []any, map[string]any).
// Mirrors awsv1.Resource.UnmarshalJSON.
func (r *Resource) UnmarshalJSON(data []byte) error {
	var named resourceAlias
	if err := json.Unmarshal(data, &named); err != nil {
		return err
	}
	*r = Resource(named)

	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	for key := range resourceKnownKeys {
		delete(raw, key)
	}
	if len(raw) > 0 {
		r.Attributes = raw
	}
	return nil
}

// MarshalJSON emits the named fields (honoring their omitempty rules) and
// flattens Attributes back to top-level keys, so an encoded Resource produces
// the same flat payload shape the collector emits. A key present in
// Attributes never overrides a named field. Mirrors
// awsv1.Resource.MarshalJSON.
func (r Resource) MarshalJSON() ([]byte, error) {
	named, err := json.Marshal(resourceAlias(r))
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
		if _, isKnown := resourceKnownKeys[key]; isKnown {
			continue
		}
		if _, alreadyNamed := merged[key]; alreadyNamed {
			continue
		}
		merged[key] = value
	}
	return json.Marshal(merged)
}
