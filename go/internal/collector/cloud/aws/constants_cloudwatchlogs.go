// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

const (
	// ServiceCloudWatchLogs identifies the regional Amazon CloudWatch Logs log
	// group metadata scan slice.
	ServiceCloudWatchLogs = "cloudwatchlogs"
)

const (
	// ResourceTypeCloudWatchLogsLogGroup identifies a CloudWatch Logs log group
	// metadata resource.
	ResourceTypeCloudWatchLogsLogGroup = "aws_cloudwatch_logs_log_group"
)

const (
	// RelationshipCloudWatchLogsLogGroupUsesKMSKey records a CloudWatch Logs
	// log group's reported KMS key dependency.
	RelationshipCloudWatchLogsLogGroupUsesKMSKey = "cloudwatch_logs_log_group_uses_kms_key"
)
