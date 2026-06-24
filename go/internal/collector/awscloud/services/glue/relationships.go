// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package glue

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

func tableInDatabaseRelationship(boundary awscloud.Boundary, table Table) *awscloud.RelationshipObservation {
	tableID := tableResourceID(table)
	databaseName := strings.TrimSpace(table.DatabaseName)
	if tableID == "" || databaseName == "" {
		return nil
	}
	return &awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipGlueTableInDatabase,
		SourceResourceID: tableID,
		TargetResourceID: databaseName,
		TargetType:       awscloud.ResourceTypeGlueDatabase,
		SourceRecordID:   tableID + "->" + awscloud.RelationshipGlueTableInDatabase + ":" + databaseName,
	}
}

func tableS3LocationRelationship(boundary awscloud.Boundary, table Table) *awscloud.RelationshipObservation {
	location := strings.TrimSpace(table.StorageLocation)
	bucket, prefix, ok := parseS3Location(location)
	if !ok {
		return nil
	}
	tableID := tableResourceID(table)
	if tableID == "" {
		return nil
	}
	bucketARN := "arn:" + awscloud.PartitionForBoundary(boundary) + ":s3:::" + bucket
	attributes := map[string]any{
		"storage_location": location,
		"bucket":           bucket,
	}
	if prefix != "" {
		attributes["object_key_prefix"] = prefix
	}
	return &awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipGlueTableStoredAtS3Location,
		SourceResourceID: tableID,
		TargetResourceID: bucketARN,
		TargetARN:        bucketARN,
		TargetType:       awscloud.ResourceTypeS3Bucket,
		Attributes:       attributes,
		SourceRecordID:   tableID + "->" + awscloud.RelationshipGlueTableStoredAtS3Location + ":" + bucketARN,
	}
}

// parseS3Location splits an `s3://bucket[/key]` URI into the bucket name and
// optional object-key prefix. It returns ok=false when the input is not an
// `s3://` URI or has no bucket segment so the caller can skip emission.
func parseS3Location(location string) (bucket string, prefix string, ok bool) {
	trimmed := strings.TrimSpace(location)
	if !strings.HasPrefix(trimmed, "s3://") {
		return "", "", false
	}
	remainder := strings.TrimPrefix(trimmed, "s3://")
	if remainder == "" {
		return "", "", false
	}
	slash := strings.IndexByte(remainder, '/')
	if slash < 0 {
		bucket = strings.TrimSpace(remainder)
		return bucket, "", bucket != ""
	}
	bucket = strings.TrimSpace(remainder[:slash])
	if bucket == "" {
		return "", "", false
	}
	prefix = strings.TrimSpace(remainder[slash+1:])
	return bucket, prefix, true
}

func crawlerDatabaseRelationship(boundary awscloud.Boundary, crawler Crawler) *awscloud.RelationshipObservation {
	crawlerName := strings.TrimSpace(crawler.Name)
	databaseName := strings.TrimSpace(crawler.DatabaseName)
	if crawlerName == "" || databaseName == "" {
		return nil
	}
	return &awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipGlueCrawlerTargetsDatabase,
		SourceResourceID: crawlerName,
		TargetResourceID: databaseName,
		TargetType:       awscloud.ResourceTypeGlueDatabase,
		SourceRecordID:   crawlerName + "->" + awscloud.RelationshipGlueCrawlerTargetsDatabase + ":" + databaseName,
	}
}

func crawlerRoleRelationship(boundary awscloud.Boundary, crawler Crawler) *awscloud.RelationshipObservation {
	roleARN := strings.TrimSpace(crawler.RoleARN)
	if !isARN(roleARN) {
		return nil
	}
	crawlerName := strings.TrimSpace(crawler.Name)
	if crawlerName == "" {
		return nil
	}
	return &awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipGlueCrawlerUsesIAMRole,
		SourceResourceID: crawlerName,
		TargetResourceID: roleARN,
		TargetARN:        roleARN,
		TargetType:       awscloud.ResourceTypeIAMRole,
		SourceRecordID:   crawlerName + "->" + awscloud.RelationshipGlueCrawlerUsesIAMRole + ":" + roleARN,
	}
}

func jobRoleRelationship(boundary awscloud.Boundary, job Job) *awscloud.RelationshipObservation {
	roleARN := strings.TrimSpace(job.RoleARN)
	if !isARN(roleARN) {
		return nil
	}
	jobName := strings.TrimSpace(job.Name)
	if jobName == "" {
		return nil
	}
	return &awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipGlueJobUsesIAMRole,
		SourceResourceID: jobName,
		TargetResourceID: roleARN,
		TargetARN:        roleARN,
		TargetType:       awscloud.ResourceTypeIAMRole,
		SourceRecordID:   jobName + "->" + awscloud.RelationshipGlueJobUsesIAMRole + ":" + roleARN,
	}
}

func triggerJobRelationships(boundary awscloud.Boundary, trigger Trigger) []awscloud.RelationshipObservation {
	triggerName := strings.TrimSpace(trigger.Name)
	if triggerName == "" || len(trigger.ActionJobs) == 0 {
		return nil
	}
	observations := make([]awscloud.RelationshipObservation, 0, len(trigger.ActionJobs))
	seen := make(map[string]struct{}, len(trigger.ActionJobs))
	for _, jobName := range trigger.ActionJobs {
		target := strings.TrimSpace(jobName)
		if target == "" {
			continue
		}
		if _, ok := seen[target]; ok {
			continue
		}
		seen[target] = struct{}{}
		observations = append(observations, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipGlueTriggerInvokesJob,
			SourceResourceID: triggerName,
			TargetResourceID: target,
			TargetType:       awscloud.ResourceTypeGlueJob,
			SourceRecordID:   triggerName + "->" + awscloud.RelationshipGlueTriggerInvokesJob + ":" + target,
		})
	}
	if len(observations) == 0 {
		return nil
	}
	return observations
}

func tableResourceID(table Table) string {
	databaseName := strings.TrimSpace(table.DatabaseName)
	name := strings.TrimSpace(table.Name)
	switch {
	case databaseName != "" && name != "":
		return databaseName + "/" + name
	case name != "":
		return name
	default:
		return ""
	}
}
