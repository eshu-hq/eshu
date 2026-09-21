// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

const (
	// ServiceSecurityHub identifies the regional AWS Security Hub metadata
	// posture scan slice.
	ServiceSecurityHub = "securityhub"
)

const (
	// ResourceTypeSecurityHubHub identifies a Security Hub hub configuration.
	ResourceTypeSecurityHubHub = "aws_securityhub_hub"
	// ResourceTypeSecurityHubStandard identifies an enabled Security Hub
	// standards subscription.
	ResourceTypeSecurityHubStandard = "aws_securityhub_standard"
	// ResourceTypeSecurityHubControl identifies a Security Hub standards
	// control.
	ResourceTypeSecurityHubControl = "aws_securityhub_control"
	// ResourceTypeSecurityHubMemberAccount identifies a Security Hub member
	// account reported by an administrator account.
	ResourceTypeSecurityHubMemberAccount = "aws_securityhub_member_account"
	// ResourceTypeSecurityHubActionTarget identifies a custom Security Hub
	// action target.
	ResourceTypeSecurityHubActionTarget = "aws_securityhub_action_target"
	// ResourceTypeSecurityHubInsight identifies a custom Security Hub insight
	// summary.
	ResourceTypeSecurityHubInsight = "aws_securityhub_insight"
	// ResourceTypeSecurityHubFindingAggregate identifies an aggregate Security
	// Hub finding posture bucket.
	ResourceTypeSecurityHubFindingAggregate = "aws_securityhub_finding_aggregate"
)

const (
	// RelationshipSecurityHubHubHasMember records Security Hub administrator
	// membership evidence.
	RelationshipSecurityHubHubHasMember = "securityhub_hub_has_member"
	// RelationshipSecurityHubStandardHasControl records Security Hub standard
	// control membership.
	RelationshipSecurityHubStandardHasControl = "securityhub_standard_has_control"
	// RelationshipSecurityHubInsightGroupsControl records a custom insight that
	// groups by Security Hub control id.
	RelationshipSecurityHubInsightGroupsControl = "securityhub_insight_groups_control"
)
