// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

const (
	// ServiceCloudTrail identifies the regional AWS CloudTrail metadata scan
	// slice. CloudTrail is the audit-config service; the scanner emits trail
	// and Lake event-data-store configuration only. CloudTrail event payloads
	// are the protected data class and must never be read through this
	// service kind.
	ServiceCloudTrail = "cloudtrail"
)

const (
	// ResourceTypeCloudTrailChannel identifies a CloudTrail channel metadata
	// resource. The scanner emits channel identity and destination metadata
	// only; event payloads are not associated with this resource.
	ResourceTypeCloudTrailChannel = "aws_cloudtrail_channel"
	// ResourceTypeCloudTrailDashboardConfig identifies a CloudTrail Lake
	// dashboard configuration metadata resource. The scanner emits dashboard
	// identity and status only; widget query bodies and result rows are not
	// part of the contract.
	ResourceTypeCloudTrailDashboardConfig = "aws_cloudtrail_dashboard_config"
	// ResourceTypeCloudTrailEventDataStore identifies a CloudTrail Lake event
	// data store metadata resource. The scanner emits store identity,
	// retention, and selector-count summary only; advanced event selector
	// bodies, Lake query strings, and query result rows are not part of the
	// contract.
	ResourceTypeCloudTrailEventDataStore = "aws_cloudtrail_event_data_store"
	// ResourceTypeCloudTrailTrail identifies a CloudTrail trail metadata
	// resource. The scanner emits trail configuration only; the audit event
	// payload itself is the protected data class and is never persisted by
	// this scanner.
	ResourceTypeCloudTrailTrail = "aws_cloudtrail_trail"
)

const (
	// RelationshipCloudTrailEventDataStoreUsesKMSKey records a CloudTrail
	// event data store's reported KMS key dependency.
	RelationshipCloudTrailEventDataStoreUsesKMSKey = "cloudtrail_event_data_store_uses_kms_key"
	// RelationshipCloudTrailTrailLogsToCloudWatchLogs records a CloudTrail
	// trail's reported CloudWatch Logs log group destination.
	RelationshipCloudTrailTrailLogsToCloudWatchLogs = "cloudtrail_trail_logs_to_cloudwatch_logs"
	// RelationshipCloudTrailTrailLogsToS3Bucket records a CloudTrail trail's
	// reported S3 destination bucket.
	RelationshipCloudTrailTrailLogsToS3Bucket = "cloudtrail_trail_logs_to_s3_bucket"
	// RelationshipCloudTrailTrailNotifiesSNSTopic records a CloudTrail trail's
	// reported SNS topic delivery target.
	RelationshipCloudTrailTrailNotifiesSNSTopic = "cloudtrail_trail_notifies_sns_topic"
	// RelationshipCloudTrailTrailUsesKMSKey records a CloudTrail trail's
	// reported KMS key dependency.
	RelationshipCloudTrailTrailUsesKMSKey = "cloudtrail_trail_uses_kms_key"
)
