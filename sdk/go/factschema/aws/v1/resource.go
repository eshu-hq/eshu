// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package v1

import "encoding/json"

// Resource is the schema-version-1 typed payload for the "aws_resource" fact
// kind (Contract System v1 §3.1,
// docs/internal/design/contract-system-v1.md).
//
// aws_resource is a POLYMORPHIC generic envelope: one fact kind carries every
// AWS resource type (S3 buckets, IAM roles, EC2 instances, RDS instances, …),
// and each resource type contributes its own service-specific payload fields.
// A single typed struct therefore cannot fully type it without either the
// optional-everything anti-pattern (design §3.3 rejects it) or a per-resource-
// type redesign (out of scope for this issue). The struct instead types and
// validates the shared IDENTITY contract and the common fields multiple
// consumers read, and passes service-specific fields through untyped in
// Attributes:
//
//   - Required (identity): AccountID, ResourceID, Region, ResourceType. The
//     decode seam rejects a payload missing any of them with a classified
//     input_invalid error naming the field, matching the collector emitter
//     (awscloud.NewResourceEnvelope), which validates them non-empty. This is
//     the accuracy fix — the identity fields are the ones whose silent absence
//     produced empty-string graph uids.
//   - Optional (common): ARN, Name, Tags, CorrelationAnchors, State,
//     ServiceKind — fields the node projector and more than one consumer read.
//     Each is a pointer/slice/map/omitempty so an absent value stays distinct
//     from an observed zero.
//   - Optional (pass-through): Attributes carries every top-level payload key
//     with no named struct field above, UNTYPED, with JSON type fidelity
//     preserved through the round trip. The collector emitter
//     (awscloud.NewResourceEnvelope) does NOT flatten service-specific fields to
//     the top level: it nests them one level deep under a single "attributes"
//     object (payload["attributes"] = {"engine": …, "role_arns": …}). So the
//     service-specific fields land at Attributes["attributes"], not directly on
//     Attributes. A service-specific consumer reads them through the decoded
//     struct with the reducer's payloadAttributes(resource.Attributes) helper
//     (which returns resource.Attributes["attributes"] as a map), e.g.
//     payloadAttributes(resource.Attributes)["engine"] — never
//     env.Payload["attributes"]["engine"] — so the "no raw payload key access"
//     contract holds while these fields are honestly not yet a typed contract.
//     (Attributes also captures the emitter's "collector_instance_id" boundary
//     key, which has no named field; it is boundary metadata, not a service
//     attribute, and no graph consumer reads it from here.)
//
// Typing service-specific attributes per resource_type is deferred, tracked
// separately (design §7, remaining families / per-type modeling); it is a
// distinct, larger increment, not a gap in this issue's identity-accuracy goal,
// which is complete and uniform across every aws_resource consumer.
//
// The generated JSON Schema at
// sdk/go/factschema/schema/aws_resource.v1.schema.json mirrors this: its
// "required" array lists only the four identity fields, and Attributes is an
// open object (additionalProperties) so the schema stays honest about the
// untyped pass-through.
type Resource struct {
	// AccountID is the cloud account or project the resource belongs to.
	// Required.
	AccountID string `json:"account_id"`

	// ResourceID is the provider-assigned unique identifier for the
	// resource (for example an ARN). Required.
	ResourceID string `json:"resource_id"`

	// Region is the cloud region the resource is provisioned in. Required.
	Region string `json:"region"`

	// ResourceType is the provider-defined resource type (for example
	// "aws_s3_bucket"). Required.
	ResourceType string `json:"resource_type"`

	// ARN is the resource's Amazon Resource Name, when the provider exposes
	// one. Optional: the collector always emits the key but it may be the
	// empty string (some AWS resources are identified only by a bare id), and
	// the reducer join index falls back to ResourceID when ARN is empty.
	ARN *string `json:"arn,omitempty"`

	// Name is the human-assigned display name for the resource, when the
	// provider exposes one. Optional: absent when the collector could not
	// observe a name.
	Name *string `json:"name,omitempty"`

	// State is the provider-reported lifecycle state of the resource.
	// Optional: a common field the node projector copies onto the CloudResource
	// node; an absent value is a valid unobserved state.
	State *string `json:"state,omitempty"`

	// ServiceKind is the collector service-kind boundary token (for example
	// the AWS service that produced the resource). Optional: a common field the
	// node projector copies onto the node.
	ServiceKind *string `json:"service_kind,omitempty"`

	// CorrelationAnchors are the redaction-safe name/URI anchors the collector
	// published for name-only join resolution (for example an s3:// URI or a
	// bare resource name). Optional: the reducer uses them only as a fallback
	// when ARN and ResourceID do not resolve a target, so an absent or empty
	// list is a valid, common state.
	CorrelationAnchors []string `json:"correlation_anchors,omitempty"`

	// Tags holds provider tags observed on the resource. Optional, and a
	// pointer so the two "empty" states stay distinct across a round trip:
	// a nil pointer means the collector did not observe tags (the field is
	// omitted from the payload), while a non-nil pointer to an empty map
	// means the collector observed the resource and found zero tags (the
	// field marshals as "tags":{} and round-trips back to a non-nil empty
	// map). A populated map round-trips as observed with tags. A plain
	// map with omitempty could not express "observed empty" because an
	// empty map would be omitted and decode back as nil.
	Tags *map[string]string `json:"tags,omitempty"`

	// Attributes carries every top-level payload key with no named struct field
	// above, untyped and with JSON type fidelity preserved by the custom
	// UnmarshalJSON. In practice the collector emitter's only such keys are the
	// nested "attributes" object (which itself holds the service-specific fields
	// such as an RDS engine, an instance-profile's role_arns, or a workload's
	// environment) and the "collector_instance_id" boundary token. Because the
	// service fields are nested, a consumer reaches a service field at
	// Attributes["attributes"][key], not Attributes[key]; the reducer's
	// payloadAttributes(resource.Attributes) helper returns that nested map. A
	// value that decodes to a float64 / bool / []any / map[string]any from the
	// raw payload decodes to the same Go type here. Optional: a payload with no
	// unmodeled top-level keys leaves it nil.
	Attributes map[string]any `json:"-"`
}

// resourceKnownKeys is the set of payload keys the named Resource fields cover.
// UnmarshalJSON removes them from the raw payload so Attributes captures only
// the service-specific remainder, and MarshalJSON re-emits the named fields
// while flattening Attributes back to top level. Keeping this list beside the
// struct means adding a named field is a one-line change in two places (the
// field tag and this set), both local to this file.
var resourceKnownKeys = map[string]struct{}{
	"account_id":          {},
	"resource_id":         {},
	"region":              {},
	"resource_type":       {},
	"arn":                 {},
	"name":                {},
	"state":               {},
	"service_kind":        {},
	"correlation_anchors": {},
	"tags":                {},
}

// resourceAlias is Resource without the custom JSON methods, so UnmarshalJSON
// and MarshalJSON can decode/encode the named fields with the standard library
// without recursing into themselves.
type resourceAlias Resource

// UnmarshalJSON decodes the named identity/common fields normally and captures
// every remaining top-level payload key into Attributes with its JSON-native Go
// type preserved (float64 for numbers, bool, string, []any, map[string]any).
// This is what lets a service-specific consumer read
// resource.Attributes["engine"] with the same value and Go type the raw payload
// carried, so migrating a consumer off env.Payload["engine"] is byte-identical.
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
// the same flat payload shape the collector emits and decode round-trips. A key
// present in Attributes never overrides a named field: named fields win, and
// resourceKnownKeys entries are dropped from the Attributes copy before merging.
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
