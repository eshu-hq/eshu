// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package catalog

// G1-category constants consolidated from constants_amp.go (#6696).
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

// G1-category constants consolidated from constants_cognito.go (#6696).
const (
	// ServiceCognito identifies the regional Amazon Cognito service scan slice.
	// It covers both Cognito User Pools (cognito-idp) and Cognito Identity
	// Pools (cognito-identity).
	ServiceCognito = "cognito"
)

const (
	// ResourceTypeCognitoUserPool identifies a Cognito user pool.
	ResourceTypeCognitoUserPool = "aws_cognito_user_pool"
	// ResourceTypeCognitoUserPoolClient identifies a Cognito user pool app
	// client.
	ResourceTypeCognitoUserPoolClient = "aws_cognito_user_pool_client"
	// ResourceTypeCognitoIdentityProvider identifies a Cognito user pool
	// identity provider.
	ResourceTypeCognitoIdentityProvider = "aws_cognito_identity_provider"
	// ResourceTypeCognitoResourceServer identifies a Cognito user pool resource
	// server.
	ResourceTypeCognitoResourceServer = "aws_cognito_resource_server"
	// ResourceTypeCognitoUserPoolGroup identifies a Cognito user pool group.
	ResourceTypeCognitoUserPoolGroup = "aws_cognito_user_pool_group"
	// ResourceTypeCognitoIdentityPool identifies a Cognito identity pool.
	ResourceTypeCognitoIdentityPool = "aws_cognito_identity_pool"
)

const (
	// RelationshipCognitoUserPoolClientUsesUserPool records the user pool an app
	// client belongs to.
	RelationshipCognitoUserPoolClientUsesUserPool = "cognito_user_pool_client_uses_user_pool"
	// RelationshipCognitoUserPoolUsesLambdaTrigger records a Lambda trigger ARN
	// configured on a user pool.
	RelationshipCognitoUserPoolUsesLambdaTrigger = "cognito_user_pool_uses_lambda_trigger"
	// RelationshipCognitoIdentityPoolUsesUserPool records a user pool wired into
	// an identity pool as a Cognito login provider.
	RelationshipCognitoIdentityPoolUsesUserPool = "cognito_identity_pool_uses_user_pool"
	// RelationshipCognitoIdentityPoolUsesIdentityProvider records an external
	// OIDC or SAML provider attached to an identity pool.
	RelationshipCognitoIdentityPoolUsesIdentityProvider = "cognito_identity_pool_uses_identity_provider"
)

// G1-category constants consolidated from constants_dms.go (#6696).
const (
	// ServiceDMS identifies the regional AWS Database Migration Service
	// metadata-only scan slice. The scanner reads control-plane describe APIs
	// (DescribeReplicationInstances, DescribeReplicationSubnetGroups,
	// DescribeEndpoints, DescribeReplicationTasks) plus ListTagsForResource and
	// never reads migrated table rows, connection credentials, endpoint
	// passwords, task settings, or table-mapping bodies, and never mutates DMS
	// state.
	ServiceDMS = "dms"
)

const (
	// ResourceTypeDMSReplicationInstance identifies an AWS DMS replication
	// instance metadata resource. The scanner emits identity, instance class,
	// engine version, allocated storage, Multi-AZ and public-accessibility
	// flags, status, and KMS/subnet-group/security-group references only.
	ResourceTypeDMSReplicationInstance = "aws_dms_replication_instance"
	// ResourceTypeDMSReplicationSubnetGroup identifies an AWS DMS replication
	// subnet group metadata resource. The scanner emits identity, description,
	// status, VPC id, and the member subnet ids only.
	ResourceTypeDMSReplicationSubnetGroup = "aws_dms_replication_subnet_group"
	// ResourceTypeDMSEndpoint identifies an AWS DMS endpoint metadata resource.
	// The scanner emits identity, endpoint type (source/target), engine name,
	// SSL mode, status, and resolvable data-store/KMS/secret references only. It
	// never persists server names as credentials, usernames, passwords,
	// connection attributes, or external table definitions.
	ResourceTypeDMSEndpoint = "aws_dms_endpoint"
	// ResourceTypeDMSReplicationTask identifies an AWS DMS replication task
	// metadata resource. The scanner emits identity, migration type, status, and
	// the source/target endpoint and replication-instance references only. It
	// never persists task settings, table-mapping bodies, CDC start/stop
	// positions, or recovery checkpoints.
	ResourceTypeDMSReplicationTask = "aws_dms_replication_task"
)

const (
	// RelationshipDMSReplicationInstanceInSubnetGroup records a replication
	// instance's placement in its replication subnet group. The target is keyed
	// by the subnet group identifier the subnet-group node publishes.
	RelationshipDMSReplicationInstanceInSubnetGroup = "dms_replication_instance_in_subnet_group"
	// RelationshipDMSReplicationInstanceInSubnet records a replication instance's
	// reported membership in an EC2 subnet (derived from its subnet group). The
	// target is keyed by the bare subnet id the EC2 scanner publishes.
	RelationshipDMSReplicationInstanceInSubnet = "dms_replication_instance_in_subnet"
	// RelationshipDMSReplicationInstanceUsesSecurityGroup records a replication
	// instance's VPC security group membership. The target is keyed by the bare
	// security-group id the EC2 scanner publishes.
	RelationshipDMSReplicationInstanceUsesSecurityGroup = "dms_replication_instance_uses_security_group"
	// RelationshipDMSReplicationInstanceUsesKMSKey records a replication
	// instance's reported KMS encryption key dependency.
	RelationshipDMSReplicationInstanceUsesKMSKey = "dms_replication_instance_uses_kms_key"
	// RelationshipDMSReplicationSubnetGroupInVPC records a replication subnet
	// group's VPC placement. The target is keyed by the bare VPC id the EC2
	// scanner publishes.
	RelationshipDMSReplicationSubnetGroupInVPC = "dms_replication_subnet_group_in_vpc"
	// RelationshipDMSReplicationSubnetGroupHasSubnet records a replication subnet
	// group's member subnet. The target is keyed by the bare subnet id the EC2
	// scanner publishes.
	RelationshipDMSReplicationSubnetGroupHasSubnet = "dms_replication_subnet_group_has_subnet"
	// RelationshipDMSEndpointUsesKMSKey records a DMS endpoint's reported KMS
	// encryption key dependency for its stored connection parameters.
	RelationshipDMSEndpointUsesKMSKey = "dms_endpoint_uses_kms_key"
	// RelationshipDMSEndpointTargetsS3Bucket records a DMS S3 endpoint's data
	// store bucket. DMS reports a bucket NAME, so the scanner synthesizes the
	// partition-aware bucket ARN to match the S3 scanner's published bucket
	// resource_id.
	RelationshipDMSEndpointTargetsS3Bucket = "dms_endpoint_targets_s3_bucket"
	// RelationshipDMSEndpointTargetsKinesisStream records a DMS Kinesis endpoint's
	// target Kinesis Data Streams stream, keyed by the stream ARN DMS reports.
	RelationshipDMSEndpointTargetsKinesisStream = "dms_endpoint_targets_kinesis_stream"
	// RelationshipDMSEndpointUsesSecret records a DMS endpoint's Secrets Manager
	// secret reference for its connection credentials. The secret value is never
	// read; only the secret id/ARN reference is recorded.
	RelationshipDMSEndpointUsesSecret = "dms_endpoint_uses_secret" // #nosec G101 -- relationship-type identifier recording a secret ARN reference, not a credential value
	// RelationshipDMSReplicationTaskUsesSourceEndpoint records a replication
	// task's source endpoint, keyed by the endpoint ARN the endpoint node
	// publishes.
	RelationshipDMSReplicationTaskUsesSourceEndpoint = "dms_replication_task_uses_source_endpoint"
	// RelationshipDMSReplicationTaskUsesTargetEndpoint records a replication
	// task's target endpoint, keyed by the endpoint ARN the endpoint node
	// publishes.
	RelationshipDMSReplicationTaskUsesTargetEndpoint = "dms_replication_task_uses_target_endpoint"
	// RelationshipDMSReplicationTaskRunsOnInstance records a replication task's
	// replication instance, keyed by the instance ARN the instance node
	// publishes.
	RelationshipDMSReplicationTaskRunsOnInstance = "dms_replication_task_runs_on_instance"
)

// G1-category constants consolidated from constants_imagebuilder.go (#6696).
const (
	// ServiceImageBuilder identifies the regional EC2 Image Builder metadata-only
	// scan slice. The scanner reads image pipeline, image recipe, container
	// recipe, infrastructure configuration, and distribution configuration
	// control-plane metadata through the imagebuilder list/get APIs and never
	// reads or persists component build-document bodies, Dockerfile bodies, user
	// data, or any build artifact, and never mutates Image Builder state.
	ServiceImageBuilder = "imagebuilder"
)

const (
	// ResourceTypeImageBuilderImagePipeline identifies an EC2 Image Builder image
	// pipeline metadata resource. The scanner emits identity, status, platform,
	// the referenced recipe/config ARNs, and lifecycle timestamps only.
	ResourceTypeImageBuilderImagePipeline = "aws_imagebuilder_image_pipeline"
	// ResourceTypeImageBuilderImageRecipe identifies an EC2 Image Builder image
	// recipe metadata resource. The scanner emits identity, platform, version,
	// the parent image reference, and component ARN references only; component
	// build-document bodies and inline component data stay outside the contract.
	ResourceTypeImageBuilderImageRecipe = "aws_imagebuilder_image_recipe"
	// ResourceTypeImageBuilderContainerRecipe identifies an EC2 Image Builder
	// container recipe metadata resource. The scanner emits identity, container
	// type, platform, version, the parent image reference, the target ECR
	// repository name, and KMS key reference only; Dockerfile template bodies are
	// never read or persisted.
	ResourceTypeImageBuilderContainerRecipe = "aws_imagebuilder_container_recipe"
	// ResourceTypeImageBuilderInfrastructureConfiguration identifies an EC2 Image
	// Builder infrastructure configuration metadata resource. The scanner emits
	// identity, instance types, the instance profile name, networking references,
	// the SNS topic, and the S3 logging location only.
	ResourceTypeImageBuilderInfrastructureConfiguration = "aws_imagebuilder_infrastructure_configuration"
	// ResourceTypeImageBuilderDistributionConfiguration identifies an EC2 Image
	// Builder distribution configuration metadata resource. The scanner emits
	// identity, the distribution target regions, and lifecycle timestamps only.
	ResourceTypeImageBuilderDistributionConfiguration = "aws_imagebuilder_distribution_configuration"
)

const (
	// RelationshipImageBuilderPipelineUsesImageRecipe records that an image
	// pipeline builds from a given image recipe. The target is keyed by the image
	// recipe ARN the recipe node publishes.
	RelationshipImageBuilderPipelineUsesImageRecipe = "imagebuilder_pipeline_uses_image_recipe"
	// RelationshipImageBuilderPipelineUsesContainerRecipe records that an image
	// pipeline builds from a given container recipe. The target is keyed by the
	// container recipe ARN the recipe node publishes.
	RelationshipImageBuilderPipelineUsesContainerRecipe = "imagebuilder_pipeline_uses_container_recipe"
	// RelationshipImageBuilderPipelineUsesInfrastructureConfiguration records that
	// an image pipeline runs on a given infrastructure configuration, keyed by the
	// infrastructure configuration ARN that node publishes.
	RelationshipImageBuilderPipelineUsesInfrastructureConfiguration = "imagebuilder_pipeline_uses_infrastructure_configuration"
	// RelationshipImageBuilderPipelineUsesDistributionConfiguration records that
	// an image pipeline distributes through a given distribution configuration,
	// keyed by the distribution configuration ARN that node publishes.
	RelationshipImageBuilderPipelineUsesDistributionConfiguration = "imagebuilder_pipeline_uses_distribution_configuration"
	// RelationshipImageBuilderPipelineUsesExecutionRole records that an image
	// pipeline assumes a given IAM execution role to run its build workflows. The
	// target is keyed by the role ARN the IAM scanner publishes.
	RelationshipImageBuilderPipelineUsesExecutionRole = "imagebuilder_pipeline_uses_execution_role"
	// RelationshipImageBuilderInfraConfigUsesInstanceProfile records that an
	// infrastructure configuration launches build instances with a given IAM
	// instance profile. AWS reports the instance profile NAME, so the scanner
	// synthesizes the partition-aware instance-profile ARN the IAM scanner
	// publishes as the join key.
	RelationshipImageBuilderInfraConfigUsesInstanceProfile = "imagebuilder_infra_config_uses_instance_profile"
	// RelationshipImageBuilderInfraConfigUsesSubnet records an infrastructure
	// configuration's build-instance subnet. The target is keyed by the bare
	// subnet id (subnet-...) the VPC scanner publishes.
	RelationshipImageBuilderInfraConfigUsesSubnet = "imagebuilder_infra_config_uses_subnet"
	// RelationshipImageBuilderInfraConfigUsesSecurityGroup records an
	// infrastructure configuration's build-instance security group. The target is
	// keyed by the bare security-group id (sg-...) the EC2 scanner publishes.
	RelationshipImageBuilderInfraConfigUsesSecurityGroup = "imagebuilder_infra_config_uses_security_group"
	// RelationshipImageBuilderInfraConfigUsesSNSTopic records an infrastructure
	// configuration's build-status SNS topic. AWS reports the topic ARN, which is
	// the resource_id the SNS scanner publishes.
	RelationshipImageBuilderInfraConfigUsesSNSTopic = "imagebuilder_infra_config_uses_sns_topic"
	// RelationshipImageBuilderInfraConfigLogsToS3 records an infrastructure
	// configuration's build-log S3 bucket. AWS reports a bucket NAME, so the
	// scanner synthesizes the partition-aware bucket ARN (arn:<partition>:s3:::
	// <bucket>) the S3 scanner publishes as the join key.
	RelationshipImageBuilderInfraConfigLogsToS3 = "imagebuilder_infra_config_logs_to_s3"
	// RelationshipImageBuilderContainerRecipeUsesECRRepository records that a
	// container recipe pushes built images to a given ECR repository. AWS reports
	// a repository NAME, so the scanner synthesizes the partition-aware ECR
	// repository ARN the ECR scanner publishes as the join key.
	RelationshipImageBuilderContainerRecipeUsesECRRepository = "imagebuilder_container_recipe_uses_ecr_repository"
	// RelationshipImageBuilderContainerRecipeUsesKMSKey records a container
	// recipe's reported KMS encryption key dependency. The reported key id/ARN/
	// alias is the join key the KMS scanner publishes; target_arn is set only for
	// ARN-shaped identifiers.
	RelationshipImageBuilderContainerRecipeUsesKMSKey = "imagebuilder_container_recipe_uses_kms_key"
)

// G1-category constants consolidated from constants_transfer.go (#6696).
const (
	// ServiceTransfer identifies the regional AWS Transfer Family metadata-only
	// scan slice covering SFTP/FTPS/FTP/AS2 servers and their service-managed
	// users. Host keys, SSH public keys, user policy JSON, and any credential
	// material stay outside the scan slice.
	ServiceTransfer = "transfer"
)

const (
	// ResourceTypeTransferServer identifies an AWS Transfer Family server
	// metadata resource. The fact carries server identity (ARN, server ID),
	// endpoint type, protocols, identity provider type, domain, and state. Host
	// key fingerprints and host key material are never persisted.
	ResourceTypeTransferServer = "aws_transfer_server"
	// ResourceTypeTransferUser identifies an AWS Transfer Family service-managed
	// user metadata resource. The fact carries user identity (ARN, user name),
	// home directory type, home directory path, and home directory mapping paths.
	// SSH public keys, user policy JSON, and POSIX credential material are never
	// persisted.
	ResourceTypeTransferUser = "aws_transfer_user"
)

const (
	// RelationshipTransferServerUsesVPCEndpoint records a Transfer server's
	// reported VPC endpoint placement when the server uses a VPC_ENDPOINT
	// endpoint type with an AWS-reported VPC endpoint ID.
	RelationshipTransferServerUsesVPCEndpoint = "transfer_server_uses_vpc_endpoint"
	// RelationshipTransferServerUsesElasticIP records a Transfer server's
	// reported Elastic IP allocation when the server's VPC endpoint attaches
	// address allocation IDs.
	RelationshipTransferServerUsesElasticIP = "transfer_server_uses_elastic_ip"
	// RelationshipTransferServerUsesACMCertificate records a Transfer server's
	// reported ACM certificate dependency when FTPS is enabled and AWS reports
	// an ARN-shaped certificate.
	RelationshipTransferServerUsesACMCertificate = "transfer_server_uses_acm_certificate"
	// RelationshipTransferServerLogsToLogGroup records a Transfer server's
	// reported CloudWatch Logs structured-log destination when AWS reports an
	// ARN-shaped log group.
	RelationshipTransferServerLogsToLogGroup = "transfer_server_logs_to_log_group"
	// RelationshipTransferServerUsesLoggingRole records a Transfer server's
	// reported CloudWatch logging IAM role when AWS reports an ARN-shaped role.
	RelationshipTransferServerUsesLoggingRole = "transfer_server_uses_logging_role"
	// RelationshipTransferUserUsesIAMRole records a Transfer user's reported IAM
	// role dependency when AWS reports an ARN-shaped role.
	RelationshipTransferUserUsesIAMRole = "transfer_user_uses_iam_role"
	// RelationshipTransferUserHomeDirectoryInS3Bucket records a Transfer user's
	// reported S3 home-directory backing bucket when the home directory resolves
	// to an S3 path. Only the path (bucket and key prefix) is recorded; object
	// contents are never read.
	RelationshipTransferUserHomeDirectoryInS3Bucket = "transfer_user_home_directory_in_s3_bucket"
	// RelationshipTransferUserHomeDirectoryInEFSFileSystem records a Transfer
	// user's reported EFS home-directory backing file system when the home
	// directory resolves to an EFS path. Only the path is recorded; file
	// contents are never read.
	RelationshipTransferUserHomeDirectoryInEFSFileSystem = "transfer_user_home_directory_in_efs_file_system"
)

// G1-category constants consolidated from constants_verifiedaccess.go (#6696).
const (
	// ServiceVerifiedAccess identifies the regional Amazon Verified Access
	// metadata-only scan slice. The scanner reads Verified Access control-plane
	// metadata through the EC2 management API
	// (DescribeVerifiedAccessInstances, DescribeVerifiedAccessGroups,
	// DescribeVerifiedAccessEndpoints, DescribeVerifiedAccessTrustProviders) and
	// never reads or persists trust-provider client secrets, policy bodies, or
	// any data-plane payload, and never mutates Verified Access state. Although
	// Verified Access ships under the EC2 SDK, it is its own service kind with
	// its own ResourceType constants, distinct from the core `ec2` scanner.
	ServiceVerifiedAccess = "verifiedaccess"
)

const (
	// ResourceTypeVerifiedAccessInstance identifies an Amazon Verified Access
	// instance metadata resource. The scanner emits identity, FIPS-enabled flag,
	// the customer-managed-KMS-key encryption flag, attached trust-provider
	// identifiers, and lifecycle timestamps only.
	ResourceTypeVerifiedAccessInstance = "aws_verifiedaccess_instance"
	// ResourceTypeVerifiedAccessGroup identifies an Amazon Verified Access group
	// metadata resource. The scanner emits identity, owning account, parent
	// instance identity, the customer-managed-KMS-key encryption flag, and
	// lifecycle timestamps only. Group policy documents stay outside the
	// contract.
	ResourceTypeVerifiedAccessGroup = "aws_verifiedaccess_group"
	// ResourceTypeVerifiedAccessEndpoint identifies an Amazon Verified Access
	// endpoint metadata resource. The scanner emits identity, endpoint and
	// attachment type, application/endpoint domains, status, parent group and
	// instance identities, attached subnet/security-group/ACM-certificate
	// references, and lifecycle timestamps only. Endpoint policy documents stay
	// outside the contract.
	ResourceTypeVerifiedAccessEndpoint = "aws_verifiedaccess_endpoint"
	// ResourceTypeVerifiedAccessTrustProvider identifies an Amazon Verified
	// Access trust provider metadata resource. The scanner emits identity, the
	// trust-provider/user-trust-provider/device-trust-provider types, policy
	// reference name, the OIDC issuer reference, and lifecycle timestamps only.
	// OIDC client identifiers and client secrets are never read or persisted.
	ResourceTypeVerifiedAccessTrustProvider = "aws_verifiedaccess_trust_provider"
)

const (
	// RelationshipVerifiedAccessGroupInInstance records a Verified Access group's
	// membership in its parent instance. The target is keyed by the instance ARN
	// the instance node publishes so the edge joins that node exactly.
	RelationshipVerifiedAccessGroupInInstance = "verifiedaccess_group_in_instance"
	// RelationshipVerifiedAccessEndpointInGroup records a Verified Access
	// endpoint's membership in its parent group. The target is keyed by the group
	// ARN the group node publishes.
	RelationshipVerifiedAccessEndpointInGroup = "verifiedaccess_endpoint_in_group"
	// RelationshipVerifiedAccessInstanceUsesTrustProvider records a Verified
	// Access instance's attachment to a trust provider. The target is keyed by
	// the trust-provider ARN the trust-provider node publishes.
	RelationshipVerifiedAccessInstanceUsesTrustProvider = "verifiedaccess_instance_uses_trust_provider"
	// RelationshipVerifiedAccessEndpointUsesSubnet records a Verified Access
	// endpoint's reported VPC subnet placement. The target is the EC2-owned
	// aws_ec2_subnet identity keyed by the bare subnet id (subnet-...).
	RelationshipVerifiedAccessEndpointUsesSubnet = "verifiedaccess_endpoint_uses_subnet"
	// RelationshipVerifiedAccessEndpointUsesSecurityGroup records a Verified
	// Access endpoint's reported security-group attachment. The target is the
	// EC2-owned aws_ec2_security_group identity keyed by the bare group id
	// (sg-...).
	RelationshipVerifiedAccessEndpointUsesSecurityGroup = "verifiedaccess_endpoint_uses_security_group"
	// RelationshipVerifiedAccessEndpointUsesACMCertificate records a Verified
	// Access endpoint's reported public TLS certificate. The target is the
	// aws_acm_certificate identity keyed by the certificate ARN the ACM scanner
	// publishes.
	RelationshipVerifiedAccessEndpointUsesACMCertificate = "verifiedaccess_endpoint_uses_acm_certificate"
)
