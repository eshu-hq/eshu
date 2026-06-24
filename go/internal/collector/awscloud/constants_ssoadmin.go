// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

const (
	// ServiceSSOAdmin identifies the AWS IAM Identity Center (sso-admin plus
	// identitystore) metadata scan slice. Identity Center is org-scoped and runs
	// against the management or a delegated-administrator account.
	ServiceSSOAdmin = "ssoadmin"
)

const (
	// ResourceTypeSSOAdminInstance identifies an IAM Identity Center instance.
	ResourceTypeSSOAdminInstance = "aws_identitycenter_instance"
	// ResourceTypeSSOAdminPermissionSet identifies an IAM Identity Center
	// permission set. Permission set inline policy bodies and customer-managed
	// policy bodies are never part of this resource.
	ResourceTypeSSOAdminPermissionSet = "aws_identitycenter_permission_set"
	// ResourceTypeSSOAdminAccountAssignment identifies an IAM Identity Center
	// account assignment binding a principal and permission set to an account.
	ResourceTypeSSOAdminAccountAssignment = "aws_identitycenter_account_assignment"
	// ResourceTypeSSOAdminApplication identifies an IAM Identity Center
	// application instance. Application access-scope attributes that can carry
	// sensitive group filters are never part of this resource.
	ResourceTypeSSOAdminApplication = "aws_identitycenter_application"
	// ResourceTypeSSOAdminTrustedTokenIssuer identifies an IAM Identity Center
	// trusted token issuer configuration.
	ResourceTypeSSOAdminTrustedTokenIssuer = "aws_identitycenter_trusted_token_issuer"
	// ResourceTypeSSOAdminPrincipal identifies an IAM Identity Center principal
	// (group or user) resolved from the connected identity store. The display
	// name is redacted before persistence.
	ResourceTypeSSOAdminPrincipal = "aws_identitycenter_principal"
)

const (
	// RelationshipSSOAdminPermissionSetInInstance records that a permission set
	// belongs to an Identity Center instance.
	RelationshipSSOAdminPermissionSetInInstance = "identitycenter_permission_set_in_instance"
	// RelationshipSSOAdminApplicationInInstance records that an application
	// instance belongs to an Identity Center instance.
	RelationshipSSOAdminApplicationInInstance = "identitycenter_application_in_instance"
	// RelationshipSSOAdminAssignmentUsesPermissionSet records that an account
	// assignment grants a permission set.
	RelationshipSSOAdminAssignmentUsesPermissionSet = "identitycenter_assignment_uses_permission_set"
	// RelationshipSSOAdminAssignmentTargetsAccount records that an account
	// assignment applies to a target AWS account.
	RelationshipSSOAdminAssignmentTargetsAccount = "identitycenter_assignment_targets_account"
	// RelationshipSSOAdminAssignmentGrantsPrincipal records that an account
	// assignment grants access to a group or user principal.
	RelationshipSSOAdminAssignmentGrantsPrincipal = "identitycenter_assignment_grants_principal"
	// RelationshipSSOAdminPermissionSetUsesManagedPolicy records an AWS managed
	// policy attached to a permission set by ARN reference.
	RelationshipSSOAdminPermissionSetUsesManagedPolicy = "identitycenter_permission_set_uses_managed_policy"
	// RelationshipSSOAdminPermissionSetUsesCustomerManagedPolicy records a
	// customer-managed policy reference attached to a permission set by name and
	// path only. The IAM policy body is never read or persisted here.
	RelationshipSSOAdminPermissionSetUsesCustomerManagedPolicy = "identitycenter_permission_set_uses_customer_managed_policy"
)
