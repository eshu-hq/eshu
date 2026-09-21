// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

const (
	// ServiceVerifiedPermissions identifies the regional Amazon Verified
	// Permissions metadata-only scan slice. The scanner reads policy store,
	// policy, and identity source control-plane metadata through the
	// verifiedpermissions management APIs (ListPolicyStores, GetPolicyStore,
	// ListPolicies, ListIdentitySources) and never reads or persists Cedar
	// policy statement bodies, schema bodies, policy template bodies, or any
	// authorization-request payload, and never mutates Verified Permissions
	// state.
	ServiceVerifiedPermissions = "verifiedpermissions"
)

const (
	// ResourceTypeVerifiedPermissionsPolicyStore identifies an Amazon Verified
	// Permissions policy store metadata resource. The scanner emits identity,
	// the validation mode, deletion-protection and encryption configuration
	// flags, Cedar language version, and lifecycle timestamps only. The Cedar
	// schema body stays outside the contract.
	ResourceTypeVerifiedPermissionsPolicyStore = "aws_verifiedpermissions_policy_store"
	// ResourceTypeVerifiedPermissionsPolicy identifies an Amazon Verified
	// Permissions policy metadata resource. The scanner emits the policy id,
	// policy type (STATIC or TEMPLATE_LINKED), effect, and parent policy store
	// id only. The Cedar policy statement body is never read or persisted.
	ResourceTypeVerifiedPermissionsPolicy = "aws_verifiedpermissions_policy"
	// ResourceTypeVerifiedPermissionsIdentitySource identifies an Amazon
	// Verified Permissions identity source metadata resource. The scanner emits
	// the identity source id, the principal Cedar entity type, the configured
	// provider kind (Cognito user pool or OIDC), and the OIDC issuer URL or
	// Cognito user pool reference only. Application client secrets and token
	// payloads are never read or persisted.
	ResourceTypeVerifiedPermissionsIdentitySource = "aws_verifiedpermissions_identity_source"
)

const (
	// RelationshipVerifiedPermissionsPolicyInStore records a Verified
	// Permissions policy's membership in its parent policy store. The target is
	// keyed by the policy store id the policy store node publishes, so the edge
	// joins the policy store node exactly.
	RelationshipVerifiedPermissionsPolicyInStore = "verifiedpermissions_policy_in_store"
	// RelationshipVerifiedPermissionsIdentitySourceInStore records a Verified
	// Permissions identity source's membership in its parent policy store. The
	// target is keyed by the policy store id the policy store node publishes.
	RelationshipVerifiedPermissionsIdentitySourceInStore = "verifiedpermissions_identity_source_in_store"
	// RelationshipVerifiedPermissionsIdentitySourceUsesCognitoUserPool records a
	// Verified Permissions identity source's dependency on an Amazon Cognito
	// user pool. The target is keyed by the bare Cognito user pool id parsed
	// from the reported user pool ARN, matching the resource_id the Cognito
	// scanner publishes for a user pool node.
	RelationshipVerifiedPermissionsIdentitySourceUsesCognitoUserPool = "verifiedpermissions_identity_source_uses_cognito_user_pool"
)
