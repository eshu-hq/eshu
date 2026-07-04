// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package v1

import "encoding/json"

// CloudRelationship is the schema-version-1 typed payload for the
// "azure_cloud_relationship" fact kind (Contract System v1 §3.1,
// docs/internal/design/contract-system-v1.md).
//
// Like CloudResource, azure_cloud_relationship is a POLYMORPHIC envelope: one
// fact kind carries every Azure relationship verb the collector observes
// (today only "managed_by" is reducer-supported; the collector may still
// observe others as provenance). The struct types and validates the shared
// IDENTITY contract and passes verb-specific fields through untyped in
// Attributes, exactly like CloudResource:
//
//   - Required (identity): RelationshipType, SourceARMResourceID,
//     TargetARMResourceID — matching the collector emitter
//     (azurecloud.NewRelationshipEnvelope), which validates all three
//     non-empty. A missing identity field dead-letters as input_invalid.
//   - Optional (common): SourceNormalizedResourceID,
//     TargetNormalizedResourceID, TargetResourceType, SupportState — always
//     emitted by the collector but the reducer join index tolerates absence,
//     preferring the normalized id and falling back to the raw ARM id.
//   - Optional (pass-through): Attributes carries every top-level payload key
//     with no named field above, UNTYPED with JSON type fidelity preserved.
//     Unlike the aws family, the Azure emitter writes remaining fields FLAT
//     at the top level (source_subscription_id, source_resource_group,
//     source_provider_namespace, source_resource_type,
//     target_subscription_id, provider_time, ...), so a consumer reads them
//     directly at Attributes[key].
type CloudRelationship struct {
	// RelationshipType is the provider relationship verb (for example
	// "managed_by"). Required — a relationship with no type carries no edge
	// truth.
	RelationshipType string `json:"relationship_type"`

	// SourceARMResourceID is the raw ARM identity of the relationship's
	// owning resource. Required.
	SourceARMResourceID string `json:"source_arm_resource_id"`

	// TargetARMResourceID is the raw ARM identity of the relationship's
	// related resource. Required. It may reference a resource outside the
	// current scope; the reducer resolves it only when it also appears among
	// the generation's materialized CloudResource facts.
	TargetARMResourceID string `json:"target_arm_resource_id"`

	// SourceNormalizedResourceID is the normalized form of
	// SourceARMResourceID. Optional: the reducer prefers this for join
	// resolution when present and falls back to SourceARMResourceID
	// otherwise.
	SourceNormalizedResourceID *string `json:"source_normalized_resource_id,omitempty"`

	// TargetNormalizedResourceID is the normalized form of
	// TargetARMResourceID. Optional: the reducer prefers this for join
	// resolution when present and falls back to TargetARMResourceID
	// otherwise.
	TargetNormalizedResourceID *string `json:"target_normalized_resource_id,omitempty"`

	// TargetResourceType is the target's ARM resource type token, used for
	// the completion log's unresolved-by-type breakdown. Optional: the
	// reducer substitutes "unknown" when it is empty, so an absent value
	// never blocks an edge.
	TargetResourceType *string `json:"target_resource_type,omitempty"`

	// SupportState classifies how completely the provider described the
	// relationship ("supported", "partial", or "unsupported"). Optional: the
	// reducer treats an absent or empty value as "supported" (the collector
	// emitter's own default), a "partial" or "unsupported" value skips edge
	// materialization without dead-lettering, and any other value is treated
	// as unknown and skipped.
	SupportState *string `json:"support_state,omitempty"`

	// Attributes carries every top-level payload key with no named struct
	// field above, untyped, mirroring CloudResource.Attributes. In practice
	// this includes collector_kind, collector_instance_id, tenant_id,
	// scope_kind, provider_scope_id, source_lane, source_subscription_id,
	// source_resource_group, source_provider_namespace,
	// source_resource_type, target_subscription_id, provider_time, and
	// redaction_policy_version. Optional: a payload with no unmodeled
	// top-level keys leaves it nil.
	Attributes map[string]any `json:"-"`
}

// cloudRelationshipKnownKeys is the set of payload keys the named
// CloudRelationship fields cover. UnmarshalJSON removes them from the raw
// payload so Attributes captures only the remainder, and MarshalJSON
// re-emits the named fields while flattening Attributes back to top level.
var cloudRelationshipKnownKeys = map[string]struct{}{
	"relationship_type":             {},
	"source_arm_resource_id":        {},
	"target_arm_resource_id":        {},
	"source_normalized_resource_id": {},
	"target_normalized_resource_id": {},
	"target_resource_type":          {},
	"support_state":                 {},
}

// cloudRelationshipAlias is CloudRelationship without the custom JSON
// methods, so UnmarshalJSON and MarshalJSON can decode/encode the named
// fields with the standard library without recursing into themselves.
type cloudRelationshipAlias CloudRelationship

// UnmarshalJSON decodes the named identity/common fields normally and
// captures every remaining top-level payload key into Attributes with its
// JSON-native Go type preserved. It mirrors CloudResource.UnmarshalJSON.
func (r *CloudRelationship) UnmarshalJSON(data []byte) error {
	var named cloudRelationshipAlias
	if err := json.Unmarshal(data, &named); err != nil {
		return err
	}
	*r = CloudRelationship(named)

	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	for key := range cloudRelationshipKnownKeys {
		delete(raw, key)
	}
	if len(raw) > 0 {
		r.Attributes = raw
	}
	return nil
}

// MarshalJSON emits the named fields (honoring their omitempty rules) and
// flattens Attributes back to top-level keys, so an encoded CloudRelationship
// produces the same flat payload shape the collector emits. Named fields win
// over any same-named Attributes key. It mirrors
// CloudResource.MarshalJSON.
func (r CloudRelationship) MarshalJSON() ([]byte, error) {
	named, err := json.Marshal(cloudRelationshipAlias(r))
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
		if _, isKnown := cloudRelationshipKnownKeys[key]; isKnown {
			continue
		}
		if _, alreadyNamed := merged[key]; alreadyNamed {
			continue
		}
		merged[key] = value
	}
	return json.Marshal(merged)
}
