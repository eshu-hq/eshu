// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

const (
	// ResourcePolicySourceResource marks a derived permission statement whose
	// origin is a resource-based policy (an S3 bucket policy or KMS key policy)
	// attached to the resource it controls, as opposed to an identity policy
	// attached to a principal. It is the resource-side analog of the
	// IAMPolicySource* values and is the only policy_source an
	// aws_resource_policy_permission fact ever carries.
	ResourcePolicySourceResource = "resource"
)

const (
	// ResourcePolicyPrincipalTypeAWS marks an IAM account/ARN grantee in a
	// resource policy Principal element ("AWS"), the type that names an account
	// id and feeds cross-account / public derivation.
	ResourcePolicyPrincipalTypeAWS = "aws"
	// ResourcePolicyPrincipalTypeService marks an AWS service principal grantee
	// (the Principal "Service" element, for example "cloudtrail.amazonaws.com").
	ResourcePolicyPrincipalTypeService = "service"
	// ResourcePolicyPrincipalTypeFederated marks a federated identity-provider
	// grantee (the Principal "Federated" element).
	ResourcePolicyPrincipalTypeFederated = "federated"
	// ResourcePolicyPrincipalTypeCanonical marks a canonical-user grantee (the
	// Principal "CanonicalUser" element).
	ResourcePolicyPrincipalTypeCanonical = "canonical"
)
