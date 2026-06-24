// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

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
