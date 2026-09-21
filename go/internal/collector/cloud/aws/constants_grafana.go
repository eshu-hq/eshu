// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

const (
	// ServiceGrafana identifies the regional Amazon Managed Grafana metadata-only
	// scan slice. The scanner reads workspace control-plane metadata through the
	// Managed Grafana management APIs (ListWorkspaces, DescribeWorkspace,
	// ListTagsForResource) and never reads dashboards, panels, alert rules, query
	// results, or any data-plane payload, never reads or persists SAML/IAM
	// Identity Center authentication secrets or workspace API keys, and never
	// mutates Grafana state.
	ServiceGrafana = "grafana"
)

const (
	// ResourceTypeGrafanaWorkspace identifies an Amazon Managed Grafana workspace
	// metadata resource. The scanner emits identity, status, the Grafana version,
	// account-access and permission type, configured data-source enums, and
	// authentication provider names only. Dashboards, alert rules, query results,
	// and authentication secrets stay outside the contract.
	ResourceTypeGrafanaWorkspace = "aws_grafana_workspace"
)

const (
	// RelationshipGrafanaWorkspaceUsesIAMRole records a Managed Grafana
	// workspace's reported workspace IAM role dependency. The target is keyed by
	// the role ARN so the edge joins the role node the IAM scanner publishes.
	RelationshipGrafanaWorkspaceUsesIAMRole = "grafana_workspace_uses_iam_role"
	// RelationshipGrafanaWorkspaceInSubnet records a Managed Grafana workspace's
	// attachment to a VPC subnet from its vpcConfiguration. The target is keyed by
	// the bare subnet id (subnet-...) so the edge joins the subnet node the EC2
	// scanner publishes.
	RelationshipGrafanaWorkspaceInSubnet = "grafana_workspace_in_subnet"
	// RelationshipGrafanaWorkspaceUsesSecurityGroup records a Managed Grafana
	// workspace's attachment to a VPC security group from its vpcConfiguration.
	// The target is keyed by the bare security-group id (sg-...) so the edge joins
	// the security-group node the EC2 scanner publishes.
	RelationshipGrafanaWorkspaceUsesSecurityGroup = "grafana_workspace_uses_security_group"
)
