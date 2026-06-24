// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

const (
	// ServiceDataSync identifies the regional AWS DataSync metadata-only scan
	// slice covering transfer tasks, transfer locations, and on-premises
	// agents.
	ServiceDataSync = "datasync"
)

const (
	// ResourceTypeDataSyncTask identifies an AWS DataSync transfer task metadata
	// resource. The task carries its source and destination location ARNs, an
	// optional CloudWatch log group ARN, a schedule expression, and status. Data
	// transferred between locations is never read.
	ResourceTypeDataSyncTask = "aws_datasync_task"
	// ResourceTypeDataSyncLocation identifies an AWS DataSync location metadata
	// resource. The location carries its type (S3, EFS, FSx, NFS, SMB, object
	// storage, HDFS, Azure Blob) and a host/path-only URI. Object contents,
	// access keys, and storage credentials are never read.
	ResourceTypeDataSyncLocation = "aws_datasync_location"
	// ResourceTypeDataSyncAgent identifies an AWS DataSync agent metadata
	// resource (the on-premises or in-cloud appliance that moves data). The
	// agent carries its status, endpoint type, and platform version.
	ResourceTypeDataSyncAgent = "aws_datasync_agent"
)

const (
	// RelationshipDataSyncTaskSourceLocation records the source location a
	// DataSync task reads from, keyed by the location ARN.
	RelationshipDataSyncTaskSourceLocation = "datasync_task_source_location"
	// RelationshipDataSyncTaskDestinationLocation records the destination
	// location a DataSync task writes to, keyed by the location ARN.
	RelationshipDataSyncTaskDestinationLocation = "datasync_task_destination_location"
	// RelationshipDataSyncTaskLogsToCloudWatch records the CloudWatch log group
	// a DataSync task writes transfer logs to, keyed by the log group ARN.
	RelationshipDataSyncTaskLogsToCloudWatch = "datasync_task_logs_to_cloudwatch"
	// RelationshipDataSyncLocationTargetsS3Bucket records the S3 bucket a
	// DataSync S3 location is backed by, keyed by the synthesized bucket ARN.
	RelationshipDataSyncLocationTargetsS3Bucket = "datasync_location_targets_s3_bucket"
	// RelationshipDataSyncLocationTargetsEFSFileSystem records the EFS file
	// system a DataSync EFS location is backed by, keyed by the synthesized file
	// system ARN.
	RelationshipDataSyncLocationTargetsEFSFileSystem = "datasync_location_targets_efs_file_system"
	// RelationshipDataSyncLocationTargetsFSxFileSystem records the FSx file
	// system a DataSync FSx location is backed by, keyed by the FSx file system
	// ARN.
	RelationshipDataSyncLocationTargetsFSxFileSystem = "datasync_location_targets_fsx_file_system"
	// RelationshipDataSyncLocationUsesIAMRole records the IAM role a DataSync
	// location uses to access its backing AWS storage, keyed by the role ARN.
	RelationshipDataSyncLocationUsesIAMRole = "datasync_location_uses_iam_role"
)
