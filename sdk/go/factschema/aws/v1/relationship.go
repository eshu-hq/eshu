// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package v1

import "encoding/json"

// Relationship is the schema-version-1 typed payload for the "aws_relationship"
// fact kind (Contract System v1 §3.1,
// docs/internal/design/contract-system-v1.md).
//
// Like aws_resource, aws_relationship is a POLYMORPHIC envelope: one fact kind
// carries every AWS relationship verb, and verb-specific relationships (for
// example a cloudwatch_alarm_observes_metric fact's dimension summary) contribute
// their own nested payload under an "attributes" object. The struct types and
// validates the shared IDENTITY contract and passes verb-specific fields through
// untyped in Attributes, exactly like awsv1.Resource:
//
//   - Required (identity): AccountID, Region, RelationshipType,
//     SourceResourceID, TargetResourceID — matching the collector emitter
//     (awscloud.NewRelationshipEnvelope), which validates them non-empty
//     (source_resource_id defaults to source_arn, target_resource_id to
//     target_arn). A missing identity field dead-letters as input_invalid.
//   - Optional (common): SourceARN, TargetARN, TargetType — always emitted but
//     may be empty; the reducer resolves an endpoint through the ARN index
//     first and the resource-id index second, and substitutes "unknown" for an
//     empty TargetType.
//   - Optional (pass-through): Attributes carries every top-level payload key
//     with no named field above, UNTYPED with JSON type fidelity preserved. As
//     with aws_resource, the emitter nests the verb-specific fields one level
//     deep under a single "attributes" object rather than flattening them, so
//     those fields land at Attributes["attributes"], not directly on Attributes;
//     a consumer reaches them via the reducer's
//     payloadAttributes(relationship.Attributes) helper through the decoded
//     struct, never env.Payload["attributes"][...]. (Attributes also captures the
//     emitter's "collector_instance_id" boundary key, which no graph consumer
//     reads from here.)
//
// Typing verb-specific relationship attributes per relationship_type is deferred
// to the same follow-up as aws_resource service attributes (design §7, one
// deferred-boundary issue covers both polymorphic AWS envelopes); it is a
// distinct increment, not a gap in this issue's identity-accuracy goal.
type Relationship struct {
	// AccountID is the AWS account the relationship was observed in. Required.
	AccountID string `json:"account_id"`

	// Region is the AWS region the relationship was observed in. Required.
	Region string `json:"region"`

	// RelationshipType is the normalized AWS relationship verb (for example
	// USES_KMS_KEY). Required — a relationship with no type carries no edge
	// truth.
	RelationshipType string `json:"relationship_type"`

	// SourceResourceID is the relationship source endpoint identity (an ARN or
	// a bare AWS id). Required: the emitter defaults it to source_arn when the
	// bare id is absent, so one identity is always present.
	SourceResourceID string `json:"source_resource_id"`

	// TargetResourceID is the relationship target endpoint identity (an ARN or
	// a bare AWS id). Required: the emitter defaults it to target_arn when the
	// bare id is absent, so one identity is always present.
	TargetResourceID string `json:"target_resource_id"`

	// SourceARN is the source endpoint ARN, when the source is ARN-identified.
	// Optional: the emitter always writes the key but it may be empty for a
	// bare-id source, and the reducer resolves the source through the ARN index
	// first and the resource-id index second.
	SourceARN *string `json:"source_arn,omitempty"`

	// TargetARN is the target endpoint ARN, when the target is ARN-identified.
	// Optional: the emitter always writes the key but it may be empty for a
	// bare-id target, and the reducer resolves the target through the ARN index
	// first and the bare-id / correlation-anchor index second.
	TargetARN *string `json:"target_arn,omitempty"`

	// TargetType is the target resource's type token, used for the completion
	// log's unresolved-by-type breakdown. Optional: the reducer substitutes
	// "unknown" when it is empty, so an absent value never blocks an edge.
	TargetType *string `json:"target_type,omitempty"`

	// Attributes carries every top-level payload key with no named struct field
	// above, untyped, mirroring awsv1.Resource.Attributes. In practice the
	// emitter's only such keys are the nested "attributes" object (which holds
	// the verb-specific fields, e.g. a cloudwatch_alarm_observes_metric fact's
	// dimension summary) and the "collector_instance_id" boundary token. Because
	// the verb-specific fields are nested, a consumer reaches one at
	// Attributes["attributes"][key], not Attributes[key]; the reducer's
	// payloadAttributes(relationship.Attributes) helper returns that nested map.
	// JSON type fidelity is preserved by the custom UnmarshalJSON. Optional: a
	// payload with no unmodeled top-level keys leaves it nil.
	Attributes map[string]any `json:"-"`
}

// relationshipKnownKeys is the set of payload keys the named Relationship fields
// cover. UnmarshalJSON removes them from the raw payload so Attributes captures
// only the verb-specific remainder, and MarshalJSON re-emits the named fields
// while flattening Attributes back to top level. It mirrors resourceKnownKeys.
var relationshipKnownKeys = map[string]struct{}{
	"account_id":         {},
	"region":             {},
	"relationship_type":  {},
	"source_resource_id": {},
	"target_resource_id": {},
	"source_arn":         {},
	"target_arn":         {},
	"target_type":        {},
}

// relationshipAlias is Relationship without the custom JSON methods, so
// UnmarshalJSON and MarshalJSON can decode/encode the named fields with the
// standard library without recursing into themselves.
type relationshipAlias Relationship

// UnmarshalJSON decodes the named identity/common fields normally and captures
// every remaining top-level payload key into Attributes with its JSON-native Go
// type preserved. It mirrors Resource.UnmarshalJSON so a verb-specific consumer
// that reads relationship.Attributes[...] gets the same value and Go type the
// raw env.Payload lookup produced today.
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
// over any same-named Attributes key. It mirrors Resource.MarshalJSON.
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
