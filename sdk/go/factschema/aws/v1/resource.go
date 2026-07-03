// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package v1

// Resource is the schema-version-1 typed payload for the "aws_resource" fact
// kind (Contract System v1 §3.1,
// docs/internal/design/contract-system-v1.md).
//
// Field mutability encodes the required/optional contract the schema
// generator and the decode seam both enforce: a field is REQUIRED when it is
// a non-pointer type with no `omitempty` tag, and OPTIONAL when it is a
// pointer type or carries `omitempty`. AccountID, ResourceID, Region, and
// ResourceType are required — decodeAndValidate rejects a payload missing
// any of them with a classified error naming the field, matching the AWS
// resource collector emitter (awscloud.NewResourceEnvelope), which validates
// account_id, region, resource_type, and resource_id (arn or resource_id;
// resource_id defaults to arn) non-empty before it emits the fact. ARN, Name,
// Tags, and CorrelationAnchors are optional: the emitter always writes arn and
// correlation_anchors but arn may be the empty string and the anchors may be
// empty, and the reducer already tolerates their absence, so requiring them
// would dead-letter valid facts.
//
// The generated JSON Schema at
// sdk/go/factschema/schema/aws_resource.v1.schema.json mirrors this exactly:
// its "required" array lists only AccountID, ResourceID, Region, and
// ResourceType.
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
}
