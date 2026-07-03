// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package v1

// ResourcePolicyPermission is the schema-version-1 typed payload for the
// "aws_resource_policy_permission" fact kind: one normalized, metadata-only
// statement from a resource-based policy (an S3 bucket policy or a KMS key
// policy).
//
// The required set matches the collector emitter
// (awscloud.NewResourcePolicyPermissionEnvelope), which validates resource_arn,
// resource_type, and effect non-empty and always emits account_id, region, and
// policy_source from the boundary. The list fields are always emitted as
// non-nil sorted slices but are semantically optional. IsPublic is an optional
// derived flag the reducer reads to skip public-grant statements.
//
// The struct never carries the raw policy JSON body or any condition value; it
// is the resource-side analog of Permission.
type ResourcePolicyPermission struct {
	// AccountID is the AWS account the statement was observed in. Required.
	AccountID string `json:"account_id"`

	// Region is the AWS region the statement was observed in. Required.
	Region string `json:"region"`

	// ResourceARN is the ARN of the resource the resource-based policy is
	// attached to. Required — it is the CAN_PERFORM edge target identity.
	ResourceARN string `json:"resource_arn"`

	// ResourceType is the attached resource's type token (for example
	// aws_s3_bucket or aws_kms_key). Required — target resolution requires the
	// matched node be this type.
	ResourceType string `json:"resource_type"`

	// Effect is the normalized statement effect ("Allow" or "Deny"). Required.
	Effect string `json:"effect"`

	// PolicySource classifies the statement source. Optional: the emitter
	// always writes the resource-policy source token, but the reducer does not
	// key on it for this kind, so it is not required.
	PolicySource *string `json:"policy_source,omitempty"`

	// Actions is the normalized, lowercased, sorted set of IAM actions the
	// statement lists. Optional; decodes to nil when absent.
	Actions []string `json:"actions,omitempty"`

	// NotActions is the normalized NotAction set. Optional; a non-empty set
	// makes the statement non-trustable for conservative grant evaluation.
	NotActions []string `json:"not_actions,omitempty"`

	// Resources is the normalized, sorted set of resource ARN patterns the
	// statement applies to. Optional; used to confirm the statement applies to
	// its attached resource.
	Resources []string `json:"resources,omitempty"`

	// NotResources is the normalized NotResource set. Optional; a non-empty set
	// makes the statement non-trustable.
	NotResources []string `json:"not_resources,omitempty"`

	// PrincipalARNs is the normalized set of grantee principal ARNs the
	// statement grants to. Optional; the reducer resolves each to a scanned
	// role/user node.
	PrincipalARNs []string `json:"principal_arns,omitempty"`

	// IsPublic is the collector-derived flag marking a statement that grants to
	// the public/anonymous principal. Optional pointer so nil (unreported)
	// stays distinct from an observed false; the reducer skips a public grant.
	IsPublic *bool `json:"is_public,omitempty"`

	// HasConditions is the collector-derived flag marking a statement that
	// carries condition keys. Optional pointer; nil or false means
	// unconditioned.
	HasConditions *bool `json:"has_conditions,omitempty"`
}
