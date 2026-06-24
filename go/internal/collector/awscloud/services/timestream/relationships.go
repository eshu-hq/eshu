// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package timestream

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// tableInDatabaseRelationship records a Timestream table's membership in its
// parent database. databaseID is the resource_id the database node publishes
// (its ARN when available), so the edge joins the database node exactly. It
// returns nil when either endpoint identity is missing.
func tableInDatabaseRelationship(
	boundary awscloud.Boundary,
	databaseID string,
	table Table,
) *awscloud.RelationshipObservation {
	tableID := tableResourceID(table)
	databaseID = strings.TrimSpace(databaseID)
	if tableID == "" || databaseID == "" {
		return nil
	}
	targetARN := ""
	if isARN(databaseID) {
		targetARN = databaseID
	}
	return &awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipTimestreamTableInDatabase,
		SourceResourceID: tableID,
		SourceARN:        strings.TrimSpace(table.ARN),
		TargetResourceID: databaseID,
		TargetARN:        targetARN,
		TargetType:       awscloud.ResourceTypeTimestreamDatabase,
		SourceRecordID:   tableID + "->" + awscloud.RelationshipTimestreamTableInDatabase + ":" + databaseID,
	}
}

// databaseKMSRelationship records a Timestream database's reported KMS
// encryption key dependency. AWS reports a key ARN, which matches how the KMS
// scanner publishes its key resource_id (bare id or ARN). It returns nil when
// no key is reported.
func databaseKMSRelationship(boundary awscloud.Boundary, database Database) *awscloud.RelationshipObservation {
	targetID := strings.TrimSpace(database.KMSKeyID)
	if targetID == "" {
		return nil
	}
	sourceID := databaseResourceID(database)
	if sourceID == "" {
		return nil
	}
	targetARN := ""
	if isARN(targetID) {
		targetARN = targetID
	}
	return &awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipTimestreamDatabaseUsesKMSKey,
		SourceResourceID: sourceID,
		SourceARN:        strings.TrimSpace(database.ARN),
		TargetResourceID: targetID,
		TargetARN:        targetARN,
		TargetType:       awscloud.ResourceTypeKMSKey,
		SourceRecordID:   sourceID + "->" + awscloud.RelationshipTimestreamDatabaseUsesKMSKey + ":" + targetID,
	}
}

// tableRejectedDataS3Relationship records a Timestream table's magnetic-store
// rejected-data report S3 bucket. Timestream reports a bucket NAME, so the
// scanner synthesizes the partition-aware bucket ARN to match the S3 scanner's
// published bucket resource_id (arn:<partition>:s3:::<bucket>). It returns nil
// when no rejected-data bucket is configured.
func tableRejectedDataS3Relationship(boundary awscloud.Boundary, table Table) *awscloud.RelationshipObservation {
	bucket := strings.TrimSpace(table.RejectedDataS3Bucket)
	if bucket == "" {
		return nil
	}
	tableID := tableResourceID(table)
	if tableID == "" {
		return nil
	}
	bucketARN := arnForBucket(awscloud.PartitionForBoundary(boundary), bucket)
	if bucketARN == "" {
		return nil
	}
	attributes := map[string]any{
		"bucket": bucket,
	}
	if prefix := strings.TrimSpace(table.RejectedDataS3Prefix); prefix != "" {
		attributes["object_key_prefix"] = prefix
	}
	if option := strings.TrimSpace(table.RejectedDataS3EncryptionOption); option != "" {
		attributes["encryption_option"] = option
	}
	return &awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipTimestreamTableRejectsToS3,
		SourceResourceID: tableID,
		SourceARN:        strings.TrimSpace(table.ARN),
		TargetResourceID: bucketARN,
		TargetARN:        bucketARN,
		TargetType:       awscloud.ResourceTypeS3Bucket,
		Attributes:       attributes,
		SourceRecordID:   tableID + "->" + awscloud.RelationshipTimestreamTableRejectsToS3 + ":" + bucketARN,
	}
}
