// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package v1 holds the schema-version-1 typed payload structs for the AWS IAM
// fact family (Contract System v1 §3.1,
// docs/internal/design/contract-system-v1.md). It is part of the standalone
// github.com/eshu-hq/eshu/sdk/go/factschema module and imports nothing under
// go/internal.
package v1

// Permission is the schema-version-1 typed payload for the "aws_iam_permission"
// fact kind: one normalized, metadata-only IAM policy statement attached to a
// principal.
//
// The required set matches the collector emitter
// (awscloud.NewIAMPermissionEnvelope), which validates principal_arn, effect,
// and policy_source non-empty and always emits account_id and region from the
// scan boundary. The list fields (Actions, Resources, and the NotAction/
// NotResource/AssumePrincipal sets) are always emitted by the collector as
// non-nil sorted slices but are semantically optional — a statement may grant no
// actions on no resources — so they carry omitempty and decode to nil when
// absent. HasConditions is an optional derived flag. PolicyARN is optional
// because inline policies have no managed-policy ARN.
//
// The struct never carries the raw policy JSON body or any condition value; it
// is the same metadata-only projection the fact kind documents.
type Permission struct {
	// AccountID is the AWS account the statement was observed in. Required.
	AccountID string `json:"account_id"`

	// Region is the AWS region the statement was observed in. Required.
	Region string `json:"region"`

	// PrincipalARN is the ARN of the principal the policy statement is attached
	// to. Required — it anchors every edge the statement can produce.
	PrincipalARN string `json:"principal_arn"`

	// Effect is the normalized statement effect ("Allow" or "Deny"). Required.
	Effect string `json:"effect"`

	// PolicySource classifies where the statement came from (inline,
	// attached_managed, permission_boundary, or trust). Required — the reducer
	// slices behave differently per source.
	PolicySource string `json:"policy_source"`

	// PolicyARN is the managed-policy ARN the statement came from, when it came
	// from a managed policy. Optional: inline statements have no policy ARN.
	PolicyARN *string `json:"policy_arn,omitempty"`

	// Actions is the normalized, lowercased, sorted set of IAM actions the
	// statement lists. Optional: always emitted by the collector but may be
	// empty; decodes to nil when absent.
	Actions []string `json:"actions,omitempty"`

	// NotActions is the normalized NotAction set. Optional; a non-empty set
	// makes the statement non-trustable for conservative grant evaluation.
	NotActions []string `json:"not_actions,omitempty"`

	// Resources is the normalized, sorted set of resource ARN patterns the
	// statement applies to. Optional; used by target resolution.
	Resources []string `json:"resources,omitempty"`

	// NotResources is the normalized NotResource set. Optional; a non-empty set
	// makes the statement non-trustable for conservative grant evaluation.
	NotResources []string `json:"not_resources,omitempty"`

	// AssumePrincipals is the normalized set of principals a trust statement
	// permits to assume the role. Optional; read only for policy_source=trust
	// by the CAN_ASSUME edge slice.
	AssumePrincipals []string `json:"assume_principals,omitempty"`

	// HasConditions is the collector-derived flag marking a statement that
	// carries condition keys. Optional pointer so nil (unreported) stays
	// distinct from an observed false; the grant evaluators treat a nil or
	// false value as unconditioned.
	HasConditions *bool `json:"has_conditions,omitempty"`
}
