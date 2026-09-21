// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

const (
	// ServiceMWAA identifies the regional Amazon Managed Workflows for Apache
	// Airflow (MWAA) metadata-only scan slice covering environments and their
	// S3, VPC, IAM, KMS, and CloudWatch Logs dependency edges.
	ServiceMWAA = "mwaa"
)

const (
	// ResourceTypeMWAAEnvironment identifies an Amazon MWAA environment metadata
	// resource. Apache Airflow configuration option values, connection strings,
	// and CLI/web login tokens stay outside the scanner contract.
	ResourceTypeMWAAEnvironment = "aws_mwaa_environment"
)

const (
	// RelationshipMWAAEnvironmentUsesS3Bucket records the Amazon S3 bucket that
	// stores an MWAA environment's DAGs and supporting files. The target is the
	// AWS-reported source-bucket ARN.
	RelationshipMWAAEnvironmentUsesS3Bucket = "mwaa_environment_uses_s3_bucket"
	// RelationshipMWAAEnvironmentUsesSubnet records a subnet an MWAA environment
	// is attached to through its VPC network configuration.
	RelationshipMWAAEnvironmentUsesSubnet = "mwaa_environment_uses_subnet"
	// RelationshipMWAAEnvironmentUsesSecurityGroup records a security group an
	// MWAA environment is attached to through its VPC network configuration.
	RelationshipMWAAEnvironmentUsesSecurityGroup = "mwaa_environment_uses_security_group"
	// RelationshipMWAAEnvironmentUsesIAMRole records the IAM execution role an
	// MWAA environment assumes to access Amazon Web Services resources.
	RelationshipMWAAEnvironmentUsesIAMRole = "mwaa_environment_uses_iam_role"
	// RelationshipMWAAEnvironmentUsesKMSKey records the KMS key an MWAA
	// environment uses to encrypt its data at rest.
	RelationshipMWAAEnvironmentUsesKMSKey = "mwaa_environment_uses_kms_key"
	// RelationshipMWAAEnvironmentLogsToCloudWatchLogGroup records a CloudWatch
	// Logs log group an MWAA environment publishes one Airflow log module to.
	RelationshipMWAAEnvironmentLogsToCloudWatchLogGroup = "mwaa_environment_logs_to_cloudwatch_log_group"
)
