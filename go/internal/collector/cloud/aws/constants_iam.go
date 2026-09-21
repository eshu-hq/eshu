// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

import awsv1 "github.com/eshu-hq/eshu/sdk/go/factschema/aws/v1"

const (
	// ServiceIAM identifies the global IAM service scan slice.
	ServiceIAM = "iam"
)

const (
	// ResourceTypeIAMRole identifies an IAM role.
	ResourceTypeIAMRole = awsv1.ResourceTypeIAMRole
	// ResourceTypeIAMUser identifies an IAM user.
	ResourceTypeIAMUser = awsv1.ResourceTypeIAMUser
	// ResourceTypeIAMGroup identifies an IAM group.
	ResourceTypeIAMGroup = awsv1.ResourceTypeIAMGroup
	// ResourceTypeIAMPolicy identifies an IAM policy.
	ResourceTypeIAMPolicy = awsv1.ResourceTypeIAMPolicy
	// ResourceTypeIAMInstanceProfile identifies an IAM instance profile.
	ResourceTypeIAMInstanceProfile = awsv1.ResourceTypeIAMInstanceProfile
	// ResourceTypeIAMPrincipal identifies a principal from an IAM trust policy.
	ResourceTypeIAMPrincipal = awsv1.ResourceTypeIAMPrincipal
)

const (
	// IAMPolicySourceInline marks a derived permission from an inline policy
	// embedded on a role, user, or group.
	IAMPolicySourceInline = "inline"
	// IAMPolicySourceAttachedManaged marks a derived permission from an attached
	// managed policy document (customer- or AWS-managed).
	IAMPolicySourceAttachedManaged = "attached_managed"
	// IAMPolicySourceTrust marks a derived permission from a role trust /
	// assume-role policy document.
	IAMPolicySourceTrust = "trust"
	// IAMPolicySourcePermissionBoundary marks a derived permission from a managed
	// policy attached as a permissions boundary. It is a ceiling, not an identity
	// grant source.
	IAMPolicySourcePermissionBoundary = "permission_boundary"
)

const (
	// RelationshipIAMRoleTrustsPrincipal records a role trust-policy principal.
	RelationshipIAMRoleTrustsPrincipal = "iam_role_trusts_principal"
	// RelationshipIAMRoleAttachedPolicy records a managed policy attachment.
	RelationshipIAMRoleAttachedPolicy = "iam_role_attached_policy"
	// RelationshipIAMRoleInInstanceProfile records a role/profile membership.
	RelationshipIAMRoleInInstanceProfile = "iam_role_in_instance_profile"
)
