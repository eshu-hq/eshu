// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package catalog

const (
	// ServiceCloudHSMV2 identifies the regional AWS CloudHSM v2 metadata-only
	// scan slice. The scanner reads control-plane metadata through the
	// CloudHSM v2 management APIs (DescribeClusters, DescribeBackups, ListTags)
	// and never reads, writes, or persists any cryptographic key material,
	// certificate private bodies, certificate signing requests, or the
	// cluster's Pre-Crypto Officer password.
	ServiceCloudHSMV2 = "cloudhsmv2"
)

const (
	// ResourceTypeCloudHSMV2Cluster identifies an AWS CloudHSM v2 cluster
	// metadata resource. The scanner emits identity, state, HSM type, mode,
	// network type, backup-policy presence, embedded HSM ENI metadata, and
	// certificate presence flags only. It never emits certificate PEM bodies,
	// the cluster CSR body, or the Pre-Crypto Officer password.
	ResourceTypeCloudHSMV2Cluster = "aws_cloudhsmv2_cluster"
	// ResourceTypeCloudHSMV2Backup identifies an AWS CloudHSM v2 backup
	// metadata resource. The scanner emits identity, state, timestamps, source
	// backup/cluster references, and never-expires/HSM-type metadata only. The
	// backup never carries key material.
	ResourceTypeCloudHSMV2Backup = "aws_cloudhsmv2_backup"
)

const (
	// RelationshipCloudHSMV2ClusterInVPC records a CloudHSM v2 cluster's parent
	// VPC. The target is keyed by the bare VPC id (vpc-…) the EC2 scanner
	// publishes as a VPC node's resource_id.
	RelationshipCloudHSMV2ClusterInVPC = "cloudhsmv2_cluster_in_vpc"
	// RelationshipCloudHSMV2ClusterInSubnet records a CloudHSM v2 cluster's
	// presence in one VPC subnet drawn from its availability-zone-to-subnet
	// mapping. The target is keyed by the bare subnet id (subnet-…) the EC2
	// scanner publishes as a subnet node's resource_id.
	RelationshipCloudHSMV2ClusterInSubnet = "cloudhsmv2_cluster_in_subnet"
	// RelationshipCloudHSMV2ClusterUsesSecurityGroup records a CloudHSM v2
	// cluster's reported AWS-managed cluster security group. The target is keyed
	// by the bare security-group id (sg-…) the EC2 scanner publishes as a
	// security-group node's resource_id.
	RelationshipCloudHSMV2ClusterUsesSecurityGroup = "cloudhsmv2_cluster_uses_security_group"
	// RelationshipCloudHSMV2BackupOfCluster records a CloudHSM v2 backup's
	// source cluster. The target is keyed by the bare cluster id the CloudHSM v2
	// cluster node publishes as its resource_id.
	RelationshipCloudHSMV2BackupOfCluster = "cloudhsmv2_backup_of_cluster"
)

const (
	// ServiceAccessAnalyzer identifies the regional IAM Access Analyzer
	// metadata-only scan slice.
	ServiceAccessAnalyzer = "accessanalyzer"
)

const (
	// ResourceTypeAccessAnalyzerAnalyzer identifies an IAM Access Analyzer
	// analyzer metadata resource.
	ResourceTypeAccessAnalyzerAnalyzer = "aws_accessanalyzer_analyzer"
	// ResourceTypeAccessAnalyzerArchiveRule identifies an Access Analyzer
	// archive-rule metadata resource.
	ResourceTypeAccessAnalyzerArchiveRule = "aws_accessanalyzer_archive_rule"
	// ResourceTypeAccessAnalyzerFindingCount identifies an aggregate Access
	// Analyzer finding-count resource.
	ResourceTypeAccessAnalyzerFindingCount = "aws_accessanalyzer_finding_count"
	// ResourceTypeAccessAnalyzerUnusedAccessSummary identifies a metadata-only
	// per-resource unused-access summary.
	ResourceTypeAccessAnalyzerUnusedAccessSummary = "aws_accessanalyzer_unused_access_summary"
)

const (
	// RelationshipAccessAnalyzerAnalyzerScopesOrganizationAccount records the
	// organization-account scope for an organization Access Analyzer analyzer.
	RelationshipAccessAnalyzerAnalyzerScopesOrganizationAccount = "accessanalyzer_analyzer_scopes_organization_account"
	// RelationshipAccessAnalyzerAnalyzerHasArchiveRule records archive-rule
	// membership on an Access Analyzer analyzer.
	RelationshipAccessAnalyzerAnalyzerHasArchiveRule = "accessanalyzer_analyzer_has_archive_rule"
)

const (
	// ServiceOutposts identifies the regional AWS Outposts metadata-only scan
	// slice. The scanner reads control-plane describe/list APIs (ListOutposts,
	// GetOutpost, ListSites, GetSite, ListAssets, ListTagsForResource) and emits
	// operational identity for outposts, sites, and rack/server assets only. It
	// never persists physical site street addresses, shipping or contact
	// details, free-form site notes, or rack physical-property logistics, and it
	// never mutates Outposts state.
	ServiceOutposts = "outposts"
)

const (
	// ResourceTypeOutpostsOutpost identifies an AWS Outposts outpost metadata
	// resource. The scanner emits identity, lifecycle status, Availability Zone,
	// owner account, supported hardware type, and the parent site id only.
	ResourceTypeOutpostsOutpost = "aws_outposts_outpost"
	// ResourceTypeOutpostsSite identifies an AWS Outposts site metadata
	// resource. The scanner emits operational identity (site id, name, account)
	// only. Physical operating addresses, free-form notes, and rack physical
	// properties are intentionally excluded from the contract.
	ResourceTypeOutpostsSite = "aws_outposts_site"
	// ResourceTypeOutpostsAsset identifies an AWS Outposts asset (a rack server
	// or server-form-factor unit) metadata resource. The scanner emits the asset
	// id, asset type, rack id, compute lifecycle state, and rack elevation only.
	ResourceTypeOutpostsAsset = "aws_outposts_asset"
)

const (
	// RelationshipOutpostsOutpostInSite records an Outposts outpost's membership
	// in its parent site. The target is keyed by the site ARN so the edge joins
	// the site node the scanner publishes.
	RelationshipOutpostsOutpostInSite = "outposts_outpost_in_site"
	// RelationshipOutpostsAssetInOutpost records an Outposts asset's membership
	// in its parent outpost. The target is keyed by the outpost ARN so the edge
	// joins the outpost node the scanner publishes.
	RelationshipOutpostsAssetInOutpost = "outposts_asset_in_outpost"
)

const (
	// ServiceDRS identifies the regional AWS Elastic Disaster Recovery (DRS)
	// metadata-only scan slice. The scanner reads control-plane describe APIs
	// only (DescribeSourceServers, DescribeRecoveryInstances,
	// DescribeReplicationConfigurationTemplates). It never installs or reads
	// replication agent secrets, never reads replicated disk data or snapshot
	// contents, and never starts, stops, recovers, or mutates DRS state.
	ServiceDRS = "drs"
)

const (
	// ResourceTypeDRSSourceServer identifies an AWS Elastic Disaster Recovery
	// source server metadata resource. The scanner emits identity (source server
	// id and ARN), reported hostname/FQDN identification hints, operating-system
	// description, data-replication state, recommended recovery instance type,
	// EC2-origin cloud properties, and the associated recovery instance id only.
	// Replication agent secrets and replicated disk data stay outside the
	// contract.
	ResourceTypeDRSSourceServer = "aws_drs_source_server"
	// ResourceTypeDRSRecoveryInstance identifies an AWS Elastic Disaster Recovery
	// recovery instance metadata resource. The scanner emits identity (recovery
	// instance id and ARN), the launched EC2 instance id, the EC2 instance state,
	// the originating source server id, the drill flag, and the origin
	// environment only.
	ResourceTypeDRSRecoveryInstance = "aws_drs_recovery_instance"
	// ResourceTypeDRSReplicationConfigurationTemplate identifies an AWS Elastic
	// Disaster Recovery replication configuration template metadata resource. The
	// scanner emits identity (template id and ARN), EBS encryption posture, the
	// staging-area subnet, the replication server instance type, and the
	// dedicated-replication-server flag only.
	ResourceTypeDRSReplicationConfigurationTemplate = "aws_drs_replication_configuration_template"
)

const (
	// RelationshipDRSSourceServerRecoversToInstance records that a DRS source
	// server's reported recovery instance is the DRS recovery instance node the
	// scanner publishes. The target is keyed by the recovery instance id so the
	// edge joins the recovery instance node within the same DRS scan.
	RelationshipDRSSourceServerRecoversToInstance = "drs_source_server_recovers_to_instance"
	// RelationshipDRSRecoveryInstanceRunsOnEC2Instance records that a DRS recovery
	// instance is backed by a launched EC2 instance. The target is keyed by the
	// bare EC2 instance id (i-...) that other scanners use to publish EC2
	// instance identity, so the edge joins the EC2 instance node.
	RelationshipDRSRecoveryInstanceRunsOnEC2Instance = "drs_recovery_instance_runs_on_ec2_instance"
)

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

const (
	// ServiceSecretsManager identifies the regional AWS Secrets Manager
	// metadata-only scan slice.
	ServiceSecretsManager = "secretsmanager"
)

const (
	// ResourceTypeSecretsManagerSecret identifies a Secrets Manager secret
	// metadata resource.
	ResourceTypeSecretsManagerSecret = "aws_secretsmanager_secret"
)

const (
	// RelationshipSecretsManagerSecretUsesKMSKey records a secret's reported
	// KMS key dependency.
	RelationshipSecretsManagerSecretUsesKMSKey = "secretsmanager_secret_uses_kms_key"
	// RelationshipSecretsManagerSecretUsesRotationLambda records a secret's
	// reported rotation Lambda dependency.
	RelationshipSecretsManagerSecretUsesRotationLambda = "secretsmanager_secret_uses_rotation_lambda"
)

const (
	// ServiceSSM identifies the regional AWS Systems Manager Parameter Store
	// metadata-only scan slice.
	ServiceSSM = "ssm"
)

const (
	// ResourceTypeSSMParameter identifies an SSM Parameter Store metadata
	// resource.
	ResourceTypeSSMParameter = "aws_ssm_parameter"
)

const (
	// RelationshipSSMParameterUsesKMSKey records an SSM SecureString
	// parameter's reported KMS key dependency.
	RelationshipSSMParameterUsesKMSKey = "ssm_parameter_uses_kms_key"
)
