// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package datasync

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/cloud/aws"
)

// taskSourceLocationRelationship records the source location a task reads from.
// The DataSync API reports both the task ARN and the source location ARN
// directly, so the edge joins the location resource the same scanner publishes
// by ARN with no synthesis required.
func taskSourceLocationRelationship(boundary aws.Boundary, task Task) *aws.RelationshipObservation {
	taskARN := strings.TrimSpace(task.ARN)
	locationARN := strings.TrimSpace(task.SourceLocationARN)
	if !isARN(taskARN) || !isARN(locationARN) {
		return nil
	}
	return &aws.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: aws.RelationshipDataSyncTaskSourceLocation,
		SourceResourceID: taskARN,
		SourceARN:        taskARN,
		TargetResourceID: locationARN,
		TargetARN:        locationARN,
		TargetType:       aws.ResourceTypeDataSyncLocation,
		SourceRecordID:   taskARN + "->" + aws.RelationshipDataSyncTaskSourceLocation + ":" + locationARN,
	}
}

// taskDestinationLocationRelationship records the destination location a task
// writes to. Both ARNs come from the API directly.
func taskDestinationLocationRelationship(boundary aws.Boundary, task Task) *aws.RelationshipObservation {
	taskARN := strings.TrimSpace(task.ARN)
	locationARN := strings.TrimSpace(task.DestinationLocationARN)
	if !isARN(taskARN) || !isARN(locationARN) {
		return nil
	}
	return &aws.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: aws.RelationshipDataSyncTaskDestinationLocation,
		SourceResourceID: taskARN,
		SourceARN:        taskARN,
		TargetResourceID: locationARN,
		TargetARN:        locationARN,
		TargetType:       aws.ResourceTypeDataSyncLocation,
		SourceRecordID:   taskARN + "->" + aws.RelationshipDataSyncTaskDestinationLocation + ":" + locationARN,
	}
}

// taskLogGroupRelationship records the CloudWatch log group a task writes
// transfer logs to. DataSync reports the plain log-group ARN; the CloudWatch
// Logs scanner publishes its resource_id as the log-group ARN with any trailing
// `:*` wildcard trimmed, so the edge is keyed to match that exact form.
func taskLogGroupRelationship(boundary aws.Boundary, task Task) *aws.RelationshipObservation {
	taskARN := strings.TrimSpace(task.ARN)
	logGroupARN := trimLogGroupWildcardARN(task.CloudWatchLogGroupARN)
	if !isARN(taskARN) || !isARN(logGroupARN) {
		return nil
	}
	return &aws.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: aws.RelationshipDataSyncTaskLogsToCloudWatch,
		SourceResourceID: taskARN,
		SourceARN:        taskARN,
		TargetResourceID: logGroupARN,
		TargetARN:        logGroupARN,
		TargetType:       aws.ResourceTypeCloudWatchLogsLogGroup,
		SourceRecordID:   taskARN + "->" + aws.RelationshipDataSyncTaskLogsToCloudWatch + ":" + logGroupARN,
	}
}

// locationS3Relationship records the S3 bucket backing an S3 location. The
// DataSync S3 location reports only the bucket name (parsed from the location
// URI), not a bucket ARN, so the scanner synthesizes the bucket ARN. The S3
// bucket scanner publishes its resource_id as `arn:<partition>:s3:::<bucket>`,
// so the synthesized ARN must inherit the scan boundary's partition or the edge
// dangles in GovCloud and China.
func locationS3Relationship(boundary aws.Boundary, location Location) *aws.RelationshipObservation {
	locationARN := strings.TrimSpace(location.ARN)
	bucket := strings.TrimSpace(location.S3BucketName)
	if !isARN(locationARN) || bucket == "" {
		return nil
	}
	bucketARN := "arn:" + partition(boundary) + ":s3:::" + bucket
	return &aws.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: aws.RelationshipDataSyncLocationTargetsS3Bucket,
		SourceResourceID: locationARN,
		SourceARN:        locationARN,
		TargetResourceID: bucketARN,
		TargetARN:        bucketARN,
		TargetType:       aws.ResourceTypeS3Bucket,
		Attributes:       map[string]any{"bucket": bucket},
		SourceRecordID:   locationARN + "->" + aws.RelationshipDataSyncLocationTargetsS3Bucket + ":" + bucketARN,
	}
}

// locationEFSRelationship records the EFS file system backing an EFS location.
// The DataSync EFS location reports only the file system id (parsed from the
// `region.fs-xxxx` URI global id), so the scanner synthesizes the file system
// ARN. The EFS scanner publishes its resource_id as the file system ARN
// `arn:<partition>:elasticfilesystem:<region>:<account>:file-system/<fs-id>`,
// so the synthesized ARN inherits the boundary partition, region, and account
// to join that node.
func locationEFSRelationship(boundary aws.Boundary, location Location) *aws.RelationshipObservation {
	locationARN := strings.TrimSpace(location.ARN)
	fileSystemID := strings.TrimSpace(location.EFSFileSystemID)
	if !isARN(locationARN) || fileSystemID == "" {
		return nil
	}
	account := strings.TrimSpace(boundary.AccountID)
	region := strings.TrimSpace(boundary.Region)
	if account == "" || region == "" {
		return nil
	}
	fileSystemARN := "arn:" + partition(boundary) + ":elasticfilesystem:" + region + ":" + account + ":file-system/" + fileSystemID
	return &aws.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: aws.RelationshipDataSyncLocationTargetsEFSFileSystem,
		SourceResourceID: locationARN,
		SourceARN:        locationARN,
		TargetResourceID: fileSystemARN,
		TargetARN:        fileSystemARN,
		TargetType:       aws.ResourceTypeEFSFileSystem,
		Attributes:       map[string]any{"file_system_id": fileSystemID},
		SourceRecordID:   locationARN + "->" + aws.RelationshipDataSyncLocationTargetsEFSFileSystem + ":" + fileSystemARN,
	}
}

// locationFSxRelationship records the FSx file system backing an FSx location.
// FSx for NetApp ONTAP locations report the file system ARN directly; the other
// FSx flavors report only the file system id (parsed from the URI), so the
// scanner synthesizes the ARN. The FSx scanner publishes its resource_id as the
// file system ARN `arn:<partition>:fsx:<region>:<account>:file-system/<fs-id>`,
// so the synthesized ARN inherits the boundary partition, region, and account.
func locationFSxRelationship(boundary aws.Boundary, location Location) *aws.RelationshipObservation {
	locationARN := strings.TrimSpace(location.ARN)
	if !isARN(locationARN) {
		return nil
	}
	fileSystemARN := strings.TrimSpace(location.FSxFileSystemARN)
	fileSystemID := strings.TrimSpace(location.FSxFileSystemID)
	attributes := map[string]any{}
	if !isARN(fileSystemARN) {
		if fileSystemID == "" {
			return nil
		}
		account := strings.TrimSpace(boundary.AccountID)
		region := strings.TrimSpace(boundary.Region)
		if account == "" || region == "" {
			return nil
		}
		fileSystemARN = "arn:" + partition(boundary) + ":fsx:" + region + ":" + account + ":file-system/" + fileSystemID
	}
	if fileSystemID != "" {
		attributes["file_system_id"] = fileSystemID
	}
	if len(attributes) == 0 {
		attributes = nil
	}
	return &aws.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: aws.RelationshipDataSyncLocationTargetsFSxFileSystem,
		SourceResourceID: locationARN,
		SourceARN:        locationARN,
		TargetResourceID: fileSystemARN,
		TargetARN:        fileSystemARN,
		TargetType:       aws.ResourceTypeFSxFileSystem,
		Attributes:       attributes,
		SourceRecordID:   locationARN + "->" + aws.RelationshipDataSyncLocationTargetsFSxFileSystem + ":" + fileSystemARN,
	}
}

// locationRoleRelationship records the IAM role a location uses to access its
// backing AWS storage (S3 bucket access role, EFS file-system access role). The
// role ARN comes from the location configuration directly, so it joins the IAM
// role node the IAM scanner publishes by ARN with no synthesis required.
func locationRoleRelationship(boundary aws.Boundary, location Location) *aws.RelationshipObservation {
	locationARN := strings.TrimSpace(location.ARN)
	roleARN := strings.TrimSpace(location.IAMRoleARN)
	if !isARN(locationARN) || !isARN(roleARN) {
		return nil
	}
	return &aws.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: aws.RelationshipDataSyncLocationUsesIAMRole,
		SourceResourceID: locationARN,
		SourceARN:        locationARN,
		TargetResourceID: roleARN,
		TargetARN:        roleARN,
		TargetType:       aws.ResourceTypeIAMRole,
		SourceRecordID:   locationARN + "->" + aws.RelationshipDataSyncLocationUsesIAMRole + ":" + roleARN,
	}
}

// partition returns the AWS partition for the scan boundary's region — aws,
// aws-cn, or aws-us-gov. DataSync task, location, and agent ARNs come from the
// API and are used directly, but S3/EFS/FSx backing-resource ARNs are
// synthesized from bare identifiers in the location config, so the boundary
// region is the partition source; hardcoding the commercial partition would
// dangle the location->storage edges in GovCloud and China.
func partition(boundary aws.Boundary) string {
	region := strings.TrimSpace(boundary.Region)
	switch {
	case strings.HasPrefix(region, "us-gov-"):
		return "aws-us-gov"
	case strings.HasPrefix(region, "cn-"):
		return "aws-cn"
	default:
		return "aws"
	}
}
