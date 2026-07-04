// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package v1

import "encoding/json"

// Relationship is the schema-version-1 typed payload for the
// "gcp_cloud_relationship" fact kind (Contract System v1 §3.1,
// docs/internal/design/contract-system-v1.md).
//
// Like Resource, gcp_cloud_relationship is a POLYMORPHIC envelope mirroring
// awsv1.Relationship: one fact kind carries every GCP relationship type, and
// the struct types and validates the shared identity contract and passes any
// remaining, unmodeled fields through untyped in Attributes:
//
//   - Required (identity): SourceFullResourceName, TargetFullResourceName,
//     RelationshipType — matching the collector emitter
//     (gcpcloud.NewCloudRelationshipEnvelope), which fails closed on any of
//     the three being empty. A missing identity field dead-letters as
//     input_invalid instead of the edge projection silently treating an
//     absent endpoint as an empty-string join key.
//   - Optional (common): SourceAssetType, TargetAssetType, SupportState —
//     always emitted by the collector but read by the reducer's completion
//     log / tally classification; each is a pointer so the reducer's
//     substitution of "unknown" / "supported" defaults for an absent value is
//     visibly distinct from an observed value.
//   - Optional (pass-through): Attributes carries every remaining top-level
//     payload key untyped, with JSON type fidelity preserved, mirroring
//     awsv1.Relationship.Attributes. In practice this is boundary/control-plane
//     metadata (collector_instance_id, parent_scope_kind, project ids,
//     redaction_policy_version, read/update time) rather than a nested
//     verb-specific object; GCP relationship facts carry no per-verb payload
//     today.
type Relationship struct {
	// SourceFullResourceName is the relationship source endpoint's Cloud Asset
	// Inventory full resource name. Required.
	SourceFullResourceName string `json:"source_full_resource_name"`

	// TargetFullResourceName is the relationship target endpoint's Cloud Asset
	// Inventory full resource name. Required.
	TargetFullResourceName string `json:"target_full_resource_name"`

	// RelationshipType is the normalized provider relationship type (for
	// example "INSTANCE_TO_DISK"). Required — a relationship with no type
	// carries no edge truth.
	RelationshipType string `json:"relationship_type"`

	// SourceAssetType is the source endpoint's CAI asset type. Optional:
	// always emitted but may be empty.
	SourceAssetType *string `json:"source_asset_type,omitempty"`

	// TargetAssetType is the target endpoint's CAI asset type, used for the
	// completion log's unresolved-by-type breakdown. Optional: the reducer
	// substitutes "unknown" when it is empty, so an absent value never blocks
	// an edge.
	TargetAssetType *string `json:"target_asset_type,omitempty"`

	// SupportState classifies how completely the provider described the
	// relationship (supported, partial, or unsupported). Optional: blank
	// normalizes to supported, matching the collector emitter's own default.
	SupportState *string `json:"support_state,omitempty"`

	// Attributes carries every top-level payload key with no named struct
	// field above, untyped, mirroring awsv1.Relationship.Attributes. Optional:
	// a payload with no unmodeled top-level keys leaves it nil.
	Attributes map[string]any `json:"-"`
}

// relationshipKnownKeys is the set of payload keys the named Relationship
// fields cover. UnmarshalJSON removes them from the raw payload so Attributes
// captures only the remainder, and MarshalJSON re-emits the named fields while
// flattening Attributes back to top level. Mirrors
// awsv1.Relationship's relationshipKnownKeys.
var relationshipKnownKeys = map[string]struct{}{
	"source_full_resource_name": {},
	"target_full_resource_name": {},
	"relationship_type":         {},
	"source_asset_type":         {},
	"target_asset_type":         {},
	"support_state":             {},
}

// relationshipAlias is Relationship without the custom JSON methods, so
// UnmarshalJSON and MarshalJSON can decode/encode the named fields with the
// standard library without recursing into themselves.
type relationshipAlias Relationship

// UnmarshalJSON decodes the named identity/common fields normally and captures
// every remaining top-level payload key into Attributes with its JSON-native
// Go type preserved. Mirrors awsv1.Relationship.UnmarshalJSON.
func (r *Relationship) UnmarshalJSON(data []byte) error {
	var named relationshipAlias
	if err := json.Unmarshal(data, &named); err != nil {
		return err
	}
	*r = Relationship(named)

	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	for key := range relationshipKnownKeys {
		delete(raw, key)
	}
	if len(raw) > 0 {
		r.Attributes = raw
	}
	return nil
}

// MarshalJSON emits the named fields (honoring their omitempty rules) and
// flattens Attributes back to top-level keys, so an encoded Relationship
// produces the same flat payload shape the collector emits. Named fields win
// over any same-named Attributes key. Mirrors
// awsv1.Relationship.MarshalJSON.
func (r Relationship) MarshalJSON() ([]byte, error) {
	named, err := json.Marshal(relationshipAlias(r))
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
		if _, isKnown := relationshipKnownKeys[key]; isKnown {
			continue
		}
		if _, alreadyNamed := merged[key]; alreadyNamed {
			continue
		}
		merged[key] = value
	}
	return json.Marshal(merged)
}
