// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

const (
	// ServiceInspector2 identifies the regional Amazon Inspector v2 metadata
	// scan slice.
	ServiceInspector2 = "inspector2"
)

const (
	// ResourceTypeInspector2Account identifies an Amazon Inspector v2 account
	// status record with its enabled scan features.
	ResourceTypeInspector2Account = "aws_inspector2_account"
	// ResourceTypeInspector2MemberAccount identifies an Amazon Inspector v2
	// member account reported by a delegated administrator account.
	ResourceTypeInspector2MemberAccount = "aws_inspector2_member_account"
	// ResourceTypeInspector2Filter identifies an Amazon Inspector v2 findings
	// filter summary. Only the filter name and non-criteria metadata are
	// persisted; filter criteria expressions are never stored.
	ResourceTypeInspector2Filter = "aws_inspector2_filter"
	// ResourceTypeInspector2CisScanConfiguration identifies an Amazon Inspector
	// v2 CIS scan configuration metadata summary.
	ResourceTypeInspector2CisScanConfiguration = "aws_inspector2_cis_scan_configuration"
)

const (
	// RelationshipInspector2MemberManagedByAdministrator records that an Amazon
	// Inspector v2 member account is managed by a delegated administrator
	// account.
	RelationshipInspector2MemberManagedByAdministrator = "inspector2_member_managed_by_administrator"
	// RelationshipInspector2CisScanConfigurationTargetsAccount records that an
	// Amazon Inspector v2 CIS scan configuration targets a member account.
	RelationshipInspector2CisScanConfigurationTargetsAccount = "inspector2_cis_scan_configuration_targets_account"
)
