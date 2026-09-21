// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

const (
	// ServiceOrganizations identifies the us-east-1 AWS Organizations metadata
	// scan slice for management or delegated-administrator accounts.
	ServiceOrganizations = "organizations"
)

const (
	// ResourceTypeOrganizationsRoot identifies an AWS Organizations root.
	ResourceTypeOrganizationsRoot = "aws_organizations_root"
	// ResourceTypeOrganizationsOrganizationalUnit identifies an AWS
	// Organizations organizational unit.
	ResourceTypeOrganizationsOrganizationalUnit = "aws_organizations_organizational_unit"
	// ResourceTypeOrganizationsAccount identifies an AWS Organizations member
	// account.
	ResourceTypeOrganizationsAccount = "aws_organizations_account"
	// ResourceTypeOrganizationsPolicy identifies an AWS Organizations policy
	// summary. Policy document bodies are not part of this resource.
	ResourceTypeOrganizationsPolicy = "aws_organizations_policy"
	// ResourceTypeOrganizationsDelegatedAdministrator identifies an AWS
	// Organizations delegated administrator binding.
	ResourceTypeOrganizationsDelegatedAdministrator = "aws_organizations_delegated_administrator"
)

const (
	// RelationshipOrganizationsAccountInOU records Organizations account
	// membership in an organizational unit.
	RelationshipOrganizationsAccountInOU = "organizations_account_in_ou"
	// RelationshipOrganizationsOUInOU records Organizations OU child
	// membership in a parent OU.
	RelationshipOrganizationsOUInOU = "organizations_ou_in_ou"
	// RelationshipOrganizationsAccountInRoot records Organizations account
	// membership directly under a root.
	RelationshipOrganizationsAccountInRoot = "organizations_account_in_root"
	// RelationshipOrganizationsPolicyTargetsResource records Organizations
	// policy attachment metadata to a root, OU, or account.
	RelationshipOrganizationsPolicyTargetsResource = "organizations_policy_targets_resource"
	// RelationshipOrganizationsDelegatedAdminForAccount records a delegated
	// administrator binding to its member account.
	RelationshipOrganizationsDelegatedAdminForAccount = "organizations_delegated_admin_for_account"
)
