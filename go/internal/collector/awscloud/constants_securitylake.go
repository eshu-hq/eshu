// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

const (
	// ServiceSecurityLake identifies the regional Amazon Security Lake
	// metadata-only scan slice. The scanner reads control-plane configuration
	// through the Security Lake management APIs (ListDataLakes, ListLogSources,
	// ListSubscribers) and never reads ingested security log records, object
	// contents, subscriber credentials, or any data-plane payload, and never
	// mutates Security Lake state.
	ServiceSecurityLake = "securitylake"
)

const (
	// ResourceTypeSecurityLakeDataLake identifies an Amazon Security Lake data
	// lake configuration in one Region. The scanner emits the data lake ARN, the
	// backing S3 bucket ARN reference, the KMS encryption key reference, the
	// create/update status, and lifecycle retention summary only.
	ResourceTypeSecurityLakeDataLake = "aws_securitylake_data_lake"
	// ResourceTypeSecurityLakeLogSource identifies one Amazon Security Lake log
	// source (an AWS-native source name/version or a third-party custom source
	// name) collected for an account and Region. The scanner emits the source
	// identity and collection scope only, never the ingested log records.
	ResourceTypeSecurityLakeLogSource = "aws_securitylake_log_source"
	// ResourceTypeSecurityLakeSubscriber identifies an Amazon Security Lake
	// subscriber. The scanner emits the subscriber ARN, id, name, access types,
	// status, principal account id, and resolvable role/bucket references only.
	// The subscriber external id, endpoint, and any credential material are never
	// persisted.
	ResourceTypeSecurityLakeSubscriber = "aws_securitylake_subscriber"
)

const (
	// RelationshipSecurityLakeDataLakeUsesS3Bucket records a Security Lake data
	// lake's backing S3 bucket. AWS reports the bucket ARN, which matches the S3
	// scanner's published bucket resource_id, so the edge joins the bucket node.
	RelationshipSecurityLakeDataLakeUsesS3Bucket = "securitylake_data_lake_uses_s3_bucket"
	// RelationshipSecurityLakeDataLakeUsesKMSKey records a Security Lake data
	// lake's reported KMS encryption key dependency. The target is keyed by the
	// reported key id or ARN, matching the KMS scanner's published key
	// resource_id.
	RelationshipSecurityLakeDataLakeUsesKMSKey = "securitylake_data_lake_uses_kms_key"
	// RelationshipSecurityLakeDataLakeRegisteredInLakeFormation records that a
	// Security Lake data lake registers its backing S3 bucket with Lake
	// Formation. The target is keyed by the bucket ARN, which matches how the
	// Lake Formation scanner publishes a registered-resource node's resource_id.
	RelationshipSecurityLakeDataLakeRegisteredInLakeFormation = "securitylake_data_lake_registered_in_lake_formation"
	// RelationshipSecurityLakeLogSourceInDataLake records a log source's
	// membership in the Region's data lake. The target is keyed by the data lake
	// ARN the data lake node publishes.
	RelationshipSecurityLakeLogSourceInDataLake = "securitylake_log_source_in_data_lake"
	// RelationshipSecurityLakeLogSourceUsesIAMRole records a third-party custom
	// log source's log-provider IAM role. AWS reports the role ARN, which matches
	// the IAM scanner's published role resource_id.
	RelationshipSecurityLakeLogSourceUsesIAMRole = "securitylake_log_source_uses_iam_role"
	// RelationshipSecurityLakeSubscriberUsesIAMRole records a subscriber's
	// access IAM role. AWS reports the role ARN, which matches the IAM scanner's
	// published role resource_id.
	RelationshipSecurityLakeSubscriberUsesIAMRole = "securitylake_subscriber_uses_iam_role"
	// RelationshipSecurityLakeSubscriberUsesS3Bucket records a subscriber's
	// backing S3 bucket. AWS reports the bucket ARN, which matches the S3
	// scanner's published bucket resource_id.
	RelationshipSecurityLakeSubscriberUsesS3Bucket = "securitylake_subscriber_uses_s3_bucket"
)
