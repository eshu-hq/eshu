// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

const (
	// ServiceAMP identifies the regional Amazon Managed Service for Prometheus
	// metadata-only scan slice. The scanner reads workspace, rule-groups
	// namespace, and scraper control-plane metadata through the Prometheus
	// service (aps) list/describe APIs and never reads ingested time-series
	// samples, query results, alert-manager definitions, rule-group definition
	// bodies, or scrape-configuration bodies, and never mutates AMP state.
	ServiceAMP = "amp"
)

const (
	// ResourceTypeAMPWorkspace identifies an Amazon Managed Service for
	// Prometheus workspace metadata resource. The scanner emits identity, alias,
	// status, the optional customer-managed KMS key ARN, and the Prometheus
	// endpoint URL only.
	ResourceTypeAMPWorkspace = "aws_amp_workspace" // #nosec G101 -- resource-type identifier for an AMP workspace, not a credential
	// ResourceTypeAMPRuleGroupsNamespace identifies an Amazon Managed Service
	// for Prometheus rule-groups namespace metadata resource. The scanner emits
	// the namespace NAME, ARN, owning workspace id, and status only. The
	// recording-rule and alerting-rule definition body is never read or
	// persisted.
	ResourceTypeAMPRuleGroupsNamespace = "aws_amp_rule_groups_namespace"
	// ResourceTypeAMPScraper identifies an Amazon Managed Service for Prometheus
	// managed collector (scraper) metadata resource. The scanner emits identity,
	// alias, status, and the scrape source/destination reference identifiers
	// only. The scrape-configuration body is never read or persisted.
	ResourceTypeAMPScraper = "aws_amp_scraper"
)

const (
	// RelationshipAMPWorkspaceUsesKMSKey records an AMP workspace's reported
	// customer-managed KMS encryption key dependency. AWS reports a key ARN,
	// which matches how the KMS scanner publishes its key resource_id, so the
	// edge targets aws_kms_key.
	RelationshipAMPWorkspaceUsesKMSKey = "amp_workspace_uses_kms_key"
	// RelationshipAMPRuleGroupsNamespaceInWorkspace records a rule-groups
	// namespace's membership in its parent workspace. The target is keyed by the
	// workspace ARN the workspace node publishes, so the edge joins the
	// workspace node exactly.
	RelationshipAMPRuleGroupsNamespaceInWorkspace = "amp_rule_groups_namespace_in_workspace"
	// RelationshipAMPScraperScrapesEKSCluster records an AMP managed-collector
	// scraper's reported Amazon EKS source cluster. AWS reports an EKS cluster
	// ARN, which matches how the EKS scanner publishes its cluster resource_id,
	// so the edge targets aws_eks_cluster.
	RelationshipAMPScraperScrapesEKSCluster = "amp_scraper_scrapes_eks_cluster"
	// RelationshipAMPScraperSendsToWorkspace records an AMP managed-collector
	// scraper's reported destination workspace. The target is keyed by the
	// workspace ARN the workspace node publishes, so the edge joins the
	// workspace node exactly.
	RelationshipAMPScraperSendsToWorkspace = "amp_scraper_sends_to_workspace"
	// RelationshipAMPScraperUsesSubnet records an AMP scraper's reported EKS VPC
	// configuration subnet. AWS reports a bare subnet id (subnet-...), the
	// resource_id the EC2 scanner publishes for a subnet node, so the edge
	// targets aws_ec2_subnet by that bare id.
	RelationshipAMPScraperUsesSubnet = "amp_scraper_uses_subnet"
	// RelationshipAMPScraperUsesSecurityGroup records an AMP scraper's reported
	// EKS VPC configuration security group. AWS reports a bare security-group id
	// (sg-...), the resource_id the EC2 scanner publishes for a security-group
	// node, so the edge targets aws_ec2_security_group by that bare id.
	RelationshipAMPScraperUsesSecurityGroup = "amp_scraper_uses_security_group"
)
