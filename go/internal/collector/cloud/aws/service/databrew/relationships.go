// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package databrew

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/cloud/aws"
)

// datasetReadsS3Relationship records a DataBrew dataset's Amazon S3 input
// bucket. DataBrew reports a bucket NAME, so the scanner synthesizes the
// partition-aware bucket ARN to match the S3 scanner's published bucket
// resource_id. It returns nil when no S3 input bucket is configured.
func datasetReadsS3Relationship(boundary aws.Boundary, dataset Dataset) *aws.RelationshipObservation {
	bucket := strings.TrimSpace(dataset.S3Bucket)
	if bucket == "" {
		return nil
	}
	sourceID := datasetResourceID(dataset)
	if sourceID == "" {
		return nil
	}
	bucketARN := arnForBucket(aws.PartitionForBoundary(boundary), bucket)
	if bucketARN == "" {
		return nil
	}
	attributes := map[string]any{"bucket": bucket}
	if key := strings.TrimSpace(dataset.S3Key); key != "" {
		attributes["object_key"] = key
	}
	return &aws.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: aws.RelationshipDatabrewDatasetReadsS3,
		SourceResourceID: sourceID,
		SourceARN:        strings.TrimSpace(dataset.ARN),
		TargetResourceID: bucketARN,
		TargetARN:        bucketARN,
		TargetType:       aws.ResourceTypeS3Bucket,
		Attributes:       attributes,
		SourceRecordID:   sourceID + "->" + aws.RelationshipDatabrewDatasetReadsS3 + ":" + bucketARN,
	}
}

// datasetReadsGlueTableRelationship records a DataBrew dataset's Glue Data
// Catalog input table. The target is keyed by the "<database>/<table>" identity
// the Glue table scanner publishes. It returns nil when the dataset does not
// read a Data Catalog table. A Redshift/JDBC database input is intentionally
// not edged: DataBrew reports only a Glue connection name and table name for
// such inputs, never a Redshift cluster ARN or identifier, so an edge to the
// Redshift cluster node would dangle.
func datasetReadsGlueTableRelationship(
	boundary aws.Boundary,
	dataset Dataset,
) *aws.RelationshipObservation {
	targetID := glueTableResourceID(dataset.GlueDatabaseName, dataset.GlueTableName)
	if targetID == "" {
		return nil
	}
	sourceID := datasetResourceID(dataset)
	if sourceID == "" {
		return nil
	}
	attributes := map[string]any{
		"glue_database": strings.TrimSpace(dataset.GlueDatabaseName),
		"glue_table":    strings.TrimSpace(dataset.GlueTableName),
	}
	if catalog := strings.TrimSpace(dataset.GlueCatalogID); catalog != "" {
		attributes["catalog_id"] = catalog
	}
	return &aws.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: aws.RelationshipDatabrewDatasetReadsGlueTable,
		SourceResourceID: sourceID,
		SourceARN:        strings.TrimSpace(dataset.ARN),
		TargetResourceID: targetID,
		TargetType:       aws.ResourceTypeGlueTable,
		Attributes:       attributes,
		SourceRecordID:   sourceID + "->" + aws.RelationshipDatabrewDatasetReadsGlueTable + ":" + targetID,
	}
}

// jobWritesS3Relationships records each distinct Amazon S3 output bucket a
// DataBrew job writes to. DataBrew reports bucket NAMES, so the scanner
// synthesizes partition-aware bucket ARNs to match the S3 scanner's published
// bucket resource_id. It returns nil when no S3 output bucket is configured.
func jobWritesS3Relationships(boundary aws.Boundary, job Job) []aws.RelationshipObservation {
	sourceID := jobResourceID(job)
	if sourceID == "" {
		return nil
	}
	partition := aws.PartitionForBoundary(boundary)
	var observations []aws.RelationshipObservation
	for _, bucket := range job.OutputS3Buckets {
		bucketARN := arnForBucket(partition, bucket)
		if bucketARN == "" {
			continue
		}
		observations = append(observations, aws.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: aws.RelationshipDatabrewJobWritesS3,
			SourceResourceID: sourceID,
			SourceARN:        strings.TrimSpace(job.ARN),
			TargetResourceID: bucketARN,
			TargetARN:        bucketARN,
			TargetType:       aws.ResourceTypeS3Bucket,
			Attributes:       map[string]any{"bucket": strings.TrimSpace(bucket)},
			SourceRecordID:   sourceID + "->" + aws.RelationshipDatabrewJobWritesS3 + ":" + bucketARN,
		})
	}
	return observations
}

// jobAssumesRoleRelationship records the IAM role a DataBrew job assumes. AWS
// reports a role ARN, which matches how the IAM scanner publishes its role
// resource_id. It returns nil when no role ARN is reported.
func jobAssumesRoleRelationship(boundary aws.Boundary, job Job) *aws.RelationshipObservation {
	return assumesRoleRelationship(
		boundary,
		jobResourceID(job),
		strings.TrimSpace(job.ARN),
		strings.TrimSpace(job.RoleARN),
		aws.RelationshipDatabrewJobAssumesRole,
	)
}

// jobProcessesDatasetRelationship records the DataBrew dataset a job processes.
// The target is keyed by the dataset name the dataset node publishes. It
// returns nil when the job reports no dataset.
func jobProcessesDatasetRelationship(boundary aws.Boundary, job Job) *aws.RelationshipObservation {
	return internalNameRelationship(
		boundary,
		jobResourceID(job),
		strings.TrimSpace(job.ARN),
		strings.TrimSpace(job.DatasetName),
		aws.RelationshipDatabrewJobProcessesDataset,
		aws.ResourceTypeDatabrewDataset,
	)
}

// projectUsesDatasetRelationship records the dataset a DataBrew project binds.
// The target is keyed by the dataset name the dataset node publishes. It
// returns nil when the project reports no dataset.
func projectUsesDatasetRelationship(boundary aws.Boundary, project Project) *aws.RelationshipObservation {
	return internalNameRelationship(
		boundary,
		projectResourceID(project),
		strings.TrimSpace(project.ARN),
		strings.TrimSpace(project.DatasetName),
		aws.RelationshipDatabrewProjectUsesDataset,
		aws.ResourceTypeDatabrewDataset,
	)
}

// projectUsesRecipeRelationship records the recipe a DataBrew project develops.
// The target is keyed by the recipe name the recipe node publishes. It returns
// nil when the project reports no recipe.
func projectUsesRecipeRelationship(boundary aws.Boundary, project Project) *aws.RelationshipObservation {
	return internalNameRelationship(
		boundary,
		projectResourceID(project),
		strings.TrimSpace(project.ARN),
		strings.TrimSpace(project.RecipeName),
		aws.RelationshipDatabrewProjectUsesRecipe,
		aws.ResourceTypeDatabrewRecipe,
	)
}

// projectAssumesRoleRelationship records the IAM role a DataBrew project
// assumes. AWS reports a role ARN, which matches how the IAM scanner publishes
// its role resource_id. It returns nil when no role ARN is reported.
func projectAssumesRoleRelationship(boundary aws.Boundary, project Project) *aws.RelationshipObservation {
	return assumesRoleRelationship(
		boundary,
		projectResourceID(project),
		strings.TrimSpace(project.ARN),
		strings.TrimSpace(project.RoleARN),
		aws.RelationshipDatabrewProjectAssumesRole,
	)
}

// assumesRoleRelationship builds an edge to the IAM role keyed by its ARN, the
// resource_id the IAM scanner publishes for a role. It returns nil when either
// endpoint identity is missing.
func assumesRoleRelationship(
	boundary aws.Boundary,
	sourceID string,
	sourceARN string,
	roleARN string,
	relationshipType string,
) *aws.RelationshipObservation {
	sourceID = strings.TrimSpace(sourceID)
	roleARN = strings.TrimSpace(roleARN)
	if sourceID == "" || roleARN == "" {
		return nil
	}
	targetARN := ""
	if isARN(roleARN) {
		targetARN = roleARN
	}
	return &aws.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: relationshipType,
		SourceResourceID: sourceID,
		SourceARN:        sourceARN,
		TargetResourceID: roleARN,
		TargetARN:        targetARN,
		TargetType:       aws.ResourceTypeIAMRole,
		SourceRecordID:   sourceID + "->" + relationshipType + ":" + roleARN,
	}
}

// internalNameRelationship builds an edge to another DataBrew resource node
// keyed by its name, the resource_id those nodes publish. It returns nil when
// either endpoint identity is missing.
func internalNameRelationship(
	boundary aws.Boundary,
	sourceID string,
	sourceARN string,
	targetName string,
	relationshipType string,
	targetType string,
) *aws.RelationshipObservation {
	sourceID = strings.TrimSpace(sourceID)
	targetName = strings.TrimSpace(targetName)
	if sourceID == "" || targetName == "" {
		return nil
	}
	return &aws.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: relationshipType,
		SourceResourceID: sourceID,
		SourceARN:        sourceARN,
		TargetResourceID: targetName,
		TargetType:       targetType,
		SourceRecordID:   sourceID + "->" + relationshipType + ":" + targetName,
	}
}
