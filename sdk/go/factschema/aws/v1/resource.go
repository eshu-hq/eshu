// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package v1

// Resource is the schema-version-1 typed payload for the "aws.resource" fact
// kind, the sample family the factschema scaffold demonstrates end to end
// (Contract System v1 §3.1,
// docs/internal/design/contract-system-v1.md).
//
// Field mutability encodes the required/optional contract the schema
// generator and the decode seam both enforce: a field is REQUIRED when it is
// a non-pointer type with no `omitempty` tag, and OPTIONAL when it is a
// pointer type or carries `omitempty`. AccountID, ResourceID, Region, and
// ResourceType are required — decodeAndValidate rejects a payload missing
// any of them with a classified error naming the field. Name and Tags are
// optional: a payload may omit them entirely, and the decoded struct leaves
// them nil rather than defaulting to a zero value that would be
// indistinguishable from an observed empty value.
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
	// "aws.s3.bucket"). Required.
	ResourceType string `json:"resource_type"`

	// Name is the human-assigned display name for the resource, when the
	// provider exposes one. Optional: absent when the collector could not
	// observe a name.
	Name *string `json:"name,omitempty"`

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
