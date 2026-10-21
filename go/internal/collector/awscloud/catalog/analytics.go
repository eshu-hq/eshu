// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package catalog

// Analytics-category constants consolidated from constants_athena.go (#6696).

const (
	// ServiceAthena identifies the regional Amazon Athena metadata-only scan
	// slice covering workgroups, data catalogs, prepared-statement names, and
	// named-query identities. Query result rows, query execution result location
	// contents, named-query SQL bodies, prepared-statement query strings, and
	// query history strings stay outside the scan slice.
	ServiceAthena = "athena"
)

const (
	// ResourceTypeAthenaWorkGroup identifies an Athena workgroup metadata
	// resource. The fact never carries query result rows, result-location
	// object contents, or query history strings.
	ResourceTypeAthenaWorkGroup = "aws_athena_workgroup"
	// ResourceTypeAthenaDataCatalog identifies an Athena data catalog metadata
	// resource. The fact never carries data plane records from the catalog.
	ResourceTypeAthenaDataCatalog = "aws_athena_data_catalog"
	// ResourceTypeAthenaPreparedStatement identifies an Athena prepared
	// statement metadata resource. The fact carries the statement name and
	// owning workgroup; the SQL `QueryStatement` body is never persisted.
	ResourceTypeAthenaPreparedStatement = "aws_athena_prepared_statement"
	// ResourceTypeAthenaNamedQuery identifies an Athena named-query metadata
	// resource. The fact carries the named-query identity (id, name, database,
	// workgroup) and never persists the SQL `QueryString` body or query
	// execution history.
	ResourceTypeAthenaNamedQuery = "aws_athena_named_query"
)

const (
	// RelationshipAthenaWorkGroupUsesResultBucket records the S3 result-location
	// bucket reported by an Athena workgroup's result configuration. The edge
	// is reported metadata only; result object contents stay outside facts.
	RelationshipAthenaWorkGroupUsesResultBucket = "athena_workgroup_uses_result_bucket"
	// RelationshipAthenaWorkGroupUsesKMSKey records the KMS key reported by an
	// Athena workgroup's result encryption configuration.
	RelationshipAthenaWorkGroupUsesKMSKey = "athena_workgroup_uses_kms_key"
	// RelationshipAthenaPreparedStatementInWorkGroup records prepared-statement
	// membership in an Athena workgroup.
	RelationshipAthenaPreparedStatementInWorkGroup = "athena_prepared_statement_in_workgroup"
	// RelationshipAthenaNamedQueryInWorkGroup records named-query membership in
	// an Athena workgroup.
	RelationshipAthenaNamedQueryInWorkGroup = "athena_named_query_in_workgroup"
)

// Analytics-category constants consolidated from constants_emr.go (#6696).

const (
	// ServiceEMR identifies the regional Amazon EMR metadata-only scan slice.
	// One service kind covers EMR on EC2 clusters, EMR Serverless
	// applications, and EMR Studio; resource types distinguish the three
	// sub-surfaces. The scanner never invokes job, step, or lifecycle mutation
	// APIs (RunJobFlow, TerminateJobFlows, AddJobFlowSteps, CancelSteps,
	// ModifyInstanceGroups, ModifyInstanceFleet, StartJobRun, CancelJobRun,
	// Create/Delete/Start/Stop Application, Create/Delete/Update Studio or
	// StudioSessionMapping) and never persists step command lines, bootstrap
	// action script bodies, security configuration policy bodies, or Serverless
	// job-run entry-point arguments.
	ServiceEMR = "emr"
)

const (
	// ResourceTypeEMRCluster identifies an EMR on EC2 cluster metadata
	// resource. The scanner emits running and recently terminated clusters.
	ResourceTypeEMRCluster = "aws_emr_cluster"
	// ResourceTypeEMRInstanceGroup identifies an EMR uniform instance group.
	ResourceTypeEMRInstanceGroup = "aws_emr_instance_group"
	// ResourceTypeEMRInstanceFleet identifies an EMR instance fleet.
	ResourceTypeEMRInstanceFleet = "aws_emr_instance_fleet"
	// ResourceTypeEMRSecurityConfiguration identifies an EMR security
	// configuration metadata resource. Only the name and creation time are
	// emitted; the encryption and authentication policy body is never
	// persisted.
	ResourceTypeEMRSecurityConfiguration = "aws_emr_security_configuration"
	// ResourceTypeEMRServerlessApplication identifies an EMR Serverless
	// application metadata resource. The wire string uses the
	// "<service>_serverless_<kind>" shape shared with Redshift Serverless
	// (aws_redshift_serverless_namespace) rather than the AWS service id
	// "emrserverless".
	ResourceTypeEMRServerlessApplication = "aws_emr_serverless_application"
	// ResourceTypeEMRStudio identifies an EMR Studio metadata resource.
	ResourceTypeEMRStudio = "aws_emr_studio"
	// ResourceTypeEMRStudioSessionMapping identifies an EMR Studio
	// session-mapping metadata resource (identity binding only; the session
	// policy ARN is recorded as a reference, never the policy body).
	ResourceTypeEMRStudioSessionMapping = "aws_emr_studio_session_mapping"
)

const (
	// RelationshipEMRClusterUsesSubnet records the EC2 subnet an EMR on EC2
	// cluster places its instances in. The cluster-to-VPC join is derived from
	// subnet membership downstream; the EMR cluster API does not report a VPC
	// id directly.
	RelationshipEMRClusterUsesSubnet = "emr_cluster_uses_subnet"
	// RelationshipEMRClusterUsesSecurityGroup records an EC2 security group
	// associated with an EMR on EC2 cluster (managed and additional groups for
	// master, slave, and service access).
	RelationshipEMRClusterUsesSecurityGroup = "emr_cluster_uses_security_group"
	// RelationshipEMRClusterUsesIAMRole records an IAM role used by an EMR on
	// EC2 cluster (EMR service role and auto-scaling role).
	RelationshipEMRClusterUsesIAMRole = "emr_cluster_uses_iam_role"
	// RelationshipEMRClusterUsesInstanceProfile records the EC2 instance
	// profile an EMR on EC2 cluster assigns to its instances.
	RelationshipEMRClusterUsesInstanceProfile = "emr_cluster_uses_instance_profile"
	// RelationshipEMRClusterUsesSecurityConfiguration records the named EMR
	// security configuration an EMR on EC2 cluster references.
	RelationshipEMRClusterUsesSecurityConfiguration = "emr_cluster_uses_security_configuration"
	// RelationshipEMRClusterUsesKMSKey records the KMS key an EMR on EC2
	// cluster uses for log encryption.
	RelationshipEMRClusterUsesKMSKey = "emr_cluster_uses_kms_key"
	// RelationshipEMRClusterHasInstanceGroup records uniform instance group
	// membership on an EMR on EC2 cluster.
	RelationshipEMRClusterHasInstanceGroup = "emr_cluster_has_instance_group"
	// RelationshipEMRClusterHasInstanceFleet records instance fleet membership
	// on an EMR on EC2 cluster.
	RelationshipEMRClusterHasInstanceFleet = "emr_cluster_has_instance_fleet"
	// RelationshipEMRServerlessApplicationUsesSubnet records an EC2 subnet in
	// an EMR Serverless application's network configuration. The
	// application-to-VPC join is derived from subnet membership downstream; the
	// EMR Serverless API does not report a VPC id directly.
	RelationshipEMRServerlessApplicationUsesSubnet = "emr_serverless_application_uses_subnet"
	// RelationshipEMRServerlessApplicationUsesSecurityGroup records an EC2
	// security group in an EMR Serverless application's network configuration.
	RelationshipEMRServerlessApplicationUsesSecurityGroup = "emr_serverless_application_uses_security_group"
	// RelationshipEMRServerlessApplicationUsesKMSKey records the KMS key an EMR
	// Serverless application uses for disk encryption.
	RelationshipEMRServerlessApplicationUsesKMSKey = "emr_serverless_application_uses_kms_key"
	// RelationshipEMRStudioInVPC records the VPC an EMR Studio is attached to.
	RelationshipEMRStudioInVPC = "emr_studio_in_vpc"
	// RelationshipEMRStudioUsesSubnet records an EC2 subnet an EMR Studio is
	// attached to.
	RelationshipEMRStudioUsesSubnet = "emr_studio_uses_subnet"
	// RelationshipEMRStudioUsesSecurityGroup records an EC2 security group an
	// EMR Studio uses (engine and workspace security groups).
	RelationshipEMRStudioUsesSecurityGroup = "emr_studio_uses_security_group"
	// RelationshipEMRStudioUsesIAMRole records an IAM role an EMR Studio uses
	// (service role and user role).
	RelationshipEMRStudioUsesIAMRole = "emr_studio_uses_iam_role"
	// RelationshipEMRStudioUsesKMSKey records the KMS key an EMR Studio uses
	// for workspace encryption.
	RelationshipEMRStudioUsesKMSKey = "emr_studio_uses_kms_key"
	// RelationshipEMRStudioHasSessionMapping records session-mapping membership
	// on an EMR Studio.
	RelationshipEMRStudioHasSessionMapping = "emr_studio_has_session_mapping"
)

// Analytics-category constants consolidated from constants_glue.go (#6696).

const (
	// ServiceGlue identifies the regional AWS Glue metadata-only scan slice
	// covering Data Catalog, crawlers, jobs, triggers, workflows, and
	// connections.
	ServiceGlue = "glue"
)

const (
	// ResourceTypeGlueDatabase identifies an AWS Glue Data Catalog database
	// metadata resource.
	ResourceTypeGlueDatabase = "aws_glue_database"
	// ResourceTypeGlueTable identifies an AWS Glue Data Catalog table metadata
	// resource. Column statistics with sample values stay outside the contract.
	ResourceTypeGlueTable = "aws_glue_table"
	// ResourceTypeGlueCrawler identifies an AWS Glue crawler metadata resource.
	ResourceTypeGlueCrawler = "aws_glue_crawler"
	// ResourceTypeGlueJob identifies an AWS Glue job metadata resource. Job
	// script bodies and default-argument secret-shaped values stay outside the
	// contract.
	ResourceTypeGlueJob = "aws_glue_job"
	// ResourceTypeGlueTrigger identifies an AWS Glue trigger metadata resource.
	ResourceTypeGlueTrigger = "aws_glue_trigger"
	// ResourceTypeGlueWorkflow identifies an AWS Glue workflow metadata
	// resource.
	ResourceTypeGlueWorkflow = "aws_glue_workflow"
	// ResourceTypeGlueConnection identifies an AWS Glue connection metadata
	// resource. Connection properties such as passwords and credential-bearing
	// JDBC URLs stay outside the contract.
	ResourceTypeGlueConnection = "aws_glue_connection"
)

const (
	// RelationshipGlueTableInDatabase records a Glue Data Catalog table's
	// membership in a database.
	RelationshipGlueTableInDatabase = "glue_table_in_database"
	// RelationshipGlueTableStoredAtS3Location records a Glue table's reported
	// S3 storage location when the location parses as `s3://bucket/key`.
	RelationshipGlueTableStoredAtS3Location = "glue_table_stored_at_s3_location"
	// RelationshipGlueCrawlerTargetsDatabase records a Glue crawler's reported
	// target database when AWS reports the database name.
	RelationshipGlueCrawlerTargetsDatabase = "glue_crawler_targets_database"
	// RelationshipGlueCrawlerUsesIAMRole records a Glue crawler's reported IAM
	// role dependency.
	RelationshipGlueCrawlerUsesIAMRole = "glue_crawler_uses_iam_role"
	// RelationshipGlueJobUsesIAMRole records a Glue job's reported IAM role
	// dependency.
	RelationshipGlueJobUsesIAMRole = "glue_job_uses_iam_role"
	// RelationshipGlueTriggerInvokesJob records a Glue trigger's reported
	// action targeting a job by name.
	RelationshipGlueTriggerInvokesJob = "glue_trigger_invokes_job"
)

// Analytics-category constants consolidated from constants_kinesis.go (#6696).

const (
	// ServiceKinesis identifies the regional Amazon Kinesis metadata-only scan
	// slice. The slice covers Kinesis Data Streams, Kinesis Data Firehose
	// delivery streams, and Kinesis Video Streams under one service_kind.
	ServiceKinesis = "kinesis"
)

const (
	// ResourceTypeKinesisDataStream identifies a Kinesis Data Streams stream
	// metadata resource. The scanner records shard count, retention, stream
	// mode, and encryption status; it never reads stream records.
	ResourceTypeKinesisDataStream = "aws_kinesis_data_stream"
	// ResourceTypeKinesisFirehoseDeliveryStream identifies a Kinesis Data
	// Firehose delivery stream metadata resource. The scanner records source,
	// destination type, and encryption status; it never persists processing
	// Lambda bodies or destination secret material.
	ResourceTypeKinesisFirehoseDeliveryStream = "aws_kinesis_firehose_delivery_stream"
	// ResourceTypeKinesisVideoStream identifies a Kinesis Video Streams stream
	// metadata resource. The scanner records status, KMS key, and data
	// retention; it never reads media fragments.
	ResourceTypeKinesisVideoStream = "aws_kinesis_video_stream"
)

const (
	// ResourceTypeFirehoseHTTPEndpoint identifies a generic HTTP endpoint
	// Firehose destination keyed by its reported URL. The scanner never
	// persists the endpoint access key.
	ResourceTypeFirehoseHTTPEndpoint = "aws_firehose_http_endpoint"
	// ResourceTypeSplunkEndpoint identifies a Splunk HEC endpoint Firehose
	// destination keyed by its reported endpoint URL. The scanner never
	// persists the HEC token.
	ResourceTypeSplunkEndpoint = "aws_splunk_endpoint"
)

const (
	// RelationshipKinesisDataStreamUsesKMSKey records a Kinesis data stream's
	// reported server-side encryption KMS key dependency. The relationship
	// emits only when AWS reports the customer-managed KMS key ARN.
	RelationshipKinesisDataStreamUsesKMSKey = "kinesis_data_stream_uses_kms_key"
	// RelationshipKinesisVideoStreamUsesKMSKey records a Kinesis video stream's
	// reported KMS key dependency. The relationship emits only when AWS reports
	// the customer-managed KMS key ARN.
	RelationshipKinesisVideoStreamUsesKMSKey = "kinesis_video_stream_uses_kms_key"
	// RelationshipFirehoseDeliveryStreamUsesIAMRole records a Firehose delivery
	// stream's reported service IAM role dependency for a destination.
	RelationshipFirehoseDeliveryStreamUsesIAMRole = "firehose_delivery_stream_uses_iam_role"
	// RelationshipFirehoseDeliveryStreamUsesLambdaTransform records a Firehose
	// delivery stream's reported data-transformation Lambda function. Only the
	// Lambda ARN is recorded; the processing configuration body is never
	// persisted.
	RelationshipFirehoseDeliveryStreamUsesLambdaTransform = "firehose_delivery_stream_uses_lambda_transform"
	// RelationshipFirehoseDeliveryStreamDeliversToS3 records a Firehose delivery
	// stream's reported S3 bucket destination.
	RelationshipFirehoseDeliveryStreamDeliversToS3 = "firehose_delivery_stream_delivers_to_s3"
	// RelationshipFirehoseDeliveryStreamDeliversToRedshift records a Firehose
	// delivery stream's reported Redshift cluster destination keyed by the
	// cluster identifier parsed from the JDBC URL host.
	RelationshipFirehoseDeliveryStreamDeliversToRedshift = "firehose_delivery_stream_delivers_to_redshift"
	// RelationshipFirehoseDeliveryStreamDeliversToOpenSearch records a Firehose
	// delivery stream's reported OpenSearch domain destination.
	RelationshipFirehoseDeliveryStreamDeliversToOpenSearch = "firehose_delivery_stream_delivers_to_opensearch"
	// RelationshipFirehoseDeliveryStreamDeliversToSplunk records a Firehose
	// delivery stream's reported Splunk HEC endpoint destination keyed by the
	// endpoint URL. The HEC token is never persisted.
	RelationshipFirehoseDeliveryStreamDeliversToSplunk = "firehose_delivery_stream_delivers_to_splunk"
	// RelationshipFirehoseDeliveryStreamDeliversToHTTPEndpoint records a
	// Firehose delivery stream's reported generic HTTP endpoint destination
	// keyed by the endpoint URL. The endpoint access key is never persisted.
	RelationshipFirehoseDeliveryStreamDeliversToHTTPEndpoint = "firehose_delivery_stream_delivers_to_http_endpoint"
)

// Analytics-category constants consolidated from constants_kinesisanalyticsv2.go (#6696).

const (
	// ServiceKinesisAnalyticsV2 identifies the regional Amazon Managed Service
	// for Apache Flink (Kinesis Data Analytics v2) metadata-only scan slice. The
	// scanner reads application control-plane metadata through the
	// kinesisanalyticsv2 management APIs (ListApplications, DescribeApplication,
	// ListApplicationVersions, ListApplicationSnapshots, ListTagsForResource) and
	// never reads or persists application code bodies, SQL text, run-configuration
	// secrets, or processing payloads, and never mutates application state.
	ServiceKinesisAnalyticsV2 = "kinesisanalyticsv2"
)

const (
	// ResourceTypeManagedFlinkApplication identifies an Amazon Managed Service
	// for Apache Flink (Kinesis Data Analytics v2) application metadata resource.
	// The scanner emits identity, runtime environment, application mode, status,
	// parallelism and auto-scaling configuration, snapshot/logging posture,
	// version counters, and lifecycle timestamps only. Application code bodies,
	// SQL text, environment property values, and run-configuration content stay
	// outside the contract.
	ResourceTypeManagedFlinkApplication = "aws_kinesisanalyticsv2_application"
)

const (
	// RelationshipManagedFlinkApplicationReadsFromKinesisStream records a Managed
	// Flink application's SQL input source that is a Kinesis data stream. The
	// target is keyed by the reported data stream ARN, matching the resource_id
	// the kinesis scanner publishes for its data streams.
	RelationshipManagedFlinkApplicationReadsFromKinesisStream = "managed_flink_application_reads_from_kinesis_stream"
	// RelationshipManagedFlinkApplicationWritesToKinesisStream records a Managed
	// Flink application's SQL output destination that is a Kinesis data stream.
	// The target is keyed by the reported data stream ARN.
	RelationshipManagedFlinkApplicationWritesToKinesisStream = "managed_flink_application_writes_to_kinesis_stream"
	// RelationshipManagedFlinkApplicationReadsFromFirehoseStream records a Managed
	// Flink application's SQL input source that is a Firehose delivery stream. The
	// target is keyed by the reported delivery stream ARN, matching the
	// resource_id the firehose scanner publishes.
	RelationshipManagedFlinkApplicationReadsFromFirehoseStream = "managed_flink_application_reads_from_firehose_stream"
	// RelationshipManagedFlinkApplicationWritesToFirehoseStream records a Managed
	// Flink application's SQL output destination that is a Firehose delivery
	// stream. The target is keyed by the reported delivery stream ARN.
	RelationshipManagedFlinkApplicationWritesToFirehoseStream = "managed_flink_application_writes_to_firehose_stream"
	// RelationshipManagedFlinkApplicationUsesS3CodeBucket records a Managed Flink
	// application's S3 code-content bucket dependency. AWS reports the bucket ARN,
	// matching the resource_id the S3 scanner publishes. Only the bucket identity
	// and object key are recorded; the application code body is never read.
	RelationshipManagedFlinkApplicationUsesS3CodeBucket = "managed_flink_application_uses_s3_code_bucket"
	// RelationshipManagedFlinkApplicationUsesSubnet records a Managed Flink
	// application's VPC configuration subnet placement. The target is keyed by the
	// bare subnet id (subnet-…), matching how the EC2 scanner publishes subnets.
	RelationshipManagedFlinkApplicationUsesSubnet = "managed_flink_application_uses_subnet"
	// RelationshipManagedFlinkApplicationUsesSecurityGroup records a Managed Flink
	// application's VPC configuration security group. The target is keyed by the
	// bare security group id (sg-…), matching how the EC2 scanner publishes
	// security groups.
	RelationshipManagedFlinkApplicationUsesSecurityGroup = "managed_flink_application_uses_security_group"
	// RelationshipManagedFlinkApplicationUsesIAMRole records a Managed Flink
	// application's service execution IAM role. AWS reports the role ARN, matching
	// the resource_id the IAM scanner publishes for its roles.
	RelationshipManagedFlinkApplicationUsesIAMRole = "managed_flink_application_uses_iam_role"
	// RelationshipManagedFlinkApplicationLogsToCloudWatchLogGroup records a Managed
	// Flink application's CloudWatch logging destination. AWS reports a log stream
	// ARN whose log-group form (the trailing `:*` wildcard trimmed) matches the
	// non-wildcard log group ARN the cloudwatchlogs scanner publishes.
	RelationshipManagedFlinkApplicationLogsToCloudWatchLogGroup = "managed_flink_application_logs_to_cloudwatch_log_group"
)

// Analytics-category constants consolidated from constants_opensearch.go (#6696).

const (
	// ServiceOpenSearch identifies the regional Amazon OpenSearch metadata scan
	// slice. It covers OpenSearch Service provisioned domains, OpenSearch custom
	// packages, and OpenSearch Serverless collections, security configurations,
	// and managed VPC endpoints under one service_kind.
	ServiceOpenSearch = "opensearch"
)

const (
	// ResourceTypeOpenSearchDomain identifies an OpenSearch Service provisioned
	// domain. Domain endpoint contents and master user passwords are never
	// persisted.
	ResourceTypeOpenSearchDomain = "aws_opensearch_domain"
	// ResourceTypeOpenSearchPackage identifies an OpenSearch custom package's
	// metadata (name, type, status); the package body is never persisted.
	ResourceTypeOpenSearchPackage = "aws_opensearch_package"
	// ResourceTypeOpenSearchServerlessCollection identifies an OpenSearch
	// Serverless collection. Collection saved-object bodies and indexed data are
	// never read or persisted.
	ResourceTypeOpenSearchServerlessCollection = "aws_opensearch_serverless_collection"
	// ResourceTypeOpenSearchServerlessSecurityConfig identifies an OpenSearch
	// Serverless security configuration summary (id, type, version); SAML
	// metadata XML and IAM Identity Center secrets stay outside the scan slice.
	ResourceTypeOpenSearchServerlessSecurityConfig = "aws_opensearch_serverless_security_config"
	// ResourceTypeOpenSearchServerlessVPCEndpoint identifies an OpenSearch
	// Serverless managed VPC interface endpoint.
	ResourceTypeOpenSearchServerlessVPCEndpoint = "aws_opensearch_serverless_vpc_endpoint"
)

const (
	// RelationshipOpenSearchDomainInVPC records an OpenSearch Service domain's
	// reported VPC placement. The target is the EC2-owned aws_ec2_vpc identity.
	RelationshipOpenSearchDomainInVPC = "opensearch_domain_in_vpc"
	// RelationshipOpenSearchDomainInSubnet records an OpenSearch Service domain's
	// reported subnet placement. The target is the EC2-owned aws_ec2_subnet
	// identity.
	RelationshipOpenSearchDomainInSubnet = "opensearch_domain_in_subnet"
	// RelationshipOpenSearchDomainUsesSecurityGroup records an OpenSearch Service
	// domain's reported security group. The target is the EC2-owned
	// aws_ec2_security_group identity.
	RelationshipOpenSearchDomainUsesSecurityGroup = "opensearch_domain_uses_security_group"
	// RelationshipOpenSearchDomainUsesKMSKey records an OpenSearch Service
	// domain's reported at-rest encryption KMS key. The target is the KMS-owned
	// aws_kms_key identity.
	RelationshipOpenSearchDomainUsesKMSKey = "opensearch_domain_uses_kms_key"
	// RelationshipOpenSearchDomainUsesIAMRole records an IAM role ARN referenced
	// by an OpenSearch Service domain's master-user mapping or resource access
	// policy. The target is the IAM-owned aws_iam_role identity.
	RelationshipOpenSearchDomainUsesIAMRole = "opensearch_domain_uses_iam_role"
	// RelationshipOpenSearchPackageAssociatedWithDomain records a custom package
	// associated with an OpenSearch Service domain. The target is the
	// OpenSearch-owned aws_opensearch_domain identity.
	RelationshipOpenSearchPackageAssociatedWithDomain = "opensearch_package_associated_with_domain"
	// RelationshipOpenSearchCollectionUsesKMSKey records an OpenSearch Serverless
	// collection's reported at-rest encryption KMS key. The target is the
	// KMS-owned aws_kms_key identity.
	RelationshipOpenSearchCollectionUsesKMSKey = "opensearch_collection_uses_kms_key"
)

// Analytics-category constants consolidated from constants_opensearchserverless.go (#6696).

const (
	// ServiceOpenSearchServerless identifies the regional Amazon OpenSearch
	// Serverless (aoss) metadata-only scan slice. The scanner reads control-plane
	// describe/list APIs (ListCollections/BatchGetCollection,
	// ListSecurityPolicies/GetSecurityPolicy,
	// ListVpcEndpoints/BatchGetVpcEndpoint, ListTagsForResource) and emits
	// collection, security-policy, and managed VPC-endpoint metadata. It never
	// reads the OpenSearch HTTP data plane (index, search, bulk, document APIs),
	// never persists access-policy or security-policy document bodies, and never
	// mutates Serverless state.
	//
	// It is a distinct service_kind from ServiceOpenSearch: the opensearch
	// scanner surfaces Serverless collections only as a side slice of the broader
	// OpenSearch Service domain scan, while this scanner is the first-class
	// Serverless owner that additionally emits security policies as resources and
	// the managed VPC endpoint's VPC/subnet/security-group network edges, mirroring
	// how the firehose scanner coexists with the kinesis Firehose side slice.
	ServiceOpenSearchServerless = "opensearchserverless"
)

const (
	// ResourceTypeOpenSearchServerlessAOSSCollection identifies an Amazon
	// OpenSearch Serverless collection (aoss). The scanner emits identity, type,
	// status, and standby-replicas metadata only. Collection endpoints, dashboard
	// endpoints, saved-object bodies, and indexed data are never read or
	// persisted. The type string is intentionally distinct from the opensearch
	// scanner's aws_opensearch_serverless_collection so the two slices do not
	// collide on graph identity.
	ResourceTypeOpenSearchServerlessAOSSCollection = "aws_opensearchserverless_collection"
	// ResourceTypeOpenSearchServerlessSecurityPolicy identifies an Amazon
	// OpenSearch Serverless security policy summary (name, type, version,
	// description, timestamps). The policy document body is never persisted; only
	// the KMS key reference parsed from an encryption policy is surfaced as a
	// graph edge.
	ResourceTypeOpenSearchServerlessSecurityPolicy = "aws_opensearchserverless_security_policy"
	// ResourceTypeOpenSearchServerlessAOSSVPCEndpoint identifies an Amazon
	// OpenSearch Serverless managed interface VPC endpoint (name, id, status, VPC,
	// subnet, and security-group references). The type string is intentionally
	// distinct from the opensearch scanner's aws_opensearch_serverless_vpc_endpoint.
	ResourceTypeOpenSearchServerlessAOSSVPCEndpoint = "aws_opensearchserverless_vpc_endpoint"
)

const (
	// RelationshipOpenSearchServerlessCollectionUsesKMSKey records the KMS
	// customer-managed encryption key an OpenSearch Serverless collection is
	// assigned through its matching encryption security policy. The target is the
	// KMS-owned aws_kms_key identity keyed by the key ARN the encryption policy
	// document reports. AWS-owned-key policies emit no edge.
	RelationshipOpenSearchServerlessCollectionUsesKMSKey = "opensearchserverless_collection_uses_kms_key"
	// RelationshipOpenSearchServerlessVPCEndpointInVPC records an OpenSearch
	// Serverless managed VPC endpoint's reported VPC placement. The target is the
	// EC2-owned aws_ec2_vpc identity keyed by the bare vpc-… id.
	RelationshipOpenSearchServerlessVPCEndpointInVPC = "opensearchserverless_vpc_endpoint_in_vpc"
	// RelationshipOpenSearchServerlessVPCEndpointInSubnet records an OpenSearch
	// Serverless managed VPC endpoint's reported subnet placement. The target is
	// the EC2-owned aws_ec2_subnet identity keyed by the bare subnet-… id.
	RelationshipOpenSearchServerlessVPCEndpointInSubnet = "opensearchserverless_vpc_endpoint_in_subnet"
	// RelationshipOpenSearchServerlessVPCEndpointUsesSecurityGroup records an
	// OpenSearch Serverless managed VPC endpoint's reported security-group
	// attachment. The target is the EC2-owned aws_ec2_security_group identity
	// keyed by the bare sg-… id.
	RelationshipOpenSearchServerlessVPCEndpointUsesSecurityGroup = "opensearchserverless_vpc_endpoint_uses_security_group"
)
