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
	if !isS3URI(location) {
		return nil
	}
	tableID := tableResourceID(table)
	if tableID == "" {
		return nil
	}
	return &awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipGlueTableStoredAtS3Location,
		SourceResourceID: tableID,
		TargetResourceID: location,
		TargetType:       awscloud.ResourceTypeS3Bucket,
		Attributes: map[string]any{
			"storage_location": location,
		},
		SourceRecordID: tableID + "->" + awscloud.RelationshipGlueTableStoredAtS3Location + ":" + location,
	}
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
