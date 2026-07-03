// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package v1

// Relationship is the schema-version-1 typed payload for the "aws_relationship"
// fact kind (Contract System v1 §3.1,
// docs/internal/design/contract-system-v1.md).
//
// The required set matches the AWS relationship collector emitter
// (awscloud.NewRelationshipEnvelope), which validates account_id, region,
// relationship_type, and both endpoint identities (source_resource_id defaults
// to source_arn, target_resource_id defaults to target_arn) non-empty before it
// emits the fact. SourceARN, TargetARN, and TargetType are optional: the emitter
// always writes the keys but they may be empty, and the reducer resolves an
// endpoint through the ARN index first and the resource-id index second, so an
// empty ARN is a valid state.
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
}
